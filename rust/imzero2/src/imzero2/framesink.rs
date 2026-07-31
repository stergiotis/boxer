//! Frame sinks consumed by the headless render loop (ADR-0024).
//!
//! The host produces tightly-packed BGRA frames once per tick; each sink
//! decides what to do with them: dump PNGs (Phase 1 verification), feed
//! the ffmpeg encoder ([`crate::imzero2::encoderpipe::EncoderSink`]), or
//! carry them to a remote viewer ([`crate::imzero2::wscarrier::WsCarrier`]).

pub trait FrameSink {
    fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, frame_idx: u64);
}

/// No-op sink for when nothing is configured.
pub struct NullSink;
impl FrameSink for NullSink {
    fn on_frame(&mut self, _bgra: &[u8], _width: u32, _height: u32, _frame_idx: u64) {}
}

/// Writes every Nth frame as `frame_NNNNNN.png` (RGBA) for Phase 1
/// verification. Swizzle buffer is reused across frames.
pub struct PngDumpSink {
    dir: std::path::PathBuf,
    every: u64,
    rgba: Vec<u8>,
}

impl PngDumpSink {
    pub fn new(dir: std::path::PathBuf, every: u64) -> std::io::Result<Self> {
        std::fs::create_dir_all(&dir)?;
        Ok(Self {
            dir,
            every,
            rgba: Vec::new(),
        })
    }
}

impl FrameSink for PngDumpSink {
    fn on_frame(&mut self, bgra: &[u8], width: u32, height: u32, frame_idx: u64) {
        if frame_idx % self.every != 0 {
            return;
        }
        self.rgba.clear();
        self.rgba.reserve(bgra.len());
        for px in bgra.chunks_exact(4) {
            if let &[b, g, r, a] = px {
                self.rgba.extend_from_slice(&[r, g, b, a]);
            }
        }
        let path = self.dir.join(format!("frame_{frame_idx:06}.png"));
        match write_png(&path, &self.rgba, width, height) {
            Ok(()) => {
                tracing::info!(path=%path.display(), width, height, frame_idx, "headless frame dumped")
            }
            Err(e) => {
                tracing::error!(path=%path.display(), error=%e, "failed to dump headless frame")
            }
        }
    }
}

/// Write one BGRA frame as a PNG named by a *remote* request (ADR-0154 SD4).
///
/// The name crosses the wire, so it is reduced to its final component before it
/// reaches a path join: the active connection already holds full input control,
/// which is no argument for also letting it choose where on the filesystem to
/// write. `..`, an absolute path and anything with directory separators all
/// collapse to a basename under `dir`, and a name that has no usable final
/// component is refused outright.
pub fn capture_named(
    dir: &std::path::Path,
    name: &str,
    bgra: &[u8],
    width: u32,
    height: u32,
) -> std::io::Result<std::path::PathBuf> {
    let base = std::path::Path::new(name)
        .file_name()
        .and_then(|s| s.to_str())
        .filter(|s| !s.is_empty() && *s != "." && *s != "..")
        .ok_or_else(|| {
            std::io::Error::new(
                std::io::ErrorKind::InvalidInput,
                format!("capture name has no usable file component: {name:?}"),
            )
        })?;
    let file =
        if std::path::Path::new(base).extension().is_some_and(|e| e.eq_ignore_ascii_case("png")) {
            base.to_owned()
        } else {
            format!("{base}.png")
        };
    std::fs::create_dir_all(dir)?;
    let path = dir.join(file);
    let mut rgba = Vec::with_capacity(bgra.len());
    for px in bgra.chunks_exact(4) {
        if let &[b, g, r, a] = px {
            rgba.extend_from_slice(&[r, g, b, a]);
        }
    }
    write_png(&path, &rgba, width, height)?;
    Ok(path)
}

pub fn write_png(
    path: &std::path::Path,
    rgba: &[u8],
    width: u32,
    height: u32,
) -> std::io::Result<()> {
    let file = std::fs::File::create(path)?;
    let w = std::io::BufWriter::new(file);
    let mut encoder = png::Encoder::new(w, width, height);
    encoder.set_color(png::ColorType::Rgba);
    encoder.set_depth(png::BitDepth::Eight);
    let mut writer = encoder.write_header().map_err(std::io::Error::other)?;
    writer.write_image_data(rgba).map_err(std::io::Error::other)?;
    Ok(())
}
