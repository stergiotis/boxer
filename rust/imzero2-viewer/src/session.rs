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

/// How a `wss://` endpoint's certificate is checked (ADR-0243 §SD1; see its
/// 2026-09-19 update). Irrelevant to `ws://`, which has no certificate.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum TlsVerification {
    /// Chain to the public web roots and match the host name.
    #[default]
    Verify,
    /// Accept whatever certificate the server presents: no chain, no name,
    /// no expiry. The handshake signatures are still checked, so the session
    /// is still encrypted to *whoever* holds that certificate's key — which
    /// is exactly what goes unestablished. Anyone on the path can terminate
    /// the connection themselves and read the video and inject keystrokes
    /// and clipboard. For a self-signed host on a network you trust.
    SkipInsecure,
    /// Accept exactly one certificate: the one whose SHA-256 over its DER
    /// encoding is this value (the fingerprint a browser's certificate viewer
    /// and `openssl x509 -fingerprint -sha256` show). The pin replaces the
    /// chain, name and expiry checks rather than adding to them — that is
    /// what makes it work for a self-signed host — and the handshake
    /// signatures are checked as usual, so this is an authenticated channel
    /// to the holder of that certificate's key. Replacing the certificate
    /// means updating the pin.
    Pinned([u8; 32]),
}

/// Parse a SHA-256 certificate fingerprint as people copy it: 64 hex digits,
/// with or without `:`/`-`/space separators, either case, optionally still
/// carrying the `sha256 Fingerprint=` prefix `openssl x509` prints.
pub fn parse_sha256_fingerprint(text: &str) -> anyhow::Result<[u8; 32]> {
    let text = text.rsplit('=').next().unwrap_or_default();
    let digits: String = text
        .chars()
        .filter(|c| !matches!(c, ':' | '-') && !c.is_whitespace())
        .collect();
    anyhow::ensure!(
        digits.len() == 64 && digits.bytes().all(|b| b.is_ascii_hexdigit()),
        "a SHA-256 pin is 64 hex digits (colons optional); got {} characters",
        digits.len()
    );
    let mut out = [0u8; 32];
    for (i, byte) in out.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&digits[2 * i..2 * i + 2], 16)?;
    }
    Ok(out)
}

/// The colon-separated uppercase spelling browsers and openssl display.
pub fn format_sha256_fingerprint(fingerprint: &[u8; 32]) -> String {
    fingerprint
        .iter()
        .map(|b| format!("{b:02X}"))
        .collect::<Vec<_>>()
        .join(":")
}

fn certificate_sha256(der: &[u8]) -> [u8; 32] {
    let digest = ring::digest::digest(&ring::digest::SHA256, der);
    let mut out = [0u8; 32];
    out.copy_from_slice(digest.as_ref());
    out
}

