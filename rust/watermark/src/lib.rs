//! Tiled luminance-grid watermark.
//!
//! Embeds a 64-bit payload into a frame as a periodically tiled grid of luma
//! deltas, such that **every 464×432 px crop** recovers all 64 bits after
//! H.264 / VP9 / AV1 compression. The full design — geometry, FEC budget, and
//! the single-tile decode guarantee — lives in `DESIGN.md`.
//!
//! Pipeline: [`Payload`] → CRC-16 → 80-bit info word → Golay [24,12,8] FEC
//! ([`fec`]) → interleave → render luma cells ([`render`]) → … → sample
//! ([`sample`]) → calibrate ([`calibrate`]) → locate ([`locate`]) →
//! Golay-decode → CRC ([`decode`]).

pub mod calibrate;
pub mod codec;
pub mod decode;
pub mod fec;
pub mod frame;
pub mod layout;
pub mod locate;
pub mod payload;
pub mod render;
pub mod sample;

pub use frame::LumaFrame;
pub use layout::TileSpec;
pub use payload::Payload;

/// Errors surfaced by the public encode/decode API.
///
/// `CrcMismatch` is the load-bearing one: a decode that produces a payload whose
/// CRC does not check **must** return this rather than the (possibly Golay
/// mis-corrected) bytes. Reporting a confident wrong answer is worse than a
/// detected failure — see `DESIGN.md` §Guardrails.
#[derive(Debug)]
pub enum Error {
    /// Recovered info word failed its CRC-16 — payload is not trustworthy.
    CrcMismatch,
    /// The locator found no fully-contained tile in the input region.
    NoCompleteTile,
    /// Input frame/crop dimensions are inconsistent with the [`payload`] spec.
    BadDimensions(String),
    /// Underlying image/file I/O failure.
    Io(std::io::Error),
    /// Image decode/encode failure (PNG).
    Image(String),
    /// External `ffmpeg` invocation failure (codec round-trip).
    Ffmpeg(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::CrcMismatch => write!(
                f,
                "CRC-16 mismatch: recovered payload failed integrity check"
            ),
            Error::NoCompleteTile => write!(f, "no complete tile found in input region"),
            Error::BadDimensions(s) => write!(f, "bad dimensions: {s}"),
            Error::Io(e) => write!(f, "io error: {e}"),
            Error::Image(s) => write!(f, "image error: {s}"),
            Error::Ffmpeg(s) => write!(f, "ffmpeg error: {s}"),
        }
    }
}

impl std::error::Error for Error {}

impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Error::Io(e)
    }
}
