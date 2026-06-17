//! Codec lane — the per-codec configuration (ADR-0088 SD4) that turns the
//! generic encoder pipeline into a concrete codec. A lane bundles the
//! ffmpeg encoder argv and an optional bitstream filter; every lane muxes
//! through NUT, which [`crate::imzero2::nutreader`] demuxes to recover each
//! frame's native bitstream and key-frame flag. Adding a codec is therefore
//! just declarative config here — no per-codec depacketizer or keyframe
//! parser.
//!
//! Backend (hardware/software) selection is [`CodecLane::best`]: per codec it
//! prefers the hardware (VAAPI) lane when [`probe_lane`] confirms it actually
//! encodes on this host, else the portable software lane (ADR-0088 SD5). The
//! same rule drives both the startup default and the runtime `setVideoPipeline`
//! switch, so the encode backend reported to the Go control always matches what
//! is used. H.264 also has an explicit-args escape hatch ([`CodecLane::h264`] —
//! the `IMZERO2_HEADLESS_ENCODER_ARGS` override) for forcing software or pinning
//! specific encoder args.

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum VideoCodec {
    H264,
    Vp9,
    Av1,
    /// AV1 High profile, 4:4:4 chroma — full-chroma desktop/text fidelity for
    /// chart linework. Encoder is `av1_vaapi` (4:4:4 surface) when the GPU
    /// supports it, else `libaom-av1` (`libsvtav1` is 4:2:0-only). Browser
    /// decode is software (dav1d) on most GPUs; the capability handshake hides
    /// it from viewers that cannot decode it.
    Av1Hi444,
}

