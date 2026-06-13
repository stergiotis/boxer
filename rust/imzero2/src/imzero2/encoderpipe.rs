//! ffmpeg encoder subprocess (ADR-0024 SD3, Phase 2; SD9 pacing, 2026-06-13).
//!
//! Feeds tightly-packed BGRA frames to ffmpeg's stdin (`-f rawvideo
//! -pix_fmt bgra`) and drains the raw Annex-B H.264 byte stream from its
//! stdout on a separate thread, into one of two targets:
//!
//! - [`EncoderTarget::File`] — append the raw elementary stream to a file
//!   (Phase 2 verification; `IMZERO2_HEADLESS_H264_OUT`).
//! - [`EncoderTarget::Channel`] — split the stream into access units
//!   (ffmpeg is told to insert Access Unit Delimiters and repeat SPS/PPS
//!   on key frames), wrap each AU in the ADR-0024 SD4 protobuf envelope
//!   with the 0x01 prefix, and push it into the WebSocket carrier's
//!   bounded channel.
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

use crate::imzero2::framesink::FrameSink;
use crate::imzero2::inputproto as pb;
use prost::Message as _;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex};

/// Cap on recycled BGRA buffers held to avoid per-frame reallocation.
/// latest(1) + free(≤2) + the one the feeder holds ≈ a handful of frame
/// buffers; bounded so a paused feeder can't grow memory without limit.
const FREE_LIST_CAP: usize = 2;

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
            if g.free.len() < FREE_LIST_CAP {
                g.free.push(old);
            }
        }
        drop(g);
        self.cv.notify_one();
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
    width: u32,
    height: u32,
    fps: f32,
    encoder_args: Vec<String>,
    target: EncoderTarget,
    restarts: u32,
}

impl EncoderSink {
    pub fn new(
        width: u32,
        height: u32,
        fps: f32,
        encoder_args: Vec<String>,
        target: EncoderTarget,
    ) -> std::io::Result<Self> {
        let mut sink = Self {
            child: None,
            feeder: None,
            drain: None,
            mailbox: FrameMailbox::new(),
            width,
            height,
            fps,
            encoder_args,
            target,
            restarts: 0,
        };
        sink.spawn(false)?;
        Ok(sink)
    }

