//! ffmpeg encoder subprocess (ADR-0024 SD3; SD9 pacing; ADR-0088 SD4 codec
//! lanes + NUT framing).
//!
//! Feeds tightly-packed BGRA frames to ffmpeg's stdin (`-f rawvideo
//! -pix_fmt bgra`) and drains the encoded stream from its stdout on a
//! separate thread, into one of two targets:
//!
//! - [`EncoderTarget::Channel`] — the wire path. ffmpeg muxes to the NUT
//!   container; [`drain_to_channel_nut`] demuxes each coded frame (native
//!   bitstream + container key-frame flag, for any codec — ADR-0088 SD4),
//!   wraps it in the ADR-0024 SD4 protobuf envelope with the 0x01 prefix,
//!   and pushes it into the WebSocket carrier's bounded channel.
//! - [`EncoderTarget::File`] — a raw H.264 elementary-stream dump for
//!   verification (`IMZERO2_HEADLESS_H264_OUT`; meaningful for H.264).
//!
//! **SD9 pacing.** The render/FFFI2 loop must never block on the encoder
//! or a slow viewer. [`EncoderSink::on_frame`] (called on the render
//! thread) only copies the frame into a depth-1, latest-wins
//! [`FrameMailbox`] and returns; a dedicated feeder thread drains the
//! mailbox into ffmpeg's stdin. When the wire congests, backpressure
//! still propagates upstream — bounded channel fills → drain thread
//! blocks → ffmpeg stdout fills → ffmpeg stops reading stdin → the
//! *feeder* blocks on `write_all` — but it stops at the feeder thread:
//! the render thread keeps producing, the mailbox coalesces to the
//! freshest frame, and the stale ones are dropped **before** the encoder.
//! Encoded frames are never dropped (with `-bf 0` every frame is a
//! reference; a post-encode gap breaks decode until the next IDR). This
//! also decouples render cadence from encoder cadence: the encoder
//! samples the latest frame as fast as the pipe sustains, rather than at
//! a fixed sub-rate.
//!
//! Supervision (SD3): the feeder marks the mailbox `dead` on a write
//! failure and exits; the render thread observes that on its next
//! `on_frame` and reaps + respawns (off any blocking path). A geometry
//! change (viewport resize) takes the same reap+respawn path, since
//! rawvideo dimensions are fixed per ffmpeg invocation. Every (re)spawn
//! begins the stream at SPS/PPS + IDR, satisfying the SD4 (re)connect
//! rule.
//!
//! That respawn is *budgeted* ([`restart_action`]). `dead` stays set until a
//! spawn installs a fresh mailbox, so the frame path is itself the retry loop:
//! an encoder that can never start — an ffmpeg missing the lane's encoder, a
//! bad [`crate::imzero2::codeclane::ffmpeg_bin`] — would otherwise be reaped
//! and respawned on every frame, at frame rate, forever, one error line each.
//! Attempts are spaced by [`RESTART_BACKOFF`], a run lasting
//! [`RESTART_STABLE_AFTER`] clears the streak (so transient deaths always
//! recover), and [`MAX_FAST_RESTARTS`] consecutive fast deaths stop the retries
//! with a single terminal error. [`CodecLane::best`] is the other half of this:
//! it probes the software lane before returning it, so an unusable lane should
//! not reach the sink in the first place.

use crate::imzero2::codeclane::{CodecLane, ffmpeg_bin};
use crate::imzero2::framesink::FrameSink;
use crate::imzero2::inputproto as pb;
use crate::imzero2::nutreader::NutReader;
use prost::Message as _;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Condvar, Mutex};

/// Cap on recycled BGRA buffers held to avoid per-frame reallocation.
/// latest(1) + free(≤2) + the one the feeder holds ≈ a handful of frame
/// buffers; bounded so a paused feeder can't grow memory without limit.
const FREE_LIST_CAP: usize = 2;

/// Poll interval for the channel drain's cancellable bounded send. While
/// the video channel is full (a slow/stalled viewer) the drain parks here
/// between retries before re-checking the reap stop flag. Short enough to
/// add no meaningful latency once a slot frees; it only spins under
/// congestion, where the mailbox is already coalescing frames pre-encoder.
const DRAIN_BACKPRESSURE_POLL: std::time::Duration = std::time::Duration::from_millis(2);

