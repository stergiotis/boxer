//! Remote-access wire types (ADR-0024 SD7; package `boxer.imzero2.v1`).
//!
//! The message types are generated from the canonical contract
//! `proto/boxer/imzero2/v1/input.proto` by `build.rs` (protox +
//! prost-build, pure Rust — no system `protoc`) and `include!`d below, so
//! the Rust representation cannot drift from the .proto. Edit the .proto,
//! not this file, to change the wire.
//!
//! The one-byte WebSocket framing prefixes (ADR-0024 SD6) are not part of
//! the protobuf schema and are defined here.

/// One-byte WebSocket message type prefixes (ADR-0024 SD6).
pub const PREFIX_VIDEO: u8 = 0x01;
pub const PREFIX_INPUT: u8 = 0x02;
pub const PREFIX_SESSION: u8 = 0x03;

include!(concat!(env!("OUT_DIR"), "/boxer.imzero2.v1.rs"));
