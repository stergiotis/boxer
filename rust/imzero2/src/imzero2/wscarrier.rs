//! WebSocket carrier (ADR-0024 SD4/SD6; ADR-0086 active/passive roster).
//!
//! Two TCP listeners (`IMZERO2_HEADLESS_LISTEN` and port+1, kept for URL
//! compatibility) run an identical dispatcher: a request that carries a
//! WebSocket upgrade header becomes a data channel; anything else is
//! answered with the embedded single-file viewer page. Sniffing uses
//! `TcpStream::peek`, so the stream reaches the WebSocket handshake
//! unconsumed. Either port therefore works for both the page and the
//! socket — the viewer connects back to its own origin (`/ws`), which
//! also makes a single TLS-terminating reverse proxy in front (or an SSH
//! tunnel) sufficient for the whole wire.
//!
//! A single binary WebSocket carries everything with a one-byte type
//! prefix (SD6): 0x01 video chunks server→client, 0x02 protobuf input
//! events client→server, 0x03 session control both ways.
//!
//! Session model (ADR-0086): connections are first-class — a [`Registry`]
//! holds each [`Conn`] with its role (`active` | `passive`) and a
//! per-connection outbound queue. The invariant is **≤ 1 active**; the
//! first connection is admitted active and the rest passive (read-only).
//! Every membership/role change rebroadcasts a per-recipient [`pb::Roster`]
//! (each connection's copy carries its own `you_id`/`you_role`, which folds
//! SD8's RoleChanged into the roster). Input, resize, cadence and clipboard
//! injection are honoured **only** from the active connection; passive
//! connections are dropped at the server. Takeover (`TakeSession`) is
//! unilateral (one principal, ADR-0082): it promotes the requester and
//! demotes the prior active; a slot freed by a disconnect auto-promotes a
//! lone passive.
//!
//! One **shared** periodic-IDR encoder serves the whole session (ADR-0086
//! SD5): it spawns on the 0→1 transition (a fresh SPS/PPS + IDR for the
//! first viewer) and stops on N→0. Its NAL units are broadcast by a
//! [`distribute`] task to every connection's queue; a late joiner starts
//! at the next *scheduled* IDR (no forced mid-stream IDR → no colour pulse
//! for viewers already watching, ADR-0024 SD3 active-scoped). Geometry and
//! codec changes re-announce the hello to all connections.
//!
//! The tokio runtime lives on a dedicated thread; the render loop talks to
//! it only through atomics, mutex-guarded vectors, the registry, and the
//! bounded encoder channel.

use crate::imzero2::codeclane::CodecLane;
use crate::imzero2::encoderpipe::{EncoderSink, EncoderTarget};
use crate::imzero2::framesink::FrameSink as _;
use crate::imzero2::inputproto as pb;
use futures_util::{SinkExt as _, StreamExt as _};
use prost::Message as _;

/// Per-connection outbound queue depth: video NAL units and per-recipient
/// control (roster, clipboard, hello re-announce) share it. A connection
/// that stops draining (a stalled viewer) overflows this and is dropped to
/// (recovering on its own decode-error reconnect) rather than backing up
/// the encoder or the other viewers.
const VIDEO_CHANNEL_CAP: usize = 16;

/// Default bound on simultaneously-parked connections (ADR-0086 SD2 /
/// ADR-0082 SD5 `IMZERO2_HEADLESS_MAX_CONNECTIONS`). Read from the env once
/// at [`WsCarrier::start`]; this is the fallback.
const DEFAULT_MAX_CONNECTIONS: usize = 8;

/// Liveness keepalive. A viewer that stops draining its socket — a backgrounded
/// or frozen tab, or a half-open connection left behind by a browser
/// decode-error reconnect — would otherwise park its session task on a pending
/// send forever, never observe the close, and **linger in the roster, inflating
/// the viewer count**. So every socket write is bounded by [`WRITE_TIMEOUT`],
/// and an otherwise-idle connection is pinged every [`PING_INTERVAL`] (the
/// browser auto-replies); a peer that cannot accept a write in time is reaped.
const PING_INTERVAL: std::time::Duration = std::time::Duration::from_secs(5);
const WRITE_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(10);

/// One first-class connection (ADR-0086 SD1). Identity stays minimal (SD9):
/// peer IP is used for rate-limiting/audit (ADR-0082) only and never enters
/// the roster.
struct Conn {
    id: u64,
    role: pb::Role,
    /// Takeover-capable: only a WebCodecs-capable connection may become
    /// active (SD2). Reported by the client's `ClientHello`; until then it
    /// reads false.
    webcodecs: bool,
    /// Optional device label ("iPad"), reported by the `ClientHello`.
    label: String,
    /// Outbound queue for this connection (video + per-recipient control).
    tx: tokio::sync::mpsc::Sender<Vec<u8>>,
}

/// The set of live connections and the single active slot (ADR-0086 SD1).
/// Guarded by [`Inner::registry`]; mutated only from the carrier's async
/// session tasks and read briefly by the render thread (active lookup).
struct Registry {
    conns: Vec<Conn>,
    /// `Some(id)` of the active connection, or `None` when the slot is empty
    /// (several passives present, none promoted — each keeps its button).
    active_id: Option<u64>,
    next_id: u64,
    max: usize,
}

