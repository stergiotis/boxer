//! Codec lane — the per-codec configuration (ADR-0088 SD4) that turns the
//! generic encoder pipeline into a concrete codec. A lane bundles the
//! ffmpeg encoder argv, how the encoded stream is framed out of ffmpeg,
//! and the codec identity. Because the NUT path ([`crate::imzero2::nutreader`])
//! carries per-frame boundaries and the keyframe flag for *every* codec,
//! adding a codec is otherwise just declarative config here — no per-codec
//! depacketizer or keyframe parser.
//!
//! Backend (hardware/software) selection and the runtime switch are later
//! ADR-0088 phases; this module currently provides software lanes plus the
//! original H.264 Annex-B path for back-compat.

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum VideoCodec {
    H264,
    Vp9,
    Av1,
}

impl VideoCodec {
    pub fn as_str(self) -> &'static str {
        match self {
            VideoCodec::H264 => "h264",
            VideoCodec::Vp9 => "vp9",
            VideoCodec::Av1 => "av1",
        }
    }

    /// Parse a codec selector (e.g. `IMZERO2_HEADLESS_CODEC`), tolerant of
    /// common spellings.
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "h264" | "avc" | "avc1" | "h.264" => Some(VideoCodec::H264),
            "vp9" | "vp09" => Some(VideoCodec::Vp9),
            "av1" | "av01" | "aom" => Some(VideoCodec::Av1),
            _ => None,
        }
    }
}

/// How the encoded elementary stream leaves ffmpeg.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Framing {
    /// Raw H.264 Annex-B, AUD-delimited access units (the original
    /// ADR-0024 path; H.264 only). Drained by the AUD splitter.
    AnnexB,
    /// NUT container, demuxed by `nutreader` (ADR-0088; any codec). The
    /// demuxed payload is the native bitstream WebCodecs consumes.
    Nut,
}

#[derive(Clone, Debug)]
pub struct CodecLane {
    pub codec: VideoCodec,
    /// ffmpeg arguments between the rawvideo input and the muxer output
    /// (selects the encoder and its rate control; excludes the `-f` muxer).
    pub encoder_args: Vec<String>,
    pub framing: Framing,
}

impl CodecLane {
    /// H.264 on the original Annex-B path, with explicit ffmpeg args
    /// (preserves `IMZERO2_HEADLESS_ENCODER_ARGS` / the VAAPI default).
    pub fn h264_annexb(encoder_args: Vec<String>) -> Self {
        Self {
            codec: VideoCodec::H264,
            encoder_args,
            framing: Framing::AnnexB,
        }
    }

    /// Default software lane for a codec, routed through NUT (except H.264,
    /// which stays on the proven Annex-B path during the staged migration —
    /// ADR-0088 Phase 1/2). Latency-tuned with an effectively-infinite GOP:
    /// a viewer (re)connect or a codec switch forces a fresh key frame, so
    /// periodic IDRs buy nothing (ADR-0024 SD3).
    pub fn software(codec: VideoCodec) -> Self {
        // `-pix_fmt yuv420p` is load-bearing: the render readback is BGRA, and
        // left to choose, ffmpeg converts it to a 4:4:4(+alpha) format the
        // software encoders reject (libvpx-vp9 picks gbrap and fails to open).
        // Forcing 4:2:0 also matches the chroma the browser path assumes.
        let args: &[&str] = match codec {
            VideoCodec::H264 => &[
                "-c:v", "libopenh264", "-rc_mode", "off", "-bf", "0", "-g", "100000",
                "-pix_fmt", "yuv420p",
            ],
            VideoCodec::Vp9 => &[
                "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "6M",
                "-g", "100000", "-pix_fmt", "yuv420p",
            ],
            VideoCodec::Av1 => &[
                "-c:v", "libsvtav1", "-preset", "8", "-g", "100000", "-pix_fmt", "yuv420p",
            ],
        };
        let framing = match codec {
            VideoCodec::H264 => Framing::AnnexB,
            VideoCodec::Vp9 | VideoCodec::Av1 => Framing::Nut,
        };
        Self {
            codec,
            encoder_args: args.iter().map(|s| (*s).to_owned()).collect(),
            framing,
        }
    }
}
