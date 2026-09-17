//! WebSocket carrier (ADR-0024 SD4/SD6; ADR-0086 active/passive roster).
//!
//! Two TCP listeners (`IMZERO2_HEADLESS_LISTEN` and port+1, kept for URL
//! compatibility) run an identical dispatcher: a request that carries a
//! WebSocket upgrade header becomes a data channel; anything else is
//! answered with the embedded single-file viewer page. Sniffing uses
//! `TcpStream::peek`, so the stream reaches the WebSocket handshake
//! unconsumed. Either port therefore works for both the page and the
//! socket — the viewer connects back to a *page-relative* `ws` (same origin,
//! and under whatever path prefix the page itself was served on), which makes
//! a single TLS-terminating reverse proxy in front (or an SSH tunnel), even
//! one that mounts the viewer under a path prefix, sufficient for the whole
//! wire. Nothing here parses the request path: the page is served for any
//! path and the handshake is accepted on any path, so a prefix mount needs no
//! carrier configuration — the client carries the prefix, both listeners
//! answer regardless of it.
//!
//! A single binary WebSocket carries everything with a one-byte type
//! prefix (SD6): 0x01 video chunks server→client, 0x02 protobuf input
//! events client→server, 0x03 session control both ways, 0x04 draw-stream
//! lane messages server→client (ADR-0128 — mesh frames + texture updates,
//! active when the session codec is `mesh`; the shared encoder never runs).
//!
//! Session model (ADR-0086): connections are first-class — a [`Registry`]
//! holds each [`Conn`] with its role (`active` | `passive`) and a
//! per-connection outbound queue. The invariant is **≤ 1 active**; the
//! first connection is admitted active and the rest passive (read-only).
//! Every membership/role change rebroadcasts a per-recipient [`pb::Roster`]
//! (each connection's copy carries its own `you_id`/`you_role`, which folds
//! SD8's `RoleChanged` into the roster). Input, resize, cadence and clipboard
//! injection are honoured **only** from the active connection; passive
//! connections are dropped at the server. Takeover (`TakeSession`) is
//! unilateral (one principal, ADR-0082): it promotes the requester and
//! demotes the prior active; a slot freed by a disconnect auto-promotes a
//! lone passive.
//!
//! Two lifetime guarantees separate authoritative session state from
//! disposable frame traffic (ADR-0242 SD1/SD2). The roster rides a
//! per-connection **latest-value mailbox** rather than that connection's
//! bounded payload queue, so a stalled viewer's full queue can no longer
//! drop the role change that tells it what it is; the writer prefers the
//! mailbox over payload and keeps the existing write timeout, so the state
//! is either delivered or the peer is reaped. And input ownership carries an
//! **owner epoch** in the registry itself: the authority check, the epoch
//! stamp and the enqueue happen under one lock, and a change of owner
//! discards what the previous one queued and makes the next drain ask the
//! host to cancel held keys, buttons and modifiers first.
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

use crate::imzero2::codeclane::{CodecLane, VideoCodec};
use crate::imzero2::encoderpipe::{EncodedFrame, EncoderSink, EncoderTarget, advance_generation};
use crate::imzero2::inputproto as pb;
use crate::imzero2::meshlane;
use futures_util::{SinkExt as _, StreamExt as _};
use prost::Message as _;

/// Bounded video and transient-control queues. Roster/hello use latest-state
/// mailboxes; mesh delivery has one queued frame batch (ADR-0242).
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
const JOIN_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(30);

#[derive(Clone)]
struct StreamHello {
    generation: u64,
    payload: Vec<u8>,
}

struct MeshBatch {
    generation: u64,
    messages: std::collections::VecDeque<Vec<u8>>,
}

impl MeshBatch {
    fn next(&mut self, generation: u64) -> Option<Vec<u8>> {
        if self.generation != generation {
            self.messages.clear();
        }
        self.messages.pop_front()
    }
}

/// One drained item of remote input, in arrival order (ADR-0242 SD2). Paste
/// shares the queue with the events rather than sitting in its own
/// latest-wins cell, so a focus change that preceded it is honoured instead
/// of the text landing on a surface the viewer has already left.
// No `Eq`: the wire events carry `f32` coordinates, so prost derives only
// `PartialEq` for them.
#[derive(Debug, Clone, PartialEq)]
pub enum InputItem {
    /// A wire input event from the connection that owned input when it
    /// arrived.
    Event(pb::input_event::Event),
    /// A clipboard paste from that owner (ADR-0082 SD6).
    Paste(String),
}

/// Sentinel for [`Inner::cursor_sent`] meaning "no shape has been delivered to
/// the current active connection" — not a valid wire code, so the next pass
/// always sends. Distinct from code 0 (`default`), which *is* a real shape and
/// must still be transmitted when the pointer returns to plain hover.
const CURSOR_UNSENT: u32 = u32::MAX;

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
    /// Latest-value mailbox for this connection's authoritative session state
    /// — the per-recipient roster today — independent of the payload queue
    /// above (ADR-0242 SD1). A `watch` channel, so a change that lands while
    /// the writer is mid-send **replaces** the pending copy instead of
    /// queueing behind it or being dropped: the peer converges on the newest
    /// state, and two changes in one tick coalesce into one write.
    state: tokio::sync::watch::Sender<Option<Vec<u8>>>,
    stream: tokio::sync::watch::Sender<Option<StreamHello>>,
    video_tx: tokio::sync::mpsc::Sender<EncodedFrame>,
    video_rx: Option<tokio::sync::mpsc::Receiver<EncodedFrame>>,
    mesh_tx: tokio::sync::mpsc::Sender<MeshBatch>,
    mesh_rx: Option<tokio::sync::mpsc::Receiver<MeshBatch>>,
    awaiting_keyframe: bool,
    video_ready: bool,
    video_failed: bool,
    joining_since: std::time::Instant,
    /// Bodies retained by the last accepted mesh frame (ADR-0242 SD5).
    mesh_sent: std::collections::HashSet<u64>,
    /// Needs the mesh bootstrap (full texture store + all-bodies frame):
    /// set on admit and again whenever a queued mesh send is dropped — a
    /// content-addressed stream has no retransmit, so the recovery from any
    /// gap is a full resync (the cheap analogue of the video lane's
    /// decode-error reconnect).
    mesh_fresh: bool,
}

/// The set of live connections and the single active slot (ADR-0086 SD1).
/// Guarded by [`Inner::registry`]; mutated only from the carrier's async
/// session tasks and read briefly by the render thread (active lookup).
struct Registry {
    conns: Vec<Conn>,
    /// `Some(id)` of the active connection, or `None` when the slot is empty
    /// (several passives present, none promoted — each keeps its button).
    /// Written only by [`Registry::set_active`], which keeps the input-owner
    /// epoch below in step with it.
    active_id: Option<u64>,
    next_id: u64,
    max: usize,
    /// Input-owner epoch (ADR-0242 SD2): advanced by every change of
    /// `active_id` — admission, disconnect, auto-promotion, takeover — and
    /// stamped on each accepted input item, so an item queued by a departed
    /// owner cannot be consumed under the next one. Living here rather than
    /// beside the queue is the point: ownership, the authority check, the
    /// stamp and the enqueue are then all decided under one mutex.
    owner_epoch: u64,
    /// Input accepted from the owner, in arrival order, each item stamped
    /// with the epoch that owned input when it arrived. Drained by the render
    /// thread ([`WsCarrier::drain_input`]).
    input: Vec<(u64, InputItem)>,
    /// Latched by every owner change, cleared by the drain that reports it:
    /// the host cancels held keys, buttons and modifiers *before* consuming
    /// anything the new owner sent. Sticky, so a change that lands between
    /// two render passes is never missed.
    owner_reset: bool,
}

impl Registry {
    fn new(max: usize) -> Self {
        Self {
            conns: Vec::new(),
            active_id: None,
            next_id: 1,
            max,
            owner_epoch: 0,
            input: Vec::new(),
            owner_reset: false,
        }
    }

    /// The single writer of `active_id` (ADR-0242 SD2). A real change advances
    /// the owner epoch, drops the input the previous owner had queued, and
    /// latches the reset the next drain reports. This also covers the change
    /// to `None`: a disconnecting owner's held press is cancelled here,
    /// server-side, rather than depending on a departing browser to deliver an
    /// unload message.
    fn set_active(&mut self, next: Option<u64>) {
        if self.active_id == next {
            return;
        }
        self.active_id = next;
        self.owner_epoch = self.owner_epoch.wrapping_add(1);
        self.input.clear();
        self.owner_reset = true;
    }

    /// Accept one wire input event from `id`, iff `id` owns input right now
    /// (ADR-0086 SD2 — a passive connection's input is dropped at the server).
    /// The check and the stamp share this critical section: the check-then-push
    /// pair they replaced could straddle a takeover and file a departed owner's
    /// event under the new owner's epoch. Returns whether it was accepted, so
    /// the caller only wakes the render thread for input that will be consumed.
    fn accept_input(&mut self, id: u64, ev: pb::input_event::Event) -> bool {
        self.accept(id, InputItem::Event(ev))
    }

    /// As [`Registry::accept_input`], for a viewer→host paste (ADR-0082 SD6),
    /// which is ordered with the events around it rather than held in its own
    /// latest-wins cell.
    fn accept_paste(&mut self, id: u64, text: String) -> bool {
        self.accept(id, InputItem::Paste(text))
    }

