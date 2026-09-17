//! Wire framing and codec-acceptance for the `boxer.imzero2.v1` remote-access
//! protocol (ADR-0024 SD6/SD7, ADR-0243 SD1).
//!
//! A single WebSocket carries everything as binary messages with a one-byte
//! type prefix (see `input.proto`'s header comment):
//!
//!   0x01 server→client  [`pb::VideoChunk`]     (encoded access unit)
//!   0x02 client→server  [`pb::InputEvent`]
//!   0x03 both ways       [`pb::SessionControl`]
//!   0x04 server→client  draw-stream ("mesh") lane — ADR-0128
//!
//! This client does not implement the mesh lane (ADR-0243 SD1: "mesh
//! capability remains false"), so [`decode`] treats prefix `0x04` as an
//! error rather than silently discarding it — a host that switched the
//! session to mesh needs a visible failure, not a viewer that quietly stops
//! updating.

/// Generated from the canonical `proto/boxer/imzero2/v1/input.proto` at
/// build time (`build.rs`: protox + prost-build, pure Rust — no system
/// `protoc`). Edit the `.proto`, not this module, to change the wire.
#[allow(
    clippy::derive_partial_eq_without_eq,
    clippy::doc_markdown,
    clippy::too_long_first_doc_paragraph
)]
pub mod pb {
    include!(concat!(env!("OUT_DIR"), "/boxer.imzero2.v1.rs"));
}

/// One-byte WebSocket message type prefixes (ADR-0024 SD6), mirrored from
/// `rust/imzero2/src/imzero2/inputproto.rs` — not part of the protobuf schema
/// itself, so not generated.
pub const PREFIX_VIDEO: u8 = 0x01;
pub const PREFIX_INPUT: u8 = 0x02;
pub const PREFIX_SESSION: u8 = 0x03;
pub const PREFIX_MESH: u8 = 0x04;

/// Server→client codecs this client can decode (ADR-0243 SD3): eight-bit
/// 4:2:0 H.264, VP9 profile 0, AV1 Main. Other profiles, bit depths and the
/// mesh draw-stream lane are not advertised and not accepted.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Codec {
    H264,
    Vp9,
    Av1,
}

impl Codec {
    /// Parse [`pb::SessionHello::codec`] (ADR-0088 SD6). Empty means H.264
    /// (the in-band SPS carries the exact profile/level, per the field's
    /// doc comment); a non-empty string is a WebCodecs codec string
    /// (`avc1.*` / `vp09.*` / `av01.*`) or the literal `"mesh"`.
    ///
    /// Rejects (as `Err`, never silently downgrading):
    /// - the mesh lane (`"mesh"`) — ADR-0243 SD1, mesh capability is false;
    /// - any VP9 profile other than 0, or AV1 profile other than Main (0);
    /// - any bit depth other than 8.
    pub fn from_hello(codec: &str) -> anyhow::Result<Self> {
        let s = codec.trim();
        if s.is_empty() {
            return Ok(Self::H264);
        }
        if s == "mesh" {
            anyhow::bail!("draw-stream (mesh) lane not supported by this client (ADR-0243 SD1)");
        }
        if let Some(rest) = s.strip_prefix("avc1.") {
            anyhow::ensure!(
                rest.len() == 6 && rest.bytes().all(|b| b.is_ascii_hexdigit()),
                "invalid AVC codec identifier"
            );
            let profile = u8::from_str_radix(&rest[..2], 16)?;
            anyhow::ensure!(matches!(profile, 66 | 77 | 100), "unsupported AVC profile");
            return Ok(Self::H264);
        }
        if let Some(rest) = s.strip_prefix("vp09.") {
            // vp09.PP.LL.BD[.chroma...] — WebCodecs VP9 codec string.
            let fields: Vec<&str> = rest.split('.').collect();
            let profile = *fields
                .first()
                .ok_or_else(|| anyhow::anyhow!("vp09 codec string missing profile: {s:?}"))?;
            let bit_depth = fields.get(2).copied().unwrap_or("08");
            if profile != "00" {
                anyhow::bail!(
                    "VP9 profile {profile} not accepted — only profile 0 (ADR-0243 SD3): {s:?}"
                );
            }
            if bit_depth != "08" {
                anyhow::bail!(
                    "VP9 bit depth {bit_depth} not accepted — only 8-bit (ADR-0243 SD3): {s:?}"
                );
            }
            return Ok(Self::Vp9);
        }
        if let Some(rest) = s.strip_prefix("av01.") {
            // av01.P.LLT.BD[.mono.chroma.cp.tc.mc.range] — WebCodecs AV1
            // codec string. P: 0=Main, 1=High, 2=Professional.
            let fields: Vec<&str> = rest.split('.').collect();
            let profile = *fields
                .first()
                .ok_or_else(|| anyhow::anyhow!("av01 codec string missing profile: {s:?}"))?;
            let bit_depth = fields.get(2).copied().unwrap_or("08");
            if profile != "0" {
                anyhow::bail!(
                    "AV1 profile {profile} not accepted — only Main (0) (ADR-0243 SD3): {s:?}"
                );
            }
            if bit_depth != "08" {
                anyhow::bail!(
                    "AV1 bit depth {bit_depth} not accepted — only 8-bit (ADR-0243 SD3): {s:?}"
                );
            }
            return Ok(Self::Av1);
        }
        anyhow::bail!("unrecognised codec string: {s:?}")
    }
}

