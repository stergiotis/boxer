//! What the unit tests feed a parser with: the calls a host makes,
//! [`Parser::process_all`], a refusal of room an error.
use crate::{Error, Options, Parser, Sink, Size};

/// Counts the pictures told not placed (`Sink::picture`).
#[derive(Default)]
pub(crate) struct Refused(pub(crate) u64);
impl Sink for Refused {
    fn picture(&mut self, placed: bool) {
        self.0 = self.0.saturating_add(u64::from(!placed));
    }
}

impl Parser {
    /// A parser with [`Options::default`].
    pub(crate) fn new(size: Size, history_bytes: usize) -> Result<Self, Error> {
        Self::with_options(size, history_bytes, Options::default())
    }
    /// [`Parser::process_all`], the replies dropped.
    pub(crate) fn process(&mut self, bytes: &[u8]) -> Result<(), Error> {
        self.process_into(bytes, &mut ())
    }
    /// [`Parser::process_all`] into `sink`; a refusal of room is an error
    /// here, for a test to see.
    pub(crate) fn process_into(&mut self, bytes: &[u8], sink: &mut impl Sink) -> Result<(), Error> {
        match self.process_all(bytes, sink) {
            0 => Ok(()),
            _ => Err(Error::Capacity),
        }
    }
}