impl Registry {
    fn new(max: usize) -> Self {
        Self {
            conns: Vec::new(),
            active_id: None,
            next_id: 1,
            max,
        }
    }

    fn find(&self, id: u64) -> Option<&Conn> {
        self.conns.iter().find(|c| c.id == id)
    }

    /// Admit a connection, assigning it the next id and a role: **active iff
    /// there is no current active, else passive** (SD2 — first device drives,
    /// later devices watch). Returns the new id, or `None` if `max` parked
    /// connections are already present (the connection is refused).
    fn admit(&mut self, tx: tokio::sync::mpsc::Sender<Vec<u8>>) -> Option<u64> {
        if self.conns.len() >= self.max {
            return None;
        }
        let id = self.next_id;
        self.next_id += 1;
        let role = if self.active_id.is_none() {
            self.active_id = Some(id);
            pb::Role::Active
        } else {
            pb::Role::Passive
        };
        self.conns.push(Conn {
            id,
            role,
            webcodecs: false,
            label: String::new(),
            tx,
        });
        Some(id)
    }

    /// Remove a connection. Whenever this leaves the active slot empty and a
    /// **lone** connection behind, that one auto-promotes (SD2 — zero-friction
    /// return to your other device); with several remaining the slot stays
    /// empty (each keeps its button). This covers both removing the active and
    /// removing a passive that leaves a single connection — so the last
    /// remaining viewer always drives.
    fn remove(&mut self, id: u64) {
        self.conns.retain(|c| c.id != id);
        if self.active_id == Some(id) {
            self.active_id = None;
        }
        if self.active_id.is_none() {
            if let [only] = self.conns.as_mut_slice() {
                only.role = pb::Role::Active;
                self.active_id = Some(only.id);
            }
        }
    }

    /// Make `id` the active connection and demote the prior active to passive
    /// (SD2 — unilateral takeover). Returns whether anything changed.
    fn take_session(&mut self, id: u64) -> bool {
        if self.active_id == Some(id) {
            return false;
        }
        for c in &mut self.conns {
            if c.id == id {
                c.role = pb::Role::Active;
            } else if c.role == pb::Role::Active {
                c.role = pb::Role::Passive;
            }
        }
        self.active_id = Some(id);
        true
    }

    /// Record a connection's reported capabilities + label (`ClientHello`).
    fn set_caps(&mut self, id: u64, webcodecs: bool, label: String) {
        if let Some(c) = self.conns.iter_mut().find(|c| c.id == id) {
            c.webcodecs = webcodecs;
            c.label = label;
        }
    }

    fn is_active(&self, id: u64) -> bool {
        self.active_id == Some(id)
    }

    /// The active connection's outbound queue, if any (clipboard → active).
    fn active_tx(&self) -> Option<tokio::sync::mpsc::Sender<Vec<u8>>> {
        self.active_id
            .and_then(|id| self.find(id))
            .map(|c| c.tx.clone())
    }
}

struct Inner {
    /// Raw wire events from the **active** connection, drained by the render
    /// thread each tick (passive input is dropped at the server, SD2/SD8).
    events: std::sync::Mutex<Vec<pb::input_event::Event>>,
    /// Latest viewport-resize from the active connection; drained by the
    /// render thread, which applies it (target rebuild + hello re-announce +
    /// encoder restart) and answers with a fresh [`pb::SessionHello`].
    resize: std::sync::Mutex<Option<pb::ViewportResize>>,
    /// Latest runtime cadence request (0 continuous / 1 reactive) from the
    /// active connection, drained by the render thread.
    cadence_request: std::sync::Mutex<Option<u32>>,
    /// Latest clipboard paste from the active connection (ADR-0082 SD6),
    /// drained by the render thread and injected as `egui::Event::Paste`.
    paste: std::sync::Mutex<Option<String>>,
    /// Wakes the render thread out of its reactive sleep when anything
    /// arrives that wants a pass soon: input, resize, cadence change,
    /// connect/disconnect, takeover, paste. Sends are fire-and-forget.
    waker: std::sync::mpsc::Sender<()>,
    /// True while ≥ 1 connection is present — the render thread checks this
    /// cheaply each frame to decide whether to render pixels and run the
    /// shared encoder. Mirrors `registry.conns.is_empty()` negated.
    connected: std::sync::atomic::AtomicBool,
    /// First-class connections + the single active slot (ADR-0086 SD1).
    registry: std::sync::Mutex<Registry>,
    /// Current stream geometry; sent on connect and after each applied
    /// resize. Updated by the render thread via [`WsCarrier::apply_geometry`].
    hello: std::sync::Mutex<pb::SessionHello>,
    /// Latest decode capabilities reported by the active connection (ADR-0088
    /// SD2/SD8), drained by the render thread to forward to the Go interpreter.
    decode_caps: std::sync::Mutex<Option<pb::DecodeCapabilities>>,
    /// Wire telemetry (ADR-0088): bytes + frames the shared encoder produced
    /// (counted once per access unit in [`distribute`], not multiplied by the
    /// viewer count — it is the stream bitrate, not aggregate egress), and the
    /// active viewer's latest decoded-frame count (from its progress pings).
    bytes_sent: std::sync::atomic::AtomicU64,
    frames_sent: std::sync::atomic::AtomicU64,
    frames_decoded: std::sync::atomic::AtomicU64,
}