/// Frame one protobuf message with its one-byte prefix, ready to send as a
/// single binary WebSocket message.
pub fn framed(prefix: u8, msg: &impl prost::Message) -> Vec<u8> {
    let mut buf = Vec::with_capacity(1 + msg.encoded_len());
    buf.push(prefix);
    // `Vec<u8>` has spare capacity reserved above and prost's own `encode`
    // only fails if the buffer lacks remaining capacity, which cannot happen
    // here (`encoded_len` sized it exactly).
    msg.encode(&mut buf)
        .expect("Vec<u8> buffer sized by encoded_len");
    buf
}

/// One decoded server→client message.
///
/// Simpler than the three-variant `Hello`/`Control`/`Video` split: a
/// [`pb::SessionHello`] already arrives *inside* a [`pb::SessionControl`]
/// (the wire has one prefix for all session control, not a separate one for
/// hello — see `input.proto`'s `SessionControl.hello`), so splitting it out
/// here would just re-destructure `Control` a second time. Callers that want
/// the hello match on `ServerMessage::Control(SessionControl { control: Some(Control::Hello(hello)), .. })`
/// or use [`ServerMessage::hello`] below.
#[derive(Debug, Clone, PartialEq)]
pub enum ServerMessage {
    Video(pb::VideoChunk),
    Control(pb::SessionControl),
}

impl ServerMessage {
    /// The [`pb::SessionHello`] carried by this message, if it is one.
    pub fn hello(&self) -> Option<&pb::SessionHello> {
        match self {
            Self::Control(pb::SessionControl {
                control: Some(pb::session_control::Control::Hello(h)),
            }) => Some(h),
            _ => None,
        }
    }
}

/// Decode one whole WebSocket binary message (prefix byte + payload) as sent
/// by the server. Rejects the mesh prefix (`0x04`) and client-only prefixes
/// (`0x02` input) explicitly, rather than silently ignoring them.
pub fn decode(bytes: &[u8]) -> anyhow::Result<ServerMessage> {
    let (&prefix, payload) = bytes
        .split_first()
        .ok_or_else(|| anyhow::anyhow!("empty WebSocket message (no type prefix)"))?;
    match prefix {
        PREFIX_VIDEO => Ok(ServerMessage::Video(pb::VideoChunk::decode(payload)?)),
        PREFIX_SESSION => Ok(ServerMessage::Control(pb::SessionControl::decode(payload)?)),
        PREFIX_MESH => anyhow::bail!(
            "server switched to the draw-stream (mesh) lane, which this client does not support (ADR-0243 SD1)"
        ),
        PREFIX_INPUT => anyhow::bail!("received client-only prefix 0x02 (InputEvent) from the server"),
        other => anyhow::bail!("unknown message prefix 0x{other:02x}"),
    }
}

use prost::Message as _;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn from_hello_accepts_empty_as_h264() {
        assert_eq!(Codec::from_hello("").unwrap(), Codec::H264);
    }

    #[test]
    fn from_hello_accepts_avc1_as_h264() {
        assert_eq!(Codec::from_hello("avc1.42001E").unwrap(), Codec::H264);
    }

    #[test]
    fn from_hello_accepts_vp9_profile0_8bit() {
        assert_eq!(Codec::from_hello("vp09.00.41.08").unwrap(), Codec::Vp9);
    }

    #[test]
    fn from_hello_rejects_vp9_profile2() {
        assert!(Codec::from_hello("vp09.02.41.10").is_err());
    }

    #[test]
    fn from_hello_rejects_vp9_10bit() {
        assert!(Codec::from_hello("vp09.00.41.10").is_err());
    }

    #[test]
    fn from_hello_accepts_av1_main_8bit() {
        assert_eq!(Codec::from_hello("av01.0.08M.08").unwrap(), Codec::Av1);
    }

    #[test]
    fn from_hello_rejects_av1_high_profile() {
        // The exact string the server's Av1Hi444 lane announces.
        assert!(Codec::from_hello("av01.1.08M.08.0.000.01.13.01.1").is_err());
    }

    #[test]
    fn from_hello_rejects_av1_10bit() {
        assert!(Codec::from_hello("av01.0.08M.10").is_err());
    }

    #[test]
    fn from_hello_rejects_mesh() {
        assert!(Codec::from_hello("mesh").is_err());
    }

    #[test]
    fn from_hello_rejects_garbage() {
        assert!(Codec::from_hello("not-a-codec").is_err());
    }

    #[test]
    fn framed_round_trips_through_decode() {
        let chunk = pb::VideoChunk {
            frame_index: 7,
            timestamp_micros: 1234,
            keyframe: true,
            data: vec![1, 2, 3],
        };
        let bytes = framed(PREFIX_VIDEO, &chunk);
        assert_eq!(bytes[0], PREFIX_VIDEO);
        match decode(&bytes).unwrap() {
            ServerMessage::Video(got) => assert_eq!(got, chunk),
            other => panic!("expected Video, got {other:?}"),
        }
    }

    #[test]
    fn decode_extracts_hello_from_session_control() {
        let hello = pb::SessionHello {
            width_px: 1920,
            height_px: 1080,
            pixels_per_point: 1.0,
            cadence: 0,
            codec: String::new(),
        };
        let control = pb::SessionControl {
            control: Some(pb::session_control::Control::Hello(hello.clone())),
        };
        let bytes = framed(PREFIX_SESSION, &control);
        let msg = decode(&bytes).unwrap();
        assert_eq!(msg.hello(), Some(&hello));
    }

    #[test]
    fn decode_rejects_mesh_prefix() {
        assert!(decode(&[PREFIX_MESH, 0, 0]).is_err());
    }

    #[test]
    fn decode_rejects_client_only_input_prefix() {
        let ev = pb::InputEvent { event: None };
        let bytes = framed(PREFIX_INPUT, &ev);
        assert!(decode(&bytes).is_err());
    }

    #[test]
    fn decode_rejects_empty_message() {
        assert!(decode(&[]).is_err());
    }
}
