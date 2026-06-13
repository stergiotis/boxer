//! WebSocket carrier (ADR-0024 SD4/SD6, Phases 4–5).
//!
//! Two TCP listeners (`IMZERO2_HEADLESS_LISTEN` and port+1, kept for URL
//! compatibility) run an identical dispatcher: a request that carries a
//! WebSocket upgrade header becomes the data channel; anything else is
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
//! Session model (v1, per the ADR): single session — a second connection
//! is rejected while one is active. The per-connection encoder is owned
//! by this carrier's [`FrameSink`] impl: it spawns on connect and drops on
//! disconnect, so every connection starts its stream at SPS/PPS + IDR
//! (the acceptance-review (re)connect rule) and no encoding happens with
//! nobody watching.
//!
//! The tokio runtime lives on a dedicated thread; the render loop talks
//! to it only through atomics, a mutex-guarded event vector, and the
//! bounded video channel.

use crate::imzero2::encoderpipe::{EncoderSink, EncoderTarget};
use crate::imzero2::framesink::FrameSink as _;
use crate::imzero2::inputproto as pb;
use futures_util::{SinkExt as _, StreamExt as _};
use prost::Message as _;

const VIDEO_CHANNEL_CAP: usize = 16;

struct Inner {
    /// Raw wire events, drained by the render thread each tick.
    events: std::sync::Mutex<Vec<pb::input_event::Event>>,
    /// Latest viewport-resize request; drained by the render thread,
    /// which applies it (target rebuild + hello re-announce + encoder
    /// restart) and answers with a fresh [`pb::SessionHello`].
    resize: std::sync::Mutex<Option<pb::ViewportResize>>,
    /// Latest runtime cadence request (0 continuous / 1 reactive),
    /// drained by the render thread.
    cadence_request: std::sync::Mutex<Option<u32>>,
    /// Wakes the render thread out of its reactive sleep when anything
    /// arrives that wants a pass soon: input, resize, cadence change,
    /// connect/disconnect. Sends are fire-and-forget.
    waker: std::sync::mpsc::Sender<()>,
    connected: std::sync::atomic::AtomicBool,
    /// Bumped on every accepted connection; the render thread compares it
    /// to decide when to (re)spawn the per-connection encoder.
    conn_gen: std::sync::atomic::AtomicU64,
    /// Sender for pre-framed outbound payloads of the *current*
    /// connection — video chunks and mid-session hello re-announcements
    /// share it, which is what orders "hello before the new stream's IDR"
    /// during a geometry change.
    video_tx: std::sync::Mutex<Option<tokio::sync::mpsc::Sender<Vec<u8>>>>,
    /// Current stream geometry; sent on connect and after each applied
    /// resize. Updated by the render thread via [`WsCarrier::apply_geometry`].
    hello: std::sync::Mutex<pb::SessionHello>,
}

pub struct WsCarrier {
    inner: std::sync::Arc<Inner>,
    encoder: Option<EncoderSink>,
    encoder_gen: u64,
    fps: f32,
    encoder_args: Vec<String>,
    /// blake3 of the last frame fed to the encoder; identical frames are
    /// skipped (no encode, no wire bytes). Reset whenever the encoder is
    /// (re)spawned so a fresh stream always begins with a real frame.
    last_frame_hash: Option<blake3::Hash>,
}