    fn accept(&mut self, id: u64, item: InputItem) -> bool {
        if !self.is_active(id) {
            return false;
        }
        self.input.push((self.owner_epoch, item));
        true
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
            pb::Role::Active
        } else {
            pb::Role::Passive
        };
        let (video_tx, video_rx) = tokio::sync::mpsc::channel(VIDEO_CHANNEL_CAP);
        let (mesh_tx, mesh_rx) = tokio::sync::mpsc::channel(1);
        self.conns.push(Conn {
            id,
            role,
            webcodecs: false,
            label: String::new(),
            tx,
            state: tokio::sync::watch::channel(None).0,
            stream: tokio::sync::watch::channel(None).0,
            video_tx,
            video_rx: Some(video_rx),
            mesh_tx,
            mesh_rx: Some(mesh_rx),
            awaiting_keyframe: true,
            video_ready: false,
            video_failed: false,
            joining_since: std::time::Instant::now(),
            mesh_sent: std::collections::HashSet::new(),
            mesh_fresh: true,
        });
        if role == pb::Role::Active {
            self.set_active(Some(id));
        }
        Some(id)
    }

    /// Remove a connection. Whenever this leaves the active slot empty and a
    /// **lone** connection behind, that one auto-promotes (SD2 — zero-friction
    /// return to your other device); with several remaining the slot stays
    /// empty (each keeps its button). This covers both removing the active and
    /// removing a passive that leaves a single connection — so the last
    /// remaining viewer always drives.
    /// Removing a connection that is **not** the owner, and does not leave a
    /// lone survivor to promote, leaves the epoch alone — so unrelated
    /// membership churn never cancels the owner's in-progress drag.
    fn remove(&mut self, id: u64) {
        self.conns.retain(|c| c.id != id);
        if self.active_id == Some(id) {
            self.set_active(None);
        }
        if self.active_id.is_none()
            && let [only] = self.conns.as_mut_slice()
        {
            only.role = pb::Role::Active;
            let promoted = only.id;
            self.set_active(Some(promoted));
        }
    }

    /// Make `id` the active connection and demote the prior active to passive
    /// (SD2 — unilateral takeover). Returns whether anything changed.
    fn take_session(&mut self, id: u64) -> bool {
        if self.active_id == Some(id) || self.find(id).is_none() {
            return false;
        }
        for c in &mut self.conns {
            if c.id == id {
                c.role = pb::Role::Active;
            } else if c.role == pb::Role::Active {
                c.role = pb::Role::Passive;
            }
        }
        self.set_active(Some(id));
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
        self.active_id.and_then(|id| self.find(id)).map(|c| c.tx.clone())
    }
}

struct Inner {
    /// Latest viewport-resize from the active connection; drained by the
    /// render thread, which applies it (target rebuild + hello re-announce +
    /// encoder restart) and answers with a fresh [`pb::SessionHello`].
    resize: std::sync::Mutex<Option<pb::ViewportResize>>,
    /// Latest runtime cadence request (0 continuous / 1 reactive) from the
    /// active connection, drained by the render thread.
    cadence_request: std::sync::Mutex<Option<u32>>,
    /// Wakes the render thread out of its reactive sleep when anything
    /// arrives that wants a pass soon: input, resize, cadence change,
    /// connect/disconnect, takeover, paste. Sends are fire-and-forget.
    waker: std::sync::mpsc::Sender<()>,
    /// True while ≥ 1 connection is present — the render thread checks this
    /// cheaply each frame to decide whether to render pixels and run the
    /// shared encoder. Mirrors `registry.conns.is_empty()` negated.
    connected: std::sync::atomic::AtomicBool,
    /// True while a passive viewer is present (≥ 2 connections — the ≤1-active
    /// invariant + lone-survivor auto-promote make a single connection always
    /// the active one). Drives the conditional GOP (ADR-0086 SD10 Update): the
    /// render thread runs the shared encoder periodic-IDR while this holds and
    /// effectively-infinite (pulse-free) otherwise. Set in `broadcast_roster`,
    /// read each frame by `on_frame`.
    want_periodic: std::sync::atomic::AtomicBool,
    generation: std::sync::Arc<std::sync::atomic::AtomicU64>,
    join_pending: std::sync::atomic::AtomicBool,
    join_failed: std::sync::atomic::AtomicBool,
    /// First-class connections + the single active slot (ADR-0086 SD1).
    registry: std::sync::Mutex<Registry>,
    /// Current stream geometry; sent on connect and after each applied
    /// resize. Updated by the render thread via [`WsCarrier::apply_geometry`].
    hello: std::sync::Mutex<pb::SessionHello>,
    /// Latest decode capabilities reported by the active connection (ADR-0088
    /// SD2/SD8), drained by the render thread to forward to the Go interpreter.
    decode_caps: std::sync::Mutex<Option<pb::DecodeCapabilities>>,
    /// Cursor shape last *delivered* to the active connection, or
    /// [`CURSOR_UNSENT`] (ADR-0024 Update 2026-07-28). The render thread reads
    /// egui's cursor icon every pass and would otherwise re-send an unchanged
    /// shape 30–60×/s, so this dedupes — the same trick `egui-winit` plays with
    /// its `current_cursor_icon`. Lives on `Inner` rather than on `WsCarrier`
    /// because `broadcast_roster` (socket task, not render thread) has to clear
    /// it: after a takeover the new active has never received the current shape,
    /// and a memo saying "already sent" would strand it on a stale cursor.
    cursor_sent: std::sync::atomic::AtomicU32,
    /// Wire telemetry (ADR-0088): bytes + frames the shared encoder produced
    /// (counted once per access unit in [`distribute`], not multiplied by the
    /// viewer count — it is the stream bitrate, not aggregate egress), and the
    /// active viewer's latest decoded-frame count (from its progress pings).
    bytes_sent: std::sync::atomic::AtomicU64,
    frames_sent: std::sync::atomic::AtomicU64,
    frames_decoded: std::sync::atomic::AtomicU64,
    /// ADR-0154 SD1: the active connection has asked for an accessibility tree
    /// and has not been served yet. Set by the socket task, cleared by the
    /// render thread once it has a snapshot to send. A flag rather than a
    /// queue: two requests before one pass are one answer, and the answer is
    /// always "the tree as of the next completed pass".
    tree_wanted: std::sync::atomic::AtomicBool,
    /// ADR-0154 SD4: capture name the active connection asked for, drained by
    /// the render thread. Latest-wins for the same reason `resize` is: a second
    /// request before the first is served asks about a newer frame.
    capture_request: std::sync::Mutex<Option<String>>,
}

pub struct WsCarrier {
    inner: std::sync::Arc<Inner>,
    /// The single shared encoder (ADR-0086 SD5): present while ≥ 1 connection,
    /// `None` otherwise. Geometry/codec changes drop it for a restart.
    encoder: Option<EncoderSink>,
    /// Stable input to the [`distribute`] fan-out task; every (re)spawned
    /// encoder targets a clone of this, so the distributor outlives any single
    /// encoder generation.
    encoder_tx: tokio::sync::mpsc::Sender<EncodedFrame>,
    encoder_generation: u64,
    /// GOP mode of the current encoder (true = periodic IDR). Tracked so the
    /// render thread restarts the encoder when passive presence flips
    /// (`Inner::want_periodic`) — the conditional GOP, ADR-0086 SD10 Update.
    encoder_periodic: bool,
    fps: f32,
    lane: CodecLane,
    /// blake3 of the last frame fed to the encoder; identical frames are
    /// skipped (no encode, no wire bytes). Reset whenever the encoder is
    /// (re)spawned so a fresh stream always begins with a real frame.
    last_frame_hash: Option<blake3::Hash>,
    /// Draw-stream lane state (ADR-0128). The texture store accumulates
    /// every frame regardless of the active codec, so a runtime switch to
    /// the mesh lane finds the complete atlas/image state for bootstrap.
    mesh_textures: meshlane::TextureStore,
    /// Incremental texture messages produced since the last mesh broadcast
    /// (kept only while the mesh lane is active).
    mesh_pending_tex: Vec<Vec<u8>>,
    /// blake3 over the last broadcast frame's hash list — the mesh analogue
    /// of `last_frame_hash`: an unchanged frame with no pending textures and
    /// no fresh connection sends nothing at all.
    last_mesh_sig: Option<blake3::Hash>,
}

