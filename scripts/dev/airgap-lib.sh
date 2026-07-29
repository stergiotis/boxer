#!/bin/bash
# airgap-lib.sh — shared, repo-agnostic primitives for building an airgap bundle
# that lets a multi-language project be built (and run) on a host with no network
# and no Go/Rust package access. SOURCE this file; do not execute it.
#
# It is the canonical core behind boxer's own bundle/unbundle wrappers
# (scripts/dev/airgap-bundle.sh, scripts/dev/airgap-unbundle.sh; ADR-0095) and is
# consumed verbatim by downstream repos that ship boxer as a dependency (they
# source it via ../boxer/scripts/dev/airgap-lib.sh — reference, don't copy, per
# the canonical-standards-upstream practice).
#
# The wrappers own orchestration and repo-specific facts (which modules, which
# build targets, which Rust crates, which environment services to preflight);
# this file owns the primitives those flows are built from.
#
# Two Go dependency modes are supported:
#   single     one module, `go mod vendor` (boxer's own case).
#   workspace  several source trees co-developed together, a pruned go.work +
#              `go work vendor` — the `use`d modules stay editable source; only
#              their external dependencies are vendored (a downstream repo that
#              tracks an unreleased boxer needs this to capture local boxer, not
#              the pinned release).

# Refuse direct execution: this is a library.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    echo "airgap-lib.sh is a sourced library, not a program." >&2
    exit 2
fi

# Where this library lives, so primitives can reach sibling scripts (e.g.
# build-static-ffmpeg.sh) regardless of which repo sourced us.
AIRGAP_LIB_DIR="$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")"

# ---- messaging --------------------------------------------------------------
airgap_die()  { echo "ERROR: $*" >&2; exit 1; }
airgap_warn() { echo "  WARN: $*" >&2; }
airgap_ok()   { echo "  ok:   $*"; }
airgap_step() { echo "=== $* ===" >&2; }

airgap_require_cmd() {  # <cmd> [message]
    command -v "$1" >/dev/null 2>&1 || airgap_die "${2:-$1 not found on PATH.}"
}

# ---- compression ------------------------------------------------------------
# Sets AIRGAP_COMPEXT and defines airgap_compress() (reads stdin, writes $1).
# Prefers zstd, falls back to gzip — matching boxer's original behaviour.
airgap_pick_compressor() {
    if command -v zstd >/dev/null 2>&1; then
        airgap_compress() { zstd -T0 -3 -q -f -o "$1"; }   # -f: overwrite on re-run
        AIRGAP_COMPEXT=zst
    else
        echo "NOTE: zstd not found, falling back to gzip (larger, slower)." >&2
        airgap_compress() { gzip > "$1"; }
        AIRGAP_COMPEXT=gz
    fi
}

# ---- source export ----------------------------------------------------------
# The source tree comes from HEAD, so uncommitted work is not included; the
# wrapper copies its own airgap files in explicitly so a pre-commit bundle still
# carries the unbundler.
airgap_export_head() {  # <repo> <dest>
    mkdir -p "$2"
    git -C "$1" archive --format=tar HEAD | tar -x -C "$2"
}

airgap_warn_if_dirty() {  # <repo> [label]
    if [ -n "$(git -C "$1" status --porcelain)" ]; then
        echo "WARNING: ${2:-$1} working tree is dirty. The bundle's source comes from HEAD;" >&2
        echo "         uncommitted changes below will NOT be included:" >&2
        git -C "$1" status --short >&2
    fi
}

# ---- Go toolchain -----------------------------------------------------------
airgap_ship_goroot() {  # <destdir>   (destdir/go <- $(go env GOROOT))
    local goroot; goroot="$(go env GOROOT)"
    [ -d "$goroot" ] || airgap_die "GOROOT '$goroot' is not a directory."
    airgap_step "copy Go SDK ($goroot)"
    cp -a "$goroot" "$1"
}

airgap_go_single_vendor() {  # <moduledir>
    airgap_step "go mod vendor"
    ( cd "$1" && go mod vendor )
}

# Write a pruned go.work referencing the given module dirs (pass paths relative
# to <workdir> for a portable, shippable workspace), then `go work vendor` into
# <workdir>/vendor. The `use`d modules are NOT vendored — they resolve from
# source, so they stay editable on the target.
#   args: <workdir> <go-version-line> <usedir...>
airgap_go_workspace_vendor() {
    local workdir="$1" gover="$2"; shift 2
    {
        echo "$gover"
        echo
        local d; for d in "$@"; do echo "use $d"; done
    } > "$workdir/go.work"
    airgap_step "go work vendor (pruned workspace: $*)"
    ( cd "$workdir" && GOFLAGS= GOWORK="$workdir/go.work" GOPROXY=off GOSUMDB=off go work vendor -e )
}