impl VideoCodec {
    pub fn as_str(self) -> &'static str {
        match self {
            VideoCodec::H264 => "h264",
            VideoCodec::Vp9 => "vp9",
            VideoCodec::Av1 => "av1",
            VideoCodec::Av1Hi444 => "av1-444",
        }
    }

    /// Parse a codec selector (e.g. `IMZERO2_HEADLESS_CODEC`), tolerant of
    /// common spellings.
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "h264" | "avc" | "avc1" | "h.264" => Some(VideoCodec::H264),
            "vp9" | "vp09" => Some(VideoCodec::Vp9),
            "av1" | "av01" | "aom" => Some(VideoCodec::Av1),
            "av1-444" | "av1_444" | "av1444" | "av1-hi444" => Some(VideoCodec::Av1Hi444),
            _ => None,
        }
    }

    /// Map the Go-side codec id (ADR-0088 `setVideoPipeline`): 1=VP9, 2=AV1,
    /// 3=AV1 4:4:4, anything else = H.264.
    pub fn from_u8(v: u8) -> Self {
        match v {
            1 => VideoCodec::Vp9,
            2 => VideoCodec::Av1,
            3 => VideoCodec::Av1Hi444,
            _ => VideoCodec::H264,
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
    /// H.264 with explicit ffmpeg args — the `IMZERO2_HEADLESS_ENCODER_ARGS`
    /// escape hatch, used verbatim in place of [`CodecLane::best`]'s auto
    /// HW/SW pick. Muxed through NUT like every other codec (ADR-0088 Phase 2).
    pub fn h264(encoder_args: Vec<String>) -> Self {
        Self {
            codec: VideoCodec::H264,
            encoder_args,
            bsf: Some(H264_BSF),
        }
    }

    /// The WebCodecs `VideoDecoder.configure` codec string the viewer needs
    /// (ADR-0088 SD6), at the stream's physical `width`×`height`. Empty for
    /// H.264 — the viewer derives `avc1.*` from the in-band SPS, which carries
    /// the exact profile/level. VP9/AV1 expose no in-band descriptor the viewer
    /// parses, so the host names them here, with the **level computed from the
    /// resolution**: WebCodecs validates the codec-string level against the
    /// coded dimensions at `configure`, so a level below what the resolution
    /// needs makes decode fail — the bug a fixed ~4.x level had once a viewer
    /// resized past ~2K (M2). The level is frame-rate-agnostic (rate is not part
    /// of `VideoDecoderConfig`). The `vp9_level` / `av1_level` tables are
    /// mirrored in the browser viewer's capability probe (`viewer/index.html`)
    /// and the Go `videopipeline` model — keep the three in sync.
    pub fn webcodecs_codec_string(&self, width: u32, height: u32) -> String {
        match self.codec {
            VideoCodec::H264 => String::new(),
            VideoCodec::Vp9 => format!("vp09.00.{}.08", vp9_level(width, height)),
            VideoCodec::Av1 => format!("av01.0.{}M.08", av1_level(width, height)),
            // Profile 1 (High), 8-bit, monochrome=0, chroma `000` = 4:4:4.
            VideoCodec::Av1Hi444 => format!("av01.1.{}M.08.0.000", av1_level(width, height)),
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
            // 4:4:4 lane: libsvtav1 is 4:2:0-only, so libaom-av1 (which does
            // yuv444p). Realtime usage + cpu-used 8 keeps it interactive
            // (verified ~6x realtime at 256²). `-pix_fmt yuv444p` is the whole
            // point — full chroma for chart/text linework.
            VideoCodec::Av1Hi444 => &[
                "-c:v", "libaom-av1", "-usage", "realtime", "-cpu-used", "8",
                "-g", "100000", "-pix_fmt", "yuv444p",
            ],
        };
        Self {
            codec,
            encoder_args: args.iter().map(|s| (*s).to_owned()).collect(),
            bsf: match codec {
                VideoCodec::H264 => Some(H264_BSF),
                VideoCodec::Vp9 | VideoCodec::Av1 | VideoCodec::Av1Hi444 => None,
            },
        }
    }

    /// Hardware (VAAPI) lane for a codec. Probe it with [`probe_lane`] before
    /// use — VAAPI opens then ENOSYS-fails on stock Fedora mesa.
    pub fn hardware(codec: VideoCodec) -> Self {
        // `cv` is the VAAPI encoder; `upload` is the sw pixel format hwupload
        // maps onto the VA surface — `nv12` (4:2:0) for the standard lanes, the
        // packed 8-bit 4:4:4 surface `vuyx` for the High-4:4:4 lane. An
        // unsupported 4:4:4 surface only probe-fails and falls back to libaom.
        let (cv, upload) = match codec {
            VideoCodec::H264 => ("h264_vaapi", "nv12"),
            VideoCodec::Vp9 => ("vp9_vaapi", "nv12"),
            VideoCodec::Av1 => ("av1_vaapi", "nv12"),
            VideoCodec::Av1Hi444 => ("av1_vaapi", "vuyx"),
        };
        Self {
            codec,
            encoder_args: vec![
                "-vaapi_device".to_owned(),
                "/dev/dri/renderD128".to_owned(),
                "-vf".to_owned(),
                format!("format={upload},hwupload"),
                "-c:v".to_owned(),
                cv.to_owned(),
                "-bf".to_owned(),
                "0".to_owned(),
                "-g".to_owned(),
                "100000".to_owned(),
            ],
            bsf: match codec {
                VideoCodec::H264 => Some(H264_BSF),
                VideoCodec::Vp9 | VideoCodec::Av1 | VideoCodec::Av1Hi444 => None,
            },
        }
    }

    /// The best working lane for a codec on this host: hardware (VAAPI) if it
    /// actually encodes here, else the portable software lane (SD5). The same
    /// rule drives the startup default and the runtime switch, so the encode
    /// backend reported to the Go control matches what is used.
    pub fn best(codec: VideoCodec) -> Self {
        let hw = Self::hardware(codec);
        if probe_lane(&hw).is_ok() {
            return hw;
        }
        Self::software(codec)
    }

    /// True when this lane uses a hardware (VAAPI) encoder.
    pub fn is_hardware(&self) -> bool {
        self.encoder_args.iter().any(|a| a.ends_with("_vaapi"))
    }
}

/// Why an encoder lane passed or failed its [`probe_lane`] trial encode.
/// Carried to the Go control packed into the capability flags (see
/// `headless::build_video_caps`) so the video-output dialog can name the
/// specific cause instead of a generic "unavailable".
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LaneProbe {
    /// The trial encode produced output — the lane works.
    Ok,
    /// ffmpeg has no such encoder (not compiled into this build).
    NotBuilt,
    /// The hardware device/driver could not be opened (no VAAPI display).
    NoDevice,
    /// The encoder opened but the driver rejected the encode — the Fedora-mesa
    /// `h264_vaapi` class ("No usable encoding profile" / ENOSYS).
    EncodeRejected,
    /// ffmpeg failed for some other reason, or could not be spawned.
    Other,
}

impl LaneProbe {
    /// True only for [`LaneProbe::Ok`].
    pub fn is_ok(self) -> bool {
        matches!(self, LaneProbe::Ok)
    }

    /// Wire reason code (0 = ok / no failure), mirrored by Go's
    /// `videopipeline.ProbeFailReason`. Packed into the capability flags.
    pub fn reason_code(self) -> u8 {
        match self {
            LaneProbe::Ok => 0,
            LaneProbe::NotBuilt => 1,
            LaneProbe::NoDevice => 2,
            LaneProbe::EncodeRejected => 3,
            LaneProbe::Other => 4,
        }
    }
}

/// Classify a failed `ffmpeg` probe from its stderr, most-specific first. The
/// substrings are taken from real ffmpeg 7 output (covered by the unit test):
/// `Unknown encoder` (not compiled in), `No VA display` / `Device creation
/// failed` (no usable VAAPI device), and `No usable encoding profile` /
/// `Function not implemented` (device opened, encode rejected). Only called
/// when the trial encode exited non-zero.
fn classify_probe_stderr(stderr: &str) -> LaneProbe {
    if stderr.contains("Unknown encoder") || stderr.contains("Encoder not found") {
        LaneProbe::NotBuilt
    } else if stderr.contains("No VA display")
        || stderr.contains("Device creation failed")
        || stderr.contains("Failed to initialise VAAPI")
        || stderr.contains("Cannot open the drm device")
    {
        LaneProbe::NoDevice
    } else if stderr.contains("No usable encoding profile")
        || stderr.contains("Function not implemented")
        || stderr.contains("Error while opening encoder")
    {
        LaneProbe::EncodeRejected
    } else {
        LaneProbe::Other
    }
}

/// SD5 host-encode probe: per codec, the **software** and **hardware** lane
/// outcomes on this host. A 2-frame probe-encode to `-f null` per lane — a
/// *listing* would miss the Fedora-mesa `h264_vaapi`→ENOSYS class, where the
/// encoder opens and only fails at encode time. The result feeds the Go control
/// so an unavailable codec is never offered, the encode backend (HW vs SW) is
/// reported truthfully, and a disabled lane carries why it failed.
pub fn probe_host_encode() -> Vec<(VideoCodec, LaneProbe, LaneProbe)> {
    [VideoCodec::H264, VideoCodec::Vp9, VideoCodec::Av1, VideoCodec::Av1Hi444]
        .into_iter()
        .map(|c| {
            (
                c,
                probe_lane(&CodecLane::software(c)),
                probe_lane(&CodecLane::hardware(c)),
            )
        })
        .collect()
}

/// Probe whether a specific lane actually encodes on this host (SD5): a 2-frame
/// probe-encode to `-f null`. Returns [`LaneProbe::Ok`] on success, else the
/// classified failure cause (e.g. `h264_vaapi` → [`LaneProbe::EncodeRejected`]
/// on stock Fedora mesa). stderr is captured to classify the failure and
/// discarded on success.
pub fn probe_lane(lane: &CodecLane) -> LaneProbe {
    let mut cmd = std::process::Command::new("ffmpeg");
    // The probe frame must clear hardware encoders' minimum coded size: AMD VCN
    // rejects anything below 128×128 ("Hardware does not support encoding at
    // size …" → "Error while opening encoder"), which classify_probe_stderr
    // would otherwise read as EncodeRejected and wrongly disqualify a VAAPI lane
    // that encodes fine at real stream sizes. 256×256 clears the floor for both
    // H.264 (128–4096) and AV1 (128–8192 × 128–4352).
    cmd.args([
        "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
        "color=c=black:s=256x256:r=30", "-frames:v", "2",
    ])
    .args(&lane.encoder_args);
    if let Some(bsf) = lane.bsf {
        cmd.arg("-bsf:v").arg(bsf);
    }
    cmd.args(["-f", "null", "-"])
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::piped());
    match cmd.output() {
        Ok(out) if out.status.success() => LaneProbe::Ok,
        Ok(out) => classify_probe_stderr(&String::from_utf8_lossy(&out.stderr)),
        Err(_) => LaneProbe::Other,
    }
}