pub struct WsCarrier {
    inner: std::sync::Arc<Inner>,
    /// The single shared encoder (ADR-0086 SD5): present while ≥ 1 connection,
    /// `None` otherwise. Geometry/codec changes drop it for a restart.
    encoder: Option<EncoderSink>,
    /// Stable input to the [`distribute`] fan-out task; every (re)spawned
    /// encoder targets a clone of this, so the distributor outlives any single
    /// encoder generation.
    encoder_tx: tokio::sync::mpsc::Sender<Vec<u8>>,
    fps: f32,
    lane: CodecLane,
    /// blake3 of the last frame fed to the encoder; identical frames are
    /// skipped (no encode, no wire bytes). Reset whenever the encoder is
    /// (re)spawned so a fresh stream always begins with a real frame.
    last_frame_hash: Option<blake3::Hash>,
}

impl WsCarrier {
    /// Bind `listen` (e.g. "127.0.0.1:8089") for WebSocket and `port+1`
    /// for the viewer page, then run both — plus the broadcast distributor —
    /// on a dedicated tokio thread. `waker` is signalled whenever wire
    /// activity wants a render pass soon.
    pub fn start(
        listen: &str,
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
        cadence: u32,
        fps: f32,
        lane: CodecLane,
        waker: std::sync::mpsc::Sender<()>,
    ) -> std::io::Result<Self> {
        let max = std::env::var("IMZERO2_HEADLESS_MAX_CONNECTIONS")
            .ok()
            .and_then(|s| s.parse::<usize>().ok())
            .filter(|n| *n >= 1)
            .unwrap_or(DEFAULT_MAX_CONNECTIONS);
        // The encoder→distributor channel is created once and lives for the
        // carrier's lifetime, independent of any single encoder generation.
        let (encoder_tx, encoder_rx) = tokio::sync::mpsc::channel::<Vec<u8>>(VIDEO_CHANNEL_CAP);
        let inner = std::sync::Arc::new(Inner {
            events: std::sync::Mutex::new(Vec::new()),
            resize: std::sync::Mutex::new(None),
            cadence_request: std::sync::Mutex::new(None),
            paste: std::sync::Mutex::new(None),
            waker,
            connected: std::sync::atomic::AtomicBool::new(false),
            registry: std::sync::Mutex::new(Registry::new(max)),
            hello: std::sync::Mutex::new(pb::SessionHello {
                width_px,
                height_px,
                pixels_per_point,
                cadence,
                codec: lane.webcodecs_codec_string(width_px, height_px),
            }),
            decode_caps: std::sync::Mutex::new(None),
            bytes_sent: std::sync::atomic::AtomicU64::new(0),
            frames_sent: std::sync::atomic::AtomicU64::new(0),
            frames_decoded: std::sync::atomic::AtomicU64::new(0),
        });
        // Bind synchronously so startup errors (port in use) fail fast in
        // the caller instead of asynchronously on the carrier thread.
        let ws_listener = std::net::TcpListener::bind(listen)?;
        ws_listener.set_nonblocking(true)?;
        let ws_addr = ws_listener.local_addr()?;
        let page_addr = std::net::SocketAddr::new(ws_addr.ip(), ws_addr.port().wrapping_add(1));
        let page_listener = std::net::TcpListener::bind(page_addr)?;
        page_listener.set_nonblocking(true)?;
        tracing::info!(viewer=%format!("http://{page_addr}/"), websocket=%format!("ws://{ws_addr}/"), max_connections=max, "remote viewer carrier listening");

        let inner_thread = inner.clone();
        std::thread::Builder::new()
            .name("imzero2-ws-carrier".to_owned())
            .spawn(move || {
                let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                    Ok(rt) => rt,
                    Err(e) => {
                        tracing::error!(error=%e, "carrier tokio runtime failed to build");
                        return;
                    }
                };
                rt.block_on(async move {
                    let page: std::sync::Arc<str> = std::sync::Arc::from(include_str!("viewer/index.html"));
                    let a = async {
                        match tokio::net::TcpListener::from_std(ws_listener) {
                            Ok(l) => accept_loop(l, inner_thread.clone(), page.clone()).await,
                            Err(e) => tracing::error!(error=%e, "ws listener conversion failed"),
                        }
                    };
                    let inner_page = inner_thread.clone();
                    let page2 = page.clone();
                    let b = async {
                        match tokio::net::TcpListener::from_std(page_listener) {
                            Ok(l) => accept_loop(l, inner_page, page2).await,
                            Err(e) => tracing::error!(error=%e, "page listener conversion failed"),
                        }
                    };
                    // The distributor fans the shared encoder's NAL units out to
                    // every connection's queue; it runs for the carrier's lifetime.
                    let dist = distribute(encoder_rx, inner_thread.clone());
                    tokio::join!(a, b, dist);
                });
            })?;
        Ok(Self {
            inner,
            encoder: None,
            encoder_tx,
            fps,
            lane,
            last_frame_hash: None,
        })
    }

    /// True while ≥ 1 viewer is connected — the host skips rendering pixels
    /// entirely when nothing consumes them.
    pub fn connected(&self) -> bool {
        self.inner.connected.load(std::sync::atomic::Ordering::Acquire)
    }

    /// Latest pending runtime cadence request from the active connection.
    pub fn take_cadence(&mut self) -> Option<u32> {
        self.inner.cadence_request.lock().ok().and_then(|mut c| c.take())
    }

    /// Record the applied cadence so future hellos report it.
    pub fn set_hello_cadence(&mut self, cadence: u32) {
        if let Ok(mut hello) = self.inner.hello.lock() {
            hello.cadence = cadence;
        }
    }

    /// Drain active-connection wire input events into `out` for the render thread.
    pub fn drain_events(&mut self, out: &mut Vec<pb::input_event::Event>) {
        if let Ok(mut events) = self.inner.events.lock() {
            out.append(&mut events);
        }
    }

    /// Latest pending viewport-resize from the active connection (latest wins).
    pub fn take_resize(&mut self) -> Option<pb::ViewportResize> {
        self.inner.resize.lock().ok().and_then(|mut r| r.take())
    }

    /// Latest pending clipboard paste from the active connection (ADR-0082
    /// SD6), drained by the render thread to inject `egui::Event::Paste`.
    pub fn take_paste(&mut self) -> Option<String> {
        self.inner.paste.lock().ok().and_then(|mut p| p.take())
    }

    /// Send host-copied text to the **active** connection's clipboard
    /// (ADR-0082 SD6 — only the active session syncs). Non-blocking on the
    /// render thread (SD9): a full/stalled active queue drops the copy.
    pub fn send_clipboard_to_active(&mut self, text: String) {
        let tx = self.inner.registry.lock().ok().and_then(|r| r.active_tx());
        if let Some(tx) = tx {
            let msg = pb::SessionControl {
                control: Some(pb::session_control::Control::Clipboard(pb::ClipboardData { text })),
            };
            let mut framed = Vec::with_capacity(1 + msg.encoded_len());
            framed.push(pb::PREFIX_SESSION);
            let _ = msg.encode(&mut framed);
            if tx.try_send(framed).is_err() {
                tracing::debug!("clipboard copy dropped — active viewer queue full or gone");
            }
        }
    }

    /// Commit a new stream geometry (already clamped by the host): update the
    /// hello for future connections, stop the current encoder, and — if any
    /// viewer is connected — re-announce the hello to **all** connections
    /// *before* the next encoder spawns, so each viewer resizes its canvas and
    /// rejoins at the new stream's first IDR.
    pub fn apply_geometry(&mut self, width_px: u32, height_px: u32, pixels_per_point: f32) {
        let mut hello = pb::SessionHello {
            width_px,
            height_px,
            pixels_per_point,
            cadence: 0,
            codec: self.lane.webcodecs_codec_string(width_px, height_px),
        };
        if let Ok(mut guard) = self.inner.hello.lock() {
            hello.cadence = guard.cadence; // geometry changes don't touch cadence
            *guard = hello.clone();
        }
        // Drop first: reap blocks until the old drain flushed its remaining
        // (old-geometry) access units into the encoder channel, so the hello
        // below lands after them at the distributor.
        if self.encoder.take().is_some() {
            tracing::info!(width_px, height_px, pixels_per_point, "geometry change — shared encoder stopped for restart");
        }
        self.last_frame_hash = None;
        broadcast_hello(&self.inner, hello);
    }

    /// ADR-0088 SD7: switch the active codec at runtime. Updates the lane and
    /// the hello's codec string, stops the current encoder (its drain flushes
    /// the old codec's tail), re-announces the hello to all connections, and
    /// lets `on_frame` respawn the encoder with the new lane (fresh key frame).
    pub fn set_video_codec(&mut self, codec: crate::imzero2::codeclane::VideoCodec) {
        if codec == self.lane.codec {
            return;
        }
        self.lane = crate::imzero2::codeclane::CodecLane::best(codec);
        if let Ok(mut h) = self.inner.hello.lock() {
            // Geometry is unchanged on a codec switch — reuse the current
            // stream size so the new codec announces a resolution-correct level.
            let (w, ht) = (h.width_px, h.height_px);
            h.codec = self.lane.webcodecs_codec_string(w, ht);
        }
        self.encoder.take(); // flush old stream; on_frame respawns the new lane
        self.last_frame_hash = None;
        let hello = self.inner.hello.lock().map(|h| (*h).clone()).unwrap_or_default();
        broadcast_hello(&self.inner, hello);
        tracing::info!(codec = self.lane.codec.as_str(), "video codec switched at runtime");
    }

    /// ADR-0088: a clone of the active connection's latest reported decode caps.
    pub fn decode_caps(&self) -> Option<pb::DecodeCapabilities> {
        self.inner.decode_caps.lock().ok().and_then(|g| g.clone())
    }

    /// ADR-0088 wire telemetry for the Go control: (bytes_sent, frames_sent,
    /// frames_decoded, frames_dropped). Cumulative for the current session
    /// (reset when the shared encoder respawns on the 0→1 transition).
    pub fn stats(&self) -> (u64, u64, u64, u64) {
        use std::sync::atomic::Ordering::Relaxed;
        let dropped = self.encoder.as_ref().map(|e| e.dropped()).unwrap_or(0);
        (
            self.inner.bytes_sent.load(Relaxed),
            self.inner.frames_sent.load(Relaxed),
            self.inner.frames_decoded.load(Relaxed),
            dropped,
        )
    }
}

