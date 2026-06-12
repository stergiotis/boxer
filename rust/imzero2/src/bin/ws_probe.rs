//! WebSocket probe client for the headless remote carrier (ADR-0024
//! Phase 5 verification; `required-features = ["headless"]`).
//!
//! Connects like the browser viewer would, prints the session hello,
//! appends every VideoChunk payload to an Annex-B file (ffprobe-able),
//! and optionally injects synthetic input mid-stream — a mouse move plus
//! click — so input round-tripping is verifiable by decoding the stream
//! afterwards and looking at the UI change.
//!
//! Usage:
//!   imzero2_ws_probe ws://127.0.0.1:8089/ /tmp/probe.h264 <num_aus> [click_x click_y after_au]

use futures_util::{SinkExt as _, StreamExt as _};
use imzero2::imzero2::inputproto as pb;
use prost::Message as _;

fn framed(prefix: u8, msg: &impl prost::Message) -> Vec<u8> {
    let mut out = Vec::with_capacity(1 + msg.encoded_len());
    out.push(prefix);
    let _ = msg.encode(&mut out);
    out
}

fn input_event(ev: pb::input_event::Event) -> Vec<u8> {
    framed(pb::PREFIX_INPUT, &pb::InputEvent { event: Some(ev) })
}

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 4 {
        eprintln!("usage: {} <ws-url> <out.h264> <num_aus> [click_x click_y after_au]", args.first().map(String::as_str).unwrap_or("ws_probe"));
        std::process::exit(2);
    }
    let url = args.get(1).expect("url");
    let out_path = args.get(2).expect("out");
    let num_aus: u64 = args.get(3).and_then(|v| v.parse().ok()).unwrap_or(90);
    let click: Option<(f32, f32, u64)> = match (args.get(4), args.get(5), args.get(6)) {
        (Some(x), Some(y), Some(after)) => Some((
            x.parse().expect("click_x"),
            y.parse().expect("click_y"),
            after.parse().expect("after_au"),
        )),
        _ => None,
    };

    let (ws, _resp) = tokio_tungstenite::connect_async(url)
        .await
        .expect("websocket connect failed");
    let (mut tx, mut rx) = ws.split();
    let mut out = std::fs::File::create(out_path).expect("create out file");
    let mut aus: u64 = 0;
    let mut bytes: u64 = 0;
    let mut keyframes: u64 = 0;
    let mut clicked = false;

    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(30);
    while aus < num_aus && std::time::Instant::now() < deadline {
        let Some(msg) = rx.next().await else { break };
        let msg = msg.expect("websocket read failed");
        let tokio_tungstenite::tungstenite::Message::Binary(data) = msg else {
            continue;
        };
        let Some((&prefix, payload)) = data.split_first() else {
            continue;
        };
        match prefix {
            pb::PREFIX_SESSION => {
                if let Ok(ctl) = pb::SessionControl::decode(payload) {
                    if let Some(pb::session_control::Control::Hello(h)) = ctl.control {
                        eprintln!(
                            "hello: {}x{} @ppp {}",
                            h.width_px, h.height_px, h.pixels_per_point
                        );
                    }
                }
            }
            pb::PREFIX_VIDEO => {
                if let Ok(chunk) = pb::VideoChunk::decode(payload) {
                    aus += 1;
                    bytes += chunk.data.len() as u64;
                    if chunk.keyframe {
                        keyframes += 1;
                    }
                    std::io::Write::write_all(&mut out, &chunk.data).expect("write au");
                    if let Some((x, y, after)) = click {
                        if !clicked && aus >= after {
                            clicked = true;
                            eprintln!("injecting mouse move + click at ({x},{y}) after AU {aus}");
                            for ev in [
                                pb::input_event::Event::MouseMove(pb::MouseMove { x, y }),
                                pb::input_event::Event::MouseButton(pb::MouseButton {
                                    x,
                                    y,
                                    button: 0,
                                    pressed: true,
                                    modifiers: 0,
                                }),
                                pb::input_event::Event::MouseButton(pb::MouseButton {
                                    x,
                                    y,
                                    button: 0,
                                    pressed: false,
                                    modifiers: 0,
                                }),
                            ] {
                                tx.send(tokio_tungstenite::tungstenite::Message::Binary(
                                    input_event(ev).into(),
                                ))
                                .await
                                .expect("send input");
                            }
                        }
                    }
                }
            }
            _ => {}
        }
    }
    let _ = tx
        .send(tokio_tungstenite::tungstenite::Message::Close(None))
        .await;
    eprintln!("probe done: {aus} AUs, {bytes} bytes, {keyframes} keyframes -> {out_path}");
}
