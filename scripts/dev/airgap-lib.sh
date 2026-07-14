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
            echo "      rustup toolchain install <channel> --component rustfmt clippy" >&2
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
airgap_preflight_services() {  # <tool...>
    local tool
    for tool in "$@"; do
        command -v "$tool" >/dev/null 2>&1 \
            && airgap_ok "$tool present" \
            || airgap_warn "$tool not on PATH (expected from the environment; some may be remote endpoints)"
    done
}