pub enum EncoderTarget {
    File(std::path::PathBuf),
    /// Pre-framed WebSocket payloads (0x01 + VideoChunk) for the carrier.
    Channel(tokio::sync::mpsc::Sender<Vec<u8>>),
}

/// Depth-1, latest-wins handoff from the render thread to the feeder
/// thread (SD9). `submit` never blocks; an unconsumed frame is dropped
/// (recycled) when a newer one arrives — the pre-encode drop.
struct FrameMailbox {
    inner: Mutex<MailboxInner>,
    cv: Condvar,
    /// Set by the feeder on a write failure; observed by the render
    /// thread to trigger a reap + respawn.
    dead: AtomicBool,
    /// Count of frames coalesced (dropped) in the mailbox under congestion
    /// (SD9 pre-encode drop), surfaced to the Go control.
    dropped: AtomicU64,
}

struct MailboxInner {
    latest: Option<Vec<u8>>,
    free: Vec<Vec<u8>>,
    closed: bool,
}

impl FrameMailbox {
    fn new() -> Arc<Self> {
        Arc::new(Self {
            inner: Mutex::new(MailboxInner {
                latest: None,
                free: Vec::new(),
                closed: false,
            }),
            cv: Condvar::new(),
            dead: AtomicBool::new(false),
            dropped: AtomicU64::new(0),
        })
    }

    /// Copy `src` into the mailbox as the new latest frame, recycling any
    /// previously-unconsumed buffer. Non-blocking.
    fn submit(&self, src: &[u8]) {
        let mut g = self.inner.lock().expect("frame mailbox poisoned");
        let mut buf = g.free.pop().unwrap_or_default();
        buf.clear();
        buf.extend_from_slice(src);
        if let Some(old) = g.latest.replace(buf) {
            self.dropped.fetch_add(1, Ordering::Relaxed);
            if g.free.len() < FREE_LIST_CAP {
                g.free.push(old);
            }
        }
        drop(g);
        self.cv.notify_one();
    }

    fn dropped(&self) -> u64 {
        self.dropped.load(Ordering::Relaxed)
    }

    /// Block until a frame is available; return None once closed and
    /// drained.
    fn wait_next(&self) -> Option<Vec<u8>> {
        let mut g = self.inner.lock().expect("frame mailbox poisoned");
        loop {
            if let Some(buf) = g.latest.take() {
                return Some(buf);
            }
            if g.closed {
                return None;
            }
            g = self.cv.wait(g).expect("frame mailbox poisoned");
        }
    }

    fn recycle(&self, buf: Vec<u8>) {
        let mut g = self.inner.lock().expect("frame mailbox poisoned");
        if g.free.len() < FREE_LIST_CAP {
            g.free.push(buf);
        }
    }

    /// Wake a feeder waiting on the condvar so it can exit. A feeder
    /// blocked inside `write_all` is unblocked separately by killing the
    /// child (broken pipe).
    fn close(&self) {
        let mut g = self.inner.lock().expect("frame mailbox poisoned");
        g.closed = true;
        drop(g);
        self.cv.notify_all();
    }
}

/// Drain the mailbox into ffmpeg's stdin until closed or a write fails.
/// Blocking here (ffmpeg stdin full under congestion) does not reach the
/// render thread — that is the point of SD9.
fn run_feeder(mailbox: Arc<FrameMailbox>, stdin: Option<std::process::ChildStdin>) {
    let Some(mut stdin) = stdin else { return };
    while let Some(buf) = mailbox.wait_next() {
        if let Err(e) = std::io::Write::write_all(&mut stdin, &buf) {
            tracing::warn!(error=%e, "ffmpeg stdin write failed — feeder stopping (render thread will respawn)");
            mailbox.dead.store(true, Ordering::Release);
            return;
        }
        mailbox.recycle(buf);
    }
    // Closed: dropping `stdin` here closes ffmpeg's input → flush + EOF.
}