pub enum Command {
    Connect(String, TlsVerification),
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
    // Explicit rather than left to rustls's feature-based auto-detection,
    // which panics once a second provider feature appears anywhere in the
    // graph. Err means one is already installed, which is equally fine.
    let _ = rustls::crypto::ring::default_provider().install_default();
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
            c=command(&mut *rx)=>match c{Command::Stop=>return Some(Command::Stop),Command::Connect(..)=>next=Some(c),_=>{}},
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
        let (endpoint, tls) = match c {
            Command::Stop => return,
            Command::Connect(e, tls) => (e, tls),
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
        // None keeps tokio-tungstenite's own verifying rustls setup; only an
        // explicit skip on a wss:// endpoint replaces it.
        let connector = match certificate_rule(tls) {
            Some(rule) if url.scheme() == "wss" => match custom_tls(rule) {
                Ok(c) => Some(tokio_tungstenite::Connector::Rustls(c)),
                Err(e) => {
                    pending = terminal(&mut rx, &events, format!("TLS setup failed: {e}")).await;
                    continue;
                }
            },
            _ => None,
        };
        let connect = tokio::time::timeout(
            Duration::from_secs(10),
            tokio_tungstenite::connect_async_tls_with_config(
                url.as_str(),
                Some(config),
                true,
                connector,
            ),
        );
        tokio::pin!(connect);
        let socket = loop {
            tokio::select! {
                c=command(&mut rx)=>match c {Command::Stop=>return,Command::Disconnect=>break Err("Disconnected".into()),Command::Connect(e,t)=>{pending=Some(Command::Connect(e,t));break Err("Connection replaced".into());},_=>{}},
                result=&mut connect=>break match result{Ok(Ok((ws,_)))=>Ok(ws),Ok(Err(e))=>Err(format!("Connection to {} failed: {}",redact(&url),describe_connect_error(&e,url.scheme()))),Err(_)=>Err("Connection timed out".into())},
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
                    Command::Connect(e,t)=>return ("Connection replaced".into(),Some(Command::Connect(e,t))),
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
/// Which certificate a non-default connection accepts; `None` is the
/// default (tokio-tungstenite's own web-roots verifier, left untouched).
#[derive(Clone, Copy, Debug)]
enum CertificateRule {
    AcceptAny,
    Pinned([u8; 32]),
}

fn certificate_rule(tls: TlsVerification) -> Option<CertificateRule> {
    match tls {
        TlsVerification::Verify => None,
        TlsVerification::SkipInsecure => Some(CertificateRule::AcceptAny),
        TlsVerification::Pinned(pin) => Some(CertificateRule::Pinned(pin)),
    }
}

/// Client TLS configuration for [`TlsVerification::SkipInsecure`] and
/// [`TlsVerification::Pinned`].
fn custom_tls(rule: CertificateRule) -> anyhow::Result<std::sync::Arc<rustls::ClientConfig>> {
    let provider = std::sync::Arc::new(rustls::crypto::ring::default_provider());
    let config = rustls::ClientConfig::builder_with_provider(provider.clone())
        .with_safe_default_protocol_versions()?
        .dangerous()
        .with_custom_certificate_verifier(std::sync::Arc::new(CertificateCheck { rule, provider }))
        .with_no_client_auth();
    Ok(std::sync::Arc::new(config))
}

/// Replaces the chain/name/expiry checks with [`CertificateRule`], and keeps
/// the handshake-signature checks, which prove the server holds the key of
/// the certificate it presented. For `AcceptAny` that is what `curl -k`
/// checks; for `Pinned` it makes the pin an identity check. Only
/// [`custom_tls`] constructs it.
#[derive(Debug)]
struct CertificateCheck {
    rule: CertificateRule,
    provider: std::sync::Arc<rustls::crypto::CryptoProvider>,
}

/// The rejection a pin produces, carried inside rustls's error so
/// [`describe_connect_error`] can report what the server presented.
#[derive(Debug)]
struct PinMismatch {
    presented: [u8; 32],
}

impl std::fmt::Display for PinMismatch {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "the server presented certificate SHA-256 {}, which is not the pinned one",
            format_sha256_fingerprint(&self.presented)
        )
    }
}

impl std::error::Error for PinMismatch {}

impl rustls::client::danger::ServerCertVerifier for CertificateCheck {
    fn verify_server_cert(
        &self,
        end_entity: &rustls::pki_types::CertificateDer<'_>,
        _intermediates: &[rustls::pki_types::CertificateDer<'_>],
        _server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp_response: &[u8],
        _now: rustls::pki_types::UnixTime,
    ) -> Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        match self.rule {
            CertificateRule::AcceptAny => {}
            CertificateRule::Pinned(pin) => {
                let presented = certificate_sha256(end_entity.as_ref());
                if presented != pin {
                    return Err(rustls::Error::InvalidCertificate(
                        rustls::CertificateError::Other(rustls::OtherError(std::sync::Arc::new(
                            PinMismatch { presented },
                        ))),
                    ));
                }
            }
        }
        Ok(rustls::client::danger::ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &rustls::pki_types::CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.provider
            .signature_verification_algorithms
            .supported_schemes()
    }
}

/// The [`PinMismatch`] inside a failed TLS handshake, if that is what failed.
fn pin_mismatch(io: &std::io::Error) -> Option<&PinMismatch> {
    match io.get_ref()?.downcast_ref::<rustls::Error>()? {
        rustls::Error::InvalidCertificate(rustls::CertificateError::Other(other)) => {
            other.0.downcast_ref::<PinMismatch>()
        }
        _ => None,
    }
}

/// Name a failed connect in terms of what to check next.
///
/// Three failures with unrelated fixes used to share one message — nothing
/// listening, a TLS handshake that did not complete, and an HTTP answer that
/// was not an upgrade — and the host logs none of them, because in each the
/// request never reaches its session handler. So the only place the cause is
/// known is here.
///
/// Nothing in the returned text comes from the URL: I/O and TLS errors carry
/// no URL, and an HTTP answer is reduced to its status. The caller prefixes
/// [`redact`]'s origin, which keeps the path and query (where a proxy token
/// would ride) out of the status bar.
fn describe_connect_error(e: &tokio_tungstenite::tungstenite::Error, scheme: &str) -> String {
    use std::io::ErrorKind;
    use tokio_tungstenite::tungstenite::Error;
    match e {
        Error::Io(io) => match io.kind() {
            ErrorKind::ConnectionRefused => {
                "nothing accepted the connection at that host and port. Check the port, and \
                 that the host's IMZERO2_HEADLESS_LISTEN is not a loopback address when this \
                 viewer runs on another machine"
                    .into()
            }
            ErrorKind::TimedOut | ErrorKind::HostUnreachable | ErrorKind::NetworkUnreachable => {
                format!("the host is unreachable from here ({io})")
            }
            // tokio-rustls reports a failed handshake as InvalidData
            // wrapping the rustls error, whose text names the cause
            // ("invalid peer certificate: UnknownIssuer", …).
            ErrorKind::InvalidData if scheme == "wss" => {
                if let Some(mismatch) = pin_mismatch(io) {
                    return format!(
                        "{mismatch}. If the host's certificate was replaced, update the pin; \
                         if not, something between here and the host is presenting its own"
                    );
                }
                let hint = if io.to_string().contains("invalid peer certificate") {
                    "This viewer trusts only the public web roots, not the Windows \
                     certificate store, so a certificate a browser accepts can still fail \
                     here. For a self-signed host, pin its SHA-256 fingerprint (the Pin \
                     field, or --pin-sha256); skipping verification (menu, or --insecure) \
                     also connects, but to anyone who answers"
                } else {
                    "If nothing terminates TLS in front of the host, use ws:// instead"
                };
                format!("the TLS handshake failed ({io}). {hint}")
            }
            _ => format!("network error ({io})"),
        },
        Error::Http(response) => {
            let status = response.status();
            let hint = if status.is_redirection() {
                "a redirect, usually a login or authentication proxy that the browser \
                 satisfied with a cookie"
            } else if status == 401 || status == 403 {
                "an authentication proxy that the browser satisfied and this viewer cannot"
            } else if status.is_success() {
                "an HTTP server that did not treat the request as a WebSocket upgrade"
            } else {
                "something in front of the host, or a different service on this port"
            };
            format!("the server answered HTTP {status} instead of switching protocols: {hint}")
        }
        Error::Protocol(p) => format!("the WebSocket handshake was rejected ({p})"),
        other => other.to_string(),
    }
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
        tx.blocking_send(Command::Connect(
            format!("ws://127.0.0.1:{port}"),
            TlsVerification::Verify,
        ))
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

    /// Connect through the real worker and return the reason it reports.
    fn connect_failure(endpoint: String) -> String {
        connect_failure_with(endpoint, TlsVerification::Verify)
    }

    fn connect_failure_with(endpoint: String, tls: TlsVerification) -> String {
        let (tx, rx, worker) = spawn();
        tx.blocking_send(Command::Connect(endpoint, tls)).unwrap();
        let reason = match rx.recv_timeout(Duration::from_secs(10)).unwrap() {
            Event::Disconnected(reason) => reason,
            other => panic!("expected a failed connect, got {other:?}"),
        };
        tx.blocking_send(Command::Stop).unwrap();
        worker.join().unwrap();
        reason
    }

    /// A one-shot peer that answers whatever arrives the way the carrier's
    /// page branch (`serve_page`) does: an HTML page, never an upgrade.
    fn page_server() -> u16 {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        thread::spawn(move || {
            use std::io::{Read as _, Write as _};
            let (mut s, _) = listener.accept().unwrap();
            let _ = s.read(&mut [0u8; 4096]);
            let body = "<!doctype html>";
            let _ = write!(
                s,
                "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
        });
        port
    }

    #[test]
    fn refused_connect_says_nothing_is_listening() {
        let port = std::net::TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port(); // listener dropped here: the port is now closed
        let reason = connect_failure(format!("ws://127.0.0.1:{port}/ws"));
        assert!(reason.contains("nothing accepted"), "{reason}");
    }

    #[test]
    fn page_instead_of_upgrade_names_the_status() {
        let reason = connect_failure(format!("ws://127.0.0.1:{}/ws", page_server()));
        assert!(reason.contains("HTTP 200"), "{reason}");
        assert!(
            reason.contains("did not treat the request as a WebSocket"),
            "{reason}"
        );
    }

    /// `wss://` against the carrier itself, which has no TLS: its sniff finds
    /// no upgrade header in a ClientHello and answers with the page. The host
    /// logs nothing, so this message is the only record of the cause.
    #[test]
    fn wss_against_a_plain_carrier_names_tls() {
        let reason = connect_failure(format!("wss://127.0.0.1:{}/ws", page_server()));
        assert!(reason.contains("TLS handshake failed"), "{reason}");
        assert!(reason.contains("use ws://"), "{reason}");
    }

    #[test]
    fn connect_failures_do_not_leak_path_or_query() {
        let reason = connect_failure(format!(
            "ws://127.0.0.1:{}/prefix/ws?token=secret",
            page_server()
        ));
        assert!(
            !reason.contains("secret") && !reason.contains("prefix"),
            "{reason}"
        );
    }

    /// A TLS WebSocket peer presenting `testdata/self-signed-localhost.*`:
    /// a throwaway P-256 key, committed only for these tests, with a
    /// certificate for `localhost` and `127.0.0.1` valid until 2126. Made with
    /// `openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes
    /// -days 36500 -subj /CN=localhost -addext
    /// subjectAltName=DNS:localhost,IP:127.0.0.1`, then converted to DER.
    fn self_signed_wss_server() -> u16 {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        listener.set_nonblocking(true).unwrap();
        thread::spawn(move || {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .unwrap()
                .block_on(async move {
                    use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};
                    let cert = CertificateDer::from(FIXTURE_CERT.to_vec());
                    let key = PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(
                        include_bytes!("../testdata/self-signed-localhost.key.der").to_vec(),
                    ));
                    let config = rustls::ServerConfig::builder_with_provider(std::sync::Arc::new(
                        rustls::crypto::ring::default_provider(),
                    ))
                    .with_safe_default_protocol_versions()
                    .unwrap()
                    .with_no_client_auth()
                    .with_single_cert(vec![cert], key)
                    .unwrap();
                    let acceptor = tokio_rustls::TlsAcceptor::from(std::sync::Arc::new(config));
                    let listener = tokio::net::TcpListener::from_std(listener).unwrap();
                    let (tcp, _) = listener.accept().await.unwrap();
                    // A verifying client aborts here; that is the point.
                    let Ok(tls) = acceptor.accept(tcp).await else {
                        return;
                    };
                    let Ok(mut ws) = tokio_tungstenite::accept_async(tls).await else {
                        return;
                    };
                    let _ = ws.close(None).await;
                })
        });
        port
    }

    #[test]
    fn self_signed_certificate_is_refused_by_default() {
        let reason = connect_failure(format!("wss://127.0.0.1:{}/ws", self_signed_wss_server()));
        assert!(reason.contains("invalid peer certificate"), "{reason}");
        assert!(reason.contains("--insecure"), "{reason}");
    }

    #[test]
    fn skip_insecure_connects_to_a_self_signed_host() {
        let port = self_signed_wss_server();
        let (tx, rx, worker) = spawn();
        tx.blocking_send(Command::Connect(
            format!("wss://127.0.0.1:{port}/ws"),
            TlsVerification::SkipInsecure,
        ))
        .unwrap();
        assert_eq!(
            rx.recv_timeout(Duration::from_secs(10)).unwrap(),
            Event::Connected
        );
        tx.blocking_send(Command::Stop).unwrap();
        worker.join().unwrap();
    }

    const FIXTURE_CERT: &[u8] = include_bytes!("../testdata/self-signed-localhost.cert.der");

    #[test]
    fn fingerprint_parses_the_spellings_people_paste() {
        let pin = certificate_sha256(FIXTURE_CERT);
        let canonical = format_sha256_fingerprint(&pin);
        assert_eq!(canonical.len(), 95, "32 pairs and 31 colons");
        let bare: String = canonical.chars().filter(|c| *c != ':').collect();
        for spelling in [
            canonical.clone(),
            canonical.to_lowercase(),
            bare.clone(),
            format!("  {bare}\n"),
            canonical.replace(':', " "),
            format!("sha256 Fingerprint={canonical}"),
        ] {
            assert_eq!(
                parse_sha256_fingerprint(&spelling).unwrap(),
                pin,
                "{spelling:?}"
            );
        }
        for bad in [
            "",
            &bare[..62],
            &format!("{bare}00"),
            &bare.replace('A', "G"),
        ] {
            assert!(parse_sha256_fingerprint(bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn matching_pin_connects_to_a_self_signed_host() {
        let port = self_signed_wss_server();
        let (tx, rx, worker) = spawn();
        tx.blocking_send(Command::Connect(
            format!("wss://127.0.0.1:{port}/ws"),
            TlsVerification::Pinned(certificate_sha256(FIXTURE_CERT)),
        ))
        .unwrap();
        assert_eq!(
            rx.recv_timeout(Duration::from_secs(10)).unwrap(),
            Event::Connected
        );
        tx.blocking_send(Command::Stop).unwrap();
        worker.join().unwrap();
    }

    #[test]
    fn wrong_pin_is_refused_and_names_what_was_presented() {
        let mut wrong = certificate_sha256(FIXTURE_CERT);
        wrong[0] ^= 0xff;
        let reason = connect_failure_with(
            format!("wss://127.0.0.1:{}/ws", self_signed_wss_server()),
            TlsVerification::Pinned(wrong),
        );
        let presented = format_sha256_fingerprint(&certificate_sha256(FIXTURE_CERT));
        assert!(reason.contains(&presented), "{reason}");
        assert!(reason.contains("not the pinned one"), "{reason}");
        assert!(reason.contains("update the pin"), "{reason}");
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