impl WsCarrier {
    /// Bind `listen` (e.g. "127.0.0.1:8089") for WebSocket and `port+1`
    /// for the viewer page, then run both — plus the broadcast distributor —
    /// on a dedicated tokio thread. `waker` is signalled whenever wire
    /// activity wants a render pass soon.
    // The carrier's whole launch configuration, given once at start.
    #[allow(clippy::too_many_arguments)]
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
        let (encoder_tx, encoder_rx) =
            tokio::sync::mpsc::channel::<EncodedFrame>(VIDEO_CHANNEL_CAP);
        let inner = std::sync::Arc::new(Inner {
            resize: std::sync::Mutex::new(None),
            cadence_request: std::sync::Mutex::new(None),
            waker,
            connected: std::sync::atomic::AtomicBool::new(false),
            want_periodic: std::sync::atomic::AtomicBool::new(false),
            generation: std::sync::Arc::new(std::sync::atomic::AtomicU64::new(0)),
            join_pending: std::sync::atomic::AtomicBool::new(false),
            join_failed: std::sync::atomic::AtomicBool::new(false),
            registry: std::sync::Mutex::new(Registry::new(max)),
            hello: std::sync::Mutex::new(pb::SessionHello {
                width_px,
                height_px,
                pixels_per_point,
                cadence,
                codec: lane.webcodecs_codec_string(width_px, height_px),
            }),
            decode_caps: std::sync::Mutex::new(None),
            cursor_sent: std::sync::atomic::AtomicU32::new(CURSOR_UNSENT),
            bytes_sent: std::sync::atomic::AtomicU64::new(0),
            frames_sent: std::sync::atomic::AtomicU64::new(0),
            frames_decoded: std::sync::atomic::AtomicU64::new(0),
            tree_wanted: std::sync::atomic::AtomicBool::new(false),
            capture_request: std::sync::Mutex::new(None),
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
        std::thread::Builder::new().name("imzero2-ws-carrier".to_owned()).spawn(move || {
            let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                Ok(rt) => rt,
                Err(e) => {
                    tracing::error!(error=%e, "carrier tokio runtime failed to build");
                    return;
                }
            };
            rt.block_on(async move {
                let page: std::sync::Arc<str> =
                    std::sync::Arc::from(include_str!("viewer/index.html"));
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
            encoder_generation: 0,
            encoder_periodic: false,
            fps,
            lane,
            last_frame_hash: None,
            mesh_textures: meshlane::TextureStore::default(),
            mesh_pending_tex: Vec::new(),
            last_mesh_sig: None,
        })
    }

    /// True when the session codec is the draw-stream lane (ADR-0128).
    pub fn is_mesh(&self) -> bool {
        self.lane.codec == VideoCodec::Mesh
    }

    /// True when the host must rasterize pixels for this carrier — every
    /// codec except the mesh lane, which consumes tessellation output only.
    pub fn wants_pixels(&self) -> bool {
        !self.is_mesh()
    }

    /// Mirror this frame's texture deltas into the lane's CPU store. Called
    /// every frame regardless of codec (cheap when the delta is empty);
    /// incremental messages are queued for broadcast only while the mesh
    /// lane is active.
    pub fn ingest_textures(&mut self, delta: &egui::TexturesDelta) {
        let msgs = self.mesh_textures.ingest(delta);
        if self.is_mesh() {
            self.mesh_pending_tex.extend(msgs);
        }
    }

    pub fn finish_texture_frame(&mut self) {
        self.mesh_textures.finish_frame();
    }

    /// Idle video joins temporarily need the configured cadence, not a heartbeat.
    pub fn needs_join_progress(&self) -> bool {
        !self.is_mesh() && self.inner.join_pending.load(std::sync::atomic::Ordering::Acquire)
    }

    /// True while ≥ 1 viewer is connected — the host skips rendering pixels
    /// entirely when nothing consumes them.
    pub fn connected(&self) -> bool {
        self.inner.connected.load(std::sync::atomic::Ordering::Acquire)
    }

    /// Latest pending runtime cadence request from the active connection.
    pub fn take_cadence(&mut self) -> Option<u32> {
        let mut c = self.inner.cadence_request.lock().ok()?;
        c.take()
    }

    /// Record the applied cadence so future hellos report it.
    pub fn set_hello_cadence(&mut self, cadence: u32) {
        if let Ok(mut hello) = self.inner.hello.lock() {
            hello.cadence = cadence;
        }
    }

    /// Drain the input owner's pending items into `out`, in arrival order, and
    /// report whether **input ownership changed** since the previous drain
    /// (ADR-0242 SD2).
    ///
    /// The contract the host relies on, in order:
    ///
    /// 1. A `true` return means the caller must cancel held keys, buttons and
    ///    modifiers *before* consuming `out` — a press that crossed the wire
    ///    under the previous owner must not complete as a click under the next.
    /// 2. Everything in `out` then belongs to the **current** owner: items
    ///    stamped with a superseded epoch are discarded here rather than
    ///    translated. (They are also cleared when the epoch advances; this is
    ///    the consumption-side half of the same check, so no queue that
    ///    outlives an owner change can revive a held press after the reset.)
    /// 3. Paste keeps its place in that order instead of being appended after
    ///    the events, so a focus change that preceded it is honoured.
    ///
    /// The epoch read, the reset take and the move into `out` are one critical
    /// section, so a takeover cannot land between them; the registry lock is
    /// held for that move and nothing else.
    pub fn drain_input(&mut self, out: &mut Vec<InputItem>) -> bool {
        drain_input(&self.inner, out)
    }

    /// Latest pending viewport-resize from the active connection (latest wins).
    pub fn take_resize(&mut self) -> Option<pb::ViewportResize> {
        let mut r = self.inner.resize.lock().ok()?;
        r.take()
    }

    /// Send host-copied text to the **active** connection's clipboard
    /// (ADR-0082 SD6 — only the active session syncs). Non-blocking on the
    /// render thread (SD9): a full/stalled active queue drops the copy.
    pub fn send_clipboard_to_active(&mut self, text: String) {
        let tx = self.inner.registry.lock().ok().and_then(|r| r.active_tx());
        if let Some(tx) = tx {
            let msg = pb::SessionControl {
                control: Some(pb::session_control::Control::Clipboard(pb::ClipboardData {
                    text,
                })),
            };
            let mut framed = Vec::with_capacity(1 + msg.encoded_len());
            framed.push(pb::PREFIX_SESSION);
            let _ = msg.encode(&mut framed);
            if tx.try_send(framed).is_err() {
                tracing::debug!("clipboard copy dropped — active viewer queue full or gone");
            }
        }
    }

    /// Push this pass's mouse cursor shape to the **active** connection
    /// (ADR-0024 Update 2026-07-28), so its canvas can set the matching CSS
    /// `cursor`. Called every pass with `inputmap::cursor_shape_code(…)`;
    /// unchanged shapes cost nothing beyond an atomic load.
    ///
    /// Active-only for the same reason input is: the shape egui resolved comes
    /// from the active pointer's position, so a passive viewer would get its
    /// local cursor morphed by someone else's hover.
    ///
    /// Non-blocking (SD9). A drop here is *not* like a dropped clipboard copy,
    /// which merely loses one copy: with a naive memo the host would believe
    /// the shape had landed and never resend, stranding the viewer on a stale
    /// cursor indefinitely. So the memo advances only on a successful enqueue —
    /// a dropped update is simply retried next pass.
    pub fn send_cursor_to_active(&self, shape: u32) {
        push_cursor(&self.inner, shape);
    }

    /// ADR-0154 SD1: has the active connection asked for a tree it has not been
    /// served yet? The render thread checks this *before* running the pass, so
    /// it can turn AccessKit generation on for that pass — the tree is built
    /// during the pass or not at all.
    pub fn tree_wanted(&self) -> bool {
        self.inner.tree_wanted.load(std::sync::atomic::Ordering::Relaxed)
    }

    /// Send one tree snapshot to the active connection and clear the request.
    ///
    /// Unlike the cursor there is no memo to strand: an unserved request stays
    /// set, so a drop here is retried on the next pass rather than answered
    /// with silence. Callers only reach this when [`Self::tree_wanted`] held.
    pub fn send_tree_to_active(&self, snapshot: pb::TreeSnapshot) {
        let nodes = snapshot.nodes.len();
        let msg = pb::SessionControl {
            control: Some(pb::session_control::Control::TreeSnapshot(snapshot)),
        };
        if send_control_to_active(&self.inner, &msg) {
            self.inner.tree_wanted.store(false, std::sync::atomic::Ordering::Relaxed);
            tracing::debug!(nodes, "accessibility tree sent to active connection");
        } else {
            tracing::debug!(nodes, "tree snapshot dropped — will retry next pass");
        }
    }

    /// Take the pending capture name, if the active connection asked for one
    /// (ADR-0154 SD4). Draining rather than peeking: one request, one capture.
    pub fn take_capture_request(&self) -> Option<String> {
        self.inner.capture_request.lock().ok()?.take()
    }

    /// Acknowledge a capture with the path actually written.
    pub fn send_capture_done(&self, done: pb::CaptureDone) {
        let msg = pb::SessionControl {
            control: Some(pb::session_control::Control::CaptureDone(done)),
        };
        if !send_control_to_active(&self.inner, &msg) {
            tracing::debug!("capture ack dropped — active viewer queue full");
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
        // Stop the producer, then retire its generation. Already queued old
        // payloads are filtered rather than reinterpreted under the new hello.
        if self.encoder.take().is_some() {
            tracing::info!(
                width_px,
                height_px,
                pixels_per_point,
                "geometry change — shared encoder stopped for restart"
            );
        }
        self.last_frame_hash = None;
        advance_generation(&self.inner.generation);
        self.last_mesh_sig = None;
        broadcast_hello(&self.inner, hello);
    }

    /// The lane currently served — after a degradation this is the mesh lane
    /// regardless of what was requested (reported to the Go control as the
    /// ninth `fetchVideoStreamInfo` value).
    pub fn video_codec(&self) -> crate::imzero2::codeclane::VideoCodec {
        self.lane.codec
    }

    /// Leave a video lane whose encoder cannot run for the encoderless mesh
    /// draw-stream lane (ADR-0206 SD1 promises this degradation; ADR-0128).
    /// Detected once — the spawn failed, or the supervisor gave up — logged
    /// once, and from then on the host serves meshes; viewers reconnect on the
    /// announced codec-class change. Losing the requested codec beats a
    /// silently dead stream.
    fn degrade_to_mesh(&mut self, why: &str) {
        if self.is_mesh() {
            return;
        }
        tracing::warn!(
            from = ?self.lane.codec,
            why,
            ffmpeg = %crate::imzero2::codeclane::ffmpeg_bin(),
            "video lane unusable — degrading to the mesh draw-stream lane"
        );
        self.set_video_codec(crate::imzero2::codeclane::VideoCodec::Mesh);
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
        self.encoder.take(); // stop the old producer before retiring its generation
        advance_generation(&self.inner.generation);
        self.last_frame_hash = None;
        // Mesh-lane per-session state restarts with the codec switch; viewers
        // reconnect on the announced codec-class change (canvas contexts are
        // single-kind), arriving fresh.
        self.last_mesh_sig = None;
        self.mesh_pending_tex.clear();
        let hello = self.inner.hello.lock().map(|h| (*h).clone()).unwrap_or_default();
        broadcast_hello(&self.inner, hello);
        tracing::info!(
            codec = self.lane.codec.as_str(),
            "video codec switched at runtime"
        );
    }

    /// ADR-0088: a clone of the active connection's latest reported decode caps.
    pub fn decode_caps(&self) -> Option<pb::DecodeCapabilities> {
        let g = self.inner.decode_caps.lock().ok()?;
        g.clone()
    }

    /// ADR-0088 wire telemetry for the Go control: (`bytes_sent`, `frames_sent`,
    /// `frames_decoded`, `frames_dropped`). Cumulative for the current session
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
        use std::sync::atomic::Ordering::{Acquire, Relaxed};
        // The mesh lane never encodes (ADR-0128): pixels reaching here under
        // it are the host's empty reap call; session state is handled by
        // `on_meshes` / `mesh_reap`.
        if self.is_mesh() {
            return;
        }
        if self.inner.join_failed.swap(false, Relaxed) {
            self.degrade_to_mesh("encoder produced no joinable keyframe before the join deadline");
            return;
        }
        let connected = self.inner.connected.load(Acquire);
        if !connected {
            if self.encoder.take().is_some() {
                tracing::info!("all viewers disconnected — stopping shared encoder");
                self.last_frame_hash = None;
                advance_generation(&self.inner.generation);
                // Telemetry is per-session: clear it so the next 0→1 spawn
                // starts a fresh byte/frame count (the EMA bitrate the Go
                // control reads). Done here, not on every (re)spawn, so a
                // mid-session GOP restart does not zero the running counts.
                self.inner.bytes_sent.store(0, Relaxed);
                self.inner.frames_sent.store(0, Relaxed);
                self.inner.frames_decoded.store(0, Relaxed);
            }
            return;
        }
        // Conditional GOP (ADR-0086 SD10 Update): periodic IDR while a passive
        // viewer is present (it needs key frames to join), else an
        // effectively-infinite, pulse-free GOP for the lone active viewer. A
        // change in passive presence restarts the shared encoder — a one-off
        // fresh IDR at the transition is the only pulse the active sees when not
        // serving a passive.
        let want_periodic = self.inner.want_periodic.load(Acquire);
        if self.encoder.is_some() && self.encoder_periodic != want_periodic {
            tracing::info!(
                want_periodic,
                "passive presence changed — restarting shared encoder for GOP switch"
            );
            self.encoder = None;
            self.last_frame_hash = None;
        }
        if self.encoder.is_none() {
            self.last_frame_hash = None;
            let lane = self.lane.with_gop(want_periodic);
            match EncoderSink::new(
                width,
                height,
                self.fps,
                lane,
                EncoderTarget::Channel {
                    tx: self.encoder_tx.clone(),
                    generation: self.inner.generation.clone(),
                },
            ) {
                Ok(enc) => {
                    tracing::info!(periodic_idr = want_periodic, "shared encoder started");
                    self.encoder = Some(enc);
                    self.encoder_periodic = want_periodic;
                }
                Err(e) => {
                    tracing::error!(error=%e, "failed to start shared encoder");
                    self.degrade_to_mesh("encoder spawn failed");
                    return;
                }
            }
        }
        let _ = frame_idx;
        let Some(enc) = &mut self.encoder else { return };
        let ready = enc.prepare_frame(width, height);
        let generation = enc.generation();
        if generation != self.encoder_generation {
            self.encoder_generation = generation;
            self.last_frame_hash = None;
            let hello = self.inner.hello.lock().map(|h| h.clone()).unwrap_or_default();
            broadcast_hello(&self.inner, hello);
        }
        if enc.gave_up() {
            self.degrade_to_mesh("encoder supervisor gave up on the lane");
            return;
        }
        if !ready {
            return;
        }
        let hash = blake3::hash(bgra);
        if (self.last_frame_hash != Some(hash) || self.inner.join_pending.load(Acquire))
            && enc.submit_frame(bgra)
        {
            self.last_frame_hash = Some(hash);
        }
    }

    /// Queue an atomic frame batch (ADR-0242): textures, draw order/bodies,
    /// then retirement. A full queue rejects the whole batch and requests a
    /// fresh snapshot. Accepted batches drain incrementally without restarting
    /// the texture prefix; retained hashes track only the last accepted frame.
    ///
    /// The host calls this only while the mesh lane is active and ≥ 1 viewer
    /// is connected; [`Self::mesh_reap`] covers the N→0 transition.
    pub fn on_meshes(&mut self, ppp: f32, w_px: f32, h_px: f32, frame: &meshlane::SerializedFrame) {
        use std::sync::atomic::Ordering::Relaxed;
        let mut sig_h = blake3::Hasher::new();
        for h in &frame.hashes {
            sig_h.update(&h.to_le_bytes());
        }
        let sig = sig_h.finalize();
        let pending_tex = std::mem::take(&mut self.mesh_pending_tex);
        let retiring = self.mesh_textures.has_pending_frees();
        let live_keys = self.mesh_textures.live_keys();
        let generation = self.inner.generation.load(std::sync::atomic::Ordering::Acquire);
        let Ok(mut reg) = self.inner.registry.lock() else {
            return;
        };
        let fresh_any = reg.conns.iter().any(|c| c.mesh_fresh);
        if pending_tex.is_empty() && !retiring && !fresh_any && self.last_mesh_sig == Some(sig) {
            return;
        }
        let active_id = reg.active_id;
        for c in &mut reg.conns {
            let textures = if c.mesh_fresh {
                self.mesh_textures.full_messages()
            } else {
                pending_tex.clone()
            };
            let missing: Vec<usize> = (0..frame.hashes.len())
                .filter(|&i| c.mesh_fresh || !c.mesh_sent.contains(&frame.hashes[i]))
                .collect();
            let msg = meshlane::frame_message(ppp, w_px, h_px, frame, &missing);
            let msg_len = msg.len() as u64;
            let mut messages: std::collections::VecDeque<_> = textures.into();
            messages.push_back(msg);
            messages.push_back(meshlane::retirement_message(&live_keys));
            if c.mesh_tx
                .try_send(MeshBatch {
                    generation,
                    messages,
                })
                .is_ok()
            {
                c.mesh_sent.clear();
                c.mesh_sent.extend(frame.hashes.iter().copied());
                c.mesh_fresh = false;
                if Some(c.id) == active_id {
                    // Stream-rate telemetry (not aggregate egress), mirroring
                    // the video path: count the active connection's bytes.
                    self.inner.bytes_sent.fetch_add(msg_len, Relaxed);
                    self.inner.frames_sent.fetch_add(1, Relaxed);
                }
            } else {
                tracing::debug!(
                    id = c.id,
                    "mesh send dropped — connection marked for full resync"
                );
                c.mesh_fresh = true;
            }
        }
        self.last_mesh_sig = Some(sig);
    }

    /// Draw-stream analogue of the encoder reap on N→0: clear per-session
    /// lane state and telemetry so the next 0→1 starts fresh. Called by the
    /// host each frame the mesh lane is active with no viewer connected.
    pub fn mesh_reap(&mut self) {
        use std::sync::atomic::Ordering::Relaxed;
        if self.last_mesh_sig.take().is_some() {
            tracing::info!("all viewers disconnected — draw-stream lane state cleared");
            self.inner.bytes_sent.store(0, Relaxed);
            self.inner.frames_sent.store(0, Relaxed);
            self.inner.frames_decoded.store(0, Relaxed);
        }
        self.mesh_pending_tex.clear();
    }
}

/// Fan out current-generation video without blocking other viewers. A missing
/// reference closes that peer rather than relying on a decoder error to recover.
async fn distribute(
    mut encoder_rx: tokio::sync::mpsc::Receiver<EncodedFrame>,
    inner: std::sync::Arc<Inner>,
) {
    while let Some(frame) = encoder_rx.recv().await {
        distribute_frame(&inner, frame);
    }
}

fn refresh_join_pending(inner: &Inner, reg: &Registry) {
    inner.join_pending.store(
        reg.conns.iter().any(|c| c.awaiting_keyframe),
        std::sync::atomic::Ordering::Release,
    );
}

fn keyframe_delivered(inner: &Inner, id: u64, generation: u64) {
    if let Ok(mut reg) = inner.registry.lock() {
        if generation != inner.generation.load(std::sync::atomic::Ordering::Acquire) {
            return;
        }
        if let Some(c) = reg.conns.iter_mut().find(|c| c.id == id) {
            c.awaiting_keyframe = false;
        }
        refresh_join_pending(inner, &reg);
    }
}

fn distribute_frame(inner: &Inner, frame: EncodedFrame) {
    use std::sync::atomic::Ordering::{Acquire, Relaxed};
    if frame.generation != inner.generation.load(Acquire) {
        return;
    }
    inner.bytes_sent.fetch_add(frame.payload.len() as u64, Relaxed);
    inner.frames_sent.fetch_add(1, Relaxed);
    let Ok(mut reg) = inner.registry.lock() else {
        return;
    };
    // A new hello may have reset the join state while we waited for the lock.
    if frame.generation != inner.generation.load(Acquire) {
        return;
    }
    for c in &mut reg.conns {
        if c.video_failed || (!c.video_ready && !frame.keyframe) {
            continue;
        }
        let copy = EncodedFrame {
            generation: frame.generation,
            keyframe: frame.keyframe,
            payload: frame.payload.clone(),
        };
        if c.video_tx.try_send(copy).is_ok() {
            c.video_ready = true;
        } else {
            // A dropped reference cannot be repaired by passing later deltas.
            // Close this peer and let its next connection join at a keyframe.
            c.video_failed = true;
            c.awaiting_keyframe = false;
        }
    }
    refresh_join_pending(inner, &reg);
}

/// Publish a generation's hello independently of payload queue capacity.
/// The writer discards obsolete payloads and delivers this before the new stream.
fn broadcast_hello(inner: &Inner, hello: pb::SessionHello) {
    let msg = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(hello)),
    };
    let mut framed = Vec::with_capacity(1 + msg.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = msg.encode(&mut framed);
    let Ok(mut reg) = inner.registry.lock() else {
        return;
    };
    let generation = inner.generation.load(std::sync::atomic::Ordering::Acquire);
    for c in &mut reg.conns {
        c.stream.send_replace(Some(StreamHello {
            generation,
            payload: framed.clone(),
        }));
        c.mesh_sent.clear();
        c.mesh_fresh = true;
        c.awaiting_keyframe = true;
        c.video_ready = false;
        c.video_failed = false;
        c.joining_since = std::time::Instant::now();
    }
    refresh_join_pending(inner, &reg);
}

/// Body of [`WsCarrier::send_cursor_to_active`], split out so it can be
/// exercised against a bare [`Inner`] (a whole carrier needs bound sockets and
/// a tokio runtime).
/// The whole check-send-record runs under the **registry lock**, which is also
/// held by `broadcast_roster` when it clears the memo. Without that mutual
/// exclusion there is a narrow but real race: a takeover could clear the memo
/// between this send succeeding and it recording the shape, after which the
/// host believes the promoted connection has a shape it was never sent — the
/// exact stale-cursor failure the reset exists to prevent. The lock is held
/// across a non-blocking `try_send` only, and `broadcast_roster` already holds
/// it for a longer fan-out, so this adds no new contention shape.
/// Frame one session-control message and enqueue it for the active connection.
/// Returns whether it was enqueued — the caller decides whether a drop is worth
/// retrying (the tree) or forgetting (a capture ack).
fn send_control_to_active(inner: &Inner, msg: &pb::SessionControl) -> bool {
    let Ok(reg) = inner.registry.lock() else {
        return false;
    };
    let Some(tx) = reg.active_tx() else {
        return false;
    };
    let mut framed = Vec::with_capacity(1 + msg.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = msg.encode(&mut framed);
    tx.try_send(framed).is_ok()
}

fn push_cursor(inner: &Inner, shape: u32) {
    use std::sync::atomic::Ordering;
    let Ok(reg) = inner.registry.lock() else {
        return;
    };
    if inner.cursor_sent.load(Ordering::Relaxed) == shape {
        return;
    }
    let Some(tx) = reg.active_tx() else {
        // No active connection: forget what was last delivered, so whoever
        // takes the slot next is told the shape from scratch.
        inner.cursor_sent.store(CURSOR_UNSENT, Ordering::Relaxed);
        return;
    };
    let msg = pb::SessionControl {
        control: Some(pb::session_control::Control::CursorShape(pb::CursorShape {
            shape,
        })),
    };
    let mut framed = Vec::with_capacity(1 + msg.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = msg.encode(&mut framed);
    if tx.try_send(framed).is_ok() {
        inner.cursor_sent.store(shape, Ordering::Relaxed);
    } else {
        tracing::debug!(
            shape,
            "cursor shape dropped — active viewer queue full; will retry"
        );
    }
}

/// Body of [`WsCarrier::drain_input`], split out so it can be exercised
/// against a bare [`Inner`] (a whole carrier needs bound sockets and a tokio
/// runtime). See that method for the ordering contract.
fn drain_input(inner: &Inner, out: &mut Vec<InputItem>) -> bool {
    let Ok(mut reg) = inner.registry.lock() else {
        return false;
    };
    let epoch = reg.owner_epoch;
    let reset = std::mem::take(&mut reg.owner_reset);
    let mut stale = 0usize;
    for (stamp, item) in reg.input.drain(..) {
        if stamp == epoch {
            out.push(item);
        } else {
            stale += 1;
        }
    }
    if stale > 0 {
        tracing::debug!(stale, "discarded input queued by a departed owner");
    }
    reset
}

/// Build and send each connection its own per-recipient [`pb::Roster`]
/// (ADR-0086 SD1/SD8): every copy shares the connection list but carries its
/// own `you_id`/`you_role`. Called on every membership/role change.
///
/// The roster goes into each connection's `state` mailbox, **not** its
/// bounded payload queue (ADR-0242 SD1): a role change is the one message a
/// viewer cannot recover from losing — it is what tells the viewer whether it
/// drives or watches — so it must not share a fate with disposable frame
/// traffic that a stalled viewer's full queue drops. The mailbox holds the
/// latest value only, and the writer prefers it over payload, so the peer
/// converges on the current roster or its write timeout reaps it. Ordering
/// against a queued hello is not a concern: the roster carries no geometry or
/// codec, and a viewer applies the two independently.
fn broadcast_roster(inner: &Inner) {
    let Ok(reg) = inner.registry.lock() else {
        return;
    };
    // Cursor shape is delivered to the active connection only and deduped
    // against what that connection already has (ADR-0024 Update 2026-07-28).
    // Membership/role changes invalidate that memo — a promoted viewer has
    // never been told the current shape — so clear it here, the one choke
    // point every such change passes through. The next render pass re-sends.
    inner.cursor_sent.store(CURSOR_UNSENT, std::sync::atomic::Ordering::Relaxed);
    // Conditional GOP (ADR-0086 SD10 Update): a passive viewer is present iff
    // there is more than one connection (≤1-active + lone-survivor auto-promote
    // ⇒ a single connection is always the active one). On a change, wake the
    // render thread so it re-picks the shared encoder's GOP promptly.
    refresh_join_pending(inner, &reg);
    let want_periodic = reg.conns.len() >= 2;
    if inner.want_periodic.swap(want_periodic, std::sync::atomic::Ordering::Release)
        != want_periodic
    {
        let _ = inner.waker.send(());
    }
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
        // `send_replace` rather than `send`: it stores (and notifies) whether
        // or not a receiver is currently subscribed, so a roster published
        // while a session task is mid-teardown is not an error to handle.
        c.state.send_replace(Some(framed));
    }
}

/// Case-insensitive ASCII substring search over a request head.
fn contains_ci(haystack: &[u8], needle: &[u8]) -> bool {
    if needle.is_empty() || haystack.len() < needle.len() {
        return false;
    }
    haystack.windows(needle.len()).any(|w| w.eq_ignore_ascii_case(needle))
}

/// Decide whether the incoming request is a WebSocket handshake by
/// peeking (not consuming) the request head. Browsers send the whole
/// head in one segment; a few short re-peeks cover stragglers.
async fn sniff_websocket(stream: &tokio::net::TcpStream) -> bool {
    let mut buf = [0u8; 2048];
    for _ in 0..10 {
        match stream.peek(&mut buf).await {
            // Nothing to peek at, or the peek failed: not a handshake.
            Ok(0) | Err(_) => return false,
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
    // The carrier serves until the process ends; there is no shutdown path to
    // break to, and an accept error is logged and retried rather than fatal.
    #[allow(clippy::infinite_loop)]
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

async fn send_payload<S>(
    sink: &mut S,
    payload: Vec<u8>,
) -> Result<(), tokio_tungstenite::tungstenite::Error>
where
    S: futures_util::Sink<
            tokio_tungstenite::tungstenite::Message,
            Error = tokio_tungstenite::tungstenite::Error,
        > + Unpin,
{
    tokio::time::timeout(
        WRITE_TIMEOUT,
        sink.send(tokio_tungstenite::tungstenite::Message::Binary(
            payload.into(),
        )),
    )
    .await
    .map_err(|elapsed| {
        tokio_tungstenite::tungstenite::Error::Io(std::io::Error::new(
            std::io::ErrorKind::TimedOut,
            elapsed,
        ))
    })?
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
    // The state mailbox receiver is taken under the same lock as the admission
    // (ADR-0242 SD1), so no roster published between the two can be missed:
    // `subscribe` marks only what is already there as seen, and `admit` leaves
    // the mailbox empty.
    let admitted = inner.registry.lock().ok().and_then(|mut r| {
        let id = r.admit(tx)?;
        let c = r.conns.iter_mut().find(|c| c.id == id)?;
        Some((
            id,
            c.state.subscribe(),
            c.stream.subscribe(),
            c.video_rx.take()?,
            c.mesh_rx.take()?,
        ))
    });
    let Some((id, mut state_rx, mut stream_rx, mut video_rx, mut mesh_rx)) = admitted else {
        tracing::info!(%peer, "rejecting connection — MAX_CONNECTIONS reached");
        return Ok(());
    };
    // ≥ 1 connection now: the render thread renders pixels and runs the
    // shared encoder. (Idempotent — already true if others were present.)
    inner.connected.store(true, std::sync::atomic::Ordering::Release);
    let _ = inner.waker.send(());
    tracing::info!(%peer, id, "viewer connected");

    // First wire message: the session hello with current stream geometry
    // (SD6 0x03), sent directly so it precedes any queued video/roster — the
    // viewer needs it to size its canvas and configure its decoder.
    let initial_generation = inner.generation.load(std::sync::atomic::Ordering::Acquire);
    let current_hello = inner.hello.lock().map(|h| (*h).clone()).unwrap_or_default();
    let hello = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(current_hello)),
    };
    let mut framed = Vec::with_capacity(1 + hello.encoded_len());
    framed.push(pb::PREFIX_SESSION);
    let _ = hello.encode(&mut framed);
    let hello_result = ws_tx
        .send(tokio_tungstenite::tungstenite::Message::Binary(
            framed.into(),
        ))
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
        let mut mesh_batch: Option<MeshBatch> = None;
        let mut mesh_started = std::time::Instant::now();
        let mut sent_generation = initial_generation;
        loop {
            let failed = inner
                .registry
                .lock()
                .map(|r| r.find(id).is_some_and(|c| c.video_failed))
                .unwrap_or(true);
            if failed || (mesh_batch.is_some() && mesh_started.elapsed() > JOIN_TIMEOUT) {
                break Ok(());
            }
            let current_generation = inner.generation.load(std::sync::atomic::Ordering::Acquire);
            if mesh_batch.as_ref().is_some_and(|b| b.generation != current_generation) {
                mesh_batch = None;
            }
            if mesh_batch.as_ref().is_some_and(|b| b.messages.is_empty()) {
                mesh_batch = None;
            }
            tokio::select! {
                // Authoritative state before disposable frame traffic
                // (ADR-0242 SD1). `biased` makes that preference explicit
                // rather than leaving it to the random branch order: when a
                // role change and a queued video unit are both ready, the role
                // change goes first. It cannot starve the payload branch — the
                // mailbox is only ready when a *new* state has been published,
                // which happens on membership/role changes, not per frame.
                biased;
                changed = state_rx.changed() => {
                    if changed.is_err() {
                        // The sender lives in this connection's `Conn`, so this
                        // only resolves once the registry has removed it.
                        break Ok(());
                    }
                    // Latest-value: if several rosters were published while the
                    // previous write was in flight, only the newest is sent.
                    // Cloned out of the borrow so no watch guard is held across
                    // the await below.
                    let state = state_rx.borrow_and_update().clone();
                    if let Some(payload) = state {
                        match tokio::time::timeout(WRITE_TIMEOUT, ws_tx.send(tokio_tungstenite::tungstenite::Message::Binary(payload.into()))).await {
                            Ok(Ok(())) => {}
                            Ok(Err(e)) => break Err(e),
                            Err(_) => {
                                // Retried until written or the peer's existing
                                // timeout closes it (SD1) — never dropped.
                                tracing::info!(%peer, id, "viewer unresponsive (state send timed out) — reaping connection");
                                break Ok(());
                            }
                        }
                    }
                }
                changed = stream_rx.changed() => {
                    if changed.is_err() { break Ok(()); }
                    let state = stream_rx.borrow_and_update().clone();
                    if let Some(state) = state {
                        if state.generation != inner.generation.load(std::sync::atomic::Ordering::Acquire)
                            || state.generation == sent_generation { continue; }
                        match send_payload(&mut ws_tx, state.payload.clone()).await {
                            Ok(()) => {
                                sent_generation = state.generation;
                                mesh_batch = None;
                            }
                            Err(e) => break Err(e),
                        }
                    }
                }
                msg = ws_rx.next() => {
                    awaiting_pong = false;
                    match msg {
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Binary(data))) => handle_client_message(&data, &inner, id),
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Close(_))) | None => break Ok(()),
                        Some(Ok(_)) => {},
                        Some(Err(e)) => break Err(e),
                    }
                }
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
                _ = ping.tick() => {
                    let join_expired = inner.registry.lock().map(|r| r.find(id).is_some_and(|c| c.awaiting_keyframe && c.joining_since.elapsed() > JOIN_TIMEOUT)).unwrap_or(false);
                    let video = inner.hello.lock().map(|h| h.codec != "mesh").unwrap_or(false);
                    if video && join_expired {
                        tracing::warn!(id, "viewer join timed out without a delivered keyframe");
                        let no_keyframe = inner.registry.lock().map(|r| r.find(id).is_some_and(|c| !c.video_ready)).unwrap_or(false);
                        if no_keyframe {
                            inner.join_failed.store(true, std::sync::atomic::Ordering::Release);
                            let _ = inner.waker.send(());
                        }
                        break Ok(());
                    }
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
                out = video_rx.recv() => {
                    let Some(frame) = out else { break Ok(()); };
                    if frame.generation != inner.generation.load(std::sync::atomic::Ordering::Acquire) { continue; }
                    if sent_generation != frame.generation {
                        let pending = stream_rx.borrow_and_update().clone();
                        let Some(state) = pending.filter(|s| s.generation == frame.generation) else { continue; };
                        if let Err(e) = send_payload(&mut ws_tx, state.payload).await { break Err(e); }
                        sent_generation = frame.generation;
                    }
                    if frame.generation != inner.generation.load(std::sync::atomic::Ordering::Acquire) { continue; }
                    if let Err(e) = send_payload(&mut ws_tx, frame.payload).await { break Err(e); }
                    if frame.keyframe { keyframe_delivered(&inner, id, frame.generation); }
                }
                batch = mesh_rx.recv(), if mesh_batch.is_none() => {
                    let Some(batch) = batch else { break Ok(()); };
                    mesh_batch = Some(batch);
                    mesh_started = std::time::Instant::now();
                }
                _ = std::future::ready(()), if mesh_batch.is_some() => {
                    let generation = inner.generation.load(std::sync::atomic::Ordering::Acquire);
                    if let Some(payload) = mesh_batch.as_mut().and_then(|b| b.next(generation)) {
                        if sent_generation != generation {
                            let pending = stream_rx.borrow_and_update().clone();
                            let Some(state) = pending.filter(|s| s.generation == generation) else { mesh_batch = None; continue; };
                            if let Err(e) = send_payload(&mut ws_tx, state.payload).await { break Err(e); }
                            sent_generation = generation;
                        }
                        if generation != inner.generation.load(std::sync::atomic::Ordering::Acquire) { mesh_batch = None; continue; }
                        if let Err(e) = send_payload(&mut ws_tx, payload).await { break Err(e); }
                    }
                    tokio::task::yield_now().await;
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
    // any connection. Input and paste are the exception to this lookup: they
    // are checked *and* epoch-stamped inside the registry lock itself
    // (ADR-0242 SD2), so an authority answer cannot go stale before the
    // enqueue.
    let active = inner.registry.lock().map(|r| r.is_active(id)).unwrap_or(false);
    match prefix {
        pb::PREFIX_INPUT => {
            match pb::InputEvent::decode(payload) {
                Ok(ev) => {
                    if let Some(ev) = ev.event {
                        // Ownership is checked inside the registry lock that
                        // also stamps the epoch and enqueues (ADR-0242 SD2):
                        // the hot input path takes that lock once, and there is
                        // no window between the check and the push for a
                        // takeover to slip through.
                        let accepted = inner
                            .registry
                            .lock()
                            .map(|mut r| r.accept_input(id, ev))
                            .unwrap_or(false);
                        if accepted {
                            let _ = inner.waker.send(()); // input wants a pass now
                        }
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
                        tracing::debug!(
                            frames_decoded = p.nonce,
                            "active viewer decode-progress ping"
                        );
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
                                tracing::debug!(
                                    id,
                                    "TakeSession ignored — connection not WebCodecs-capable"
                                );
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
                    // Viewer→host paste (active-only, ADR-0082 SD6), ordered
                    // and stamped with the input it arrives among (SD2).
                    let accepted = inner
                        .registry
                        .lock()
                        .map(|mut r| r.accept_paste(id, c.text))
                        .unwrap_or(false);
                    if accepted {
                        let _ = inner.waker.send(());
                    }
                }
                Some(pb::session_control::Control::TreeRequest(_)) => {
                    // ADR-0154 SD1: hand the next completed pass's accessibility
                    // tree to whoever asked. Active-only, like input: the tree
                    // carries the labels and values on screen, so it is data
                    // egress and belongs to the connection already driving.
                    if active {
                        inner.tree_wanted.store(true, std::sync::atomic::Ordering::Relaxed);
                        let _ = inner.waker.send(()); // a tree wants a pass now
                    }
                }
                Some(pb::session_control::Control::CaptureRequest(c)) => {
                    // ADR-0154 SD4. The host sanitises the name and resolves the
                    // directory; nothing from the wire reaches a path join.
                    if active {
                        if let Ok(mut pending) = inner.capture_request.lock() {
                            *pending = Some(c.name);
                        }
                        let _ = inner.waker.send(());
                    }
                }
                // Server→client only; ignore if a client echoes one.
                Some(
                    pb::session_control::Control::Hello(_)
                    | pb::session_control::Control::Roster(_)
                    | pb::session_control::Control::CursorShape(_)
                    | pb::session_control::Control::TreeSnapshot(_)
                    | pb::session_control::Control::CaptureDone(_),
                )
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

    fn carrier_for_test(lane: CodecLane) -> WsCarrier {
        WsCarrier {
            inner: std::sync::Arc::new(mk_inner()),
            encoder: None,
            encoder_tx: tokio::sync::mpsc::channel(16).0,
            encoder_generation: 0,
            encoder_periodic: false,
            fps: 30.0,
            lane,
            last_frame_hash: None,
            mesh_textures: meshlane::TextureStore::default(),
            mesh_pending_tex: Vec::new(),
            last_mesh_sig: None,
        }
    }

    fn triangle(at: f32) -> meshlane::SerializedFrame {
        let mut mesh = egui::epaint::Mesh::default();
        for pos in [
            egui::pos2(at, 0.0),
            egui::pos2(at + 10.0, 0.0),
            egui::pos2(at, 10.0),
        ] {
            mesh.vertices.push(egui::epaint::Vertex {
                pos,
                uv: egui::pos2(0.0, 0.0),
                color: egui::Color32::WHITE,
            });
        }
        mesh.indices.extend([0, 1, 2]);
        meshlane::serialize(
            &[egui::ClippedPrimitive {
                clip_rect: egui::Rect::EVERYTHING,
                primitive: egui::epaint::Primitive::Mesh(mesh),
            }],
            1.0,
        )
    }

    #[test]
    fn mesh_bootstrap_larger_than_queue_drains_once_and_in_order() {
        let mut c = carrier_for_test(CodecLane::mesh());
        let id = c.inner.registry.lock().unwrap().admit(mk_tx()).unwrap();
        let mut rx = c.inner.registry.lock().unwrap().conns[0].mesh_rx.take().unwrap();
        let mut delta = egui::TexturesDelta::default();
        for n in 0..40 {
            delta.set.push((
                egui::TextureId::Managed(n),
                egui::epaint::ImageDelta::full(
                    egui::ColorImage::filled([1, 1], egui::Color32::WHITE),
                    egui::TextureOptions::LINEAR,
                ),
            ));
        }
        c.ingest_textures(&delta);
        c.on_meshes(1.0, 640.0, 480.0, &triangle(0.0));
        c.finish_texture_frame();
        // No room for a second batch. Reject it without corrupting the first.
        c.on_meshes(1.0, 640.0, 480.0, &triangle(1.0));
        assert!(c.inner.registry.lock().unwrap().find(id).unwrap().mesh_fresh);
        let mut batch = rx.try_recv().unwrap();
        for _ in 0..40 {
            assert_eq!(batch.next(0).unwrap()[1], meshlane::MESH_MSG_TEXTURE);
        }
        assert_eq!(batch.next(0).unwrap()[1], meshlane::MESH_MSG_FRAME);
        assert_eq!(batch.next(0).unwrap()[1], meshlane::MESH_MSG_RETIRE);
        assert!(batch.next(0).is_none());
        c.on_meshes(1.0, 640.0, 480.0, &triangle(1.0));
        assert!(rx.try_recv().is_ok());
        assert!(!c.inner.registry.lock().unwrap().find(id).unwrap().mesh_fresh);
    }

    #[test]
    fn mesh_retention_is_bounded_and_free_only_frames_are_delivered() {
        let mut c = carrier_for_test(CodecLane::mesh());
        c.inner.registry.lock().unwrap().admit(mk_tx()).unwrap();
        let mut rx = c.inner.registry.lock().unwrap().conns[0].mesh_rx.take().unwrap();
        for n in 0..200 {
            c.on_meshes(1.0, 640.0, 480.0, &triangle(n as f32));
            assert!(rx.try_recv().is_ok());
            assert_eq!(c.inner.registry.lock().unwrap().conns[0].mesh_sent.len(), 1);
        }
        let delta = egui::TexturesDelta {
            set: Vec::new(),
            free: vec![egui::TextureId::Managed(77)],
        };
        c.ingest_textures(&delta);
        c.on_meshes(1.0, 640.0, 480.0, &triangle(199.0));
        let mut batch = rx.try_recv().expect("free-only changes are not deduped");
        assert_eq!(
            batch.messages.pop_back().unwrap(),
            meshlane::retirement_message(&[])
        );
        c.finish_texture_frame();
    }

    #[test]
    fn obsolete_mesh_batch_cannot_finish_after_a_new_hello() {
        let mut batch = MeshBatch {
            generation: 1,
            messages: vec![vec![4, 2], vec![4, 1], vec![4, 3]].into(),
        };
        assert!(batch.next(1).is_some());
        assert!(batch.next(2).is_none());
        assert!(batch.messages.is_empty());
    }

    #[test]
    fn late_idle_video_join_drives_progress_until_keyframe_delivery() {
        let inner = mk_inner();
        inner.generation.store(7, std::sync::atomic::Ordering::Release);
        let mut reg = inner.registry.lock().unwrap();
        for _ in 0..2 {
            reg.admit(mk_tx()).unwrap();
            let c = reg.conns.last_mut().unwrap();
            c.awaiting_keyframe = false;
            c.video_ready = true;
        }
        let id = reg.admit(mk_tx()).unwrap();
        let mut rx = reg.conns.last_mut().unwrap().video_rx.take().unwrap();
        drop(reg);
        broadcast_roster(&inner);
        assert!(inner.join_pending.load(std::sync::atomic::Ordering::Acquire));
        let frame = |keyframe| EncodedFrame {
            generation: 7,
            keyframe,
            payload: vec![1],
        };
        distribute_frame(&inner, frame(false));
        assert!(
            rx.try_recv().is_err(),
            "joining decoder never receives a delta first"
        );
        distribute_frame(&inner, frame(true));
        assert!(rx.try_recv().unwrap().keyframe);
        assert!(
            inner.join_pending.load(std::sync::atomic::Ordering::Acquire),
            "queue admission is not delivery"
        );
        keyframe_delivered(&inner, id, 6);
        assert!(
            inner.join_pending.load(std::sync::atomic::Ordering::Acquire),
            "old generation cannot complete a join"
        );
        keyframe_delivered(&inner, id, 7);
        assert!(!inner.join_pending.load(std::sync::atomic::Ordering::Acquire));
        assert_eq!(
            inner.generation.load(std::sync::atomic::Ordering::Acquire),
            7,
            "joining did not force an encoder restart"
        );
        distribute_frame(&inner, frame(false));
        assert!(!rx.try_recv().unwrap().keyframe);
    }

    #[test]
    fn stale_video_is_filtered_and_a_dropped_reference_reaps_only_its_peer() {
        let inner = mk_inner();
        inner.generation.store(2, std::sync::atomic::Ordering::Release);
        let mut reg = inner.registry.lock().unwrap();
        reg.admit(mk_tx()).unwrap();
        let mut rx = reg.conns[0].video_rx.take().unwrap();
        drop(reg);
        distribute_frame(
            &inner,
            EncodedFrame {
                generation: 1,
                keyframe: true,
                payload: vec![1],
            },
        );
        assert!(rx.try_recv().is_err());
        for n in 0..=VIDEO_CHANNEL_CAP {
            distribute_frame(
                &inner,
                EncodedFrame {
                    generation: 2,
                    keyframe: n == 0,
                    payload: vec![1],
                },
            );
        }
        assert!(inner.registry.lock().unwrap().conns[0].video_failed);
    }

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

    /// `MAX_CONNECTIONS` refuses the surplus connection.
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

    // ---- cursor shape (ADR-0024 Update 2026-07-28) ----

    /// A bare [`Inner`] — everything `push_cursor` and `broadcast_roster`
    /// touch, without binding sockets or starting a runtime.
    fn mk_inner() -> Inner {
        Inner {
            resize: std::sync::Mutex::new(None),
            cadence_request: std::sync::Mutex::new(None),
            waker: std::sync::mpsc::channel().0,
            connected: std::sync::atomic::AtomicBool::new(false),
            want_periodic: std::sync::atomic::AtomicBool::new(false),
            generation: std::sync::Arc::new(std::sync::atomic::AtomicU64::new(0)),
            join_pending: std::sync::atomic::AtomicBool::new(false),
            join_failed: std::sync::atomic::AtomicBool::new(false),
            registry: std::sync::Mutex::new(Registry::new(8)),
            hello: std::sync::Mutex::new(pb::SessionHello::default()),
            decode_caps: std::sync::Mutex::new(None),
            cursor_sent: std::sync::atomic::AtomicU32::new(CURSOR_UNSENT),
            bytes_sent: std::sync::atomic::AtomicU64::new(0),
            frames_sent: std::sync::atomic::AtomicU64::new(0),
            frames_decoded: std::sync::atomic::AtomicU64::new(0),
            tree_wanted: std::sync::atomic::AtomicBool::new(false),
            capture_request: std::sync::Mutex::new(None),
        }
    }

    /// Decode a framed session-control message back to its cursor shape.
    fn framed_cursor_shape(framed: &[u8]) -> u32 {
        assert_eq!(
            framed[0],
            pb::PREFIX_SESSION,
            "cursor rides the session prefix"
        );
        match pb::SessionControl::decode(&framed[1..]).expect("decodes").control {
            Some(pb::session_control::Control::CursorShape(c)) => c.shape,
            other => panic!("expected CursorShape, got {other:?}"),
        }
    }

    /// The shape goes to the active connection once; an unchanged shape on the
    /// next pass sends nothing (this runs at the render cadence, 30–60×/s).
    #[test]
    fn cursor_sends_once_then_dedupes() {
        let inner = mk_inner();
        let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<u8>>(4);
        inner.registry.lock().unwrap().admit(tx).unwrap();

        push_cursor(&inner, 9); // Text
        assert_eq!(framed_cursor_shape(&rx.try_recv().expect("sent")), 9);
        push_cursor(&inner, 9);
        push_cursor(&inner, 9);
        assert!(rx.try_recv().is_err(), "unchanged shape is not re-sent");
        push_cursor(&inner, 0); // back to Default — a real shape, must be sent
        assert_eq!(framed_cursor_shape(&rx.try_recv().expect("sent")), 0);
    }

    /// Passive connections never receive the shape: it is resolved from the
    /// *active* pointer's position, so it would morph their local cursor from
    /// someone else's hover.
    #[test]
    fn cursor_goes_only_to_the_active_connection() {
        let inner = mk_inner();
        let (atx, mut arx) = tokio::sync::mpsc::channel::<Vec<u8>>(4);
        let (ptx, mut prx) = tokio::sync::mpsc::channel::<Vec<u8>>(4);
        {
            let mut reg = inner.registry.lock().unwrap();
            reg.admit(atx).unwrap();
            reg.admit(ptx).unwrap();
        }
        push_cursor(&inner, 16); // Grab
        assert_eq!(
            framed_cursor_shape(&arx.try_recv().expect("active receives")),
            16
        );
        assert!(prx.try_recv().is_err(), "passive receives nothing");
    }

    /// A drop must not latch. With the memo advanced on a failed enqueue the
    /// host would believe the shape had landed and never resend it, stranding
    /// the viewer on a stale cursor for as long as it kept hovering.
    #[test]
    fn cursor_drop_on_full_queue_retries() {
        let inner = mk_inner();
        let (tx, mut rx) = tokio::sync::mpsc::channel::<Vec<u8>>(1);
        inner.registry.lock().unwrap().admit(tx.clone()).unwrap();
        tx.try_send(vec![0xff]).expect("fill the queue"); // capacity 1, now full

        push_cursor(&inner, 19); // ResizeHorizontal — dropped
        assert_eq!(rx.try_recv().expect("the filler"), vec![0xff]);
        assert!(
            rx.try_recv().is_err(),
            "the cursor update was dropped, not queued"
        );

        push_cursor(&inner, 19); // same shape, but never delivered → retried
        assert_eq!(framed_cursor_shape(&rx.try_recv().expect("retried")), 19);
    }

    /// A takeover hands the pointer to a connection that has never been told
    /// the current shape; the roster broadcast (every membership/role change
    /// passes through it) clears the memo so the next pass re-sends.
    #[test]
    fn roster_change_clears_the_cursor_memo() {
        let inner = mk_inner();
        let (atx, mut arx) = tokio::sync::mpsc::channel::<Vec<u8>>(8);
        let (btx, mut brx) = tokio::sync::mpsc::channel::<Vec<u8>>(8);
        let b = {
            let mut reg = inner.registry.lock().unwrap();
            reg.admit(atx).unwrap();
            reg.admit(btx).unwrap()
        };
        push_cursor(&inner, 17); // Grabbing → the first active
        assert_eq!(framed_cursor_shape(&arx.try_recv().expect("sent")), 17);

        inner.registry.lock().unwrap().take_session(b);
        broadcast_roster(&inner);

        push_cursor(&inner, 17); // unchanged shape, new active → must be sent
        assert_eq!(
            framed_cursor_shape(&brx.try_recv().expect("re-sent after promotion")),
            17,
            "the promoted connection is told the current shape"
        );
    }

    // ---- roster mailbox (ADR-0242 SD1) ----

    /// Decode a framed session-control message back to its roster.
    fn framed_roster(framed: &[u8]) -> pb::Roster {
        assert_eq!(
            framed[0],
            pb::PREFIX_SESSION,
            "the roster rides the session prefix"
        );
        match pb::SessionControl::decode(&framed[1..]).expect("decodes").control {
            Some(pb::session_control::Control::Roster(r)) => r,
            other => panic!("expected Roster, got {other:?}"),
        }
    }

    /// The mailbox receiver a session task would hold for connection `id`.
    fn state_rx(inner: &Inner, id: u64) -> tokio::sync::watch::Receiver<Option<Vec<u8>>> {
        inner.registry.lock().unwrap().find(id).expect("admitted").state.subscribe()
    }

    /// A viewer whose payload queue is completely full still learns that it was
    /// promoted and then demoted: the roster rides its own latest-value mailbox
    /// (SD1), so the queue that drops video cannot drop the message that tells
    /// the viewer whether it drives or watches.
    #[test]
    fn roster_survives_a_saturated_payload_queue() {
        let inner = mk_inner();
        // Capacity 1, filled: every `try_send` on this connection now fails.
        let (btx, mut brx) = tokio::sync::mpsc::channel::<Vec<u8>>(1);
        let (a, b) = {
            let mut reg = inner.registry.lock().unwrap();
            (reg.admit(mk_tx()).unwrap(), reg.admit(btx.clone()).unwrap())
        };
        btx.try_send(vec![0xff]).expect("fill the queue");
        let mut rx = state_rx(&inner, b);

        inner.registry.lock().unwrap().take_session(b);
        broadcast_roster(&inner);
        let promoted = framed_roster(rx.borrow_and_update().clone().expect("state").as_slice());
        assert_eq!(promoted.you_id, b);
        assert_eq!(promoted.you_role, pb::Role::Active as i32);

        inner.registry.lock().unwrap().take_session(a);
        broadcast_roster(&inner);
        let demoted = framed_roster(rx.borrow_and_update().clone().expect("state").as_slice());
        assert_eq!(demoted.you_role, pb::Role::Passive as i32);
        assert_eq!(demoted.active_id, a);

        // Nothing was smuggled onto the payload queue either way.
        assert_eq!(brx.try_recv().expect("the filler"), vec![0xff]);
        assert!(brx.try_recv().is_err(), "the roster did not use the queue");
    }

    /// Changes that land before the writer wakes coalesce: the peer sends one
    /// write carrying the *latest* roster, never a backlog of superseded ones.
    #[test]
    fn roster_mailbox_coalesces_to_the_latest() {
        let inner = mk_inner();
        let (a, b) = {
            let mut reg = inner.registry.lock().unwrap();
            (reg.admit(mk_tx()).unwrap(), reg.admit(mk_tx()).unwrap())
        };
        let mut rx = state_rx(&inner, a);

        inner.registry.lock().unwrap().take_session(b);
        broadcast_roster(&inner);
        inner.registry.lock().unwrap().take_session(a);
        broadcast_roster(&inner);

        assert!(rx.has_changed().unwrap(), "the writer has state to send");
        let latest = framed_roster(rx.borrow_and_update().clone().expect("state").as_slice());
        assert_eq!(latest.you_role, pb::Role::Active as i32, "the newest role");
        assert!(
            !rx.has_changed().unwrap(),
            "two changes coalesced into one write, not a queued backlog"
        );
    }

    // ---- input ownership (ADR-0242 SD2) ----

    fn mk_move(x: f32) -> pb::input_event::Event {
        pb::input_event::Event::MouseMove(pb::MouseMove { x, y: 0.0 })
    }

    /// Input is accepted only from the owner, and the check happens under the
    /// same lock that decides ownership — a passive connection's events never
    /// reach the queue at all.
    #[test]
    fn only_the_owner_can_enqueue_input() {
        let inner = mk_inner();
        let (a, b) = {
            let mut reg = inner.registry.lock().unwrap();
            (reg.admit(mk_tx()).unwrap(), reg.admit(mk_tx()).unwrap())
        };
        {
            let mut reg = inner.registry.lock().unwrap();
            assert!(reg.accept_input(a, mk_move(1.0)), "the owner is honoured");
            assert!(!reg.accept_input(b, mk_move(2.0)), "the passive is dropped");
            assert!(!reg.accept_paste(b, "nope".to_owned()), "and its paste");
        }
        let mut out = Vec::new();
        drain_input(&inner, &mut out);
        assert_eq!(out, vec![InputItem::Event(mk_move(1.0))]);
    }

    /// A takeover discards what the previous owner had queued and asks the host
    /// to cancel held input *before* the new owner's events — the failure this
    /// prevents is a press sent by one device completing as a click under
    /// another.
    #[test]
    fn takeover_resets_before_new_owner_input() {
        let inner = mk_inner();
        let (a, b) = {
            let mut reg = inner.registry.lock().unwrap();
            (reg.admit(mk_tx()).unwrap(), reg.admit(mk_tx()).unwrap())
        };
        let mut out = Vec::new();
        assert!(drain_input(&inner, &mut out), "admission opens an interval");
        out.clear();

        {
            let mut reg = inner.registry.lock().unwrap();
            reg.accept_input(a, mk_move(1.0));
            reg.accept_paste(a, "departing".to_owned());
            reg.take_session(b);
            reg.accept_input(b, mk_move(2.0));
        }
        assert!(drain_input(&inner, &mut out), "the owner changed");
        assert_eq!(
            out,
            vec![InputItem::Event(mk_move(2.0))],
            "only the new owner's input survives the change"
        );

        // A steady interval reports no further reset — cancellation must not
        // fire every pass, or no drag could ever complete.
        out.clear();
        inner.registry.lock().unwrap().accept_input(b, mk_move(3.0));
        assert!(!drain_input(&inner, &mut out));
        assert_eq!(out, vec![InputItem::Event(mk_move(3.0))]);
    }

    /// Paste keeps its place among the events it arrived between, so a focus
    /// change that preceded it is honoured instead of the text landing after
    /// everything else.
    #[test]
    fn paste_is_drained_in_arrival_order() {
        let inner = mk_inner();
        let a = inner.registry.lock().unwrap().admit(mk_tx()).unwrap();
        {
            let mut reg = inner.registry.lock().unwrap();
            reg.accept_input(a, mk_move(1.0));
            reg.accept_paste(a, "middle".to_owned());
            reg.accept_input(a, mk_move(2.0));
        }
        let mut out = Vec::new();
        drain_input(&inner, &mut out);
        assert_eq!(
            out,
            vec![
                InputItem::Event(mk_move(1.0)),
                InputItem::Paste("middle".to_owned()),
                InputItem::Event(mk_move(2.0)),
            ]
        );
    }

    /// The owner disconnecting cancels its held input server-side: the epoch
    /// advances when the slot empties, without waiting for an unload message
    /// the departing browser may never send. The lone survivor's promotion is
    /// the same kind of change.
    #[test]
    fn disconnect_and_auto_promotion_reset_the_owner() {
        let inner = mk_inner();
        let (a, b, c) = {
            let mut reg = inner.registry.lock().unwrap();
            (
                reg.admit(mk_tx()).unwrap(),
                reg.admit(mk_tx()).unwrap(),
                reg.admit(mk_tx()).unwrap(),
            )
        };
        let mut out = Vec::new();
        drain_input(&inner, &mut out); // consume the admission reset
        out.clear();

        {
            let mut reg = inner.registry.lock().unwrap();
            reg.accept_input(a, mk_move(1.0));
            reg.remove(a); // two passives remain → the slot empties
            assert_eq!(reg.active_id, None);
        }
        assert!(drain_input(&inner, &mut out), "the owner left");
        assert!(out.is_empty(), "its queued input went with it");

        inner.registry.lock().unwrap().remove(c); // b is now the lone survivor
        assert!(
            drain_input(&inner, &mut out),
            "auto-promotion is an owner change too"
        );
        assert_eq!(inner.registry.lock().unwrap().active_id, Some(b));
    }

    /// Removing a connection that neither owns input nor leaves a lone
    /// survivor to promote must *not* report a reset: cancelling held input on
    /// unrelated membership churn would break the owner's in-progress drag.
    #[test]
    fn unrelated_disconnect_does_not_reset_the_owner() {
        let inner = mk_inner();
        let (a, _b, c) = {
            let mut reg = inner.registry.lock().unwrap();
            (
                reg.admit(mk_tx()).unwrap(),
                reg.admit(mk_tx()).unwrap(),
                reg.admit(mk_tx()).unwrap(),
            )
        };
        let mut out = Vec::new();
        drain_input(&inner, &mut out); // consume the admission reset
        out.clear();

        {
            let mut reg = inner.registry.lock().unwrap();
            reg.accept_input(a, mk_move(1.0));
            reg.remove(c); // a passive leaves; two connections and the owner remain
        }
        assert!(!drain_input(&inner, &mut out), "the owner did not change");
        assert_eq!(
            out,
            vec![InputItem::Event(mk_move(1.0))],
            "the owner's input is untouched"
        );
    }

    /// The consumption-side half of the authority check (the plan's
    /// "checked at consumption as well as admission"): an item that outlives
    /// its epoch is discarded at the drain, not translated under the next
    /// owner — even if it reached the queue by some other route than
    /// [`Registry::accept_input`].
    #[test]
    fn stale_epoch_items_are_discarded_at_the_drain() {
        let inner = mk_inner();
        let a = inner.registry.lock().unwrap().admit(mk_tx()).unwrap();
        {
            let mut reg = inner.registry.lock().unwrap();
            let stale = reg.owner_epoch.wrapping_sub(1);
            reg.input.push((stale, InputItem::Event(mk_move(1.0))));
            reg.input.push((stale, InputItem::Paste("stale".to_owned())));
            reg.accept_input(a, mk_move(2.0));
        }
        let mut out = Vec::new();
        drain_input(&inner, &mut out);
        assert_eq!(out, vec![InputItem::Event(mk_move(2.0))]);
    }

    /// With the active slot empty the memo is cleared rather than left
    /// pointing at a connection that is gone.
    #[test]
    fn cursor_memo_cleared_when_no_active() {
        let inner = mk_inner();
        assert_eq!(
            inner.cursor_sent.load(std::sync::atomic::Ordering::Relaxed),
            CURSOR_UNSENT
        );
        push_cursor(&inner, 4);
        assert_eq!(
            inner.cursor_sent.load(std::sync::atomic::Ordering::Relaxed),
            CURSOR_UNSENT,
            "nothing was delivered, so nothing is memoised"
        );
    }
}