impl WsCarrier {
    /// Feed one rendered frame to the shared encoder (ADR-0086 SD5). The
    /// encoder spawns on the first connection (0→1, a fresh SPS/PPS + IDR for
    /// the first viewer) and stops when the last leaves (N→0); a connection
    /// that joins an existing session does **not** respawn it — it waits for
    /// the next scheduled IDR, so no viewer already watching is pulsed.
    /// Frames whose pixels are identical to the previous fed one (blake3) are
    /// skipped, except the first frame of a fresh encoder.
    pub fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, frame_idx: u64) {
        let connected = self.inner.connected.load(std::sync::atomic::Ordering::Acquire);
        if !connected {
            if self.encoder.take().is_some() {
                tracing::info!("all viewers disconnected — stopping shared encoder");
                self.last_frame_hash = None;
            }
            return;
        }
        if self.encoder.is_none() {
            self.last_frame_hash = None;
            // Telemetry is per-session: a fresh shared encoder starts a fresh
            // byte/frame count (the EMA bitrate the Go control reads).
            use std::sync::atomic::Ordering::Relaxed;
            self.inner.bytes_sent.store(0, Relaxed);
            self.inner.frames_sent.store(0, Relaxed);
            self.inner.frames_decoded.store(0, Relaxed);
            match EncoderSink::new(width, height, self.fps, self.lane.clone(), EncoderTarget::Channel(self.encoder_tx.clone())) {
                Ok(enc) => {
                    tracing::info!("first viewer connected — shared periodic-IDR encoder started");
                    self.encoder = Some(enc);
                }
                Err(e) => {
                    tracing::error!(error=%e, "failed to start shared encoder");
                    return;
                }
            }
        }
        let hash = blake3::hash(bgra);
        if self.last_frame_hash == Some(hash) {
            return; // pixel-identical to the last fed frame
        }
        if let Some(enc) = &mut self.encoder {
            enc.on_frame(bgra, width, height, frame_idx);
            self.last_frame_hash = Some(hash);
        }
    }
}

