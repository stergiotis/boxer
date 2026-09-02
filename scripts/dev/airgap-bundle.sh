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
# For --scope full the vendored crates are COMPILED here, offline, with the very
# toolchain the bundle ships, before the tarball is packed. This is slow (a full
# release build of the imzero2 crate set) but it is the only check that the Rust
# half of the bundle is actually buildable on the target: nothing else catches a
# crate whose MSRV has drifted past rust/imzero2/rust-toolchain. That is not
# hypothetical — h3o 0.10 calls f64::mul_add from a const fn, const-stable only
# since rustc 1.94, which the then-1.92 pin could not compile. A crate that
# declares no `rust-version` (h3o does not) defeats cargo's MSRV-aware resolver,
# so this compile is the only line of defence.
#   --skip-rust-verify  pack without compiling (local iteration; the bundle may
#                       then ship a Rust tree its own toolchain cannot build).
#
# tinygo rides along as a pinned upstream *prebuilt* release at
# _airgap/toolchains/tinygo, because it cannot be produced the way the rest of the
# bundle is: building it means building LLVM. It is the compiler the wasm survey
# shells out to, so without it that path does not run offline. The unbundler puts
# it on PATH and points extbin's BOXER_TINYGO override at it. Packing verifies it
# by compiling a wasm smoke module with the very Go SDK the bundle ships, which is
# the only check that the two halves of that pair agree; a failure drops it rather
# than shipping a tinygo the target cannot use. It is fetched by exact version and
# refused on a SHA-256 mismatch (pins in airgap-lib.sh), and like ffmpeg it is
# best-effort — a packing host that cannot fetch or verify it produces a bundle
# without it, and says so. `--no-tinygo` opts out (~1.2 GB unpacked).
#
# ffmpeg USED to be on that list and no longer is: the bundle now builds a
# static, software-only ffmpeg carrying exactly the components the imzero2
# headless encoder invokes, ships it at _airgap/bin/ffmpeg, and the unbundler
# points IMZERO2_FFMPEG_BIN at it. That is best-effort — a packing host without
# cmake/nasm/a static libc produces a bundle that falls back to the environment's
# ffmpeg as before — and `--no-ffmpeg` opts out deliberately. It is software-only
# by construction (a static binary cannot dlopen, which is how both VAAPI and
# NVENC load their drivers); CodecLane::best probes and falls back on its own.
#
# What the bundle deliberately does NOT carry (provided by the target
# environment, per the deploy contract): systemd, clickhouse, ollama.
# The NATS core bus is the exception among infra dependencies: nats-server is a
# Go program, so it rides the Go vendor as a `tool` dependency and the target
# builds it from that vendored source — no separate binary, no repo bloat
# (ADR-0026 §SD4: still an external binary the monolith neither imports nor
# supervises; showcase/onbox/nats.service runs it).
# What no language vendoring can supply depends on WHICH RENDER HEAD is packed, so
# the unbundler derives it from the MANIFEST's `imzero2_features` line rather than
# assuming a fixed list. For the default lean head (--no-default-features
# --features headless) the answer is: nothing. Measured 2026-08-25 — it builds and
# links with no C compiler on the box, and its binary references no libvulkan.
#   - a rasterizing head (headless_soft, headless_wgpu) REQUIRES ffmpeg at
#     runtime: the encoder lane is then compiled in and spawns it as soon as
#     IMZERO2_HEADLESS_LISTEN or IMZERO2_HEADLESS_H264_OUT is set.
#   - a wgpu head additionally needs a Vulkan loader + ICD at runtime (hardware
#     driver, or lavapipe for software rendering), and — in full scope, where the
#     target compiles it — a C compiler + pkg-config for `wayland-sys`.
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
verify_rust=1
ship_ffmpeg=1
ship_tinygo=1
while [ $# -gt 0 ]; do
    case "$1" in
        --scope)        scope="${2:-}"; shift 2 ;;
        --scope=*)      scope="${1#*=}"; shift ;;
        --out)          out="${2:-}"; shift 2 ;;
        --out=*)        out="${1#*=}"; shift ;;
        --verify-rust)  verify_rust=1; shift ;;  # now the default; kept for callers that pass it
        --skip-rust-verify) verify_rust=0; shift ;;
        --no-ffmpeg)    ship_ffmpeg=0; shift ;;  # rely on the target's own ffmpeg
        --no-tinygo)    ship_tinygo=0; shift ;;  # drop ~1.2 GB; the wasm survey then needs a host tinygo
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
# The render head THIS repo ships, in both scopes: rust/imzero2/build_rust_headless.sh
# is what the unbundler runs (full scope) and what produced the prebuilt binary
# (go-only), and it builds `headless_wgpu`. Declared here rather than
# taken from the library default, which is the lean head hackathon2026 uses — the
# difference decides whether the target is told it needs a Vulkan ICD and a C
# compiler, and boxer's answer is yes to both.
#
# Keep this in step with build_rust_headless.sh. The offline verify below compiles
# THIS string, so a drift between the two shows up as a verify that tested
# something other than what the target will build.
AIRGAP_IMZERO2_FEATURES="${AIRGAP_IMZERO2_FEATURES:-headless_wgpu}"
# No menu here: the unbundler runs a fixed build script rather than taking a head
# argument, so this bundle offers exactly the one head above. hackathon2026's
# bundle is where the selectable menu lives.
AIRGAP_IMZERO2_HEADS="${AIRGAP_IMZERO2_HEADS:-$AIRGAP_IMZERO2_FEATURES}"
# Refuse an unsupported CPU here rather than one best-effort artefact at a time:
# the stagers degrade individually, so an unsupported host otherwise yields a
# bundle quietly missing its pinned Go SDK, tinygo and ffmpeg. x86_64/aarch64.
arch="$(airgap_require_arch)"
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
# ...and the ffmpeg toolchain, so the target can re-verify or rebuild the
# bundled encoder binary without a checkout.
cp "$here/build-static-ffmpeg.sh" "$here/verify-ffmpeg-lanes.sh" "$here/bench-ffmpeg-lanes.sh" \
   "$src/scripts/dev/" 2>/dev/null || true
