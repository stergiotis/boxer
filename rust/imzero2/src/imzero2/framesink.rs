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
        if !frame_idx.is_multiple_of(self.every) {
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
                tracing::info!(path=%path.display(), width, height, frame_idx, "headless frame dumped");
            }
            Err(e) => {
                tracing::error!(path=%path.display(), error=%e, "failed to dump headless frame");
            }
        }
    }
}

/// The file a capture named `name` is written to under `dir`, with extension
/// `ext` — the sanitising half of [`capture_named`], shared with the sidecars a
/// capture may carry (ADR-0257 (proposed) §SD5) so a PNG and its SVG always
/// share a basename. A trailing `.png` on the name is dropped first, so
/// `"a.png"` and `"a"` name the same capture.
pub fn capture_path(
    dir: &std::path::Path,
    name: &str,
    ext: &str,
) -> std::io::Result<std::path::PathBuf> {
    let unusable = || {
        std::io::Error::new(
            std::io::ErrorKind::InvalidInput,
            format!("capture name has no usable file component: {name:?}"),
        )
    };
    let base = std::path::Path::new(name)
        .file_name()
        .and_then(|s| s.to_str())
        .filter(|s| !s.is_empty() && *s != "." && *s != "..")
        .ok_or_else(unusable)?;
    let stem = match std::path::Path::new(base).extension() {
        Some(e) if e.eq_ignore_ascii_case("png") => &base[..base.len() - e.len() - 1],
        _ => base,
    };
    if stem.is_empty() {
        return Err(unusable());
    }
    Ok(dir.join(format!("{stem}.{ext}")))
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
    let path = capture_path(dir, name, "png")?;
    std::fs::create_dir_all(dir)?;
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

#[cfg(test)]
mod tests {
    use super::capture_path;
    use std::path::Path;

    #[test]
    fn capture_path_shares_a_basename_across_extensions() {
        let dir = Path::new("/dump");
        assert_eq!(capture_path(dir, "a", "png").unwrap(), dir.join("a.png"));
        assert_eq!(
            capture_path(dir, "a.png", "png").unwrap(),
            dir.join("a.png")
        );
        assert_eq!(
            capture_path(dir, "a.PNG", "svg").unwrap(),
            dir.join("a.svg")
        );
        assert_eq!(
            capture_path(dir, "a.b", "svg").unwrap(),
            dir.join("a.b.svg")
        );
    }

    #[test]
    fn capture_path_reduces_to_a_basename_under_dir() {
        let dir = Path::new("/dump");
        assert_eq!(
            capture_path(dir, "../../etc/x", "svg").unwrap(),
            dir.join("x.svg")
        );
        assert_eq!(
            capture_path(dir, "/abs/y.png", "png").unwrap(),
            dir.join("y.png")
        );
        assert!(capture_path(dir, "..", "png").is_err());
        assert!(capture_path(dir, "", "png").is_err());
    }
}