/// Fan the shared encoder's pre-framed NAL units (0x01 + VideoChunk) out to
/// every connection's queue (ADR-0086 SD5 broadcast). Each connection's send
/// is non-blocking `try_send`: a stalled viewer drops the unit (and recovers
/// at its next decode-error reconnect) without backing up the encoder or the
/// other viewers. Telemetry counts each unit once (stream bitrate, not egress).
async fn distribute(mut encoder_rx: tokio::sync::mpsc::Receiver<Vec<u8>>, inner: std::sync::Arc<Inner>) {
    use std::sync::atomic::Ordering::Relaxed;
    while let Some(payload) = encoder_rx.recv().await {
        inner.bytes_sent.fetch_add(payload.len() as u64, Relaxed);
        inner.frames_sent.fetch_add(1, Relaxed);
        // Clone the senders under the lock, send outside it — never hold the
        // registry mutex across a (non-blocking, but still) channel op.
        let txs: Vec<tokio::sync::mpsc::Sender<Vec<u8>>> = match inner.registry.lock() {
            Ok(r) => r.conns.iter().map(|c| c.tx.clone()).collect(),
            Err(_) => continue,
        };
        for tx in txs {
            let _ = tx.try_send(payload.clone());
        }
    }
}

/// Re-announce a hello to every connection from the render thread (geometry /
/// codec change). Strictly non-blocking (SD9): a per-connection `try_send`
/// that finds a full queue drops the re-announce for that viewer, which
/// resyncs on its own decode-error reconnect. The SD9 ride-out retry the
/// single-session path used is intentionally not applied here — it cannot be
/// summed across N connections without risking an N×-bounded render stall.
fn broadcast_hello(inner: &Inner, hello: pb::SessionHello) {
    let msg = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(hello)),
    };
    let mut framed = Vec::with_capacity(1 + msg.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = msg.encode(&mut framed);
    let Ok(reg) = inner.registry.lock() else { return };
    for c in &reg.conns {
        if c.tx.try_send(framed.clone()).is_err() {
            tracing::debug!(id = c.id, "hello re-announce skipped — viewer stalled or mid-teardown");
        }
    }
}

/// Build and send each connection its own per-recipient [`pb::Roster`]
/// (ADR-0086 SD1/SD8): every copy shares the connection list but carries its
/// own `you_id`/`you_role`. Called on every membership/role change.
fn broadcast_roster(inner: &Inner) {
    let Ok(reg) = inner.registry.lock() else { return };
    let entries: Vec<pb::RosterEntry> = reg
        .conns
        .iter()
        .map(|c| pb::RosterEntry {
            id: c.id,
            role: c.role as i32,
            label: c.label.clone(),
            webcodecs: c.webcodecs,
        })
        .collect();
    let active_id = reg.active_id.unwrap_or(0);
    let count = reg.conns.len() as u32;
    let max = reg.max as u32;
    for c in &reg.conns {
        let roster = pb::Roster {
            you_id: c.id,
            you_role: c.role as i32,
            active_id,
            count,
            max,
            connections: entries.clone(),
        };
        let msg = pb::SessionControl {
            control: Some(pb::session_control::Control::Roster(roster)),
        };
        let mut framed = Vec::with_capacity(1 + msg.encoded_len());
        framed.push(pb::PREFIX_SESSION);
        let _ = msg.encode(&mut framed);
        let _ = c.tx.try_send(framed); // drop-on-full (stalled viewer)
    }
}

