//! Build-time protobuf codegen for the remote-access wire (ADR-0024 SD7).
//!
//! Compiles `proto/boxer/imzero2/v1/input.proto` (the canonical contract)
//! into `$OUT_DIR/boxer.imzero2.v1.rs`, which `src/imzero2/inputproto.rs`
//! includes. This replaces a hand-tagged prost mirror that drifted from
//! the .proto once (a `oneof` variant was added without its `tags` entry,
//! silently dropping the message); generating from the source makes that
//! class of drift impossible.
//!
//! Pure Rust, no system `protoc`: protox parses the .proto into a
//! FileDescriptorSet and prost-build emits the Rust from it.
//!
//! Gated on the `headless` feature — only that build compiles the wire
//! types (`inputproto.rs` is itself `#[cfg(feature = "headless")]`), so a
//! desktop build does no codegen.

fn main() {
    // The proto lives at the repo root, shared with any other consumer of
    // the contract; the crate sits two levels down.
    const PROTO_ROOT: &str = "../../proto";
    const PROTO_FILE: &str = "boxer/imzero2/v1/input.proto";
    println!("cargo:rerun-if-changed={PROTO_ROOT}/{PROTO_FILE}");

    if std::env::var_os("CARGO_FEATURE_HEADLESS").is_none() {
        return;
    }

    let out_dir = std::env::var("OUT_DIR").expect("OUT_DIR set by cargo");
    let fds = protox::compile([PROTO_FILE], [PROTO_ROOT]).expect("protox compile failed");

    let mut cfg = prost_build::Config::new();
    cfg.out_dir(&out_dir);
    // prost-build already derives `Copy` (and `Eq`/`Hash`) for messages
    // whose fields are all scalar, which preserves the hand-written API —
    // two call sites copy `SessionHello` out of a mutex guard.
    cfg.compile_fds(fds).expect("prost-build codegen failed");
}
