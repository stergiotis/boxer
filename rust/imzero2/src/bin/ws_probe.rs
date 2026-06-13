//! WebSocket probe client for the headless remote carrier (ADR-0024
//! Phase 5 verification; `required-features = ["headless"]`).
//!
//! Connects like the browser viewer would, prints every session hello,
//! appends VideoChunk payloads to an Annex-B file (ffprobe-able), and can
//! inject synthetic input mid-stream:
//!
//! - a mouse move + click (input round-trip verifiable by decoding the
//!   stream and looking at the UI change), and/or
//! - a ViewportResize request — the host then re-announces the hello and
//!   restarts the stream at the new geometry; the probe writes all AUs
//!   received after that hello to `<out>.resized` so both stream segments
//!   can be ffprobe'd independently for their dimensions.
//!
//! Usage:
//!   imzero2_ws_probe <ws-url> <out.h264> <num_aus> [click_x click_y after_au] [resize lw lh scale after_au]

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
    // Optional trailing groups: `<x> <y> <after>` (click) and/or
    // `resize <lw> <lh> <scale> <after>`.
    let mut click: Option<(f32, f32, u64)> = None;
    let mut resize: Option<(f32, f32, f32, u64)> = None;
    let mut set_cadence: Option<(u32, u64)> = None;
    let mut i = 4;
    while i < args.len() {
        if args.get(i).map(String::as_str) == Some("cadence") {
            set_cadence = Some((
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("cadence mode"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("cadence after_au"),
            ));
            i += 3;
        } else if args.get(i).map(String::as_str) == Some("resize") {
            resize = Some((
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("resize lw"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("resize lh"),
                args.get(i + 3).and_then(|v| v.parse().ok()).expect("resize scale"),
                args.get(i + 4).and_then(|v| v.parse().ok()).expect("resize after_au"),
            ));
            i += 5;
        } else {
            click = Some((
                args.get(i).and_then(|v| v.parse().ok()).expect("click_x"),
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("click_y"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("click after_au"),
            ));
            i += 3;
        }
    }

    let (ws, _resp) = tokio_tungstenite::connect_async(url)
        .await
        .expect("websocket connect failed");
    let (mut tx, mut rx) = ws.split();
    let mut out = std::fs::File::create(out_path).expect("create out file");
    let mut resized_out: Option<std::fs::File> = None;
    let mut aus: u64 = 0;
    let mut bytes: u64 = 0;
    let mut keyframes: u64 = 0;
    let mut clicked = false;
    let mut resize_sent = false;
    let mut hellos: u32 = 0;

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
                        hellos += 1;
                        eprintln!(
                            "hello #{hellos}: {}x{} @ppp {}",
                            h.width_px, h.height_px, h.pixels_per_point
                        );
                        if hellos > 1 && resized_out.is_none() {
                            // Geometry changed: split subsequent AUs into a
                            // second file so each segment can be ffprobe'd.
                            let path = format!("{out_path}.resized");
                            resized_out = Some(std::fs::File::create(&path).expect("create resized out"));
                            eprintln!("geometry change — subsequent AUs go to {path}");
                        }
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
                    let sink: &mut std::fs::File = resized_out.as_mut().unwrap_or(&mut out);
                    std::io::Write::write_all(sink, &chunk.data).expect("write au");
                    if let Some((mode, after)) = set_cadence {
                        if aus >= after {
                            set_cadence = None;
                            eprintln!("injecting SetCadence({mode}) after AU {aus}");
                            let msg = pb::SessionControl {
                                control: Some(pb::session_control::Control::SetCadence(
                                    pb::SetCadence { cadence: mode },
                                )),
                            };
                            tx.send(tokio_tungstenite::tungstenite::Message::Binary(
                                framed(pb::PREFIX_SESSION, &msg).into(),
                            ))
                            .await
                            .expect("send cadence");
                        }
                    }
                    if let Some((lw, lh, scale, after)) = resize {
                        if !resize_sent && aus >= after {
                            resize_sent = true;
                            eprintln!("injecting viewport resize {lw}x{lh}@{scale} after AU {aus}");
                            let msg = pb::SessionControl {
                                control: Some(pb::session_control::Control::ViewportResize(
                                    pb::ViewportResize {
                                        logical_width: lw,
                                        logical_height: lh,
                                        pixel_scale: scale,
                                    },
                                )),
                            };
                            tx.send(tokio_tungstenite::tungstenite::Message::Binary(
                                framed(pb::PREFIX_SESSION, &msg).into(),
                            ))
                            .await
                            .expect("send resize");
                        }
                    }
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
    eprintln!("probe done: {aus} AUs, {bytes} bytes, {keyframes} keyframes, {hellos} hello(s) -> {out_path}");
}
