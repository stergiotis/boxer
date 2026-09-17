//! Cooperative image/session identification through a tiled luminance signal.
//!
//! A 464×432 unscaled, axis-aligned crop contains a complete tile; recovery also
//! depends on the image-processing channel. This is not authentication. See
//! ADR-0241 and `EXPLANATION.md` for the encoding trade-off and limitations.

pub mod calibrate;
pub mod codec;
pub mod decode;
pub mod fec;
pub mod frame;
pub mod image_io;
pub mod layout;
pub mod locate;
pub mod payload;
pub mod render;
pub mod sample;

pub use decode::{decode_frame, decode_report};
pub use frame::LumaFrame;
pub use layout::TileSpec;
pub use payload::Payload;
pub use render::encode_frame;

#[derive(Debug)]
pub enum Error {
    CrcMismatch,
    Uncorrectable,
    InvalidPadding,
    NoCompleteTile,
    NoUsableTile,
    InvalidLuma,
    InvalidDelta,
    InvalidPayload,
    NonOpaque,
    BadDimensions(String),
    Io(std::io::Error),
    Image(image::ImageError),
    Ffmpeg(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::CrcMismatch => write!(f, "recovered payload failed CRC-16 check"),
            Self::Uncorrectable => write!(f, "uncorrectable Golay word"),
            Self::InvalidPadding => write!(f, "nonzero reserved padding"),
            Self::NoCompleteTile => write!(f, "no complete tile in image"),
            Self::NoUsableTile => write!(f, "no tile with usable reference evidence"),
            Self::InvalidLuma => write!(f, "luminance must be finite and in 0..255"),
            Self::InvalidDelta => write!(f, "delta must be finite and in 2..127"),
            Self::InvalidPayload => write!(f, "payload must contain exactly 16 ASCII hex digits"),
            Self::NonOpaque => write!(
                f,
                "non-opaque image: composite onto the intended background first"
            ),
            Self::BadDimensions(s) => write!(f, "bad dimensions: {s}"),
            Self::Io(e) => write!(f, "I/O: {e}"),
            Self::Image(e) => write!(f, "image: {e}"),
            Self::Ffmpeg(s) => write!(f, "ffmpeg: {s}"),
        }
    }
}
impl std::error::Error for Error {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::Io(e) => Some(e),
            Self::Image(e) => Some(e),
            _ => None,
        }
    }
}
impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Self::Io(e)
    }
}
impl From<image::ImageError> for Error {
    fn from(e: image::ImageError) -> Self {
        Self::Image(e)
    }
}