/// Case-insensitive ASCII substring search over a request head.
fn contains_ci(haystack: &[u8], needle: &[u8]) -> bool {
    if needle.is_empty() || haystack.len() < needle.len() {
        return false;
    }
    haystack
        .windows(needle.len())
        .any(|w| w.eq_ignore_ascii_case(needle))
}

/// Decide whether the incoming request is a WebSocket handshake by
/// peeking (not consuming) the request head. Browsers send the whole
/// head in one segment; a few short re-peeks cover stragglers.
async fn sniff_websocket(stream: &tokio::net::TcpStream) -> bool {
    let mut buf = [0u8; 2048];
    for _ in 0..10 {
        match stream.peek(&mut buf).await {
            Ok(0) => return false,
            Ok(n) => {
                let head = buf.get(..n).unwrap_or_default();
                if contains_ci(head, b"upgrade: websocket") {
                    return true;
                }
                // Full head seen (or buffer exhausted) without an upgrade
                // header: it is a plain HTTP request.
                if n == buf.len() || head.windows(4).any(|w| w == b"\r\n\r\n") {
                    return false;
                }
            }
            Err(_) => return false,
        }
        tokio::time::sleep(std::time::Duration::from_millis(15)).await;
    }
    false
}

async fn serve_page(mut stream: tokio::net::TcpStream, page: &str) {
    use tokio::io::{AsyncReadExt as _, AsyncWriteExt as _};
    // Consume whatever fits of the request and answer unconditionally; a
    // single-page server has no routing worth parsing.
    let mut buf = [0u8; 4096];
    let _ = stream.read(&mut buf).await;
    // no-store: the page is embedded in the binary and tiny; a browser
    // heuristically caching yesterday's viewer against today's server is
    // the only thing caching could buy here.
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n{}",
        page.len(),
        page
    );
    let _ = stream.write_all(response.as_bytes()).await;
    let _ = stream.shutdown().await;
}

async fn accept_loop(
    listener: tokio::net::TcpListener,
    inner: std::sync::Arc<Inner>,
    page: std::sync::Arc<str>,
) {
    loop {
        let (stream, peer) = match listener.accept().await {
            Ok(x) => x,
            Err(e) => {
                tracing::error!(error=%e, "carrier accept failed");
                continue;
            }
        };
        let inner = inner.clone();
        let page = page.clone();
        tokio::spawn(async move {
            if !sniff_websocket(&stream).await {
                serve_page(stream, &page).await;
                return;
            }
            // Admission (capacity + role) happens inside handle_session under
            // the registry mutex, which is the synchronisation point — there is
            // no single-slot to race for anymore (ADR-0086 SD1).
            if let Err(e) = handle_session(stream, peer, inner).await {
                tracing::info!(%peer, error=%e, "viewer session ended with error");
            }
        });
    }
}

