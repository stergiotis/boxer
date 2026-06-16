//! Codec lane — the per-codec configuration (ADR-0088 SD4) that turns the
//! generic encoder pipeline into a concrete codec. A lane bundles the
//! ffmpeg encoder argv and an optional bitstream filter; every lane muxes
//! through NUT, which [`crate::imzero2::nutreader`] demuxes to recover each
//! frame's native bitstream and key-frame flag. Adding a codec is therefore
//! just declarative config here — no per-codec depacketizer or keyframe
//! parser.
//!
//! Backend (hardware/software) selection and the runtime switch are later
//! ADR-0088 phases; this module currently provides software lanes plus an
//! explicit-args H.264 lane (the legacy `IMZERO2_HEADLESS_ENCODER_ARGS` /
//! VAAPI default).

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

/// H.264 bitstream filter: repeat SPS/PPS on every key frame so a
/// (re)joining viewer can configure its decoder from the stream alone. NUT
/// supplies frame boundaries, so no AUD insertion is needed (ADR-0088 SD4 —
/// this replaces the retired `aud=insert` of the original Annex-B path).
const H264_BSF: &str = "dump_extra=freq=keyframe";

#[derive(Clone, Debug)]
pub struct CodecLane {
    pub codec: VideoCodec,
    /// ffmpeg arguments between the rawvideo input and the muxer output
    /// (selects the encoder and its rate control; excludes the `-f` muxer).
    pub encoder_args: Vec<String>,
    /// Optional `-bsf:v` bitstream filter applied after the encoder.
    pub bsf: Option<&'static str>,
}

impl CodecLane {
    /// H.264 with explicit ffmpeg args (preserves the legacy
    /// `IMZERO2_HEADLESS_ENCODER_ARGS` override / the VAAPI default), muxed
    /// through NUT like every other codec (ADR-0088 Phase 2).
    pub fn h264(encoder_args: Vec<String>) -> Self {
        Self {
            codec: VideoCodec::H264,
            encoder_args,
            bsf: Some(H264_BSF),
        }
    }

    /// The WebCodecs `VideoDecoder.configure` codec string the viewer needs
    /// (ADR-0088 SD6). Empty for H.264 — the viewer derives `avc1.*` from the
    /// in-band SPS, which carries the exact profile/level. VP9/AV1 expose no
    /// in-band descriptor the viewer parses, so the host names them here.
    /// These are generous-but-valid defaults (profile + 8-bit certain; level
    /// set high enough for desktop resolutions); ADR-0088 Phase 4's
    /// `isConfigSupported` probe confirms or corrects them per browser.
    pub fn webcodecs_codec_string(&self) -> &'static str {
        match self.codec {
            VideoCodec::H264 => "",
            VideoCodec::Vp9 => "vp09.00.41.08",
            VideoCodec::Av1 => "av01.0.08M.08",
        }
    }

    /// Default software lane for a codec, muxed through NUT (ADR-0088 SD4).
    /// Latency-tuned with an effectively-infinite GOP: a viewer (re)connect
    /// or a codec switch forces a fresh key frame, so periodic IDRs buy
    /// nothing (ADR-0024 SD3).
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
        Self {
            codec,
            encoder_args: args.iter().map(|s| (*s).to_owned()).collect(),
            bsf: match codec {
                VideoCodec::H264 => Some(H264_BSF),
                VideoCodec::Vp9 | VideoCodec::Av1 => None,
            },
        }
    }
}