    fn spawn(&mut self, restart: bool) -> std::io::Result<()> {
        let mut cmd = std::process::Command::new("ffmpeg");
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
            .args(&self.encoder_args);
        if matches!(self.target, EncoderTarget::Channel(_)) {
            // AU-accurate framing for the live carrier: AUDs delimit access
            // units without slice-header parsing, and SPS/PPS repeat on
            // every key frame so a (re)joining viewer can configure its
            // decoder from the stream alone (ADR-0024 SD4).
            cmd.arg("-bsf:v")
                .arg("h264_metadata=aud=insert,dump_extra=freq=keyframe");
        }
        cmd.arg("-f")
            .arg("h264")
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
        let drain = match &self.target {
            EncoderTarget::File(path) => {
                let path = path.clone();
                std::thread::Builder::new()
                    .name("imzero2-h264-drain".to_owned())
                    .spawn(move || drain_to_file(stdout, &path, restart))?
            }
            EncoderTarget::Channel(tx) => {
                let tx = tx.clone();
                std::thread::Builder::new()
                    .name("imzero2-h264-drain".to_owned())
                    .spawn(move || drain_to_channel(stdout, &tx))?
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
        self.feeder = Some(feeder);
        self.drain = Some(drain);
        Ok(())
    }

    fn reap(&mut self) {
        // Wake a feeder parked on the condvar...
        self.mailbox.close();
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
                tracing::info!(from_w = self.width, from_h = self.height, to_w = width, to_h = height,
                    "frame geometry changed — restarting encoder");
            } else {
                self.restarts += 1;
                tracing::error!(restarts = self.restarts, "ffmpeg encoder feeder died — restarting encoder");
            }
            self.reap();
            self.width = width;
            self.height = height;
            if let Err(e) = self.spawn(true) {
                tracing::error!(error=%e, "ffmpeg encoder respawn failed; will retry on next frame");
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

/// NAL unit type of the Annex-B unit starting at `idx` (the position of
/// the 00 00 01 prefix).
fn nal_type_at(buf: &[u8], idx: usize) -> Option<u8> {
    buf.get(idx + 3).map(|b| b & 0x1f)
}

/// Positions of all 00-00-01 start codes in `buf` whose NAL type is AUD
/// (9). A leading 00 of a 4-byte start code stays with the preceding AU,
/// which decoders ignore.
fn find_aud_positions(buf: &[u8]) -> Vec<usize> {
    let mut out = Vec::new();
    let mut i = 0usize;
    while i + 3 < buf.len() {
        if buf.get(i) == Some(&0) && buf.get(i + 1) == Some(&0) && buf.get(i + 2) == Some(&1) {
            if nal_type_at(buf, i) == Some(9) {
                out.push(i);
            }
            i += 3;
        } else {
            i += 1;
        }
    }
    out
}

/// True when the access unit contains an IDR slice (NAL type 5).
fn au_has_idr(au: &[u8]) -> bool {
    let mut i = 0usize;
    while i + 3 < au.len() {
        if au.get(i) == Some(&0) && au.get(i + 1) == Some(&0) && au.get(i + 2) == Some(&1) {
            if nal_type_at(au, i) == Some(5) {
                return true;
            }
            i += 3;
        } else {
            i += 1;
        }
    }
    false
}

/// Incrementally split ffmpeg's Annex-B output into access units (AUD to
/// AUD), wrap each in the SD4 envelope, and push framed payloads into the
/// carrier channel. Returns when ffmpeg's stdout closes or the channel is
/// dropped (viewer disconnected).
fn drain_to_channel(
    stdout: Option<std::process::ChildStdout>,
    tx: &tokio::sync::mpsc::Sender<Vec<u8>>,
) {
    use std::io::Read as _;
    let Some(mut so) = stdout else { return };
    let started = std::time::Instant::now();
    let mut pending: Vec<u8> = Vec::with_capacity(256 * 1024);
    let mut chunk = vec![0u8; 64 * 1024];
    let mut frame_index: u64 = 0;
    let mut sent_bytes: u64 = 0;
    loop {
        let n = match so.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => n,
            Err(e) => {
                tracing::error!(error=%e, "h264 drain read failed");
                break;
            }
        };
        pending.extend_from_slice(chunk.get(..n).unwrap_or_default());
        let auds = find_aud_positions(&pending);
        // Emit every complete AU (between consecutive AUDs); keep the tail
        // from the last AUD onwards — it is still accumulating.
        if auds.len() >= 2 {
            let last = *auds.last().unwrap_or(&0);
            for pair in auds.windows(2) {
                if let &[a, b] = pair {
                    let au = pending.get(a..b).unwrap_or_default();
                    let envelope = pb::VideoChunk {
                        frame_index,
                        timestamp_micros: started.elapsed().as_micros() as u64,
                        keyframe: au_has_idr(au),
                        data: au.to_vec(),
                    };
                    frame_index += 1;
                    let mut framed = Vec::with_capacity(1 + envelope.encoded_len());
                    framed.push(pb::PREFIX_VIDEO);
                    if envelope.encode(&mut framed).is_err() {
                        continue; // Vec encode cannot fail in practice
                    }
                    sent_bytes += framed.len() as u64;
                    if tx.blocking_send(framed).is_err() {
                        tracing::info!(frames = frame_index, "viewer channel closed — stopping h264 drain");
                        // Keep consuming so ffmpeg can exit cleanly once our
                        // stdin side closes.
                        let _ = std::io::copy(&mut so, &mut std::io::sink());
                        return;
                    }
                }
            }
            pending.drain(..last);
        }
    }
    tracing::info!(frames = frame_index, bytes = sent_bytes, "h264 drain finished (encoder eof)");
}