async fn handle_session(
    stream: tokio::net::TcpStream,
    peer: std::net::SocketAddr,
    inner: std::sync::Arc<Inner>,
) -> Result<(), tokio_tungstenite::tungstenite::Error> {
    let ws = tokio_tungstenite::accept_async(stream).await?;
    let (mut ws_tx, mut ws_rx) = ws.split();

    let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<u8>>(VIDEO_CHANNEL_CAP);

    // Admit into the registry. A full roster (MAX_CONNECTIONS) refuses the
    // connection — the loser is dropped (its viewer reconnects with backoff).
    let id = match inner.registry.lock().ok().and_then(|mut r| r.admit(tx)) {
        Some(id) => id,
        None => {
            tracing::info!(%peer, "rejecting connection — MAX_CONNECTIONS reached");
            return Ok(());
        }
    };
    // ≥ 1 connection now: the render thread renders pixels and runs the
    // shared encoder. (Idempotent — already true if others were present.)
    inner.connected.store(true, std::sync::atomic::Ordering::Release);
    let _ = inner.waker.send(());
    tracing::info!(%peer, id, "viewer connected");

    // First wire message: the session hello with current stream geometry
    // (SD6 0x03), sent directly so it precedes any queued video/roster — the
    // viewer needs it to size its canvas and configure its decoder.
    let current_hello = inner.hello.lock().map(|h| (*h).clone()).unwrap_or_default();
    let hello = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(current_hello)),
    };
    let mut framed = Vec::with_capacity(1 + hello.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = hello.encode(&mut framed);
    let hello_result = ws_tx
        .send(tokio_tungstenite::tungstenite::Message::Binary(framed.into()))
        .await;

    let result: Result<(), tokio_tungstenite::tungstenite::Error> = if let Err(e) = hello_result {
        Err(e)
    } else {
        // Announce the new arrival to everyone (this connection learns its own
        // id/role here, which drives its viewer ViewMode).
        broadcast_roster(&inner);
        // Liveness (see PING_INTERVAL/WRITE_TIMEOUT): bound writes and ping an
        // idle peer so an unresponsive connection is reaped, not leaked.
        let mut ping = tokio::time::interval(PING_INTERVAL);
        ping.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
        ping.tick().await; // the first interval tick fires immediately — drop it
        // Liveness state: set when we send a keepalive ping, cleared by any
        // inbound frame (the browser's automatic Pong, or real input). If a ping
        // goes a whole interval unanswered, the peer has stopped reading its
        // socket and is reaped.
        let mut awaiting_pong = false;
        loop {
            tokio::select! {
                out = rx.recv() => {
                    match out {
                        Some(payload) => {
                            match tokio::time::timeout(WRITE_TIMEOUT, ws_tx.send(tokio_tungstenite::tungstenite::Message::Binary(payload.into()))).await {
                                Ok(Ok(())) => {}
                                Ok(Err(e)) => break Err(e),
                                Err(_) => {
                                    tracing::info!(%peer, id, "viewer unresponsive (send timed out) — reaping connection");
                                    break Ok(());
                                }
                            }
                        }
                        None => break Ok(()), // all senders gone (only after registry removal)
                    }
                }
                msg = ws_rx.next() => {
                    awaiting_pong = false; // any inbound frame proves the peer is reading
                    match msg {
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Binary(data))) => {
                            handle_client_message(&data, &inner, id);
                        }
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Close(_))) | None => break Ok(()),
                        Some(Ok(_)) => {} // text/ping/pong — ignore
                        Some(Err(e)) => break Err(e),
                    }
                }
                _ = ping.tick() => {
                    if awaiting_pong {
                        // Our previous keepalive ping went a full interval
                        // unanswered — the peer has stopped reading its socket (a
                        // backgrounded or half-open tab). Reap it so it stops
                        // lingering in the roster and inflating the viewer count.
                        tracing::info!(%peer, id, "viewer unresponsive (keepalive unanswered) — reaping connection");
                        break Ok(());
                    }
                    awaiting_pong = true;
                    // A reading peer auto-replies with Pong (which clears the flag
                    // above); the write is still bounded in case the send buffer
                    // is already full against a non-draining peer.
                    match tokio::time::timeout(WRITE_TIMEOUT, ws_tx.send(tokio_tungstenite::tungstenite::Message::Ping(Vec::<u8>::new().into()))).await {
                        Ok(Ok(())) => {}
                        Ok(Err(_)) => break Ok(()),
                        Err(_) => {
                            tracing::info!(%peer, id, "viewer unresponsive (ping send timed out) — reaping connection");
                            break Ok(());
                        }
                    }
                }
            }
        }
    };

    // Remove from the registry on every exit path, auto-promoting a lone
    // survivor (SD2). Clear `connected` only when the last connection leaves,
    // then rebroadcast the roster and wake the host (reap the shared encoder
    // promptly if the session is now empty).
    let became_empty = match inner.registry.lock() {
        Ok(mut r) => {
            r.remove(id);
            r.conns.is_empty()
        }
        Err(_) => false,
    };
    if became_empty {
        inner.connected.store(false, std::sync::atomic::Ordering::Release);
    }
    broadcast_roster(&inner);
    let _ = inner.waker.send(());
    tracing::info!(%peer, id, "viewer disconnected");
    result
}