pub struct EncoderSink {
    child: Option<std::process::Child>,
    feeder: Option<std::thread::JoinHandle<()>>,
    drain: Option<std::thread::JoinHandle<()>>,
    mailbox: Arc<FrameMailbox>,
    /// Set by [`EncoderSink::reap`] to break a channel drain out of its
    /// bounded-send backpressure wait. Killing ffmpeg unblocks the *feeder*
    /// (broken stdin pipe), but a drain parked trying to push a frame into a
    /// full video channel (a slow viewer not draining the socket) is waiting
    /// on channel capacity, not on stdout — so `reap` must signal it
    /// explicitly, or `drain.join()` would block the render thread that calls
    /// `reap` on a resize or a runtime codec switch (ADR-0024 SD9).
    drain_stop: Arc<AtomicBool>,
    width: u32,
    height: u32,
    fps: f32,
    lane: CodecLane,
    target: EncoderTarget,
    restarts: u32,
    /// When the current ffmpeg was spawned — the supervisor's clock for
    /// [`RESTART_BACKOFF`] and [`RESTART_STABLE_AFTER`].
    last_spawn: std::time::Instant,
    /// Consecutive restarts where the encoder died before it had run for
    /// [`RESTART_STABLE_AFTER`]. Reset by any run that lasts longer.
    fast_restarts: u32,
    /// Set once the fast-restart budget is spent: the encoder is broken in a
    /// way retrying cannot fix, so stop retrying and stop logging. Cleared by
    /// a geometry change, which is a genuinely new configuration.
    gave_up: bool,
}

/// Minimum spacing between respawn attempts. Without it the sink retries on
/// every frame — at 60 fps that is 60 process spawns and 60 error lines per
/// second, which is the log flood that makes a broken encoder look like a
/// render-loop bug.
const RESTART_BACKOFF: std::time::Duration = std::time::Duration::from_millis(500);

/// An encoder that ran at least this long before dying counts as healthy: the
/// failure was transient (an OOM kill, a GPU reset, a full disk on the file
/// target) rather than a configuration that can never work.
const RESTART_STABLE_AFTER: std::time::Duration = std::time::Duration::from_secs(5);

/// Consecutive fast restarts tolerated before the sink gives up. With the
/// backoff above this is a few seconds of trying, which covers a transient
/// stall without spinning forever on an ffmpeg that exits immediately.
const MAX_FAST_RESTARTS: u32 = 5;

/// What the supervisor does about an encoder that just died.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum RestartAction {
    /// Drop this frame: either the backoff has not elapsed, or the sink has
    /// already given up on the lane.
    Wait,
    /// Respawn, carrying the new consecutive-fast-restart count.
    Restart { fast_restarts: u32 },
    /// The budget is spent — stop retrying and stop logging.
    GiveUp,
}

/// The restart policy, separated from the sink so it is testable without
/// spawning a real encoder: given how long the dead encoder ran and how many
/// fast restarts preceded it, decide whether to wait, respawn, or give up.
fn restart_action(
    gave_up: bool,
    ran_for: std::time::Duration,
    fast_restarts: u32,
) -> RestartAction {
    if gave_up || ran_for < RESTART_BACKOFF {
        return RestartAction::Wait;
    }
    // A run that lasted counts as healthy, so the streak restarts at one.
    let fast = if ran_for >= RESTART_STABLE_AFTER {
        1
    } else {
        fast_restarts + 1
    };
    if fast > MAX_FAST_RESTARTS {
        RestartAction::GiveUp
    } else {
        RestartAction::Restart {
            fast_restarts: fast,
        }
    }
}

impl EncoderSink {
    pub fn new(
        width: u32,
        height: u32,
        fps: f32,
        lane: CodecLane,
        target: EncoderTarget,
    ) -> std::io::Result<Self> {
        let mut sink = Self {
            child: None,
            feeder: None,
            drain: None,
            mailbox: FrameMailbox::new(),
            drain_stop: Arc::new(AtomicBool::new(false)),
            width,
            height,
            fps,
            lane,
            target,
            restarts: 0,
            last_spawn: std::time::Instant::now(),
            fast_restarts: 0,
            gave_up: false,
        };
        sink.spawn(false)?;
        Ok(sink)
    }

    /// Frames coalesced (dropped) before the encoder under congestion (SD9).
    pub fn dropped(&self) -> u64 {
        self.mailbox.dropped()
    }

