#!/bin/bash
# Build a self-contained tarball that lets boxer be built (and run) on a host
# with no network and no Go/Rust package access. The bundle vendors every
# language-level dependency and ships the toolchains; the matching unbundler is
# scripts/dev/airgap-unbundle.sh. Decision and rationale: ADR-0095.
#
# This is a thin wrapper: the repo-agnostic primitives live in
# scripts/dev/airgap-lib.sh (the canonical core; downstream repos source it to
# build their own bundles). Here we only declare boxer's specifics and drive them.
#
# Two scopes (the one decision that swings bundle size):
#   --scope full     (default) Go AND Rust buildable from vendored source on the
#                    target. Ships go mod vendor, cargo vendor, the Go SDK and
#                    the Rust toolchain. Largest; needed only if developers must
#                    recompile the imzero2 Rust render host without a network.
#   --scope go-only  Go buildable from source; imzero2 (the Rust head) is built
#                    here and shipped as a prebuilt binary. Drops the Rust
#                    toolchain + ~660 vendored crates + the build-time C
#                    compiler requirement. Much smaller.
#
# What the bundle deliberately does NOT carry (provided by the target
# environment, per the deploy contract): systemd, clickhouse, ffmpeg, ollama.
# And two things no language vendoring can supply, which the target still needs:
#   - build-time (full scope only): a C compiler + pkg-config (libmimalloc-sys
#     compiles bundled C via `cc`).
#   - runtime: a Vulkan loader + ICD for wgpu (hardware driver, or lavapipe for
#     software rendering). The unbundler preflights for both.
#
# Requires (on this connected build host):
#   - `go` (the SSH-signed release toolchain; its GOROOT is shipped verbatim).
#   - full scope: `cargo`/`rustc` via **rustup** — a distro-packaged Rust whose
#     sysroot is under /usr cannot be shipped as an isolated copy (this script
#     refuses it and tells you to `rustup toolchain install`).
#   - `git` (the source tree is taken from `git archive HEAD`), `tar`, and
#     `zstd` (falls back to gzip if absent).
#
# Note: the source tree comes from HEAD, so commit (or stash-pop) your work
# before bundling — uncommitted changes are NOT included, except the two airgap
# files copied in explicitly so a pre-commit bundle is still self-contained.

set -euo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
repo=$(readlink -f "$here/../..")
# shellcheck source=scripts/dev/airgap-lib.sh
source "$here/airgap-lib.sh"
cd "$repo"

# ---- args -------------------------------------------------------------------
scope=full
out=""
verify_rust=0
while [ $# -gt 0 ]; do
    case "$1" in
        --scope)        scope="${2:-}"; shift 2 ;;
        --scope=*)      scope="${1#*=}"; shift ;;
        --out)          out="${2:-}"; shift 2 ;;
        --out=*)        out="${1#*=}"; shift ;;
        --verify-rust)  verify_rust=1; shift ;;  # full Rust compile is slow; off by default
        -h|--help)
            grep '^#' "$BASH_SOURCE" | sed 's/^# \?//'; exit 0 ;;
        *) echo "ERROR: unknown argument: $1" >&2; exit 2 ;;
    esac
done
case "$scope" in
    full|go-only) ;;
    *) echo "ERROR: --scope must be 'full' or 'go-only' (got '$scope')" >&2; exit 2 ;;
esac

tags="$(tr -d '\n' < "$repo/tags")"
arch="$(uname -m)"
stamp="$(date +%Y%m%d)"
[ -n "$out" ] || out="$repo/boxer-airgap-${scope}-${arch}-${stamp}.tar.zst"

airgap_pick_compressor
[ "${out##*.}" = "$AIRGAP_COMPEXT" ] || out="${out%.*}.${AIRGAP_COMPEXT}"

# ---- presence checks (fail early, à la build_h3_wasm.sh) --------------------
airgap_require_cmd go  "go not found on PATH."
airgap_require_cmd git "git not found on PATH."

if [ "$scope" = full ]; then
    # Resolve the toolchain the imzero2 crate actually pins (rust-toolchain),
    # refusing a system sysroot we cannot relocate.
    rust_sysroot="$(airgap_rust_sysroot rust/imzero2)"
fi

airgap_warn_if_dirty "$repo" boxer

# ---- staging ----------------------------------------------------------------
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
src="$stage/boxer"

airgap_step "export source tree from HEAD"
airgap_export_head "$repo" "$src"

