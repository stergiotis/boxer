#[derive(Debug, thiserror::Error)]
pub enum FffiError {
    #[error(transparent)]
    Utf8Error(#[from] std::string::FromUtf8Error),
    #[error(transparent)]
    Io(#[from] std::io::Error),
    #[error("unable to convert from representation")]
    FromRepr(u32),
    #[error("serialized payload size {0} bytes exceeds u32 wire format")]
    SerializedSizeOverflow(usize),
    /// A capture replay re-runs recorded opcodes and must never answer the
    /// server (ADR-0281 §SD5); a write reaching the pipe then is a bug in the
    /// effect marks.
    #[error("write of {0} bytes toward the server during a capture replay")]
    WriteDuringCaptureReplay(usize),
}
pub type FffiResult<T> = Result<T, FffiError>;