    fn spawn(&mut self, restart: bool) -> std::io::Result<()> {
        let mut cmd = std::process::Command::new(ffmpeg_bin());
        cmd.arg("-hide_banner")
            .arg("-loglevel")
            .arg("warning")
            .arg("-f")
            .arg("rawvideo")
            .arg("-pix_fmt")
            .arg("bgra")
            .arg("-video_size")
            .arg(format!("{}x{}", self.width, self.height))
            .arg("-framerate")
            .arg(format!("{}", self.fps))
            .arg("-i")
            .arg("pipe:0")
            .args(&self.lane.encoder_args);
        // Per-codec bitstream filter (e.g. H.264 SPS/PPS on every key frame).
        if let Some(bsf) = self.lane.bsf {
            cmd.arg("-bsf:v").arg(bsf);
        }
        // The wire path muxes to NUT (ADR-0088 SD4) — one container the host
        // demuxes for every codec — and flushes each frame so the muxer adds
        // no latency. The File verification dump stays a raw H.264 elementary
        // stream (IMZERO2_HEADLESS_H264_OUT; meaningful for the H.264 codec).
        let out_fmt = match &self.target {
            EncoderTarget::Channel(_) => {
                cmd.arg("-flush_packets").arg("1");
                "nut"
            }
            EncoderTarget::File(_) => "h264",
        };
        cmd.arg("-f")
            .arg(out_fmt)
            .arg("pipe:1")
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            // ffmpeg diagnostics join our stderr; this binary's stdout is the
            // FFFI2 data channel and stays untouched.
            .stderr(std::process::Stdio::inherit());
        tracing::info!(args=?cmd.get_args().collect::<Vec<_>>(), restart, "spawning ffmpeg encoder");
        let mut child = cmd.spawn()?;
        let stdin = child.stdin.take();
        let stdout = child.stdout.take();
        // Fresh per-spawn stop flag: a later reap() sets it to abort a channel
        // drain parked in backpressure, so the render thread never blocks
        // (ADR-0024 SD9). The file drain reads stdout and is unblocked by the
        // child kill, so it needs no flag.
        let drain_stop = Arc::new(AtomicBool::new(false));
        let drain = match &self.target {
            EncoderTarget::File(path) => {
                let path = path.clone();
                std::thread::Builder::new()
                    .name("imzero2-h264-drain".to_owned())
                    .spawn(move || drain_to_file(stdout, &path, restart))?
            }
            EncoderTarget::Channel(tx) => {
                let tx = tx.clone();
                let stop = drain_stop.clone();
                std::thread::Builder::new()
                    .name("imzero2-video-drain".to_owned())
                    .spawn(move || drain_to_channel_nut(stdout, &tx, &stop))?
            }
        };
        // Fresh mailbox per spawn: a new generation cleanly separates the
        // new feeder from any prior one, and discards stale-geometry
        // buffers from before a resize.
        let mailbox = FrameMailbox::new();
        let feeder = {
            let mb = mailbox.clone();
            std::thread::Builder::new()
                .name("imzero2-h264-feeder".to_owned())
                .spawn(move || run_feeder(mb, stdin))?
        };
        self.child = Some(child);
        self.mailbox = mailbox;
        self.drain_stop = drain_stop;
        self.feeder = Some(feeder);
        self.drain = Some(drain);
        // Supervisor clock: how long this encoder survives decides whether its
        // death counts as transient or as another fast restart.
        self.last_spawn = std::time::Instant::now();
        Ok(())
    }

    fn reap(&mut self) {
        // Wake a feeder parked on the condvar...
        self.mailbox.close();
        // ...signal a channel drain parked in backpressure to abandon its
        // pending frame and exit (a full-channel send can't be unblocked by
        // the child kill below — it is waiting on channel capacity, not
        // stdout); this is what keeps reap() — and the render thread calling
        // it on resize / codec switch — from blocking on a stalled viewer...
        self.drain_stop.store(true, Ordering::Release);
        // ...and unblock one stuck inside write_all under congestion by
        // killing the child (broken pipe makes the write return). On every
        // reap we are restarting (fresh IDR) or shutting down, so the old
        // stream's flushed tail is not wanted.
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            match child.wait() {
                Ok(status) => tracing::info!(%status, "ffmpeg encoder exited"),
                Err(e) => tracing::error!(error=%e, "failed to reap ffmpeg encoder"),
            }
        }
        if let Some(feeder) = self.feeder.take() {
            let _ = feeder.join();
        }
        if let Some(drain) = self.drain.take() {
            let _ = drain.join();
        }
    }
}

