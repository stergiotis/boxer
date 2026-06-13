//! Hand-tagged prost mirror of `proto/boxer/imzero2/v1/input.proto`
//! (ADR-0024 SD7; package `boxer.imzero2.v1`).
//!
//! protoc is not part of the build yet, so these structs carry explicit
//! `#[prost(...)]` tags that MUST match the .proto file — it is the
//! canonical contract; this file is the mirror. Replacing the mirror with
//! `prost-build` (or protox) codegen is a named follow-up. Keep changes
//! additive per the ADR's versioning policy.

/// One-byte WebSocket message type prefixes (ADR-0024 SD6).
pub const PREFIX_VIDEO: u8 = 0x01;
pub const PREFIX_INPUT: u8 = 0x02;
pub const PREFIX_SESSION: u8 = 0x03;

#[derive(Clone, PartialEq, prost::Message)]
pub struct InputEvent {
    #[prost(oneof = "input_event::Event", tags = "1, 2, 3, 4, 5, 6, 7")]
    pub event: Option<input_event::Event>,
}

pub mod input_event {
    #[derive(Clone, PartialEq, prost::Oneof)]
    pub enum Event {
        #[prost(message, tag = "1")]
        MouseMove(super::MouseMove),
        #[prost(message, tag = "2")]
        MouseButton(super::MouseButton),
        #[prost(message, tag = "3")]
        MouseWheel(super::MouseWheel),
        #[prost(message, tag = "4")]
        Key(super::KeyEvent),
        #[prost(message, tag = "5")]
        Text(super::TextInput),
        #[prost(message, tag = "6")]
        PointerGone(super::PointerGone),
        #[prost(message, tag = "7")]
        PinchZoom(super::PinchZoom),
    }
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct MouseMove {
    #[prost(float, tag = "1")]
    pub x: f32,
    #[prost(float, tag = "2")]
    pub y: f32,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct MouseButton {
    #[prost(float, tag = "1")]
    pub x: f32,
    #[prost(float, tag = "2")]
    pub y: f32,
    #[prost(uint32, tag = "3")]
    pub button: u32,
    #[prost(bool, tag = "4")]
    pub pressed: bool,
    #[prost(uint32, tag = "5")]
    pub modifiers: u32,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct MouseWheel {
    #[prost(float, tag = "1")]
    pub dx: f32,
    #[prost(float, tag = "2")]
    pub dy: f32,
    #[prost(uint32, tag = "3")]
    pub unit: u32,
    #[prost(uint32, tag = "4")]
    pub modifiers: u32,
}

#[derive(Clone, PartialEq, prost::Message)]
pub struct KeyEvent {
    #[prost(string, tag = "1")]
    pub key: String,
    #[prost(string, tag = "2")]
    pub code: String,
    #[prost(bool, tag = "3")]
    pub pressed: bool,
    #[prost(bool, tag = "4")]
    pub repeat: bool,
    #[prost(uint32, tag = "5")]
    pub modifiers: u32,
}

#[derive(Clone, PartialEq, prost::Message)]
pub struct TextInput {
    #[prost(string, tag = "1")]
    pub text: String,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct PointerGone {}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct PinchZoom {
    #[prost(float, tag = "1")]
    pub factor: f32,
}

#[derive(Clone, PartialEq, prost::Message)]
pub struct SessionControl {
    #[prost(oneof = "session_control::Control", tags = "1, 2, 3, 4")]
    pub control: Option<session_control::Control>,
}

pub mod session_control {
    #[derive(Clone, PartialEq, prost::Oneof)]
    pub enum Control {
        #[prost(message, tag = "1")]
        Hello(super::SessionHello),
        #[prost(message, tag = "2")]
        ViewportResize(super::ViewportResize),
        #[prost(message, tag = "3")]
        Ping(super::Ping),
        #[prost(message, tag = "4")]
        SetCadence(super::SetCadence),
    }
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct SessionHello {
    #[prost(uint32, tag = "1")]
    pub width_px: u32,
    #[prost(uint32, tag = "2")]
    pub height_px: u32,
    #[prost(float, tag = "3")]
    pub pixels_per_point: f32,
    #[prost(uint32, tag = "4")]
    pub cadence: u32,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct SetCadence {
    #[prost(uint32, tag = "1")]
    pub cadence: u32,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct ViewportResize {
    #[prost(float, tag = "1")]
    pub logical_width: f32,
    #[prost(float, tag = "2")]
    pub logical_height: f32,
    #[prost(float, tag = "3")]
    pub pixel_scale: f32,
}

#[derive(Clone, Copy, PartialEq, prost::Message)]
pub struct Ping {
    #[prost(uint64, tag = "1")]
    pub nonce: u64,
}

#[derive(Clone, PartialEq, prost::Message)]
pub struct VideoChunk {
    #[prost(uint64, tag = "1")]
    pub frame_index: u64,
    #[prost(uint64, tag = "2")]
    pub timestamp_micros: u64,
    #[prost(bool, tag = "3")]
    pub keyframe: bool,
    #[prost(bytes = "vec", tag = "4")]
    pub data: Vec<u8>,
}