cp "$repo/doc/howto/airgapped-build.md" "$src/doc/howto/" 2>/dev/null || true

# ---- Go: vendor + offline-readiness verify ----------------------------------
airgap_go_single_vendor "$src"

airgap_step "verify Go builds offline from vendor/ (the step people skip)"
(
    airgap_set_go_offline_env "$(go env GOROOT)" single
    cd "$src"
    go build -tags "$tags"            -o /dev/null ./public/app
    go build -tags "$tags,binary_log" -o /dev/null ./public/thestack/cmd/imzero2/
    # The NATS core bus ships as a vendored Go *tool* dependency, not a monolith
    # import (ADR-0026 §SD4: external binary, built here from source, never
    # linked into the carrier). The target builds it from this same vendor/
    # tree; prove its tool-package subtree vendored completely. No repo build
    # tags apply — it is an ordinary upstream main package.
    go build                          -o /dev/null github.com/nats-io/nats-server/v2
)
echo "    Go vendor is offline-complete (carrier, imzero2 cmd, nats-server)."

mkdir -p "$src/_airgap/toolchains"

# ---- Rust: per-scope ---------------------------------------------------------
if [ "$scope" = full ]; then
    # --sync pulls h3bridge's lock into one shared dir; its sources vendor fine
    # even without the wasm32 std (only its *build* needs the target).
    ( cd "$src" && airgap_cargo_vendor "$rust_sysroot" "_airgap/cargo-config.toml.in" \
        rust/vendor rust/imzero2/Cargo.toml rust/h3bridge/Cargo.toml )
    echo "    wrote rust/vendor and _airgap/cargo-config.toml.in"

    # One head here, not a menu (see the declaration above) — but through the same
    # primitive hackathon2026 uses, so the toolchain pinning, the graded failure
    # and the timing report do not exist in two versions.
    #
    # This now compiles `headless_wgpu`, i.e. what build_rust_headless.sh
    # actually builds on the target. It previously compiled a hardcoded `headless`,
    # so the verify was testing a feature set the bundle does not ship — the check
    # passed while saying nothing about the binary the target would produce.
    if [ "$verify_rust" = 1 ]; then
        airgap_step "verify the render head compiles offline from rust/vendor (slow)"
        airgap_verify_imzero2_heads "$rust_sysroot" "$src/_airgap/cargo-config.toml.in" \
            "$src/rust/vendor" "$src/rust/imzero2" \
            "$AIRGAP_IMZERO2_FEATURES" \
            $AIRGAP_IMZERO2_HEADS `# unquoted on purpose: the menu splits into args` \
            || airgap_die "the render head does not build offline from rust/vendor.
  Refusing to pack a Rust tree this bundle's own pinned toolchain cannot build."
        heads_verified="$AIRGAP_IMZERO2_HEADS_VERIFIED"
    else
        airgap_warn "skipped the Rust offline compile (--skip-rust-verify)."
        airgap_warn "  The bundle may ship a Rust tree its own pinned toolchain cannot build."
        heads_verified=""
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

