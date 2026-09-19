//! Bounded WebSocket worker. Overflow abandons the stream, never its references
//! (ADR-0243 §SD4). The terminal event remains pending while Stop stays serviceable.
use crate::wire::{self, pb};
use futures_util::{SinkExt, StreamExt};
use std::{
    sync::mpsc::{self, Receiver, SyncSender, TrySendError},
    thread,
    time::Duration,
};
use tokio_tungstenite::{
    tungstenite::{protocol::WebSocketConfig, Message},
    MaybeTlsStream, WebSocketStream,
};

pub enum Command {
    Connect(String),
    Disconnect,
    Send(pb::SessionControl),
    Input(pb::InputEvent),
    Stop,
}
#[derive(Debug, Clone, PartialEq)]
pub enum Event {
    Connected,
    Disconnected(String),
    Hello(pb::SessionHello),
    Control(pb::SessionControl),
    Video(pb::VideoChunk),
}
const MAX_BYTES: usize = 16 * 1024 * 1024;
const WRITE_TIMEOUT: Duration = Duration::from_secs(2);
type Socket = WebSocketStream<MaybeTlsStream<tokio::net::TcpStream>>;

/// Start the network worker.
///
/// The two channels are deliberately of different kinds, because their two
/// ends are:
///
/// - **Commands** (shell → worker) are a [`tokio::sync::mpsc`] pair. The
///   worker's only job while idle is to wait for one, so it awaits the
///   receiver and is woken by the send. The sender half is still usable from
///   the Win32 message loop without a runtime: `try_send`/`blocking_send`
///   are not `async`.
/// - **Events** (worker → shell) stay a [`std::sync::mpsc`] pair, because
///   the consumer is a `WM_TIMER` handler that must never block or await;
///   it drains with `try_recv` on the shell's own cadence.
///
/// Both are bounded (ADR-0243 §SD4); nothing here ever blocks the render
/// thread or the message loop.
pub fn spawn() -> (
    tokio::sync::mpsc::Sender<Command>,
    Receiver<Event>,
    thread::JoinHandle<()>,
) {
    let (tx, rx) = tokio::sync::mpsc::channel(64);
    let (events, out) = mpsc::sync_channel(16);
    let worker = thread::spawn(move || {
        match tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
        {
            Ok(rt) => rt.block_on(run(rx, events)),
            Err(e) => {
                let _ = events.try_send(Event::Disconnected(format!("start network runtime: {e}")));
            }
        }
    });
    (tx, out, worker)
}
/// Await the next command. A closed channel means the shell is gone, which
/// is [`Command::Stop`].
///
/// Cancel-safe, which every `tokio::select!` below relies on:
/// `Receiver::recv` guarantees no message is consumed when another branch
/// wins the race.
async fn command(rx: &mut tokio::sync::mpsc::Receiver<Command>) -> Command {
    rx.recv().await.unwrap_or(Command::Stop)
}
async fn terminal(
    rx: &mut tokio::sync::mpsc::Receiver<Command>,
    events: &SyncSender<Event>,
    reason: String,
) -> Option<Command> {
    let mut event = Event::Disconnected(reason);
    let mut next = None;
    loop {
        match events.try_send(event) {
            Ok(()) => return next,
            Err(TrySendError::Disconnected(_)) => return Some(Command::Stop),
            Err(TrySendError::Full(e)) => event = e,
        }
        tokio::select! {
            c=command(&mut *rx)=>match c{Command::Stop=>return Some(Command::Stop),Command::Connect(_)=>next=Some(c),_=>{}},
            _=tokio::time::sleep(Duration::from_millis(2))=>{},
        }
    }
}
async fn run(mut rx: tokio::sync::mpsc::Receiver<Command>, events: SyncSender<Event>) {
    let mut pending = None;
    loop {
        let c = match pending.take() {
            Some(c) => c,
            None => command(&mut rx).await,
        };
        let endpoint = match c {
            Command::Stop => return,
            Command::Connect(e) => e,
            _ => continue,
        };
        let url = match validate_endpoint(&endpoint) {
            Ok(u) => u,
            Err(e) => {
                pending = terminal(&mut rx, &events, e.to_string()).await;
                continue;
            }
        };
        let config = WebSocketConfig::default()
            .max_message_size(Some(MAX_BYTES))
            .max_frame_size(Some(MAX_BYTES));
        let connect = tokio::time::timeout(
            Duration::from_secs(10),
            tokio_tungstenite::connect_async_with_config(url.as_str(), Some(config), true),
        );
        tokio::pin!(connect);
        let socket = loop {
            tokio::select! {
                c=command(&mut rx)=>match c {Command::Stop=>return,Command::Disconnect=>break Err("Disconnected".into()),Command::Connect(e)=>{pending=Some(Command::Connect(e));break Err("Connection replaced".into());},_=>{}},
                result=&mut connect=>break match result{Ok(Ok((ws,_)))=>Ok(ws),Ok(Err(_))=>Err(format!("Connection to {} failed (transport/TLS/handshake)",redact(&url))),Err(_)=>Err("Connection timed out".into())},
            }
        };
        let reason = match socket {
            Ok(mut ws) => {
                if events.try_send(Event::Connected).is_err() {
                    "Event backlog exceeded limit".into()
                } else {
                    let (reason, next) = connected(&mut ws, &mut rx, &events).await;
                    pending = next;
                    reason
                }
            }
            Err(reason) => reason,
        };
        if matches!(pending, Some(Command::Stop)) {
            return;
        }
        let next = terminal(&mut rx, &events, reason).await;
        if next.is_some() {
            pending = next;
        }
    }
}
async fn write(ws: &mut Socket, msg: Message) -> anyhow::Result<()> {
    tokio::time::timeout(WRITE_TIMEOUT, ws.send(msg))
        .await
        .map_err(|_| anyhow::anyhow!("WebSocket write timeout"))??;
    Ok(())
}
async fn connected(
    ws: &mut Socket,
    rx: &mut tokio::sync::mpsc::Receiver<Command>,
    events: &SyncSender<Event>,
) -> (String, Option<Command>) {
    let mut clock = tokio::time::interval(Duration::from_secs(5));
    clock.tick().await;
    let mut activity = tokio::time::Instant::now();
    loop {
        tokio::select! {
            c=command(&mut *rx)=>{
                let message=match c{
                    Command::Stop=>return ("Stopped".into(),Some(Command::Stop)),
                    Command::Disconnect=>return ("Disconnected".into(),None),
                    Command::Connect(e)=>return ("Connection replaced".into(),Some(Command::Connect(e))),
                    Command::Send(c)=>Message::Binary(wire::framed(wire::PREFIX_SESSION,&c).into()),
                    Command::Input(i)=>Message::Binary(wire::framed(wire::PREFIX_INPUT,&i).into()),
                };
                // Writes have a hard short deadline; no unbounded close handshake.
                if write(ws,message).await.is_err(){return ("WebSocket send failed or timed out".into(),None);}
            },
            _=clock.tick()=>{
                if activity.elapsed()>Duration::from_secs(20){return ("Peer liveness timeout".into(),None);}
                if write(ws,Message::Ping(Vec::new().into())).await.is_err(){return ("WebSocket ping failed".into(),None);}
            },
            incoming=ws.next()=>{
                activity=tokio::time::Instant::now();
                let message=match incoming{Some(Ok(m))=>m,Some(Err(_))=>return ("WebSocket receive failed".into(),None),None=>return ("Peer disconnected".into(),None)};
                let event=match message {
                    Message::Binary(b)=>match wire::decode(&b){
                        Ok(wire::ServerMessage::Video(f))=>Event::Video(f),
                        Ok(wire::ServerMessage::Control(c))=>{
                            if let Some(pb::session_control::Control::Hello(h))=&c.control {
                                if events.try_send(Event::Hello(h.clone())).is_err(){return ("Stream state backlog exceeded limit".into(),None);}
                            }
                            Event::Control(c)
                        },
                        Err(e)=>return (format!("Invalid server message: {e}"),None),
                    },
                    Message::Ping(p)=>{if write(ws,Message::Pong(p)).await.is_err(){return ("WebSocket pong failed".into(),None);}continue;},
                    Message::Pong(_)=>continue,
                    Message::Close(_)=>return ("Peer closed the connection".into(),None),
                    _=>return ("Unexpected non-binary server message".into(),None),
                };
                if events.try_send(event).is_err(){return ("Event backlog exceeded limit; reconnect for a fresh keyframe".into(),None);}
            }
        }
    }
}
fn validate_endpoint(raw: &str) -> anyhow::Result<url::Url> {
    anyhow::ensure!(raw.len() <= 4096, "endpoint URL too long");
    let url = url::Url::parse(raw)?;
    anyhow::ensure!(
        matches!(url.scheme(), "ws" | "wss") && url.host_str().is_some(),
        "endpoint requires ws:// or wss:// and a host"
    );
    anyhow::ensure!(
        url.username().is_empty() && url.password().is_none(),
        "credentials in endpoint URLs are not supported"
    );
    anyhow::ensure!(
        url.fragment().is_none(),
        "endpoint fragments are not supported"
    );
    Ok(url)
}
fn redact(url: &url::Url) -> String {
    url.origin().ascii_serialization()
}
pub fn client_hello(label: impl Into<String>) -> pb::SessionControl {
    pb::SessionControl {
        control: Some(pb::session_control::Control::ClientHello(pb::ClientHello {
            webcodecs: true,
            label: label.into(),
        })),
    }
}
pub fn capabilities(width: u32, height: u32) -> pb::SessionControl {
    let pixels = u64::from(width) * u64::from(height);
    let macroblocks = u64::from(width.div_ceil(16)) * u64::from(height.div_ceil(16));
    let h264 = if macroblocks <= 3600 {
        31
    } else if macroblocks <= 8192 {
        40
    } else if macroblocks <= 8704 {
        42
    } else {
        51
    };
    let vp9 = if pixels <= 921600 {
        31
    } else if pixels <= 2228224 {
        40
    } else if pixels <= 8912896 {
        50
    } else {
        60
    };
    let av1 = if pixels <= 2359296 {
        8
    } else if pixels <= 8912896 {
        12
    } else {
        16
    };
    let supported = width > 0 && height > 0 && width <= 8192 && height <= 8192;
    let codecs = [
        format!("avc1.42E0{h264:02X}"),
        format!("vp09.00.{vp9:02}.08"),
        format!("av01.0.{av1:02}M.08"),
    ]
    .into_iter()
    .map(|codec| pb::CodecCapability {
        codec,
        supported,
        smooth: false,
        power_efficient: false,
    })
    .collect();
    pb::SessionControl {
        control: Some(pb::session_control::Control::DecodeCapabilities(
            pb::DecodeCapabilities {
                codecs,
                webgl2: false,
            },
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use prost::Message as _;
    #[test]
    fn endpoint_validation_and_redaction() {
        for s in [
            "http://localhost/",
            "ws://user:secret@host/",
            "wss://host/#secret",
        ] {
            assert!(validate_endpoint(s).is_err());
        }
        let u = validate_endpoint("wss://example.invalid:8443/prefix/ws?secret=1").unwrap();
        assert!(!redact(&u).contains("secret"));
        assert_eq!(u.path(), "/prefix/ws");
    }
    #[test]
    fn caps_are_conservative() {
        let c = capabilities(1920, 1080);
        let Some(pb::session_control::Control::DecodeCapabilities(c)) = c.control else {
            panic!()
        };
        assert!(!c.webgl2);
        assert_eq!(c.codecs.len(), 3);
        for c in c.codecs {
            assert!(c.supported);
            assert!(!c.smooth && !c.power_efficient);
        }
    }
    #[test]
    fn localhost_roundtrip() {
        let (port_tx, port_rx) = mpsc::sync_channel(1);
        let server = thread::spawn(move || {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .unwrap()
                .block_on(async move {
                    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
                    port_tx.send(listener.local_addr().unwrap().port()).unwrap();
                    let (tcp, _) = listener.accept().await.unwrap();
                    let mut ws = tokio_tungstenite::accept_async(tcp).await.unwrap();
                    let hello = pb::SessionControl {
                        control: Some(pb::session_control::Control::Hello(pb::SessionHello {
                            width_px: 320,
                            height_px: 180,
                            pixels_per_point: 1.0,
                            ..Default::default()
                        })),
                    };
                    ws.send(Message::Binary(
                        wire::framed(wire::PREFIX_SESSION, &hello).into(),
                    ))
                    .await
                    .unwrap();
                    let Message::Binary(bytes) = ws.next().await.unwrap().unwrap() else {
                        panic!()
                    };
                    assert_eq!(bytes[0], wire::PREFIX_SESSION);
                    let c = pb::SessionControl::decode(&bytes[1..]).unwrap();
                    assert!(matches!(
                        c.control,
                        Some(pb::session_control::Control::ClientHello(_))
                    ));
                    let f = pb::VideoChunk {
                        keyframe: true,
                        data: vec![1, 2, 3],
                        ..Default::default()
                    };
                    ws.send(Message::Binary(wire::framed(wire::PREFIX_VIDEO, &f).into()))
                        .await
                        .unwrap();
                    ws.close(None).await.unwrap();
                })
        });
        let (tx, rx, worker) = spawn();
        let port = port_rx.recv_timeout(Duration::from_secs(5)).unwrap();
        tx.blocking_send(Command::Connect(format!("ws://127.0.0.1:{port}")))
            .unwrap();
        assert_eq!(
            rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            Event::Connected
        );
        tx.blocking_send(Command::Send(client_hello("test")))
            .unwrap();
        assert!(matches!(
            rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            Event::Hello(_)
        ));
        assert!(matches!(
            rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            Event::Control(_)
        ));
        assert!(matches!(
            rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            Event::Video(_)
        ));
        assert!(matches!(
            rx.recv_timeout(Duration::from_secs(5)).unwrap(),
            Event::Disconnected(_)
        ));
        tx.blocking_send(Command::Stop).unwrap();
        worker.join().unwrap();
        server.join().unwrap();
    }
    #[test]
    fn terminal_backpressure_does_not_block_stop() {
        let (tx, mut rx) = tokio::sync::mpsc::channel(2);
        let (events, _out) = mpsc::sync_channel(1);
        events.send(Event::Connected).unwrap();
        tx.try_send(Command::Stop).unwrap();
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        assert!(matches!(
            rt.block_on(terminal(&mut rx, &events, "overflow".into())),
            Some(Command::Stop)
        ));
    }

    /// Pins the two contracts [`command`] has to keep now that it awaits the
    /// channel instead of polling it: an empty channel yields nothing (it
    /// must not invent a command to break its own wait), and a closed one is
    /// [`Command::Stop`], which is the worker's only exit when the shell is
    /// gone. That second mapping was explicit in the polling version
    /// (`TryRecvError::Disconnected`) and is now `recv`'s `None`.
    ///
    /// It does not, and cannot from in here, observe *how* the wait is
    /// implemented — a 2 ms poll loop would satisfy both assertions too.
    #[test]
    fn command_waits_when_empty_and_stops_when_closed() {
        let (tx, mut rx) = tokio::sync::mpsc::channel(4);
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        rt.block_on(async {
            assert!(
                tokio::time::timeout(Duration::from_millis(100), command(&mut rx))
                    .await
                    .is_err(),
                "an idle worker must not produce a command"
            );
            tx.send(Command::Disconnect).await.unwrap();
            assert!(matches!(
                tokio::time::timeout(Duration::from_millis(100), command(&mut rx)).await,
                Ok(Command::Disconnect)
            ));
        });
        // A dropped shell is Stop, so the worker always has a way to exit.
        drop(tx);
        assert!(matches!(rt.block_on(command(&mut rx)), Command::Stop));
    }
}