impl FrameSink for EncoderSink {
    fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, _frame_idx: u64) {
        let geometry_changed = width != self.width || height != self.height;
        let died = self.mailbox.dead.load(Ordering::Acquire);
        if geometry_changed || died {
            if geometry_changed {
                tracing::info!(
                    from_w = self.width,
                    from_h = self.height,
                    to_w = width,
                    to_h = height,
                    "frame geometry changed — restarting encoder"
                );
                // A resize is a new configuration, not a retry of the failed
                // one: it may well encode where the old geometry could not
                // (hardware encoders reject sizes below their coded minimum),
                // so it re-arms a sink that had given up.
                self.gave_up = false;
                self.fast_restarts = 0;
            } else {
                // Supervised restart. `died` stays set until a spawn installs a
                // fresh mailbox, so without rate limiting this branch runs on
                // every frame for as long as the encoder stays broken.
                match restart_action(self.gave_up, self.last_spawn.elapsed(), self.fast_restarts) {
                    RestartAction::Wait => return,
                    RestartAction::GiveUp => {
                        self.gave_up = true;
                        self.reap();
                        tracing::error!(
                            restarts = self.restarts,
                            lane = ?self.lane.codec,
                            ffmpeg = %crate::imzero2::codeclane::ffmpeg_bin(),
                            "ffmpeg encoder died {MAX_FAST_RESTARTS} times without staying up — \
                             giving up on this lane. The stream is dead until the viewport \
                             resizes or the codec is switched; the mesh lane needs no encoder."
                        );
                        return;
                    }
                    RestartAction::Restart { fast_restarts } => {
                        self.fast_restarts = fast_restarts;
                        self.restarts += 1;
                        tracing::error!(
                            restarts = self.restarts,
                            fast_restarts = self.fast_restarts,
                            "ffmpeg encoder feeder died — restarting encoder"
                        );
                    }
                }
            }
            self.reap();
            self.width = width;
            self.height = height;
            if let Err(e) = self.spawn(true) {
                tracing::error!(error=%e, "ffmpeg encoder respawn failed; will retry after backoff");
                // Count the failed attempt against the budget: spawn() never
                // installed a fresh mailbox, so `died` is still set and this
                // branch is what the next frame re-enters.
                self.last_spawn = std::time::Instant::now();
                return;
            }
        }
        // Non-blocking handoff (SD9): the render thread never waits on the
        // encoder or the wire.
        self.mailbox.submit(bgra);
    }
}

impl Drop for EncoderSink {
    fn drop(&mut self) {
        self.reap();
    }
}

fn drain_to_file(stdout: Option<std::process::ChildStdout>, path: &std::path::Path, append: bool) {
    let file = std::fs::OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(!append)
        .append(append)
        .open(path);
    let mut file = match file {
        Ok(f) => std::io::BufWriter::new(f),
        Err(e) => {
            tracing::error!(path=%path.display(), error=%e, "cannot open h264 output file; encoder output discarded");
            if let Some(mut so) = stdout {
                let _ = std::io::copy(&mut so, &mut std::io::sink());
            }
            return;
        }
    };
    if let Some(mut so) = stdout {
        match std::io::copy(&mut so, &mut file) {
            Ok(n) => tracing::info!(bytes = n, path=%path.display(), "h264 drain finished"),
            Err(e) => tracing::error!(error=%e, "h264 drain failed"),
        }
    }
    let _ = std::io::Write::flush(&mut file);
}

/// Wrap one coded frame's native payload in the ADR-0024 SD4 envelope
/// (0x01 prefix + `VideoChunk`), ready to push to the carrier channel.
fn frame_payload(
    frame_index: u64,
    started: std::time::Instant,
    keyframe: bool,
    data: Vec<u8>,
) -> Vec<u8> {
    let envelope = pb::VideoChunk {
        frame_index,
        timestamp_micros: started.elapsed().as_micros() as u64,
        keyframe,
        data,
    };
    let mut framed = Vec::with_capacity(1 + envelope.encoded_len());
    framed.push(pb::PREFIX_VIDEO);
    // Encoding into a Vec is infallible in practice.
    let _ = envelope.encode(&mut framed);
    framed
}

/// Result of a [`cancellable_send`].
enum SendOutcome {
    /// The payload reached the channel.
    Sent,
    /// The receiver was dropped (viewer disconnected).
    Closed,
    /// The reap stop flag fired while parked on a full channel — teardown.
    Cancelled,
}