/// Smallest VP9 level code (the `LL` field of `vp09.PP.LL.BD`) whose max luma
/// picture size covers `width*height`. WebCodecs validates the codec-string
/// level against the coded dimensions at `configure`, so the level must be ≥
/// what the resolution needs; it is frame-rate-agnostic. Mirrored in
/// `viewer/index.html` and the Go `videopipeline` model — keep them identical.
fn vp9_level(width: u32, height: u32) -> &'static str {
    match width as u64 * height as u64 {
        s if s <= 36_864 => "10",
        s if s <= 73_728 => "11",
        s if s <= 122_880 => "20",
        s if s <= 245_760 => "21",
        s if s <= 552_960 => "30",
        s if s <= 983_040 => "31",
        s if s <= 2_228_224 => "40",
        s if s <= 8_912_896 => "50",
        s if s <= 35_651_584 => "60",
        _ => "61",
    }
}

/// Smallest AV1 `seq_level_idx` code (the `LL` field of `av01.P.LLT.BD`) whose
/// max picture size covers `width*height`. Mirrored in the viewer and Go model.
fn av1_level(width: u32, height: u32) -> &'static str {
    match width as u64 * height as u64 {
        s if s <= 147_456 => "00",
        s if s <= 278_784 => "01",
        s if s <= 665_856 => "04",
        s if s <= 1_065_024 => "05",
        s if s <= 2_359_296 => "08",
        s if s <= 8_912_896 => "12",
        _ => "16",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Real ffmpeg 7 stderr captured on a Fedora-mesa host for each failure
    // class. These pin classify_probe_stderr against the actual wording so a
    // regression (or an ffmpeg phrasing change) is caught without a GPU.
    #[test]
    fn classifies_encode_rejected_h264_vaapi() {
        // h264_vaapi on stock Fedora mesa: opens, then ENOSYS at encode.
        let s = "[h264_vaapi @ 0x0] No usable encoding profile found.\n\
                 [enc:h264_vaapi @ 0x0] Error while opening encoder - maybe incorrect parameters\n\
                 [vf#0:0 @ 0x0] Task finished with error code: -38 (Function not implemented)\n";
        assert_eq!(classify_probe_stderr(s), LaneProbe::EncodeRejected);
    }

    #[test]
    fn classifies_not_built_unknown_encoder() {
        let s = "[vost#0:0 @ 0x0] Unknown encoder 'libsvtav1'\n\
                 Error opening output files: Encoder not found\n";
        assert_eq!(classify_probe_stderr(s), LaneProbe::NotBuilt);
    }

    #[test]
    fn classifies_no_device_no_va_display() {
        let s = "[VAAPI @ 0x0] No VA display found for device /dev/dri/renderD128.\n\
                 Device creation failed: -22.\n";
        assert_eq!(classify_probe_stderr(s), LaneProbe::NoDevice);
    }

    #[test]
    fn unrecognised_failure_is_other() {
        assert_eq!(classify_probe_stderr("some unrelated error\n"), LaneProbe::Other);
    }

    #[test]
    fn reason_codes_match_go_contract() {
        // Mirrors videopipeline.ProbeFailReason (0=ok..4=other).
        assert_eq!(LaneProbe::Ok.reason_code(), 0);
        assert_eq!(LaneProbe::NotBuilt.reason_code(), 1);
        assert_eq!(LaneProbe::NoDevice.reason_code(), 2);
        assert_eq!(LaneProbe::EncodeRejected.reason_code(), 3);
        assert_eq!(LaneProbe::Other.reason_code(), 4);
    }

    /// M2: the codec-string level must scale with resolution. A fixed ~4.x
    /// level under-declared once a viewer resized past ~2K, failing decode.
    #[test]
    fn codec_string_level_tracks_resolution() {
        let vp9 = CodecLane::software(VideoCodec::Vp9);
        let av1 = CodecLane::software(VideoCodec::Av1);
        // H.264 self-describes via its in-band SPS — the host names nothing.
        assert_eq!(CodecLane::software(VideoCodec::H264).webcodecs_codec_string(3840, 2160), "");
        // Default 1280×800: minimal sufficient level.
        assert_eq!(vp9.webcodecs_codec_string(1280, 800), "vp09.00.40.08");
        assert_eq!(av1.webcodecs_codec_string(1280, 800), "av01.0.05M.08");
        // AV1 4:4:4: profile 1, chroma 000, level tracks resolution like AV1.
        assert_eq!(
            CodecLane::software(VideoCodec::Av1Hi444).webcodecs_codec_string(1280, 800),
            "av01.1.05M.08.0.000"
        );
        // 4K must declare a higher level than the ~2K default (the M2 fix).
        assert_eq!(vp9.webcodecs_codec_string(3840, 2160), "vp09.00.50.08");
        assert_eq!(av1.webcodecs_codec_string(3840, 2160), "av01.0.12M.08");
        // Up to the clamp_resize ceiling (8192²).
        assert_eq!(vp9.webcodecs_codec_string(7680, 4320), "vp09.00.60.08");
        assert_eq!(av1.webcodecs_codec_string(7680, 4320), "av01.0.16M.08");
    }
}
