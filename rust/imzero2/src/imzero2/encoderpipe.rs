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
//!   and pushes it into the WebSocket carrier's bounded channel as an
//!   [`EncodedFrame`] — the payload plus the key-frame flag and the
//!   *producing* encoder generation, so the carrier can recognise output
//!   from an encoder it has already retired (ADR-0242 SD3).
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
//! **Supervision is its own step** ([`EncoderSink::prepare_frame`],
//! ADR-0242 SD3), separate from handing over pixels
//! ([`EncoderSink::submit_frame`]). The caller runs it *before* its own
//! frame deduplication, because the three things that can kill an encoder
//! are all invisible to the pixels: the feeder marks the mailbox `dead` on
//! a write failure, ffmpeg can exit on its own (observed via
//! `Child::try_wait` — on an idle screen nothing is ever written, so a
//! write failure would never be reached), and the drain thread can end
//! early (stdout EOF, an unparseable stream). A geometry change (viewport
//! resize) takes the same reap+respawn path, since rawvideo dimensions are
//! fixed per ffmpeg invocation. Every (re)spawn begins the stream at
//! SPS/PPS + IDR, satisfying the SD4 (re)connect rule — and carries a
//! fresh [`EncoderSink::generation`], which is how the caller knows the
//! new encoder needs the current frame even when its pixels are unchanged.
//! Only a submission the sink *accepted* may advance the caller's dedup
//! state.
//!
//! That respawn is *budgeted* ([`restart_action`], composed with the death
//! observation by [`supervise`]). A dead encoder stays dead until a spawn
//! installs a fresh mailbox and child, so the frame path is itself the retry
//! loop: an encoder that can never start — an ffmpeg missing the lane's
//! encoder, a bad [`crate::imzero2::codeclane::ffmpeg_bin`] — would otherwise
//! be reaped and respawned on every frame, at frame rate, forever, one error
//! line each.
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

/// One coded frame leaving the encoder, as the carrier receives it
/// (ADR-0242 SD3).
///
/// The `payload` alone was ambiguous in two ways the carrier has to resolve.
/// It could not tell *which* encoder produced it — after a resize, a codec
/// switch or a supervised restart, frames of the retired encoder are still in
/// flight behind the new hello, and delivering them would feed a viewer
/// bitstream for the wrong geometry/codec. And joining progress could not be
/// measured: whether a waiting viewer has a decodable start is a question
/// about *key frames*, which only the container knows, not about how many
/// frames were submitted (the SD9 mailbox coalesces those).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EncodedFrame {
    /// Generation of the encoder that produced this frame, stamped at the
    /// producer. Deliberately not assigned at fan-out time: a frame relabelled
    /// when it is distributed would claim to belong to whichever encoder is
    /// current then, which is exactly the stale frame the stamp exists to
    /// catch.
    pub generation: u64,
    /// Container-level key-frame flag (NUT — ADR-0088 SD4), for any codec.
    pub keyframe: bool,
    /// Pre-framed WebSocket payload: the 0x01 prefix + `VideoChunk`.
    pub payload: Vec<u8>,
}

pub enum EncoderTarget {
    File(std::path::PathBuf),
    /// [`EncodedFrame`]s for the carrier's bounded fan-out channel.
    Channel {
        tx: tokio::sync::mpsc::Sender<EncodedFrame>,
        /// The encoder-generation counter, shared with the carrier that owns
        /// it. The sink allocates from it on every spawn attempt; the carrier
        /// reads it to know which generation is current and can bump it itself
        /// to retire one without spawning ([`advance_generation`]).
        ///
        /// Living on the carrier's side rather than inside the sink is what
        /// makes generations monotonic *across sink replacements*: dropping
        /// the sink (last viewer leaves, lane switch to mesh) and building a
        /// new one continues the sequence instead of restarting it, so a frame
        /// from the old sink can never carry a number the new one will reuse.
        generation: Arc<AtomicU64>,
    },
}