impl WsCarrier {
    /// Bind `listen` (e.g. "127.0.0.1:8089") for WebSocket and `port+1`
    /// for the viewer page, then run both on a dedicated tokio thread.
    /// `waker` is signalled whenever wire activity wants a render pass
    /// soon (input, resize, cadence change, connect/disconnect).
    pub fn start(
        listen: &str,
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
        cadence: u32,
        fps: f32,
        encoder_args: Vec<String>,
        waker: std::sync::mpsc::Sender<()>,
    ) -> std::io::Result<Self> {
        let inner = std::sync::Arc::new(Inner {
            events: std::sync::Mutex::new(Vec::new()),
            resize: std::sync::Mutex::new(None),
            cadence_request: std::sync::Mutex::new(None),
            waker,
            connected: std::sync::atomic::AtomicBool::new(false),
            conn_gen: std::sync::atomic::AtomicU64::new(0),
            video_tx: std::sync::Mutex::new(None),
            hello: std::sync::Mutex::new(pb::SessionHello {
                width_px,
                height_px,
                pixels_per_point,
                cadence,
            }),
        });
        // Bind synchronously so startup errors (port in use) fail fast in
        // the caller instead of asynchronously on the carrier thread.
        let ws_listener = std::net::TcpListener::bind(listen)?;
        ws_listener.set_nonblocking(true)?;
        let ws_addr = ws_listener.local_addr()?;
        let page_addr = std::net::SocketAddr::new(ws_addr.ip(), ws_addr.port().wrapping_add(1));
        let page_listener = std::net::TcpListener::bind(page_addr)?;
        page_listener.set_nonblocking(true)?;
        tracing::info!(viewer=%format!("http://{page_addr}/"), websocket=%format!("ws://{ws_addr}/"), "remote viewer carrier listening");

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
                    tokio::join!(a, b);
                });
            })?;
        Ok(Self {
            inner,
            encoder: None,
            encoder_gen: 0,
            fps,
            encoder_args,
            last_frame_hash: None,
        })
    }

    /// True while a viewer session is active — the host skips rendering
    /// pixels entirely when nothing consumes them.
    pub fn connected(&self) -> bool {
        self.inner.connected.load(std::sync::atomic::Ordering::Acquire)
    }

    /// Latest pending runtime cadence request, if any.
    pub fn take_cadence(&mut self) -> Option<u32> {
        self.inner.cadence_request.lock().ok().and_then(|mut c| c.take())
    }

    /// Record the applied cadence so future hellos report it.
    pub fn set_hello_cadence(&mut self, cadence: u32) {
        if let Ok(mut hello) = self.inner.hello.lock() {
            hello.cadence = cadence;
        }
    }

    /// Drain wire input events into `out` for the render thread.
    pub fn drain_events(&mut self, out: &mut Vec<pb::input_event::Event>) {
        if let Ok(mut events) = self.inner.events.lock() {
            out.append(&mut events);
        }
    }

    /// Latest pending viewport-resize request, if any (latest wins; the
    /// render thread applies at tick granularity).
    pub fn take_resize(&mut self) -> Option<pb::ViewportResize> {
        self.inner.resize.lock().ok().and_then(|mut r| r.take())
    }

    /// Commit a new stream geometry (already clamped by the host): update
    /// the hello for future connections, stop the current encoder, and —
    /// if a viewer is connected — re-announce the hello through the
    /// outbound channel *before* the next encoder spawns, so the viewer
    /// resizes its canvas and rejoins at the new stream's first IDR.
    pub fn apply_geometry(&mut self, width_px: u32, height_px: u32, pixels_per_point: f32) {
        let mut hello = pb::SessionHello {
            width_px,
            height_px,
            pixels_per_point,
            cadence: 0,
        };
        if let Ok(mut guard) = self.inner.hello.lock() {
            hello.cadence = guard.cadence; // geometry changes don't touch cadence
            *guard = hello;
        }
        // Drop first: reap blocks until the old drain flushed its remaining
        // (old-geometry) access units into the channel, so the hello below
        // lands after them and before anything from the new stream.
        if self.encoder.take().is_some() {
            tracing::info!(width_px, height_px, pixels_per_point, "geometry change — encoder stopped for restart");
        }
        let tx = self.inner.video_tx.lock().ok().and_then(|g| g.clone());
        if let Some(tx) = tx {
            let msg = pb::SessionControl {
                control: Some(pb::session_control::Control::Hello(hello)),
            };
            let mut framed = Vec::with_capacity(1 + msg.encoded_len());
            framed.push(pb::PREFIX_SESSION);
            let _ = msg.encode(&mut framed);
            if tx.blocking_send(framed).is_err() {
                tracing::debug!("hello re-announce skipped — connection mid-teardown");
            }
        }
    }
}

