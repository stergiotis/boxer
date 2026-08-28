#!/bin/bash
# Build-and-test gate for rust/imzero2 (ADR-0205 §Verification plan, M4).
#
# rust/imzero2 has had no automated gate. That is not a policy — it is a hole
# that has broken the tree twice, both times through dependency bumps that
# merged green:
#
#   - 2026-08-10: three Dependabot PRs (rand, png, wgpu) merged and sat broken
#     until a local `cargo build` found them;
#   - 2026-08-19: an eframe 0.36 PR floated egui to 0.36 in a LOCK-ONLY commit
#     with nothing in the manifest diff to review — 84 type errors.
#
# Both are exactly what a `cargo check` across the feature matrix catches, and
# neither needs a GPU. That is this script's first job.
#
# Its second is the CPU rasterizer. `headless_soft` (ADR-0205) carries the only
# first-party rendering code in the crate, plus a vendored rasterizer, and its
# tests assert properties no other lane can see: that the incremental blit
# agrees with the full one, that a resize drops the frame-buffer priming, and
# that a texture mutated in place still repaints. They need no GPU, no display
# and no ClickHouse.
#
# What this deliberately does NOT run:
#
#   - `--all-features`. It enables desktop and both pixel hosts at once, which
#     resolves (see headless.rs `Raster`) but builds far more than any shipped
#     configuration.
#   - anything needing a GPU, a display, or a network.
#
# Graceful skip when cargo or the pinned toolchain is absent, matching
# h3_wasm_parity.sh and watermark_test.sh: prints a 'skipped' line and exits 0,
# so contributors without the Rust toolchain still see green and CI is the
# enforcer.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
crate="$here/../../rust/imzero2"

if ! command -v cargo >/dev/null 2>&1; then
    echo "skipped: cargo not on PATH"
    exit 0
fi
if ! (cd "$crate" && cargo --version >/dev/null 2>&1); then
    echo "skipped: pinned toolchain for rust/imzero2 unavailable"
    exit 0
fi

cd "$crate"

# Each step is captured and echoed only when it fails — the same shape lint.sh
# uses for its own steps.
run_step() {
    local label="$1"
    shift
    local out
    if out=$("$@" 2>&1); then
        echo "$label"
    else
        echo "$label -- FAILED"
        echo "$out"
        return 1
    fi
}

# One target dir for every invocation below, so the feature sets share
# dependency artifacts instead of rebuilding the world five times.
target="target/ci"

# The feature matrix. Each is a configuration something actually builds and
# ships: the desktop seat, the three headless hosts, and the mesh-only
# appliance build. `check` rather than `build` — these exist to catch the
# does-it-still-compile class, and codegen would triple the runtime.
# `fast_alloc` rides along on each because every build script passes it; the
# allocator-free shape is checked separately below.
for features in \
    "--no-default-features --features headless,fast_alloc" \
    "--no-default-features --features headless_svg,fast_alloc" \
    "--no-default-features --features headless_wgpu,fast_alloc" \
    "--no-default-features --features headless_soft,fast_alloc"; do
    # shellcheck disable=SC2086 # deliberate word splitting of the flag pair
    run_step "check $features" cargo check --quiet $features --target-dir "$target" --all-targets
done
run_step "check desktop (default features)" cargo check --quiet --target-dir "$target" --all-targets

# The appliance shape (ADR-0205 M6): no mimalloc, so `libmimalloc-sys` and its
# C toolchain requirement leave the graph. Nothing ships this way yet — it is
# checked because it is the configuration a musl-static build needs, and it
# would otherwise break unnoticed between here and that build.
run_step "check headless_soft without fast_alloc" \
    cargo check --quiet --no-default-features --features headless_soft \
    --target-dir "$target" --all-targets

# Clippy, on the default feature set. The crate reached zero findings under the
# curated lint list in Cargo.toml (the deliberately-off block there says which
# lints were dropped and why), and a gate is what keeps it there — the findings
# came back in the hundreds the moment nobody was looking. The default set is
# gated rather than the whole matrix because clippy's findings are almost all
# feature-independent and a second full build is not worth the CI minutes;
# rust/imzero2/check.sh runs the --all-features form locally.
run_step "clippy (default features)" \
    cargo clippy --quiet --target-dir "$target" --all-targets -- -D warnings -W clippy::all

# Tests run once, under the feature set that has them. The CPU rasterizer's
# own tests plus the crate's existing ~106.
#
# NOT --release: `text_edit_highlight` asserts a `debug_assert!` fires, so it
# is a should_panic test that release compiles out and fails.
run_step "test --features headless_soft" \
    cargo test --quiet --no-default-features --features headless_soft --target-dir "$target"

# ADR-0128 SD6 and ADR-0205 both promise that a GPU-less host carries no wgpu —
# "a hard guarantee (the feature never names them), not a soft promise". A
# feature edit could break that silently, since everything would still build.
for features in headless headless_svg headless_soft; do
    if cargo tree --quiet --no-default-features --features "$features" \
        -e normal --prefix none --target x86_64-unknown-linux-gnu 2>/dev/null |
        grep -qiE '^(wgpu|naga|egui-wgpu|wgpu-core|wgpu-hal|ash|glow) '; then
        echo "FAIL: --features $features pulls a wgpu-family crate; the GPU-less guarantee is broken"
        cargo tree --quiet --no-default-features --features "$features" \
            -e normal --prefix none --target x86_64-unknown-linux-gnu 2>/dev/null |
            grep -iE '^(wgpu|naga|egui-wgpu|wgpu-core|wgpu-hal|ash|glow) ' | sed 's/^/    /'
        exit 1
    fi
done
echo "GPU-less feature sets carry no wgpu family"

echo "passed"