# Configure the CURRENT shell for an offline Go build. Used both to self-verify
# at bundle time (pass the host GOROOT) and, via the generated env file, on the
# target (pass the shipped GOROOT).
#   args: <goroot> <mode: single|workspace> [gowork-path]
airgap_set_go_offline_env() {
    export GOROOT="$1"
    export GOTOOLCHAIN=local     # never try to fetch the go.mod-pinned toolchain
    export GOPROXY=off           # never reach a module proxy
    export GOSUMDB=off           # sumdb unreachable; go.sum still enforces integrity
    export GOFLAGS=-mod=vendor   # build from the shipped vendor/ tree
    export CGO_ENABLED=0         # the Go builds here are CGO-free
    export PATH="$1/bin:$PATH"
    if [ "${2:-single}" = workspace ]; then
        export GOWORK="$3"       # workspace: resolve use'd modules from source
    fi
}

# Emit the same offline Go env as `export` lines (for the generated env file).
#   args: <goroot> <mode> [gowork-path]
airgap_go_env_lines() {
    echo "export GOROOT=\"$1\""
    echo "export GOTOOLCHAIN=local"
    echo "export GOPROXY=off"
    echo "export GOSUMDB=off"
    echo "export GOFLAGS=-mod=vendor"
    echo "export CGO_ENABLED=0"
    echo "export PATH=\"$1/bin:\$PATH\""
    [ "${2:-single}" = workspace ] && echo "export GOWORK=\"$3\""
    return 0
}

# ---- Rust toolchain ---------------------------------------------------------
# Resolve the rustup-managed toolchain sysroot pinned by <cratedir> (its
# rust-toolchain file) and refuse a distro sysroot under /usr, which cannot be
# relocated into the bundle. Echoes the sysroot on stdout.
airgap_rust_sysroot() {  # <cratedir>
    command -v cargo >/dev/null 2>&1 || {
        echo "ERROR: cargo not found; required for --scope full." >&2
        echo "  Install via 'rustup-init -y' (rustup-managed, shippable)." >&2
        exit 1; }
    local sysroot; sysroot="$(cd "$1" && rustc --print sysroot)"
    case "$sysroot" in
        /usr|/usr/*|/bin|/bin/*)
            echo "ERROR: Rust sysroot is a system path: $sysroot" >&2
            echo "  A distro-packaged Rust cannot be shipped as an isolated toolchain" >&2
            echo "  and ignores $1/rust-toolchain." >&2
            echo "  Install the pinned toolchain via rustup so it can be bundled, e.g.:" >&2
            echo "      rustup toolchain install <channel> -c rustfmt -c clippy" >&2
            exit 1 ;;
    esac
    [ -x "$sysroot/bin/cargo" ] || {
        echo "ERROR: no cargo under $sysroot/bin (unexpected toolchain layout)." >&2; exit 1; }
    echo "$sysroot"
}

airgap_ship_rust_toolchain() {  # <sysroot> <destdir>
    airgap_step "copy Rust toolchain ($1)"
    cp -a "$1" "$2"
}

# Vendor a cargo workspace with the pinned toolchain's cargo, keep only the TOML
# stanza cargo emits (drop any human-readable preamble), write it to <configout>.
#   args: <sysroot> <configout> <vendordir> <manifest> [sync-manifest...]
airgap_cargo_vendor() {
    local sysroot="$1" configout="$2" vendordir="$3" manifest="$4"; shift 4
    local args=( vendor --manifest-path "$manifest" )
    local s; for s in "$@"; do args+=( --sync "$s" ); done
    args+=( "$vendordir" )
    airgap_step "cargo vendor (${manifest}${*:+ + syncs})"
    "$sysroot/bin/cargo" "${args[@]}" > "$configout"
    sed -i -n '/^\[/,$p' "$configout"
}

# Materialize a cargo source-replacement config from the *.in template with an
# absolute vendor path (the .in ships a placeholder; the target's path differs).
#   args: <config.in> <configout> <abs-vendordir>
airgap_cargo_config_materialize() {
    mkdir -p "$(dirname "$2")"
    sed -E "s#^directory = .*#directory = \"$3\"#" "$1" > "$2"
}

airgap_cargo_env_lines() {  # <rust_tc_bin_parent> <cargo_home>
    echo "export CARGO_HOME=\"$2\""
    echo "export CARGO_NET_OFFLINE=true"
    echo "export PATH=\"$1/bin:\$PATH\""
}

# ---- preflight (target-side; warnings only) ---------------------------------
airgap_preflight_c_compiler() {
    if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || command -v clang >/dev/null 2>&1
        then airgap_ok "C compiler present (needed at build time for libmimalloc-sys)"
        else airgap_warn "no C compiler (cc/gcc/clang) — the Rust build will fail on libmimalloc-sys"; fi
    command -v pkg-config >/dev/null 2>&1 \
        && airgap_ok "pkg-config present" || airgap_warn "pkg-config absent — some -sys crates probe with it"
}

airgap_preflight_vulkan() {
    if command -v vulkaninfo >/dev/null 2>&1 && vulkaninfo >/dev/null 2>&1; then
        airgap_ok "Vulkan reports a device (wgpu runtime ok)"
    elif { command -v ldconfig >/dev/null 2>&1 && ldconfig -p 2>/dev/null | grep -q 'libvulkan\.so\.1'; }; then
        airgap_warn "libvulkan present but no enumerable device — install an ICD (mesa-vulkan-drivers, or lavapipe for software)"
    else
        airgap_warn "no Vulkan loader — the wgpu render head needs libvulkan + an ICD (hardware driver or lavapipe) at runtime"
    fi
}

# Preflight environment-provided runtime services (informational; not bundled).
# ---- ffmpeg (imzero2 headless encoder) --------------------------------------
# Build the static, software-only ffmpeg the headless encoder spawns and stage it
# into the bundle, so the target does not have to supply one.
#
# ffmpeg is the last runtime dependency of the video path that the environment
# had to provide, and the hardest to assume: the encoder needs a specific
# component set (rawvideo in, NUT out, the lavfi probe path, dump_extra, the
# software encoders), and a distro build satisfying that pulls in ~290 shared
# objects. One self-contained ~20 MiB binary removes the assumption entirely.
#
# Software-only by construction: a static binary cannot dlopen, which is how both
# libva and NVENC load their drivers. That costs nothing on the hosts this
# targets — a server CPU with no iGPU has no hardware encoder either way — and
# CodecLane::best probes and falls back on its own.
#
# BEST-EFFORT: a build host without cmake/nasm/a static libc simply does not get
# one, and the bundle falls back to the environment's ffmpeg exactly as before.
# Echoes nothing; returns non-zero when no binary was staged.
#   args: <destdir> <source-cache-dir> [extra build-static-ffmpeg.sh args...]
airgap_ship_ffmpeg() {  # <destdir> <srccache> [args...]
    local dest="$1" cache="$2"; shift 2
    local builder="$AIRGAP_LIB_DIR/build-static-ffmpeg.sh"
    if [ ! -x "$builder" ]; then
        airgap_warn "build-static-ffmpeg.sh not found at $builder — not bundling ffmpeg."
        return 1
    fi
    airgap_step "build static ffmpeg for the headless encoder"
    mkdir -p "$dest"
    # --fetch is safe here: this runs on the CONNECTED packing host. Sources are
    # cached across runs, so a re-pack does not re-download.
    if "$builder" --src-dir "$cache" --out "$dest/ffmpeg" --fetch "$@"; then
        airgap_ok "shipped static ffmpeg ($(du -h "$dest/ffmpeg" | cut -f1)) -> ${dest##*/}/ffmpeg"
        return 0
    fi
    airgap_warn "static ffmpeg build failed — the target will need one from its environment."
    rm -f "$dest/ffmpeg"
    return 1
}