/// Push one payload into the bounded video channel, blocking for
/// backpressure but cancellable on teardown (ADR-0024 SD9). A full channel
/// means a slow/stalled viewer; parking here keeps backpressure flowing
/// upstream (full channel → full ffmpeg stdout → paused encode → the SD9
/// mailbox coalesces pre-encoder), which is the steady-state behaviour we
/// want. The difference from `tx.blocking_send` — which cannot be
/// interrupted — is that this bails the instant `stop` is set, so a
/// `reap()` triggered by a resize or a runtime codec switch never waits on
/// a viewer that has stopped reading the socket. `try_send` + a short poll
/// gives the sync drain thread a wakeup it can re-check the flag on.
fn cancellable_send(
    tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
    payload: Vec<u8>,
    stop: &AtomicBool,
) -> SendOutcome {
    let mut pending = payload;
    loop {
        match tx.try_send(pending) {
            Ok(()) => return SendOutcome::Sent,
            Err(tokio::sync::mpsc::error::TrySendError::Closed(_)) => return SendOutcome::Closed,
            Err(tokio::sync::mpsc::error::TrySendError::Full(returned)) => {
                if stop.load(Ordering::Acquire) {
                    return SendOutcome::Cancelled;
                }
                pending = returned;
                std::thread::sleep(DRAIN_BACKPRESSURE_POLL);
            }
        }
    }
}

