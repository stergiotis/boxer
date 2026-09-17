//! Stage 8 — codec round-trip via external `ffmpeg`.
//!
//! We deliberately shell out to `ffmpeg` (no libav binding): write the luma
//! frame as a grayscale PNG, encode a single intra frame to H.264 / VP9 / AV1 at
//! a given CRF in `yuv420p`, then pull the first frame's Y plane back out as
//! grayscale. The watermark is luma-only, so chroma subsampling is irrelevant.
//! Output resolution is checked against the input — a silent rescale would
//! invalidate every cell-size assumption (`EXPLANATION.md` §Guardrails 7).
//!
//! Limited-range quantization (the `yuv420p` default) scales/offsets our luma,
//! but the reference-cell calibration absorbs exactly that affine change.

use std::io::Read;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

use crate::frame::LumaFrame;
use crate::Error;

/// The three codecs from the requirement.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Codec {
    H264,
    Vp9,
    Av1,
}

impl Codec {
    pub fn name(self) -> &'static str {
        match self {
            Codec::H264 => "h264",
            Codec::Vp9 => "vp9",
            Codec::Av1 => "av1",
        }
    }

    pub fn all() -> [Codec; 3] {
        [Codec::H264, Codec::Vp9, Codec::Av1]
    }

    pub fn parse(s: &str) -> Option<Codec> {
        match s.to_ascii_lowercase().as_str() {
            "h264" | "x264" | "avc" => Some(Codec::H264),
            "vp9" => Some(Codec::Vp9),
            "av1" => Some(Codec::Av1),
            _ => None,
        }
    }

    /// A sensible default CRF per codec (the Stage 8/9 test-matrix points).
    pub fn default_crf(self) -> u32 {
        match self {
            Codec::H264 => 23,
            Codec::Vp9 => 31,
            Codec::Av1 => 30,
        }
    }

    fn encode_args(self, crf: u32) -> Vec<String> {
        let crf = crf.to_string();
        match self {
            Codec::H264 => vec![
                "-c:v".into(),
                "libx264".into(),
                "-crf".into(),
                crf,
                "-preset".into(),
                "medium".into(),
            ],
            Codec::Vp9 => vec![
                "-c:v".into(),
                "libvpx-vp9".into(),
                "-crf".into(),
                crf,
                "-b:v".into(),
                "0".into(),
                "-deadline".into(),
                "good".into(),
                "-cpu-used".into(),
                "4".into(),
            ],
            Codec::Av1 => vec![
                "-c:v".into(),
                "libsvtav1".into(),
                "-crf".into(),
                crf,
                "-preset".into(),
                "8".into(),
            ],
        }
    }
}

/// Is `ffmpeg` on PATH and runnable?
pub fn ffmpeg_available() -> bool {
    Command::new("ffmpeg")
        .arg("-version")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

/// Upper bound on a single `ffmpeg` invocation (encode or decode) before we
/// kill it and surface an error, rather than hang the caller on a wedged
/// subprocess indefinitely.
const FFMPEG_TIMEOUT: Duration = Duration::from_secs(60);

/// How often to poll the child for exit while waiting on it below.
const POLL_INTERVAL: Duration = Duration::from_millis(20);

/// Encode `frame` with `codec` at `crf`, decode the first frame, and return the
/// recovered luma. Errors if `ffmpeg` fails or the resolution changes.
///
/// A private temporary directory holds intermediates and is removed on drop.
pub fn roundtrip(frame: &LumaFrame, codec: Codec, crf: u32) -> Result<LumaFrame, Error> {
    let dir = tempfile::Builder::new()
        .prefix("wm_codec_")
        .tempdir()
        .map_err(Error::Io)?;

    let inp = dir.path().join("in.png");
    let enc = dir.path().join("enc.mkv");
    let dec = dir.path().join("dec.png");

    frame.save_png(&inp)?;

    let mut e = Command::new("ffmpeg");
    e.args(["-y", "-loglevel", "error", "-i"]).arg(&inp);
    e.args(codec.encode_args(crf));
    e.args(["-pix_fmt", "yuv420p", "-frames:v", "1"]).arg(&enc);
    run(e)?;

    let mut d = Command::new("ffmpeg");
    d.args(["-y", "-loglevel", "error", "-i"]).arg(&enc);
    d.args(["-vf", "format=gray", "-frames:v", "1"]).arg(&dec);
    run(d)?;

    let out = LumaFrame::load_png(&dec)?;
    // `dir` drops here (success or error above already returned), removing
    // the scratch directory.
    if out.w() != frame.w() || out.h() != frame.h() {
        return Err(Error::Ffmpeg(format!(
            "resolution changed {}x{} -> {}x{}",
            frame.w(),
            frame.h(),
            out.w(),
            out.h()
        )));
    }
    Ok(out)
}

/// Run `c` to completion, bounded by [`FFMPEG_TIMEOUT`]. Stderr is drained on
/// a background thread while we poll for exit, so a chatty `ffmpeg` can't wedge
/// on a full pipe while we wait; stdout is discarded (`-loglevel error` sends
/// nothing there worth keeping).
fn run(mut c: Command) -> Result<(), Error> {
    c.stdin(Stdio::null());
    c.stdout(Stdio::null());
    c.stderr(Stdio::piped());

    let mut child = c
        .spawn()
        .map_err(|e| Error::Ffmpeg(format!("spawn ffmpeg: {e}")))?;

    let mut stderr_pipe = child.stderr.take();
    let stderr_reader = std::thread::spawn(move || {
        let mut buf = String::new();
        if let Some(s) = stderr_pipe.as_mut() {
            let _ = s.read_to_string(&mut buf);
        }
        buf
    });

    let deadline = Instant::now() + FFMPEG_TIMEOUT;
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) => {}
            Err(e) => {
                let _ = child.kill();
                let _ = child.wait();
                let _ = stderr_reader.join();
                return Err(Error::Ffmpeg(format!("wait ffmpeg: {e}")));
            }
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            let _ = stderr_reader.join();
            return Err(Error::Ffmpeg(format!(
                "ffmpeg exceeded {}s — killed",
                FFMPEG_TIMEOUT.as_secs()
            )));
        }
        std::thread::sleep(POLL_INTERVAL);
    };

    let stderr = stderr_reader.join().unwrap_or_default();
    if !status.success() {
        return Err(Error::Ffmpeg(format!(
            "ffmpeg exited {}: {}",
            status,
            stderr.trim()
        )));
    }
    Ok(())
}
