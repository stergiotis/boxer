# Sourced by scripts/dev/build_h3_wasm.sh and scripts/ci/h3_wasm_parity.sh.
#
# h3.wasm is the output of wasm-strip and wasm-opt -Oz, and wasm-opt's output
# differs between binaryen releases: the same cargo build optimised by two of
# them gives two blobs. The rustc that compiles the crate is pinned
# (rust/h3bridge/rust-toolchain.toml); the post-processors are whatever the
# machine's package manager installed. So the build records which ones wrote
# the blob, beside it, and the parity check compares a rebuild against the
# blob only where the same ones are installed.

# h3_wasm_tools: the post-processors on PATH, one "name version" line each,
# in the form the record beside the blob holds.
h3_wasm_tools() {
    printf 'wasm-opt %s\n' "$(wasm-opt --version | head -n 1 | sed 's/^wasm-opt version //')"
    printf 'wasm-strip %s\n' "$(wasm-strip --version | head -n 1)"
}