/// Allocate the next encoder generation from a shared counter, returning it.
///
/// The counter starts at 0 and the first allocation yields 1, so 0 is usable
/// as "no encoder has ever run". [`EncoderSink::spawn`] calls this *before*
/// the child exists — a frame of generation N therefore cannot reach the
/// channel before the counter reads N, which is what lets the carrier publish
/// a hello for the new generation and then treat everything older as stale
/// (ADR-0242 SD1/SD3). A spawn that then fails burns a number, which is
/// harmless: generations are only ever compared, never counted.
///
/// The carrier calls it directly to retire a generation without spawning
/// anything — dropping the encoder when the last viewer leaves, or switching
/// to the encoderless mesh lane, both of which must invalidate work already
/// queued for the old generation.
pub fn advance_generation(counter: &AtomicU64) -> u64 {
    counter.fetch_add(1, Ordering::AcqRel).wrapping_add(1)
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
    /// The generation counter this sink allocates from — the carrier's for a
    /// [`EncoderTarget::Channel`], a private one for a file dump (which has no
    /// consumer that cares).
    gen_counter: Arc<AtomicU64>,
    /// Generation of the running encoder: the stamp on its output, and the
    /// caller's signal that a fresh encoder needs the current frame. 0 until
    /// the first spawn succeeds.
    generation: u64,
    /// One-shot guard on the geometry-mismatch warning in
    /// [`Self::submit_frame`], cleared per spawn: a caller passing a
    /// wrong-sized buffer does so every frame, and the first line already says
    /// everything the later 59 a second would.
    len_warned: bool,
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

/// Everything the supervisor can observe about the current encoder, gathered
/// by [`EncoderSink::observe`]. Split out as plain data so the rule that reads
/// it is a function of its inputs, testable without an ffmpeg, a codec or a
/// process (ADR-0242 verification plan).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
struct SinkHealth {
    /// A child process is installed (a spawn succeeded and nothing reaped it).
    spawned: bool,
    /// The feeder failed a write to ffmpeg's stdin and exited.
    feeder_dead: bool,
    /// ffmpeg exited by itself — the death an idle screen would otherwise
    /// hide, since a mailbox with no traffic never attempts a write.
    child_exited: bool,
    /// The drain thread returned: stdout EOF, an unparseable stream, or a
    /// dropped receiver. Whatever the reason, encoded output is going nowhere.
    drain_finished: bool,
}

/// Why the supervisor considers the current encoder dead. Carried into the log
/// line so "restarting" names what was actually observed rather than assuming
/// the feeder-write case that used to be the only one detected.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum DeathCause {
    /// No child at all: the first spawn failed, a respawn failed, or the sink
    /// gave up and reaped.
    NotSpawned,
    FeederWrite,
    ChildExited,
    DrainEnded,
}

/// Classify [`SinkHealth`], most informative cause first. A dying ffmpeg
/// usually trips several of these within a frame or two of each other (the
/// child exits, then its stdout EOFs and the drain returns, then the next
/// write fails), so the order decides only which one the log names.
fn death_cause(h: SinkHealth) -> Option<DeathCause> {
    if !h.spawned {
        Some(DeathCause::NotSpawned)
    } else if h.feeder_dead {
        Some(DeathCause::FeederWrite)
    } else if h.child_exited {
        Some(DeathCause::ChildExited)
    } else if h.drain_finished {
        Some(DeathCause::DrainEnded)
    } else {
        None
    }
}

/// What [`EncoderSink::prepare_frame`] should do this frame.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Supervision {
    /// The encoder is running — submit to it.
    Ready,
    /// Dead, but not to be respawned now (inside the backoff, or the lane has
    /// been given up on): this frame goes nowhere.
    Wait,
    Restart {
        cause: DeathCause,
        fast_restarts: u32,
    },
    GiveUp {
        cause: DeathCause,
    },
}

