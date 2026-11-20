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
    /// A length prefix larger than any field the Go side writes: the stream
    /// is desynchronised (or the peer is not the Go side), and allocating
    /// what it asks for would abort the process instead of failing.
    #[error("length prefix of {0} bytes exceeds the {1}-byte field cap; the stream is out of step")]
    FieldTooLarge(u64, usize),
}
pub type FffiResult<T> = Result<T, FffiError>;