# Check ONLY the above build's host prerequisites, building nothing. For a caller
# that ships ffmpeg late in a long flow, gate on this up front: airgap_ship_ffmpeg
# is best-effort, so without it the first sign of a missing cmake is a finished
# bundle with no ffmpeg in it. Pass the same extra args as airgap_ship_ffmpeg, or
# the two disagree about what to check (--without-h264 decides whether a static
# libstdc++ is required). Success is silent; a failure leaves the builder's report
# of what is missing, and what package supplies it, on stderr.
#   args: [extra build-static-ffmpeg.sh args...]
airgap_preflight_ffmpeg_build() {  # [args...]
    local builder="$AIRGAP_LIB_DIR/build-static-ffmpeg.sh"
    if [ ! -x "$builder" ]; then
        airgap_warn "build-static-ffmpeg.sh not found at $builder — cannot preflight the ffmpeg build."
        return 1
    fi
    # --fetch mirrors airgap_ship_ffmpeg's own invocation: it is what decides
    # whether curl counts as a prerequisite.
    "$builder" --preflight-only --fetch "$@" >/dev/null
}

# Emit the env line that points the imzero2 headless encoder at a bundled
# ffmpeg. Boxer's Rust client reads IMZERO2_FFMPEG_BIN for both the lane probe
# and the stream encoder; pointing it here rather than prepending to PATH keeps
# the bundled build from shadowing the system ffmpeg for every other tool.
# No-op when nothing was bundled, so the PATH lookup stays in force.
#   args: <ffmpeg-path>
airgap_ffmpeg_env_lines() {  # <ffmpeg-path>
    [ -x "$1" ] || return 0
    echo "export IMZERO2_FFMPEG_BIN=\"$1\""
}

# Report the bundled ffmpeg, or fall through to preflighting the environment for
# one. Returns 0 when the bundle supplies it (so the caller can drop `ffmpeg`
# from the environment-services list).
#   args: <ffmpeg-path>
airgap_preflight_ffmpeg() {  # <ffmpeg-path>
    if [ -x "$1" ]; then
        airgap_ok "ffmpeg bundled ($("$1" -hide_banner -version 2>/dev/null | head -1 | cut -d' ' -f1-3)); IMZERO2_FFMPEG_BIN points at it"
        return 0
    fi
    return 1
}

airgap_preflight_services() {  # <tool...>
    local tool
    for tool in "$@"; do
        command -v "$tool" >/dev/null 2>&1 \
            && airgap_ok "$tool present" \
            || airgap_warn "$tool not on PATH (expected from the environment; some may be remote endpoints)"
    done
}
