//! Standalone client for ADR-0243. The wire and geometry core also builds on Linux.
pub mod geometry;
pub mod input;
pub mod session;
pub mod wire;

#[cfg(windows)]
pub mod win;
