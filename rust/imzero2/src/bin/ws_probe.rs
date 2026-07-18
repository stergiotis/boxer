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
//! On connect it sends a `ClientHello` (webcodecs, label "probe") so the host
//! lists it in the roster as takeover-capable (ADR-0086), prints every roster
//! and clipboard message it receives, and can request takeover mid-stream.
//! It also sends a representative `DecodeCapabilities` (H.264 supported +
//! `webgl2`) so the Go video-output dialog offers both the H.264 and the mesh
//! (ADR-0128) lanes — the precondition for a dialog-driven codec switch.
//!
//! Counting is per received *frame* (a video access unit **or** a mesh frame,
//! prefix `0x04`), so scripted input still fires after a switch onto the mesh
//! lane, which carries no VideoChunks. Multiple `click` groups are honoured in
//! order (e.g. open the dialog, pick Mesh, pick H.264). Every SessionHello is
//! printed with its `codec` string, so a runtime switch is observable.
//!
//! Usage:
//!   imzero2_ws_probe <ws-url> <out.h264> <num_frames> [x y after_frame]... [resize lw lh scale after_au] [take after_au]

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
        eprintln!(
            "usage: {} <ws-url> <out.h264> <num_aus> [click_x click_y after_au]",
            args.first().map(String::as_str).unwrap_or("ws_probe")
        );
        std::process::exit(2);
    }
    let url = args.get(1).expect("url");
    let out_path = args.get(2).expect("out");
    let num_aus: u64 = args.get(3).and_then(|v| v.parse().ok()).unwrap_or(90);
    // Optional trailing groups: one or more `<x> <y> <after>` (click, fired in
    // order on frame count) and/or `resize <lw> <lh> <scale> <after>`.
    let mut clicks: Vec<(f32, f32, u64)> = Vec::new();
    let mut resize: Option<(f32, f32, f32, u64)> = None;
    let mut set_cadence: Option<(u32, u64)> = None;
    // SD9 verification: after `after_au` AUs, stop reading the socket for
    // `hold_secs` — the server's send buffer + bounded channel + ffmpeg
    // pipes fill, congesting the encoder. With SD9 the server's render
    // loop must keep running regardless.
    let mut stall: Option<(u64, u64)> = None;
    // After `after_au` AUs, request to become the active connection
    // (ADR-0086 TakeSession) — scripted takeover verification.
    let mut take_session: Option<u64> = None;
    let mut i = 4;
    while i < args.len() {
        if args.get(i).map(String::as_str) == Some("cadence") {
            set_cadence = Some((
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("cadence mode"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("cadence after_au"),
            ));
            i += 3;
        } else if args.get(i).map(String::as_str) == Some("stall") {
            stall = Some((
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("stall after_au"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("stall hold_secs"),
            ));
            i += 3;
        } else if args.get(i).map(String::as_str) == Some("take") {
            take_session =
                Some(args.get(i + 1).and_then(|v| v.parse().ok()).expect("take after_au"));
            i += 2;
        } else if args.get(i).map(String::as_str) == Some("resize") {
            resize = Some((
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("resize lw"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("resize lh"),
                args.get(i + 3).and_then(|v| v.parse().ok()).expect("resize scale"),
                args.get(i + 4).and_then(|v| v.parse().ok()).expect("resize after_au"),
            ));
            i += 5;
        } else {
            clicks.push((
                args.get(i).and_then(|v| v.parse().ok()).expect("click_x"),
                args.get(i + 1).and_then(|v| v.parse().ok()).expect("click_y"),
                args.get(i + 2).and_then(|v| v.parse().ok()).expect("click after_frame"),
            ));
            i += 3;
        }
    }
    // Fire clicks in ascending frame order regardless of the order given.
    clicks.sort_by_key(|&(_, _, after)| after);

    let (ws, _resp) =
        tokio_tungstenite::connect_async(url).await.expect("websocket connect failed");
    let (mut tx, mut rx) = ws.split();
    // Announce caps + a label so the host marks this probe takeover-capable and
    // lists it in the roster (ADR-0086 SD8). Without it the probe reads
    // webcodecs=false and a TakeSession would be refused.
    {
        let hello = pb::SessionControl {
            control: Some(pb::session_control::Control::ClientHello(pb::ClientHello {
                webcodecs: true,
                label: "probe".to_owned(),
            })),
        };
        tx.send(tokio_tungstenite::tungstenite::Message::Binary(
            framed(pb::PREFIX_SESSION, &hello).into(),
        ))
        .await
        .expect("send client hello");
    }
    // Representative decode capabilities so the Go video-output dialog offers a
    // switchable set (ADR-0088/-0128): H.264 (avc1) as a decodable video lane,
    // plus `webgl2` so the mesh draw-stream lane is offerable too. Without this
    // the host reports no viewer decode support and nothing is selectable.
    {
        let caps = pb::SessionControl {
            control: Some(pb::session_control::Control::DecodeCapabilities(
                pb::DecodeCapabilities {
                    codecs: vec![pb::CodecCapability {
                        codec: "avc1.42E01E".to_owned(),
                        supported: true,
                        smooth: true,
                        power_efficient: false,
                    }],
                    webgl2: true,
                },
            )),
        };
        tx.send(tokio_tungstenite::tungstenite::Message::Binary(
            framed(pb::PREFIX_SESSION, &caps).into(),
        ))
        .await
        .expect("send decode capabilities");
    }
    let mut out = std::fs::File::create(out_path).expect("create out file");
    let mut resized_out: Option<std::fs::File> = None;
    let mut aus: u64 = 0;
    let mut bytes: u64 = 0;
    let mut keyframes: u64 = 0;
    // Received frames across both lanes (video AU or mesh frame). Scripted
    // clicks fire on this, so they keep firing after a switch onto mesh.
    let mut frames: u64 = 0;
    let mut click_idx = 0usize;
    let mut mesh_frames: u64 = 0;
    let mut resize_sent = false;
    let mut hellos: u32 = 0;

    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(30);
    while frames < num_aus && std::time::Instant::now() < deadline {
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
                    match ctl.control {
                        Some(pb::session_control::Control::Hello(h)) => {
                            hellos += 1;
                            eprintln!(
                                "hello #{hellos}: {}x{} @ppp {} codec={:?} cadence={}",
                                h.width_px, h.height_px, h.pixels_per_point, h.codec, h.cadence
                            );
                            if hellos > 1 && resized_out.is_none() {
                                // Geometry changed: split subsequent AUs into a
                                // second file so each segment can be ffprobe'd.
                                let path = format!("{out_path}.resized");
                                resized_out =
                                    Some(std::fs::File::create(&path).expect("create resized out"));
                                eprintln!("geometry change — subsequent AUs go to {path}");
                            }
                        }
                        Some(pb::session_control::Control::Roster(r)) => {
                            let role = if r.you_role == pb::Role::Active as i32 {
                                "active"
                            } else {
                                "passive"
                            };
                            let conns: Vec<String> = r
                                .connections
                                .iter()
                                .map(|c| {
                                    format!(
                                        "#{}{}{}",
                                        c.id,
                                        if c.role == pb::Role::Active as i32 {
                                            "=active"
                                        } else {
                                            ""
                                        },
                                        if c.webcodecs { ":wc" } else { "" }
                                    )
                                })
                                .collect();
                            eprintln!(
                                "roster: you=#{} {} active=#{} {}/{} [{}]",
                                r.you_id,
                                role,
                                r.active_id,
                                r.count,
                                r.max,
                                conns.join(" ")
                            );
                        }
                        Some(pb::session_control::Control::Clipboard(c)) => {
                            eprintln!("clipboard <- {:?}", c.text);
                        }
                        _ => {}
                    }
                }
            }
            pb::PREFIX_VIDEO => {
                if let Ok(chunk) = pb::VideoChunk::decode(payload) {
                    aus += 1;
                    frames += 1;
                    bytes += chunk.data.len() as u64;
                    if chunk.keyframe {
                        keyframes += 1;
                    }
                    let sink: &mut std::fs::File = resized_out.as_mut().unwrap_or(&mut out);
                    std::io::Write::write_all(sink, &chunk.data).expect("write au");
                    if let Some((after, hold)) = stall {
                        if aus >= after {
                            stall = None;
                            eprintln!(
                                "STALL: not reading the socket for {hold}s after AU {aus} (congesting the encoder)"
                            );
                            tokio::time::sleep(std::time::Duration::from_secs(hold)).await;
                            eprintln!("STALL: resuming reads");
                        }
                    }
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
                    if let Some(after) = take_session {
                        if aus >= after {
                            take_session = None;
                            eprintln!("injecting TakeSession after AU {aus}");
                            let msg = pb::SessionControl {
                                control: Some(pb::session_control::Control::TakeSession(
                                    pb::TakeSession {},
                                )),
                            };
                            tx.send(tokio_tungstenite::tungstenite::Message::Binary(
                                framed(pb::PREFIX_SESSION, &msg).into(),
                            ))
                            .await
                            .expect("send take_session");
                        }
                    }
                }
            }
            pb::PREFIX_MESH => {
                // ADR-0128 draw-stream frame (no protobuf body to decode here —
                // the probe only counts them so scripted clicks keep firing).
                mesh_frames += 1;
                frames += 1;
                if mesh_frames == 1 {
                    eprintln!(
                        "first mesh frame at frame {frames} ({} bytes)",
                        payload.len()
                    );
                }
            }
            _ => {}
        }
        // Fire any clicks now due (keyed on frame count, so they land on the
        // mesh lane too). Sorted ascending, so a simple index walk suffices.
        while let Some(&(x, y, after)) = clicks.get(click_idx) {
            if frames < after {
                break;
            }
            click_idx += 1;
            eprintln!("injecting mouse move + click at ({x},{y}) after frame {frames}");
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
    let _ = tx.send(tokio_tungstenite::tungstenite::Message::Close(None)).await;
    eprintln!(
        "probe done: {aus} AUs ({mesh_frames} mesh), {bytes} bytes, {keyframes} keyframes, {hellos} hello(s) -> {out_path}"
    );
}