# Carry the airgap tooling even when not yet committed, so a pre-commit bundle
# is self-contained and the target has the unbundler + library + how-to.
mkdir -p "$src/scripts/dev" "$src/doc/howto"
cp "$here/airgap-unbundle.sh" "$here/airgap-bundle.sh" "$here/airgap-lib.sh" "$src/scripts/dev/" 2>/dev/null || true
cp "$repo/doc/howto/airgapped-build.md" "$src/doc/howto/" 2>/dev/null || true

# ---- Go: vendor + offline-readiness verify ----------------------------------
airgap_go_single_vendor "$src"

airgap_step "verify Go builds offline from vendor/ (the step people skip)"
(
    airgap_set_go_offline_env "$(go env GOROOT)" single
    cd "$src"
    go build -tags "$tags"            -o /dev/null ./public/app
    go build -tags "$tags,binary_log" -o /dev/null ./public/thestack/cmd/imzero2/
)
echo "    Go vendor is offline-complete."

mkdir -p "$src/_airgap/toolchains"

# ---- Rust: per-scope ---------------------------------------------------------
if [ "$scope" = full ]; then
    # --sync pulls h3bridge's lock into one shared dir; its sources vendor fine
    # even without the wasm32 std (only its *build* needs the target).
    ( cd "$src" && airgap_cargo_vendor "$rust_sysroot" "_airgap/cargo-config.toml.in" \
        rust/vendor rust/imzero2/Cargo.toml rust/h3bridge/Cargo.toml )
    echo "    wrote rust/vendor and _airgap/cargo-config.toml.in"
    echo "    (config includes the egui-snarl git-source stanza; airgap-unbundle rewrites the abs path)"

    if [ "$verify_rust" = 1 ]; then
        airgap_step "verify Rust builds offline from rust/vendor (slow: full compile)"
        tmp_cargo="$(mktemp -d)"
        airgap_cargo_config_materialize "$src/_airgap/cargo-config.toml.in" \
            "$tmp_cargo/config.toml" "$src/rust/vendor"
        # Pin every rustc to the toolchain we ship (the rustup proxy resolves
        # per-crate by cwd; vendored crates carry no pin, so it would fall back
        # to the host default).
        ( cd "$src/rust/imzero2" && \
          CARGO_HOME="$tmp_cargo" CARGO_NET_OFFLINE=true \
          RUSTUP_TOOLCHAIN="$(basename "$rust_sysroot")" \
            "$rust_sysroot/bin/cargo" build --release --frozen --no-default-features --features headless \
              --target-dir "$tmp_cargo/target" )
        rm -rf -- "$tmp_cargo"
        echo "    Rust vendor is offline-complete."
    else
        echo "NOTE: skipped the Rust offline compile (pass --verify-rust to run it)." >&2
    fi

    airgap_ship_goroot "$src/_airgap/toolchains/go"
    airgap_ship_rust_toolchain "$rust_sysroot" "$src/_airgap/toolchains/rust"

else  # go-only: ship imzero2 prebuilt, drop the Rust toolchain + crates
    airgap_step "build prebuilt imzero2 (Rust headless render host)"
    if command -v cargo >/dev/null 2>&1; then
        ( cd rust/imzero2 && ./build_rust_headless.sh )
        prebuilt="rust/imzero2/target/headless/release/imzero2"
        [ -x "$prebuilt" ] || airgap_die "expected $prebuilt after build."
        mkdir -p "$src/_airgap/prebuilt"
        cp "$prebuilt" "$src/_airgap/prebuilt/imzero2"
        echo "    staged _airgap/prebuilt/imzero2"
    else
        echo "ERROR: cargo not found; go-only scope still builds imzero2 here once." >&2
        echo "  (Build it on a matching host and place it at _airgap/prebuilt/imzero2.)" >&2
        exit 1
    fi
    airgap_ship_goroot "$src/_airgap/toolchains/go"
fi

# ---- record what we built ----------------------------------------------------
{
    echo "scope=$scope"
    echo "arch=$arch"
    echo "date=$stamp"
    echo "go=$(go version)"
    echo "tags=$tags"
    [ "$scope" = full ] && echo "rust=$(cd rust/imzero2 && rustc --version 2>/dev/null || true)"
    echo "head=$(git rev-parse HEAD)"
} > "$src/_airgap/MANIFEST"

# ---- pack -------------------------------------------------------------------
airgap_step "pack -> $out"
tar -C "$stage" -cf - boxer | airgap_compress "$out"
echo "=== done: $out ($(du -h "$out" | cut -f1)) ==="
echo "    On the target: extract, then run boxer/scripts/dev/airgap-unbundle.sh"
