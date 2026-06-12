//! WebSocket carrier (ADR-0024 SD4/SD6, Phases 4–5).
//!
//! One TCP port serves the WebSocket data channel (`IMZERO2_HEADLESS_LISTEN`),
//! port+1 serves the embedded single-file viewer page over plain HTTP.
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
use crate::imzero2::framesink::FrameSink;
use crate::imzero2::inputproto as pb;
use futures_util::{SinkExt as _, StreamExt as _};
use prost::Message as _;

const VIDEO_CHANNEL_CAP: usize = 16;

struct Inner {
    /// Raw wire events, drained by the render thread each tick.
    events: std::sync::Mutex<Vec<pb::input_event::Event>>,
    /// Latest viewport-resize request (v1: logged, not yet applied).
    resize: std::sync::Mutex<Option<pb::ViewportResize>>,
    connected: std::sync::atomic::AtomicBool,
    /// Bumped on every accepted connection; the render thread compares it
    /// to decide when to (re)spawn the per-connection encoder.
    conn_gen: std::sync::atomic::AtomicU64,
    /// Sender for pre-framed video payloads of the *current* connection.
    video_tx: std::sync::Mutex<Option<tokio::sync::mpsc::Sender<Vec<u8>>>>,
    hello: pb::SessionHello,
}

pub struct WsCarrier {
    inner: std::sync::Arc<Inner>,
    encoder: Option<EncoderSink>,
    encoder_gen: u64,
    fps: f32,
    encoder_args: Vec<String>,
    resize_logged: bool,
}

impl WsCarrier {
    /// Bind `listen` (e.g. "127.0.0.1:8089") for WebSocket and `port+1`
    /// for the viewer page, then run both on a dedicated tokio thread.
    pub fn start(
        listen: &str,
        width_px: u32,
        height_px: u32,
        pixels_per_point: f32,
        fps: f32,
        encoder_args: Vec<String>,
    ) -> std::io::Result<Self> {
        let inner = std::sync::Arc::new(Inner {
            events: std::sync::Mutex::new(Vec::new()),
            resize: std::sync::Mutex::new(None),
            connected: std::sync::atomic::AtomicBool::new(false),
            conn_gen: std::sync::atomic::AtomicU64::new(0),
            video_tx: std::sync::Mutex::new(None),
            hello: pb::SessionHello {
                width_px,
                height_px,
                pixels_per_point,
            },
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
                    let ws = async {
                        match tokio::net::TcpListener::from_std(ws_listener) {
                            Ok(l) => accept_loop(l, inner_thread.clone()).await,
                            Err(e) => tracing::error!(error=%e, "ws listener conversion failed"),
                        }
                    };
                    let page = async {
                        match tokio::net::TcpListener::from_std(page_listener) {
                            Ok(l) => page_loop(l, ws_addr.port()).await,
                            Err(e) => tracing::error!(error=%e, "page listener conversion failed"),
                        }
                    };
                    tokio::join!(ws, page);
                });
            })?;
        Ok(Self {
            inner,
            encoder: None,
            encoder_gen: 0,
            fps,
            encoder_args,
            resize_logged: false,
        })
    }

    /// Drain wire input events into `out` for the render thread.
    pub fn drain_events(&mut self, out: &mut Vec<pb::input_event::Event>) {
        if let Ok(mut events) = self.inner.events.lock() {
            out.append(&mut events);
        }
        if !self.resize_logged {
            if let Ok(resize) = self.inner.resize.lock() {
                if let Some(r) = resize.as_ref() {
                    // ADR-0024 acceptance amendment: the schema carries the
                    // client pixel scale; applying it (texture + encoder
                    // restart at new geometry) is named Phase 5 work.
                    tracing::info!(logical_width=r.logical_width, logical_height=r.logical_height, pixel_scale=r.pixel_scale,
                        "viewer requested viewport resize — not applied at v1 (fixed geometry)");
                    self.resize_logged = true;
                }
            }
        }
    }
}

impl FrameSink for WsCarrier {
    fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, frame_idx: u64) {
        let connected = self.inner.connected.load(std::sync::atomic::Ordering::Acquire);
        let cur_gen = self.inner.conn_gen.load(std::sync::atomic::Ordering::Acquire);
        if !connected {
            if self.encoder.take().is_some() {
                tracing::info!("viewer disconnected — stopping encoder");
            }
            return;
        }
        if self.encoder.is_none() || self.encoder_gen != cur_gen {
            self.encoder = None; // reap a previous connection's encoder first
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
        if let Some(enc) = &mut self.encoder {
            enc.on_frame(bgra, width, height, frame_idx);
        }
    }
}

async fn accept_loop(listener: tokio::net::TcpListener, inner: std::sync::Arc<Inner>) {
    loop {
        let (stream, peer) = match listener.accept().await {
            Ok(x) => x,
            Err(e) => {
                tracing::error!(error=%e, "ws accept failed");
                continue;
            }
        };
        if inner.connected.load(std::sync::atomic::Ordering::Acquire) {
            // Single session at v1 (ADR-0024): reject while busy.
            tracing::info!(%peer, "rejecting second viewer connection (single-session v1)");
            drop(stream);
            continue;
        }
        let inner = inner.clone();
        tokio::spawn(async move {
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
    tracing::info!(%peer, "viewer connected");

    // First message: session hello with the stream geometry (SD6 0x03).
    let hello = pb::SessionControl {
        control: Some(pb::session_control::Control::Hello(inner.hello)),
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

/// Minimal HTTP responder for the embedded viewer page: every GET gets the
/// page (it is the only asset); the WebSocket port is templated in.
async fn page_loop(listener: tokio::net::TcpListener, ws_port: u16) {
    const VIEWER_HTML: &str = include_str!("viewer/index.html");
    let page = VIEWER_HTML.replace("{{WS_PORT}}", &ws_port.to_string());
    loop {
        let (mut stream, _) = match listener.accept().await {
            Ok(x) => x,
            Err(e) => {
                tracing::error!(error=%e, "page accept failed");
                continue;
            }
        };
        let page = page.clone();
        tokio::spawn(async move {
            use tokio::io::{AsyncReadExt as _, AsyncWriteExt as _};
            // Read whatever fits of the request and answer unconditionally;
            // a single-page server has no routing worth parsing.
            let mut buf = [0u8; 4096];
            let _ = stream.read(&mut buf).await;
            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                page.len(),
                page
            );
            let _ = stream.write_all(response.as_bytes()).await;
            let _ = stream.shutdown().await;
        });
    }
}
