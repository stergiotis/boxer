#!/bin/bash
# Build a self-contained tarball that lets boxer be built (and run) on a host
# with no network and no Go/Rust package access. The bundle vendors every
# language-level dependency and ships the toolchains; the matching unpacker is
# scripts/dev/airgap-unpack.sh. Decision and rationale: ADR-0095.
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
#     software rendering). The unpacker preflights for both.
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

if command -v zstd >/dev/null 2>&1; then
    comp() { zstd -T0 -3 -q -f -o "$1"; }   # reads stdin, writes $1 (-f: overwrite on re-run)
    compext="zst"
else
    echo "NOTE: zstd not found, falling back to gzip (larger, slower)." >&2
    comp() { gzip > "$1"; }
    compext="gz"
fi
[ "${out##*.}" = "$compext" ] || out="${out%.*}.${compext}"

# ---- presence checks (fail early, à la build_h3_wasm.sh) --------------------
command -v go  >/dev/null 2>&1 || { echo "ERROR: go not found on PATH." >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "ERROR: git not found on PATH." >&2; exit 1; }

goroot="$(go env GOROOT)"
[ -d "$goroot" ] || { echo "ERROR: GOROOT '$goroot' is not a directory." >&2; exit 1; }

if [ "$scope" = full ]; then
    command -v cargo >/dev/null 2>&1 || {
        echo "ERROR: cargo not found; required for --scope full." >&2
        echo "  Install via 'rustup-init -y' (rustup-managed, shippable)." >&2
        exit 1; }
    # Resolve the toolchain the imzero2 crate actually pins (rust-toolchain),
    # then refuse a system sysroot we cannot relocate.
    rust_sysroot="$(cd rust/imzero2 && rustc --print sysroot)"
    case "$rust_sysroot" in
        /usr|/usr/*|/bin|/bin/*)
            echo "ERROR: Rust sysroot is a system path: $rust_sysroot" >&2
            echo "  A distro-packaged Rust cannot be shipped as an isolated toolchain" >&2
            echo "  and ignores rust/imzero2/rust-toolchain (channel 1.92)." >&2
            echo "  Install the pinned toolchain via rustup so it can be bundled:" >&2
            echo "      rustup toolchain install 1.92 --component rustfmt clippy" >&2
            exit 1 ;;
    esac
    [ -x "$rust_sysroot/bin/cargo" ] || {
        echo "ERROR: no cargo under $rust_sysroot/bin (unexpected toolchain layout)." >&2; exit 1; }
fi

if [ -n "$(git status --porcelain)" ]; then
    echo "WARNING: working tree is dirty. The bundle's source comes from HEAD;" >&2
    echo "         uncommitted changes below will NOT be included:" >&2
    git status --short >&2
fi

# ---- staging ----------------------------------------------------------------
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
src="$stage/boxer"
mkdir -p "$src"

echo "=== export source tree from HEAD ==="
git archive --format=tar HEAD | tar -x -C "$src"

# Carry the airgap tooling even when not yet committed, so a pre-commit bundle
# is self-contained and the target has the unpacker + how-to.
mkdir -p "$src/scripts/dev" "$src/doc/howto"
cp "$repo/scripts/dev/airgap-unpack.sh" "$src/scripts/dev/" 2>/dev/null || true
cp "$repo/scripts/dev/airgap-bundle.sh" "$src/scripts/dev/" 2>/dev/null || true
cp "$repo/doc/howto/airgapped-build.md" "$src/doc/howto/" 2>/dev/null || true

# ---- Go: vendor + offline-readiness verify ----------------------------------
echo "=== go mod vendor ==="
( cd "$src" && go mod vendor )

echo "=== verify Go builds offline from vendor/ (the step people skip) ==="
( cd "$src" && \
  GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local CGO_ENABLED=0 \
    go build -tags "$tags"            -o /dev/null ./public/app && \
  GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local CGO_ENABLED=0 \
    go build -tags "$tags,binary_log" -o /dev/null ./public/thestack/cmd/imzero2/ )
echo "    Go vendor is offline-complete."

mkdir -p "$src/_airgap/toolchains"

# ---- Rust: per-scope ---------------------------------------------------------
if [ "$scope" = full ]; then
    echo "=== cargo vendor (imzero2 workspace + h3bridge; ~660 crates) ==="
    # --sync pulls h3bridge's lock into one shared dir; its sources vendor fine
    # even without the wasm32 std (only its *build* needs the target).
    # Invoke the pinned toolchain's cargo by absolute path: cargo vendor runs
    # from the repo root, where no rust-toolchain pin applies, so plain `cargo`
    # fails when rustup has no default toolchain configured.
    ( cd "$src" && "$rust_sysroot/bin/cargo" vendor \
        --manifest-path rust/imzero2/Cargo.toml \
        --sync rust/h3bridge/Cargo.toml \
        rust/vendor > "_airgap/cargo-config.toml.in" )
    # Keep only the TOML (drop any human-readable preamble cargo may emit).
    sed -i -n '/^\[/,$p' "$src/_airgap/cargo-config.toml.in"
    echo "    wrote rust/vendor and _airgap/cargo-config.toml.in"
    echo "    (config includes the egui-snarl git-source stanza; unpack rewrites the abs path)"

    if [ "$verify_rust" = 1 ]; then
        echo "=== verify Rust builds offline from rust/vendor (slow: full compile) ==="
        tmp_cargo="$(mktemp -d)"
        sed -E "s#^directory = .*#directory = \"$src/rust/vendor\"#" \
            "$src/_airgap/cargo-config.toml.in" > "$tmp_cargo/config.toml"
        # Pin every rustc to the toolchain we ship. The rustup `rustc` proxy on
        # PATH otherwise resolves per-crate by cwd, and the vendored crates carry
        # no rust-toolchain pin — so it falls back to the host default (often
        # `stable`), compiling the deps with the wrong compiler and, if that
        # toolchain isn't fully installed, racing parallel auto-installs.
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

    echo "=== copy Go SDK ($goroot) ==="
    cp -a "$goroot" "$src/_airgap/toolchains/go"
    echo "=== copy Rust toolchain ($rust_sysroot) ==="
    cp -a "$rust_sysroot" "$src/_airgap/toolchains/rust"

else  # go-only: ship imzero2 prebuilt, drop the Rust toolchain + crates
    echo "=== build prebuilt imzero2 (Rust headless render host) ==="
    if command -v cargo >/dev/null 2>&1; then
        ( cd rust/imzero2 && ./build_rust_headless.sh )
        prebuilt="rust/imzero2/target/headless/release/imzero2"
        [ -x "$prebuilt" ] || { echo "ERROR: expected $prebuilt after build." >&2; exit 1; }
        mkdir -p "$src/_airgap/prebuilt"
        cp "$prebuilt" "$src/_airgap/prebuilt/imzero2"
        echo "    staged _airgap/prebuilt/imzero2"
    else
        echo "ERROR: cargo not found; go-only scope still builds imzero2 here once." >&2
        echo "  (Build it on a matching host and place it at _airgap/prebuilt/imzero2.)" >&2
        exit 1
    fi
    echo "=== copy Go SDK ($goroot) ==="
    cp -a "$goroot" "$src/_airgap/toolchains/go"
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
echo "=== pack -> $out ==="
tar -C "$stage" -cf - boxer | comp "$out"
echo "=== done: $out ($(du -h "$out" | cut -f1)) ==="
echo "    On the target: extract, then run boxer/scripts/dev/airgap-unpack.sh"