/// The whole per-frame supervision rule: observe, then apply the restart
/// budget. Keeping it a function of ([`SinkHealth`], budget state, elapsed) is
/// what makes "an exited child is noticed on a frame identical to the last
/// one" a test that needs neither an encoder nor a clock.
fn supervise(
    health: SinkHealth,
    gave_up: bool,
    ran_for: std::time::Duration,
    fast_restarts: u32,
) -> Supervision {
    let Some(cause) = death_cause(health) else {
        return Supervision::Ready;
    };
    match restart_action(gave_up, ran_for, fast_restarts) {
        RestartAction::Wait => Supervision::Wait,
        RestartAction::GiveUp => Supervision::GiveUp { cause },
        RestartAction::Restart { fast_restarts } => Supervision::Restart {
            cause,
            fast_restarts,
        },
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
        // A file dump has no consumer that compares generations, so it gets a
        // private counter rather than making every caller supply one.
        let gen_counter = match &target {
            EncoderTarget::Channel { generation, .. } => generation.clone(),
            EncoderTarget::File(_) => Arc::new(AtomicU64::new(0)),
        };
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
            gen_counter,
            generation: 0,
            len_warned: false,
            restarts: 0,
            last_spawn: std::time::Instant::now(),
            fast_restarts: 0,
            gave_up: false,
        };
        sink.spawn(false)?;
        Ok(sink)
    }

    /// True once the supervisor spent its fast-restart budget on this lane:
    /// the encoder cannot be made to run and the stream is dead until the
    /// lane changes. The carrier reads this to degrade to the mesh lane.
    pub fn gave_up(&self) -> bool {
        self.gave_up
    }

    /// Frames coalesced (dropped) before the encoder under congestion (SD9).
    pub fn dropped(&self) -> u64 {
        self.mailbox.dropped()
    }

    /// Generation of the running encoder — the stamp on every
    /// [`EncodedFrame`] it produces, and 0 while none runs.
    ///
    /// A value that differs from the one the caller last submitted under means
    /// a fresh encoder: it starts at an IDR and has seen no pixels, so it
    /// needs *this* frame even if nothing changed. That is the signal
    /// deduplication must not swallow (ADR-0242 SD3).
    pub fn generation(&self) -> u64 {
        self.generation
    }

    /// Supervise the encoder and make it ready for a frame of `width` ×
    /// `height`; returns whether it can accept one now.
    ///
    /// Call this **every** frame and **before** deciding the frame is a
    /// duplicate. It is the only place the supervisor runs, and none of the
    /// three deaths it looks for (see the module docs) shows up in the pixels,
    /// so a dedup-gated supervisor stops progressing exactly when it is needed
    /// most: on a static screen. Respawning is still budgeted — a `false`
    /// return is either "inside the backoff", "the respawn attempt failed", or
    /// "this lane has been given up on" ([`Self::gave_up`]).
    ///
    /// A geometry change reaps and respawns regardless of the budget: rawvideo
    /// dimensions are fixed per invocation, so the new size is a genuinely new
    /// configuration that may well encode where the old one could not
    /// (hardware encoders reject sizes below their coded minimum).
    pub fn prepare_frame(&mut self, width: u32, height: u32) -> bool {
        if width != self.width || height != self.height {
            tracing::info!(
                from_w = self.width,
                from_h = self.height,
                to_w = width,
                to_h = height,
                "frame geometry changed — restarting encoder"
            );
            self.gave_up = false;
            self.fast_restarts = 0;
            self.reap();
            self.width = width;
            self.height = height;
            return self.respawn();
        }
        let health = self.observe();
        match supervise(
            health,
            self.gave_up,
            self.last_spawn.elapsed(),
            self.fast_restarts,
        ) {
            Supervision::Ready => true,
            Supervision::Wait => false,
            Supervision::GiveUp { cause } => {
                self.gave_up = true;
                self.reap();
                tracing::error!(
                    restarts = self.restarts,
                    ?cause,
                    lane = ?self.lane.codec,
                    ffmpeg = %crate::imzero2::codeclane::ffmpeg_bin(),
                    "ffmpeg encoder died {MAX_FAST_RESTARTS} times without staying up — \
                     giving up on this lane. The stream is dead until the viewport \
                     resizes or the codec is switched; the mesh lane needs no encoder."
                );
                false
            }
            Supervision::Restart {
                cause,
                fast_restarts,
            } => {
                self.fast_restarts = fast_restarts;
                self.restarts += 1;
                tracing::error!(
                    restarts = self.restarts,
                    fast_restarts = self.fast_restarts,
                    ?cause,
                    "ffmpeg encoder died — restarting encoder"
                );
                self.reap();
                self.respawn()
            }
        }
    }

    /// Hand one tightly-packed BGRA frame to the encoder; returns whether it
    /// was **accepted**.
    ///
    /// Only an accepted frame may advance the caller's deduplication state: a
    /// rejected one never reached an encoder, so remembering it as "sent"
    /// would make the next identical frame a duplicate of something nobody
    /// encoded — a frozen stream that looks like an idle one. Rejected when no
    /// encoder is running, when the feeder has since died, or when the buffer
    /// does not match the geometry [`Self::prepare_frame`] was given (feeding
    /// a wrong-sized buffer to `-f rawvideo` desynchronises every later frame).
    ///
    /// Non-blocking (SD9): the frame is copied into the depth-1 mailbox and a
    /// feeder thread does the writing.
    pub fn submit_frame(&mut self, bgra: &[u8]) -> bool {
        if self.child.is_none() || self.mailbox.dead.load(Ordering::Acquire) {
            return false;
        }
        let expected = self.width as usize * self.height as usize * 4;
        if bgra.len() != expected {
            if !self.len_warned {
                self.len_warned = true;
                tracing::warn!(
                    got = bgra.len(),
                    expected,
                    width = self.width,
                    height = self.height,
                    "frame buffer does not match the prepared geometry — not submitted"
                );
            }
            return false;
        }
        self.mailbox.submit(bgra);
        true
    }

    /// Poll everything that can report the encoder dead. Cheap enough for
    /// every frame: two atomic loads, a `waitpid(WNOHANG)` and a thread-handle
    /// check.
    fn observe(&mut self) -> SinkHealth {
        // Read the flags before borrowing the child mutably for try_wait.
        let feeder_dead = self.mailbox.dead.load(Ordering::Acquire);
        let drain_finished = self.drain.as_ref().is_some_and(|d| d.is_finished());
        let (spawned, child_exited) = match self.child.as_mut() {
            None => (false, false),
            // Nothing is logged here: while the backoff holds this runs on
            // every frame, and `reap` reports the exit status once, when the
            // decision to restart has actually been taken.
            Some(child) => match child.try_wait() {
                Ok(exited) => (true, exited.is_some()),
                Err(e) => {
                    tracing::warn!(error=%e, "cannot poll ffmpeg encoder status");
                    (true, false)
                }
            },
        };
        SinkHealth {
            spawned,
            feeder_dead,
            child_exited,
            drain_finished,
        }
    }

    /// Spawn after a reap, charging a failed attempt to the backoff clock so
    /// the next frame waits rather than retrying immediately.
    fn respawn(&mut self) -> bool {
        match self.spawn(true) {
            Ok(()) => true,
            Err(e) => {
                tracing::error!(error=%e, "ffmpeg encoder respawn failed; will retry after backoff");
                self.last_spawn = std::time::Instant::now();
                false
            }
        }
    }

    fn spawn(&mut self, restart: bool) -> std::io::Result<()> {
        // Allocate this encoder's generation *before* the child exists: no
        // frame can then carry a number the carrier has not already been able
        // to read, which is what makes "publish the hello, then treat older
        // generations as stale" sound (ADR-0242 SD1/SD3). A failed spawn below
        // burns the number — harmless, since generations are only compared.
        let generation = advance_generation(&self.gen_counter);
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
            EncoderTarget::Channel { .. } => {
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
            EncoderTarget::Channel { tx, .. } => {
                let tx = tx.clone();
                let stop = drain_stop.clone();
                // The generation travels into the drain thread *by value*, so
                // every frame it emits is stamped with the encoder that
                // produced it however long it spends in the channel.
                std::thread::Builder::new()
                    .name("imzero2-video-drain".to_owned())
                    .spawn(move || drain_to_channel_nut(stdout, &tx, &stop, generation))?
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
        self.generation = generation;
        self.len_warned = false;
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
    /// The plain sink path, for callers that feed every frame (the
    /// verification file dump): supervise, then submit. A caller that
    /// deduplicates pixels must instead call [`EncoderSink::prepare_frame`]
    /// and [`EncoderSink::submit_frame`] itself, so that supervision runs on
    /// frames it is about to drop and only accepted frames advance its hash
    /// (ADR-0242 SD3).
    fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, _frame_idx: u64) {
        if self.prepare_frame(width, height) {
            self.submit_frame(bgra);
        }
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
fn cancellable_send<T>(
    tx: &tokio::sync::mpsc::Sender<T>,
    payload: T,
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
///
/// `generation` is the encoder generation this drain belongs to, fixed when
/// its ffmpeg was spawned: every frame carries it, so output that outlives its
/// encoder is recognisable as stale wherever it is eventually read
/// (ADR-0242 SD3). Generic over the reader only so a test can drive it from a
/// NUT stream in memory.
fn drain_to_channel_nut<R: std::io::Read>(
    stdout: Option<R>,
    tx: &tokio::sync::mpsc::Sender<EncodedFrame>,
    stop: &AtomicBool,
    generation: u64,
) {
    // `Read` needs no import here: the generic bound brings `read` into scope.
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
                    let keyframe = frame.keyframe;
                    let framed = frame_payload(frame_index, started, keyframe, frame.data);
                    frame_index += 1;
                    sent_bytes += framed.len() as u64;
                    let encoded = EncodedFrame {
                        generation,
                        keyframe,
                        payload: framed,
                    };
                    match cancellable_send(tx, encoded, stop) {
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
            restart_action(
                false,
                RESTART_BACKOFF.checked_sub(Duration::from_millis(1)).unwrap(),
                0
            ),
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
    /// reading the socket) must abandon its send the moment `reap()` sets the
    /// stop flag — this is what keeps `reap()`, and the render thread that
    /// calls it on a resize / codec switch, from blocking on a stalled
    /// viewer. (No tokio runtime needed: `try_send/try_recv` are sync.)
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

    // ---------------------------------------------------------------------
    // ADR-0242 SD3: supervision independent of the pixels.
    //
    // Before this, the only death the sink noticed was a *feeder write
    // failure* — and the frame path that noticed it ran after the carrier's
    // blake3 dedup. Two consequences, both invisible to the tests that
    // existed: an ffmpeg that exits on its own was never observed at all on a
    // screen that is not changing (an idle mailbox never writes, so no write
    // can fail), and even a noticed death was only acted on when some pixel
    // moved. A static dashboard therefore kept a dead encoder forever.

    #[test]
    fn a_child_that_exited_is_a_death_even_with_no_write_failure() {
        // The idle case: ffmpeg quit, the feeder never wrote, the pixels never
        // changed. Nothing but the child poll can see this.
        let h = SinkHealth {
            spawned: true,
            child_exited: true,
            ..Default::default()
        };
        assert_eq!(death_cause(h), Some(DeathCause::ChildExited));
        assert_eq!(
            supervise(h, false, RESTART_BACKOFF, 0),
            Supervision::Restart {
                cause: DeathCause::ChildExited,
                fast_restarts: 1
            }
        );
    }

    #[test]
    fn a_finished_drain_and_a_dead_feeder_are_deaths_too() {
        // stdout EOF / an unparseable stream ends the drain: encoded output is
        // going nowhere even if the child is somehow still alive.
        assert_eq!(
            death_cause(SinkHealth {
                spawned: true,
                drain_finished: true,
                ..Default::default()
            }),
            Some(DeathCause::DrainEnded)
        );
        assert_eq!(
            death_cause(SinkHealth {
                spawned: true,
                feeder_dead: true,
                ..Default::default()
            }),
            Some(DeathCause::FeederWrite)
        );
        // A failed spawn leaves no child, which is its own cause (and is what
        // keeps the retry loop running after `spawn` returned an error).
        assert_eq!(
            death_cause(SinkHealth::default()),
            Some(DeathCause::NotSpawned)
        );
    }

    #[test]
    fn a_running_encoder_is_ready() {
        let healthy = SinkHealth {
            spawned: true,
            ..Default::default()
        };
        assert_eq!(death_cause(healthy), None);
        // Ready regardless of the clock and the streak — the budget only ever
        // gates *restarts*, never submission to an encoder that is alive.
        assert_eq!(
            supervise(healthy, false, Duration::ZERO, MAX_FAST_RESTARTS),
            Supervision::Ready
        );
    }

    #[test]
    fn supervision_reuses_the_existing_backoff_and_budget() {
        let dead = SinkHealth {
            spawned: true,
            child_exited: true,
            ..Default::default()
        };
        // Inside the backoff: this frame goes nowhere, no spawn attempt.
        assert_eq!(supervise(dead, false, Duration::ZERO, 0), Supervision::Wait);
        // Budget spent: stop, and name what killed it.
        assert_eq!(
            supervise(dead, false, RESTART_BACKOFF, MAX_FAST_RESTARTS),
            Supervision::GiveUp {
                cause: DeathCause::ChildExited
            }
        );
        // Already given up: quiet forever.
        assert_eq!(
            supervise(dead, true, RESTART_STABLE_AFTER * 100, 0),
            Supervision::Wait
        );
    }

    #[test]
    fn generations_are_monotonic_across_sink_replacement() {
        // The counter is the carrier's, not the sink's: dropping a sink (last
        // viewer left, lane switched to mesh) and building another must not
        // rewind the sequence, or a frame from the old encoder could carry a
        // number the new one will claim.
        let counter = Arc::new(AtomicU64::new(0));
        assert_eq!(counter.load(Ordering::Acquire), 0, "0 means no encoder yet");
        let first = advance_generation(&counter);
        let second = advance_generation(&counter);
        assert_eq!((first, second), (1, 2));
        // The carrier retiring a generation by hand (no spawn) still advances.
        assert_eq!(advance_generation(&counter), 3);
        assert_eq!(counter.load(Ordering::Acquire), 3);
    }

    /// A sink with no live encoder, built field-by-field so the frame path can
    /// be driven against a chosen death and a chosen clock without an ffmpeg,
    /// a codec or a GPU anywhere near the test.
    fn down_sink(
        child: Option<std::process::Child>,
        last_spawn: std::time::Instant,
    ) -> EncoderSink {
        EncoderSink {
            child,
            feeder: None,
            drain: None,
            mailbox: FrameMailbox::new(),
            drain_stop: Arc::new(AtomicBool::new(false)),
            width: 2,
            height: 2,
            fps: 30.0,
            lane: CodecLane::software(crate::imzero2::codeclane::VideoCodec::H264),
            // Never written: nothing in these tests spawns or drains.
            target: EncoderTarget::File(std::path::PathBuf::from("/dev/null")),
            gen_counter: Arc::new(AtomicU64::new(7)),
            generation: 7,
            len_warned: false,
            restarts: 0,
            last_spawn,
            fast_restarts: 0,
            gave_up: false,
        }
    }

    /// A child process that has already exited, standing in for an ffmpeg that
    /// quit on its own: this test binary re-invoked with `--list`, which prints
    /// its test names and exits without running any. No ffmpeg, no codec, no
    /// external binary — the only thing needed is a process that dies.
    /// `wait` here leaves the exit status cached, so the sink's own `try_wait`
    /// is what has to notice it.
    fn exited_child() -> Option<std::process::Child> {
        let exe = std::env::current_exe().ok()?;
        let mut child = std::process::Command::new(exe)
            .arg("--list")
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .ok()?;
        child.wait().ok()?;
        Some(child)
    }

    #[test]
    fn prepare_frame_observes_an_exited_child() {
        let Some(child) = exited_child() else {
            eprintln!("cannot spawn a child process here; skipping exited-child observation");
            return;
        };
        // Inside the backoff on purpose: the point is the *observation*, and a
        // restart decision here would spawn a real ffmpeg.
        let mut sink = down_sink(Some(child), std::time::Instant::now());
        let health = sink.observe();
        assert!(health.spawned, "the child is installed");
        assert!(health.child_exited, "an exited child must be observed");
        assert!(
            !health.feeder_dead,
            "nothing wrote to it — the pre-SD3 detector would have seen nothing"
        );
        assert_eq!(death_cause(health), Some(DeathCause::ChildExited));
        // Within the backoff the frame is simply dropped, and no generation is
        // burned because no spawn was attempted.
        assert!(!sink.prepare_frame(2, 2));
        assert_eq!(sink.gen_counter.load(Ordering::Acquire), 7);
    }

    #[test]
    fn nothing_is_submitted_while_the_encoder_is_down() {
        // A failed spawn leaves no child. The frame must not land in the
        // mailbox, and the caller must be told so — a caller that advanced its
        // dedup hash here would treat the next identical frame as already
        // sent, freezing the stream until the pixels happen to change twice.
        let mut sink = down_sink(None, std::time::Instant::now());
        assert!(!sink.prepare_frame(2, 2), "inside the backoff");
        assert!(!sink.submit_frame(&[0u8; 16]), "no encoder to submit to");
        assert!(
            sink.mailbox.inner.lock().expect("mailbox").latest.is_none(),
            "no frame may reach the mailbox"
        );
        // No spawn attempt, so the generation is untouched and the caller sees
        // no reason to re-send.
        assert_eq!(sink.generation(), 7);
        assert_eq!(sink.gen_counter.load(Ordering::Acquire), 7);
    }

    #[test]
    fn submit_frame_rejects_a_buffer_that_does_not_match_the_geometry() {
        let Some(child) = exited_child() else {
            eprintln!("cannot spawn a child process here; skipping geometry-mismatch check");
            return;
        };
        let mut sink = down_sink(Some(child), std::time::Instant::now());
        // 2×2 BGRA is 16 bytes. Anything else desynchronises `-f rawvideo`
        // for every later frame, so it is refused rather than written.
        assert!(!sink.submit_frame(&[]));
        assert!(!sink.submit_frame(&[0u8; 12]));
        assert!(!sink.submit_frame(&[0u8; 20]));
        assert!(
            sink.mailbox.inner.lock().expect("mailbox").latest.is_none(),
            "a mismatched buffer must not be written"
        );
        assert!(sink.submit_frame(&[0u8; 16]), "the prepared geometry fits");
        assert!(sink.mailbox.inner.lock().expect("mailbox").latest.is_some());
    }

    /// Encode a short NUT stream with the host ffmpeg, or None if none of the
    /// software encoders this repo ships a lane for is built into it.
    /// Nothing here reimplements a muxer: NUT frame boundaries and the
    /// container key-frame flag are exactly what is under test, and the stamp
    /// is codec-independent by construction (ADR-0088 SD4), so whichever
    /// encoder the host has is a valid witness.
    fn nut_fixture() -> Option<Vec<u8>> {
        for codec in ["libopenh264", "libsvtav1", "libvpx-vp9"] {
            let path = std::env::temp_dir().join(format!(
                "imzero2_encoderpipe_stamp_{}_{}.nut",
                codec.replace('-', "_"),
                std::process::id()
            ));
            let ok = std::process::Command::new(crate::imzero2::codeclane::ffmpeg_bin())
                .args([
                    "-hide_banner",
                    "-loglevel",
                    "error",
                    "-y",
                    "-f",
                    "lavfi",
                    "-i",
                    "testsrc=size=128x128:rate=10",
                    "-frames:v",
                    "10",
                    "-c:v",
                    codec,
                    "-bf",
                    "0",
                    "-g",
                    "5",
                    "-pix_fmt",
                    "yuv420p",
                    "-flush_packets",
                    "1",
                    "-f",
                    "nut",
                ])
                .arg(&path)
                .stdin(std::process::Stdio::null())
                .stdout(std::process::Stdio::null())
                .stderr(std::process::Stdio::null())
                .status()
                .map(|s| s.success())
                .unwrap_or(false);
            let data = if ok { std::fs::read(&path).ok() } else { None };
            let _ = std::fs::remove_file(&path);
            if let Some(d) = data.filter(|d| !d.is_empty()) {
                eprintln!("drain stamp fixture encoded with {codec}");
                return Some(d);
            }
        }
        None
    }

    #[test]
    fn the_drain_stamps_every_frame_with_its_producing_generation() {
        // The stamp is applied where the frame is produced. Assigning it at
        // fan-out instead would label a frame with whichever encoder is
        // current by then — precisely the stale frame it exists to catch.
        let Some(nut) = nut_fixture() else {
            eprintln!("no usable ffmpeg/libopenh264 here; skipping drain stamp test");
            return;
        };
        let (tx, mut rx) = tokio::sync::mpsc::channel::<EncodedFrame>(64);
        let stop = AtomicBool::new(false);
        drain_to_channel_nut(Some(std::io::Cursor::new(nut)), &tx, &stop, 42);
        let mut frames = Vec::new();
        while let Ok(f) = rx.try_recv() {
            frames.push(f);
        }
        assert!(
            frames.len() >= 2,
            "expected coded frames, got {}",
            frames.len()
        );
        assert!(
            frames.iter().all(|f| f.generation == 42),
            "every frame carries its producer's generation"
        );
        assert!(frames[0].keyframe, "a stream starts decodable");
        assert!(
            frames.iter().any(|f| !f.keyframe),
            "-g 5 over 10 frames must also produce non-key frames"
        );
        assert!(
            frames.iter().all(|f| f.payload.first() == Some(&pb::PREFIX_VIDEO)),
            "the payload bytes are unchanged: 0x01 + VideoChunk"
        );
    }
}