/// Drain ffmpeg's NUT output (ADR-0088 SD4). [`NutReader`] yields each
/// coded frame's native bitstream plus the container-level key-frame flag
/// for any codec — no per-codec depacketizer or keyframe parser — so this
/// is the single drain that serves H.264, VP9, AV1, and future lanes.
/// Returns when ffmpeg's stdout closes, the channel is dropped (viewer
/// disconnected), or the stream is unparseable.
fn drain_to_channel_nut(
    stdout: Option<std::process::ChildStdout>,
    tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
    stop: &AtomicBool,
) {
    use std::io::Read as _;
    let Some(mut so) = stdout else { return };
    let started = std::time::Instant::now();
    let mut reader = NutReader::new();
    let mut chunk = vec![0u8; 64 * 1024];
    let mut frame_index: u64 = 0;
    let mut sent_bytes: u64 = 0;
    loop {
        let n = match so.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => n,
            Err(e) => {
                tracing::error!(error=%e, "nut drain read failed");
                break;
            }
        };
        reader.push(chunk.get(..n).unwrap_or_default());
        loop {
            match reader.next_frame() {
                Ok(Some(frame)) => {
                    let framed = frame_payload(frame_index, started, frame.keyframe, frame.data);
                    frame_index += 1;
                    sent_bytes += framed.len() as u64;
                    match cancellable_send(tx, framed, stop) {
                        SendOutcome::Sent => {}
                        SendOutcome::Closed => {
                            tracing::info!(
                                frames = frame_index,
                                "viewer channel closed — stopping nut drain"
                            );
                            // Keep consuming so ffmpeg can exit cleanly once
                            // our stdin side closes.
                            let _ = std::io::copy(&mut so, &mut std::io::sink());
                            return;
                        }
                        SendOutcome::Cancelled => {
                            // reap() is tearing this encoder down (resize /
                            // codec switch / disconnect) and kills the child,
                            // so just stop — abandoning the in-flight frame is
                            // intended (the old stream's tail is discarded).
                            tracing::debug!(
                                frames = frame_index,
                                "nut drain cancelled under backpressure — encoder teardown"
                            );
                            return;
                        }
                    }
                }
                Ok(None) => break,
                Err(e) => {
                    tracing::error!(error=%e, "nut demux error — stopping drain (encoder respawns on reconnect)");
                    let _ = std::io::copy(&mut so, &mut std::io::sink());
                    return;
                }
            }
        }
    }
    tracing::info!(
        frames = frame_index,
        bytes = sent_bytes,
        "nut drain finished (encoder eof)"
    );
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    // Restart supervision. Before it, a lane that could never encode (an
    // ffmpeg without the encoder, ADR-0088 SD5) made on_frame reap and respawn
    // a doomed ffmpeg on every frame: 60 spawns and 60 error lines a second,
    // forever. `died` stays set until a spawn installs a fresh mailbox, so the
    // frame path itself is the retry loop and the policy has to stop it.

    #[test]
    fn restart_waits_until_the_backoff_elapses() {
        // The frame immediately after a death must not respawn.
        assert_eq!(
            restart_action(false, Duration::ZERO, 0),
            RestartAction::Wait
        );
        assert_eq!(
            restart_action(false, RESTART_BACKOFF - Duration::from_millis(1), 0),
            RestartAction::Wait
        );
    }

    #[test]
    fn restart_counts_a_short_run_against_the_budget() {
        // Past the backoff but well short of "healthy": each death advances
        // the streak, and the last one inside the budget still restarts.
        assert_eq!(
            restart_action(false, RESTART_BACKOFF, 0),
            RestartAction::Restart { fast_restarts: 1 }
        );
        assert_eq!(
            restart_action(false, RESTART_BACKOFF, MAX_FAST_RESTARTS - 1),
            RestartAction::Restart {
                fast_restarts: MAX_FAST_RESTARTS
            }
        );
    }

    #[test]
    fn restart_gives_up_once_the_budget_is_spent() {
        assert_eq!(
            restart_action(false, RESTART_BACKOFF, MAX_FAST_RESTARTS),
            RestartAction::GiveUp
        );
    }

    #[test]
    fn restart_forgives_a_streak_after_a_healthy_run() {
        // A transient death (OOM kill, GPU reset) after a long healthy run
        // must not inherit an old streak, or a host that loses its encoder
        // once an hour would eventually stop recovering.
        assert_eq!(
            restart_action(false, RESTART_STABLE_AFTER, MAX_FAST_RESTARTS),
            RestartAction::Restart { fast_restarts: 1 }
        );
    }

    #[test]
    fn restart_stays_quiet_once_given_up() {
        // No respawn and no log line, however long the sink keeps receiving
        // frames — this is what turns the flood into a single error.
        assert_eq!(
            restart_action(true, RESTART_STABLE_AFTER * 100, 0),
            RestartAction::Wait
        );
    }

    /// H1: a drain parked on a full video channel (a viewer that stopped
    /// reading the socket) must abandon its send the moment reap() sets the
    /// stop flag — this is what keeps reap(), and the render thread that
    /// calls it on a resize / codec switch, from blocking on a stalled
    /// viewer. (No tokio runtime needed: try_send/try_recv are sync.)
    #[test]
    fn cancellable_send_unblocks_on_stop() {
        let (tx, _rx) = tokio::sync::mpsc::channel::<Vec<u8>>(1);
        tx.try_send(vec![0u8; 4]).expect("fill the one slot"); // _rx never reads
        let stop = Arc::new(AtomicBool::new(false));
        let (tx2, stop2) = (tx.clone(), stop.clone());
        let h = std::thread::spawn(move || cancellable_send(&tx2, vec![1u8; 4], &stop2));

        // It should be parked (channel full, stop not yet set).
        std::thread::sleep(std::time::Duration::from_millis(20));
        assert!(
            !h.is_finished(),
            "send must still be parked on the full channel"
        );

        // Once reap signals teardown it must return promptly.
        stop.store(true, Ordering::Release);
        let start = std::time::Instant::now();
        let outcome = h.join().expect("join send thread");
        assert!(
            matches!(outcome, SendOutcome::Cancelled),
            "must report Cancelled"
        );
        assert!(
            start.elapsed() < std::time::Duration::from_secs(1),
            "cancel must be prompt, not wedged"
        );
    }

    /// A dropped receiver (viewer disconnected) is reported as Closed, not
    /// retried forever.
    #[test]
    fn cancellable_send_reports_closed() {
        let (tx, rx) = tokio::sync::mpsc::channel::<Vec<u8>>(1);
        drop(rx);
        let stop = Arc::new(AtomicBool::new(false));
        assert!(matches!(
            cancellable_send(&tx, vec![2u8; 4], &stop),
            SendOutcome::Closed
        ));
    }

    /// The happy path: room in the channel delivers immediately.
    #[test]
    fn cancellable_send_delivers_when_space() {
        let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<u8>>(1);
        let stop = Arc::new(AtomicBool::new(false));
        assert!(matches!(
            cancellable_send(&tx, vec![3u8; 4], &stop),
            SendOutcome::Sent
        ));
        assert_eq!(rx.try_recv().expect("delivered"), vec![3u8; 4]);
    }
}