fn handle_client_message(data: &[u8], inner: &Inner, id: u64) {
    let Some((&prefix, payload)) = data.split_first() else {
        return;
    };
    // Input, resize, cadence, decode-caps and clipboard are honoured ONLY from
    // the active connection (ADR-0086 SD2/SD8) — a passive connection's are
    // dropped here at the server. ClientHello and TakeSession are accepted from
    // any connection.
    let active = inner
        .registry
        .lock()
        .map(|r| r.is_active(id))
        .unwrap_or(false);
    match prefix {
        pb::PREFIX_INPUT => {
            if !active {
                return;
            }
            match pb::InputEvent::decode(payload) {
                Ok(ev) => {
                    if let Some(ev) = ev.event {
                        if let Ok(mut events) = inner.events.lock() {
                            events.push(ev);
                        }
                        let _ = inner.waker.send(()); // input wants a pass now
                    }
                }
                Err(e) => tracing::debug!(error=%e, "undecodable input event"),
            }
        }
        pb::PREFIX_SESSION => match pb::SessionControl::decode(payload) {
            Ok(ctl) => match ctl.control {
                Some(pb::session_control::Control::ViewportResize(r)) => {
                    if active {
                        if let Ok(mut resize) = inner.resize.lock() {
                            *resize = Some(r);
                        }
                        let _ = inner.waker.send(());
                    }
                }
                Some(pb::session_control::Control::SetCadence(c)) => {
                    if active {
                        if let Ok(mut pending) = inner.cadence_request.lock() {
                            *pending = Some(c.cadence);
                        }
                        let _ = inner.waker.send(());
                    }
                }
                Some(pb::session_control::Control::Ping(p)) => {
                    // The active viewer pings with its decoded-frame count: a
                    // remote attestation that WebCodecs decode is working.
                    if active {
                        inner.frames_decoded.store(p.nonce, std::sync::atomic::Ordering::Relaxed);
                        tracing::debug!(frames_decoded = p.nonce, "active viewer decode-progress ping");
                    }
                }
                Some(pb::session_control::Control::DecodeCapabilities(caps)) => {
                    if active {
                        tracing::info!(
                            codecs = ?caps.codecs.iter()
                                .map(|c| format!("{}:s{}m{}p{}", c.codec, c.supported as u8, c.smooth as u8, c.power_efficient as u8))
                                .collect::<Vec<_>>(),
                            "active viewer decode capabilities"
                        );
                        if let Ok(mut guard) = inner.decode_caps.lock() {
                            *guard = Some(caps);
                        }
                        let _ = inner.waker.send(());
                    }
                }
                Some(pb::session_control::Control::ClientHello(h)) => {
                    // Caps + label arrive shortly after connect; update and
                    // rebroadcast so the roster shows the device label and
                    // takeover-capability.
                    if let Ok(mut r) = inner.registry.lock() {
                        r.set_caps(id, h.webcodecs, h.label);
                    }
                    broadcast_roster(inner);
                }
                Some(pb::session_control::Control::TakeSession(_)) => {
                    // Honoured only from a WebCodecs-capable connection (SD2).
                    let changed = match inner.registry.lock() {
                        Ok(mut r) => {
                            if r.find(id).map(|c| c.webcodecs).unwrap_or(false) {
                                r.take_session(id)
                            } else {
                                tracing::debug!(id, "TakeSession ignored — connection not WebCodecs-capable");
                                false
                            }
                        }
                        Err(_) => false,
                    };
                    if changed {
                        tracing::info!(id, "session taken — new active connection");
                        broadcast_roster(inner);
                        // The new active owns geometry: its viewer re-reports
                        // ViewportResize, which (now active) rebuilds the stream
                        // if its geometry differs (SD5). Wake to apply promptly.
                        let _ = inner.waker.send(());
                    }
                }
                Some(pb::session_control::Control::Clipboard(c)) => {
                    // Viewer→host paste (active-only, ADR-0082 SD6).
                    if active {
                        if let Ok(mut p) = inner.paste.lock() {
                            *p = Some(c.text);
                        }
                        let _ = inner.waker.send(());
                    }
                }
                // Server→client only; ignore if a client echoes one.
                Some(pb::session_control::Control::Hello(_))
                | Some(pb::session_control::Control::Roster(_))
                | None => {}
            },
            Err(e) => tracing::debug!(error=%e, "undecodable session control"),
        },
        other => tracing::debug!(prefix = other, "unknown message prefix from viewer"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mk_tx() -> tokio::sync::mpsc::Sender<Vec<u8>> {
        tokio::sync::mpsc::channel::<Vec<u8>>(1).0
    }

    /// First admit is active; the rest are passive (ADR-0086 SD2).
    #[test]
    fn first_admit_active_rest_passive() {
        let mut r = Registry::new(8);
        let a = r.admit(mk_tx()).expect("first admitted");
        let b = r.admit(mk_tx()).expect("second admitted");
        assert_eq!(r.find(a).unwrap().role, pb::Role::Active);
        assert_eq!(r.find(b).unwrap().role, pb::Role::Passive);
        assert_eq!(r.active_id, Some(a));
    }

    /// MAX_CONNECTIONS refuses the surplus connection.
    #[test]
    fn admit_refuses_past_max() {
        let mut r = Registry::new(2);
        assert!(r.admit(mk_tx()).is_some());
        assert!(r.admit(mk_tx()).is_some());
        assert!(r.admit(mk_tx()).is_none(), "third refused at max=2");
    }

    /// Takeover promotes the requester and demotes the prior active (SD2).
    #[test]
    fn take_session_promotes_and_demotes() {
        let mut r = Registry::new(8);
        let a = r.admit(mk_tx()).unwrap();
        let b = r.admit(mk_tx()).unwrap();
        assert!(r.take_session(b), "takeover changes state");
        assert_eq!(r.find(b).unwrap().role, pb::Role::Active);
        assert_eq!(r.find(a).unwrap().role, pb::Role::Passive);
        assert_eq!(r.active_id, Some(b));
        assert!(!r.take_session(b), "no-op when already active");
    }

    /// When the active disconnects and exactly one passive remains, it
    /// auto-promotes (SD2 — lone-passive return); with several, the slot
    /// stays empty.
    #[test]
    fn lone_passive_auto_promotes_else_slot_empty() {
        let mut r = Registry::new(8);
        let a = r.admit(mk_tx()).unwrap();
        let b = r.admit(mk_tx()).unwrap();
        let c = r.admit(mk_tx()).unwrap();
        r.remove(a); // two passives remain → slot stays empty
        assert_eq!(r.active_id, None);
        assert_eq!(r.find(b).unwrap().role, pb::Role::Passive);
        r.remove(c); // now a lone passive remains → it auto-promotes
        assert_eq!(r.active_id, Some(b));
        assert_eq!(r.find(b).unwrap().role, pb::Role::Active);
    }

    /// Removing a passive leaves the active untouched.
    #[test]
    fn removing_passive_keeps_active() {
        let mut r = Registry::new(8);
        let a = r.admit(mk_tx()).unwrap();
        let b = r.admit(mk_tx()).unwrap();
        r.remove(b);
        assert_eq!(r.active_id, Some(a));
        assert_eq!(r.find(a).unwrap().role, pb::Role::Active);
    }
}