impl WsCarrier {
    /// Feed one rendered frame. Frames whose pixels are identical to the
    /// previous fed one (blake3) are skipped — no encode, no wire
    /// traffic — except the first frame of a fresh encoder, which must
    /// exist for the stream to start at an IDR.
    pub fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, frame_idx: u64) {
        let connected = self.inner.connected.load(std::sync::atomic::Ordering::Acquire);
        let cur_gen = self.inner.conn_gen.load(std::sync::atomic::Ordering::Acquire);
        if !connected {
            if self.encoder.take().is_some() {
                tracing::info!("viewer disconnected — stopping encoder");
                self.last_frame_hash = None;
            }
            return;
        }
        if self.encoder.is_none() || self.encoder_gen != cur_gen {
            self.encoder = None; // reap a previous connection's encoder first
            self.last_frame_hash = None;
            let tx = self.inner.video_tx.lock().ok().and_then(|g| g.clone());
            if let Some(tx) = tx {
                match EncoderSink::new(width, height, self.fps, self.encoder_args.clone(), EncoderTarget::Channel(tx)) {
                    Ok(enc) => {
                        tracing::info!(conn_gen = cur_gen, "viewer connected — encoder started");
                        self.encoder = Some(enc);
                        self.encoder_gen = cur_gen;
                    }
                    Err(e) => {
                        tracing::error!(error=%e, "failed to start encoder for viewer session");
                        return;
                    }
                }
            } else {
                return; // connection is mid-teardown
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
            if inner.connected.load(std::sync::atomic::Ordering::Acquire) {
                // Single session at v1 (ADR-0024): reject while busy.
                tracing::info!(%peer, "rejecting second viewer connection (single-session v1)");
                drop(stream);
                return;
            }
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

    let (video_tx, mut video_rx) = tokio::sync::mpsc::channel::<Vec<u8>>(VIDEO_CHANNEL_CAP);
    if let Ok(mut guard) = inner.video_tx.lock() {
        *guard = Some(video_tx);
    }
    inner.conn_gen.fetch_add(1, std::sync::atomic::Ordering::AcqRel);
    inner.connected.store(true, std::sync::atomic::Ordering::Release);
    let _ = inner.waker.send(()); // a fresh viewer wants a frame promptly
    tracing::info!(%peer, "viewer connected");

    // First message: session hello with the current stream geometry
    // (SD6 0x03). Geometry changes mid-session re-announce the same
    // message through the outbound channel (see apply_geometry).
    let current_hello = inner.hello.lock().map(|h| *h).unwrap_or_default();
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
        loop {
            tokio::select! {
                chunk = video_rx.recv() => {
                    match chunk {
                        Some(payload) => {
                            if let Err(e) = ws_tx.send(tokio_tungstenite::tungstenite::Message::Binary(payload.into())).await {
                                break Err(e);
                            }
                        }
                        None => break Ok(()), // encoder side gone
                    }
                }
                msg = ws_rx.next() => {
                    match msg {
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Binary(data))) => {
                            handle_client_message(&data, &inner);
                        }
                        Some(Ok(tokio_tungstenite::tungstenite::Message::Close(_))) | None => break Ok(()),
                        Some(Ok(_)) => {} // text/ping/pong — ignore
                        Some(Err(e)) => break Err(e),
                    }
                }
            }
        }
    };

    inner.connected.store(false, std::sync::atomic::Ordering::Release);
    if let Ok(mut guard) = inner.video_tx.lock() {
        *guard = None;
    }
    let _ = inner.waker.send(()); // let the host reap the encoder promptly
    tracing::info!(%peer, "viewer disconnected");
    result
}

fn handle_client_message(data: &[u8], inner: &Inner) {
    let Some((&prefix, payload)) = data.split_first() else {
        return;
    };
    match prefix {
        pb::PREFIX_INPUT => match pb::InputEvent::decode(payload) {
            Ok(ev) => {
                if let Some(ev) = ev.event {
                    if let Ok(mut events) = inner.events.lock() {
                        events.push(ev);
                    }
                    let _ = inner.waker.send(()); // input wants a pass now
                }
            }
            Err(e) => tracing::debug!(error=%e, "undecodable input event"),
        },
        pb::PREFIX_SESSION => match pb::SessionControl::decode(payload) {
            Ok(ctl) => match ctl.control {
                Some(pb::session_control::Control::ViewportResize(r)) => {
                    if let Ok(mut resize) = inner.resize.lock() {
                        *resize = Some(r);
                    }
                    let _ = inner.waker.send(());
                }
                Some(pb::session_control::Control::SetCadence(c)) => {
                    if let Ok(mut pending) = inner.cadence_request.lock() {
                        *pending = Some(c.cadence);
                    }
                    let _ = inner.waker.send(());
                }
                Some(pb::session_control::Control::Ping(p)) => {
                    // The viewer pings with its decoded-frame count: a remote
                    // attestation that WebCodecs decode is working client-side.
                    tracing::info!(frames_decoded = p.nonce, "viewer decode-progress ping");
                }
                Some(pb::session_control::Control::Hello(_)) | None => {}
            },
            Err(e) => tracing::debug!(error=%e, "undecodable session control"),
        },
        other => tracing::debug!(prefix = other, "unknown message prefix from viewer"),
    }
}