# ---- ffmpeg for the headless encoder ----------------------------------------
# Cached outside the staging dir so re-packing does not re-download ~89 MB of
# codec sources; the bundle then carries BOTH the linked binary and a copy of the
# ~90 MB of tarballs, so the target can rebuild rather than only re-verify.
ffmpeg_shipped=0
ffmpeg_src_shipped=0
if [ "$ship_ffmpeg" = 1 ]; then
    if airgap_ship_ffmpeg "$src/_airgap/bin" "$repo/.airgap-ffmpeg-src"; then
        ffmpeg_shipped=1
    fi
    # ...and the source tarballs beside it, so the shipped build-static-ffmpeg.sh
    # can actually be run on the target. Staged whether or not the binary built:
    # if this host failed mid-compile, a target with the build toolchain can still
    # produce what it could not.
    if airgap_ship_ffmpeg_sources "$src/_airgap/ffmpeg-src" "$repo/.airgap-ffmpeg-src"; then
        ffmpeg_src_shipped=1
    fi
else
    echo "NOTE: --no-ffmpeg: the target must supply its own ffmpeg (and gets no sources)." >&2
fi

# ---- pinned prebuilt tools ---------------------------------------------------
# Cached alongside the ffmpeg sources so a re-pack re-uses the ~170 MB download;
# the pins in airgap-lib.sh are re-verified on every use.
dl_cache="$repo/.airgap-dl"
tinygo_root="$src/_airgap/toolchains/tinygo"

tinygo_shipped=0
if [ "$ship_tinygo" = 1 ]; then
    if airgap_ship_tinygo "$tinygo_root" "$dl_cache"; then
        # The pair matters, not either half: a TinyGo release that has not caught
        # up with the Go version in the shipped GOROOT fails here, on the
        # connected host, instead of on the target where neither can be replaced.
        # Fail closed — an unverified tinygo is worse than none, because the
        # unbundler would pin BOXER_TINYGO at it and mask whatever the target has.
        if airgap_verify_tinygo "$tinygo_root" "$src/_airgap/toolchains/go"; then
            tinygo_shipped=1
        else
            airgap_warn "dropping the staged tinygo; the target falls back to its own."
            rm -rf -- "$tinygo_root"
        fi
    fi
else
    echo "NOTE: --no-tinygo: the wasm survey needs a tinygo from the target's environment." >&2
fi

# ---- record what we built ----------------------------------------------------
{
    echo "scope=$scope"
    echo "arch=$arch"
    echo "date=$stamp"
    echo "go=$(go version)"
    echo "tags=$tags"
    # The render head's feature set. The unbundler derives the target's real
    # requirements from this (Vulkan? a C toolchain? ffmpeg?), so it has to be
    # recorded rather than re-guessed there.
    echo "imzero2_features=$AIRGAP_IMZERO2_FEATURES"
    # Same two keys hackathon2026's MANIFEST carries, so one reader serves both.
    # No menu here: offered == the single declared head (this unbundler runs a
    # fixed build script rather than taking a --head argument).
    echo "imzero2_heads=$AIRGAP_IMZERO2_HEADS"
    echo "imzero2_heads_verified=${heads_verified:-$AIRGAP_IMZERO2_FEATURES}"
    [ "$scope" = full ] && echo "rust=$(cd rust/imzero2 && rustc --version 2>/dev/null || true)"
    echo "ffmpeg=$([ "$ffmpeg_shipped" = 1 ] && "$src/_airgap/bin/ffmpeg" -hide_banner -version 2>/dev/null | head -1 || echo "none (environment-provided)")"
    echo "ffmpeg_src=$([ "$ffmpeg_src_shipped" = 1 ] && echo "yes (build-static-ffmpeg.sh can rebuild offline)" || echo "none")"
    # A tinygo appears here only if the wasm smoke build passed; a failure drops
    # it from the bundle, so the version line doubles as the verification record.
    if [ "$tinygo_shipped" = 1 ]; then
        echo "tinygo=$("$tinygo_root/bin/tinygo" version 2>/dev/null | head -1) (wasm smoke build verified)"
    else
        echo "tinygo=none (environment-provided)"
    fi
    echo "head=$(git rev-parse HEAD)"
} > "$src/_airgap/MANIFEST"

# ---- pack -------------------------------------------------------------------
airgap_step "pack -> $out"
tar -C "$stage" -cf - boxer | airgap_compress "$out"
echo "=== done: $out ($(du -h "$out" | cut -f1)) ==="
echo "    On the target: extract, then run boxer/scripts/dev/airgap-unbundle.sh"
