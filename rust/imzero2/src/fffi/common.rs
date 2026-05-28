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
}
pub type FffiResult<T> = Result<T, FffiError>;
