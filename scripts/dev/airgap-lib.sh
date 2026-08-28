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

# ---- signing (SSHSIG detached signatures) -----------------------------------
# Sign <file>, emitting <file>.sig and <file>.allowed_signers.
#
# One implementation, because there is now more than one producer of a signable
# artefact: airgap-bundle.sh signs what it packs, and airgap-augment.sh signs what
# it augmented (the pack -> augment -> sign order). Two copies of an ssh-keygen
# invocation is how a namespace or an allowed_signers format quietly diverges, and
# the failure mode is a bundle the target's verifier rejects for no visible reason.
#
# The NAMESPACE is a required argument with no default here on purpose: it is
# per-project domain separation, so the library must not invent one. Each wrapper
# supplies its own, and it has to match what the verifier expects.
#
#   args: <file> <private-key> <identity-or-empty> <namespace>
airgap_sign_artifact() {
    local file="$1" key="$2" identity="$3" ns="$4" pub="$2.pub"
    airgap_require_cmd ssh-keygen "ssh-keygen not found; cannot sign (needs OpenSSH >= 8.2)."
    [ -f "$key" ] || { airgap_warn "signing key not found: $key"; return 1; }
    [ -f "$pub" ] || { airgap_warn "public key not found: $pub — needed for the allowed_signers anchor."; return 1; }
    airgap_step "sign ${file##*/} (SSHSIG, namespace '$ns')"
    ssh-keygen -Y sign -f "$key" -n "$ns" "$file" >/dev/null || {
        airgap_warn "ssh-keygen -Y sign failed (a passphrase-protected key needs an agent/askpass)."
        return 1; }
    # allowed_signers line: <principal> <keytype> <base64>. Principal is the
    # explicit identity, else the .pub comment, else a stable fallback.
    [ -n "$identity" ] || identity="$(awk '{print $3}' "$pub")"
    [ -n "$identity" ] || identity="airgap-signer"
    printf '%s %s\n' "$identity" "$(awk '{print $1" "$2}' "$pub")" > "$file.allowed_signers"
    airgap_ok "signature       -> ${file##*/}.sig"
    airgap_ok "trust anchor    -> ${file##*/}.allowed_signers  (identity: $identity)"
    airgap_ok "key fingerprint -> $(ssh-keygen -l -f "$pub" | awk '{print $2}')"
    airgap_warn "deliver ${file##*/}.allowed_signers to the target OUT-OF-BAND and confirm the"
    airgap_warn "fingerprint above against a value you already trust — do NOT ship it inside the bundle."
    return 0
}

# ---- architecture -----------------------------------------------------------
# One place that answers "which CPU are we packing for". Two vocabularies are in
# play and both are load-bearing, so both are named rather than converted ad hoc
# at each call site:
#
#   airgap_arch        Go's spelling (amd64, arm64). What upstream release URLs
#                      and the AIRGAP_*_SHA256_<arch> constants are keyed by.
#   airgap_arch_uname  `uname -m`'s spelling (x86_64, aarch64). What bundle
#                      FILENAMES carry, and therefore what the ingress router
#                      and the per-(scope,arch) git-daemon repos are named after
#                      (boxer ADR-0095; hackathon ADR-0015). An operator reading
#                      a tarball name expects the arch string their own host
#                      prints, so the two conventions stay separate even though
#                      one function derives from the other.
#
# Supported today: x86_64 and aarch64. Every pinned artefact a bundle carries —
# the Go SDK, TinyGo, Grafana, mcp-grafana, rclone — publishes a linux release
# for both. Anything else normalises to EMPTY rather than being guessed at, which
# is how the pinned-artefact stagers below decide to stage nothing and degrade.
#
# A bundle is NATIVE-ONLY by construction: nothing here cross-compiles, so the
# packing host's CPU is the bundle's CPU, and airgap_require_manifest_arch
# refuses one on a host it does not match.

# Normalise any of the spellings a system might print into Go's, or empty when
# the bundle flow has no support for it. Takes the name as an argument so it also
# serves the unbundler, which normalises what a MANIFEST recorded rather than
# what this host reports.
airgap_normalize_arch() {  # <arch-name>
    case "$1" in
        x86_64|amd64)  echo amd64 ;;
        aarch64|arm64) echo arm64 ;;
        *)             echo "" ;;
    esac
}

airgap_arch() { airgap_normalize_arch "$(uname -m)"; }

airgap_arch_uname() {
    case "$(airgap_arch)" in
        amd64) echo x86_64 ;;
        arm64) echo aarch64 ;;
        *)     echo "" ;;
    esac
}

# Resolve the pinned SHA-256 for THIS host out of a family of <prefix>_<goarch>
# constants, echoing it on stdout.
#
# Exists so that adding an architecture is a two-line change that cannot half
# apply. Every stager used to expand `eval "sha=\$AIRGAP_X_SHA256_$arch"`
# directly, which under the `set -u` the wrappers run with turns a case arm added
# WITHOUT its hash constant into `unbound variable` — aborting the whole pack
# instead of skipping one best-effort artefact. Here that is a warning naming the
# artefact and the missing constant, and a non-zero return the caller already
# knows how to handle.
#   args: <constant-prefix, e.g. AIRGAP_TINYGO_SHA256> <artefact label>
airgap_pinned_sha_for_arch() {
    local prefix="$1" label="$2" arch sha
    arch="$(airgap_arch)"
    [ -n "$arch" ] || {
        airgap_warn "unsupported architecture $(uname -m) — no pinned $label release (x86_64/aarch64 only)."
        return 1; }
    eval "sha=\${${prefix}_${arch}:-}"
    [ -n "$sha" ] || {
        airgap_warn "no pinned $label release for $arch (${prefix}_${arch} is unset) — not bundling it."
        return 1; }
    echo "$sha"
}

# Refuse to pack for an architecture this flow does not support, naming what it
# would have produced. Called by a bundler up front: the pinned-artefact stagers
# are individually best-effort, so without this an unsupported host does not fail
# — it quietly yields a bundle with no Grafana, no rclone, no pinned Go SDK, and
# an unbuilt render head, which is a worse thing to discover across an air gap
# than a refusal at pack time. Echoes the `uname -m` spelling on stdout.
airgap_require_arch() {
    local arch; arch="$(airgap_arch_uname)"
    [ -n "$arch" ] || airgap_die "unsupported architecture: $(uname -m).
  Airgap bundles are native-only (nothing here cross-compiles) and pinned
  artefacts are published for linux x86_64 and aarch64 only. Pack on one of those."
    echo "$arch"
}

# Refuse an extracted bundle whose recorded architecture is not this host's.
#
# A bundle carries native binaries for exactly one CPU: the shipped Go SDK and
# rustc, tinygo, Grafana, mcp-grafana, rclone, the static ffmpeg, and — in
# go-only scope — the prebuilt render head. With more than one architecture in
# circulation, grabbing the wrong tarball is an ordinary mistake, and unchecked
# its first symptom is
#     .../_airgap/toolchains/go/bin/go: cannot execute binary file: Exec format error
# several steps into provisioning; worse, a go-only bundle can provision cleanly
# and only fail when the prebuilt render head is launched.
#
# A bundle whose MANIFEST has no `arch=` line predates this record: warn, do not
# refuse. Such a bundle is far more likely to be an old one on its native host
# than a mismatched one, and there is nothing better to go on.
#   args: <manifest-path>
airgap_require_manifest_arch() {
    local manifest="$1" want got wantn gotn
    want="$(sed -n 's/^arch=//p' "$manifest" | head -1)"
    got="$(uname -m)"
    if [ -z "$want" ]; then
        airgap_warn "this bundle's MANIFEST records no arch — cannot confirm it was packed for $got."
        return 0
    fi
    wantn="$(airgap_normalize_arch "$want")"
    gotn="$(airgap_arch)"
    # Compare normalised where both are known, and literally otherwise, so an
    # architecture this library has never heard of still gets a useful check
    # rather than silently passing as "empty equals empty".
    if [ -n "$wantn" ] && [ -n "$gotn" ]; then
        [ "$wantn" = "$gotn" ] || airgap_die "architecture mismatch: this bundle was packed for $want, this host is $got.
  A bundle carries native binaries for one CPU only — the Go SDK, rustc, the
  static ffmpeg, the render head — and nothing in it cross-compiles.
  Fetch the $got bundle, or provision this one on an $want host."
    elif [ "$want" != "$got" ]; then
        airgap_die "architecture mismatch: this bundle was packed for $want, this host is $got."
    fi
    airgap_ok "architecture $got matches the bundle"
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

# Compile every DECLARED head offline from the vendor tree, with the toolchain the
# bundle ships, and record which ones actually built.
#
# This is what turns AIRGAP_IMZERO2_HEADS from a hint into a promise. The vendor
# tree already carries every head's crates (see that variable's note), so without
# this the bundle would be offering the target feature sets nothing had ever
# compiled — which is precisely the failure the single-head verify was added to
# prevent (an h3o release whose MSRV had drifted past the pinned channel shipped
# in a bundle before that check existed).
#
# ONE shared CARGO_HOME and ONE shared target dir across all heads, deliberately:
# the graphs overlap heavily, so each head after the first reuses most of the
# previous artifacts. Measured 2026-08-25 on a warm registry cache, release
# builds, in that shared dir:
#
#   headless        115s   (cold — pays for the shared base)
#   headless_soft   +24s   (166 crates, 161 of them already built)
#   headless_wgpu  +168s   (wgpu/ash/naga is a large new subtree)
#   ------------------------
#   all three       307s   = 2.7x one head, not 3x
#
# The shared target dir peaked at 1.6 GB. It lives under `mktemp -d`, so a packing
# host with a small tmpfs /tmp may need TMPDIR pointed at real disk — the
# single-head verify this replaced already used ~1 GB there.
#
# Returns non-zero ONLY when the DEFAULT head fails. That one is the bundle's
# primary deliverable, and shipping a Rust tree its own toolchain cannot build is
# the thing this check exists to stop. A secondary head that fails is dropped from
# the verified menu with a warning — the bundle then offers less rather than
# promising something untested.
#
# Sets AIRGAP_IMZERO2_HEADS_VERIFIED to the space-separated subset that compiled.
# A global rather than a stdout return, following airgap_pick_compressor: the
# progress output below would corrupt anything echoed.
#
#   args: <sysroot> <config.in> <vendordir> <cratedir> <default-head> <head...>
airgap_verify_imzero2_heads() {
    local sysroot="$1" configin="$2" vendordir="$3" cratedir="$4" default="$5"; shift 5
    local tmp_cargo rc=0 h t0 elapsed
    local -a heads=() ok=()
    AIRGAP_IMZERO2_HEADS_VERIFIED=""
    [ $# -gt 0 ] || { airgap_warn "no render heads declared — nothing to verify."; return 0; }
    heads=( "$@" )

    tmp_cargo="$(mktemp -d)"
    airgap_cargo_config_materialize "$configin" "$tmp_cargo/config.toml" "$vendordir"
    for h in "${heads[@]}"; do
        airgap_step "verify offline compile: --features $h"
        t0=$SECONDS
        if ( cd "$cratedir" && \
             CARGO_HOME="$tmp_cargo" CARGO_NET_OFFLINE=true \
             RUSTUP_TOOLCHAIN="$(basename "$sysroot")" \
               "$sysroot/bin/cargo" build --release --frozen \
                 --no-default-features --features "$h" \
                 --target-dir "$tmp_cargo/target" ); then
            elapsed=$(( SECONDS - t0 ))
            airgap_ok "$h compiles offline from the vendor tree (${elapsed}s)"
            ok+=( "$h" )
        else
            elapsed=$(( SECONDS - t0 ))
            if [ "$h" = "$default" ]; then
                airgap_warn "the DEFAULT head '$h' does NOT compile offline (${elapsed}s)."
                rc=1
            else
                airgap_warn "'$h' does not compile offline (${elapsed}s) — dropping it from the"
                airgap_warn "  declared menu, so the bundle will not offer it to the target."
            fi
        fi
    done
    rm -rf -- "$tmp_cargo"
    AIRGAP_IMZERO2_HEADS_VERIFIED="${ok[*]:-}"
    return $rc
}

# Is <head> in <menu>? Used by both the pack (to sanity-check its declaration)
# and the unbundler (to gate what the target may select).
airgap_head_in_menu() {  # <head> <menu>
    case " $2 " in *" $1 "*) return 0 ;; *) return 1 ;; esac
}

airgap_cargo_env_lines() {  # <rust_tc_bin_parent> <cargo_home>
    echo "export CARGO_HOME=\"$2\""
    echo "export CARGO_NET_OFFLINE=true"
    echo "export PATH=\"$1/bin:\$PATH\""
}

# ---- the render head's feature set ------------------------------------------
# The imzero2 feature set the airgap flow builds, in ONE place. The bundlers pass
# it to cargo, the MANIFEST publishes it, and the unbundler derives the target's
# requirements from it — so the binary that ships, the record of what shipped,
# and the list of things the operator is told to supply cannot disagree.
#
# `headless` is the lean appliance host (ADR-0128 SD6): carrier + FFFI2
# interpreter + the mesh draw-stream lane. Overridable so a caller can pack a
# different head; whatever it is set to is what the pack COMPILES (the full-scope
# offline verify and the go-only prebuilt both use this variable), so an override
# is verified rather than merely declared.
# NOTE the default here is only a conservative fallback. Each bundler DECLARES its
# own, because the two repos genuinely build different heads: boxer's
# rust/imzero2/build_rust_headless.sh — which its unbundler runs in both scopes —
# builds `headless_wgpu,fast_alloc`, while hackathon's flow builds `headless`.
# Getting this wrong is not cosmetic: it decides whether the operator is told to
# supply a Vulkan ICD and a C compiler.
AIRGAP_IMZERO2_FEATURES="${AIRGAP_IMZERO2_FEATURES:-headless}"

# The render heads a bundle DECLARES it can build on the airgapped side — the
# menu, of which AIRGAP_IMZERO2_FEATURES is the default pick.
#
# WHY A MENU IS POSSIBLE AT ALL. `cargo vendor` has no feature or target filter:
# it materializes every entry in Cargo.lock, all 560 of them, including `wgpu`,
# `ash`, `eframe`, `winit`, `mimalloc` and `egui_software_backend`. So a bundle
# that ships the Rust toolchain and the vendor tree already carries the crates for
# EVERY head — the choice of head is not a property of the payload, it is a
# decision the target can make at provision time. What the pack adds is the
# guarantee: each declared head is COMPILED here, offline, with the toolchain the
# bundle ships, before it is offered.
#
# The declaration is therefore a promise, not a hint, and it is bounded by what
# the pack is willing to compile. A head that fails to build here is dropped from
# the menu rather than offered untested — except the default, whose failure is
# fatal, because that one is the bundle's primary deliverable.
#
# Only meaningful for a bundle that ships toolchain + vendor. One that ships a
# prebuilt head offers exactly that head and no choice.
AIRGAP_IMZERO2_HEADS="${AIRGAP_IMZERO2_HEADS:-headless headless_soft headless_wgpu}"

# Capabilities a feature set implies, as space-separated words. This is the whole
# of the mapping from "what was built" to "what the target must supply". The three
# are INDEPENDENT — deriving one from another is how the old contract went wrong:
#
#   raster  the build can rasterize frames into host memory (`headless_raster`),
#           so the ffmpeg encoder lane is COMPILED IN and fires as soon as
#           IMZERO2_HEADLESS_LISTEN or _H264_OUT is set. ffmpeg becomes a
#           requirement, not a nicety. Note `desktop` does NOT imply this: it
#           renders to a window through eframe and never enables headless_raster,
#           so the encoder lane is absent there.
#   wgpu    the build carries wgpu, so it needs a Vulkan loader + ICD at runtime.
#   cc      a C toolchain (and pkg-config) at BUILD time. Two independent causes:
#           the wgpu/eframe graph, which pulls `wayland-sys` and probes with
#           pkg-config; and mimalloc via `fast_alloc`, which `default` carries.
#           Either one alone is enough, which is why this is not folded into wgpu.
#
# Measured on 2026-08-25 with `cargo tree` plus release builds run with CC and
# CXX pointed at a nonexistent binary:
#
#   feature set     crates  wgpu  builds w/o cc  libvulkan  ffmpeg
#   headless           161   no        yes          no      unused (mesh lane)
#   headless_soft      166   no        yes          no      REQUIRED
#   headless_wgpu      199  yes         -          yes      REQUIRED
#
# The lean pair's only -sys crate is `linux-raw-sys` (pure Rust); headless_wgpu
# adds `wayland-sys` and `renderdoc-sys`. An unrecognised feature set yields
# `unknown`, and the preflight then checks EVERYTHING rather than quietly telling
# an operator they need nothing.
airgap_render_head_caps() {  # <feature-string>
    local f=",${1//[[:space:]]/,}," caps="" known=0
    case "$f" in *,headless_soft,*|*,headless_wgpu,*|*,headless_raster,*)
        caps="raster"; known=1 ;; esac
    case "$f" in *,headless_wgpu,*|*,desktop,*|*,default,*)
        caps="$caps wgpu"; known=1 ;; esac
    case "$f" in *,headless_wgpu,*|*,desktop,*|*,default,*|*,fast_alloc,*)
        caps="$caps cc"; known=1 ;; esac
    # Bare `headless` implies none of the three, but it IS a known set — so it
    # must not fall through to `unknown`.
    case "$f" in *,headless,*) known=1 ;; esac
    [ "$known" = 1 ] || { echo unknown; return 0; }
    caps="${caps# }"
    echo "$caps"
}

airgap_caps_have() {  # <caps> <word>
    case " $1 " in *" $2 "*) return 0 ;; *) return 1 ;; esac
}

# Caps with an unrecognised feature set resolved to the STRICTEST answer rather
# than the emptiest. A bundle packed before `imzero2_features` was recorded has
# no feature line at all, and for that case asking the operator for too much is
# the safe direction — promising them "you need nothing" is not.
airgap_render_head_caps_effective() {  # <feature-string>
    local c; c="$(airgap_render_head_caps "$1")"
    [ "$c" = unknown ] && c="raster wgpu cc"
    echo "$c"
}

# ---- preflight (target-side; warnings only) ---------------------------------
# Only reached for a build whose graph actually pulls a C-requiring crate — see
# airgap_preflight_render_head. The lean heads do not: they build AND link with
# no compiler on the box at all (measured, see the table above).
airgap_preflight_c_compiler() {
    if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || command -v clang >/dev/null 2>&1
        then airgap_ok "C compiler present (the wgpu graph needs one at build time)"
        else airgap_warn "no C compiler (cc/gcc/clang) — building the wgpu render head will fail"; fi
    command -v pkg-config >/dev/null 2>&1 \
        && airgap_ok "pkg-config present" || airgap_warn "pkg-config absent — wayland-sys and friends probe with it"
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

# Stage the pinned ffmpeg SOURCE tarballs, so the target can rebuild or
# re-configure the encoder offline rather than only re-verify the binary.
#
# WHY: the bundle already carried build-static-ffmpeg.sh, verify-ffmpeg-lanes.sh
# and bench-ffmpeg-lanes.sh, on the stated grounds that the target could
# "re-verify or rebuild the bundled encoder binary without a boxer checkout".
# Only re-verify actually worked. A rebuild died on
#   missing <src-dir>/<tarball> (pass --fetch, or stage the tarballs first)
# and --fetch needs exactly the network the target does not have. ~90 MB closes
# that gap, and it is the doctrine the Rust and Go halves already follow: the
# target gets source it can change, not just a binary it must accept.
#
# ONLY the tarballs. The source cache is also the build's working directory — it
# accumulates extracted trees, _b/, _prefix/ and _logs/, several hundred MB of
# host-specific output that must not travel.
#
# BEST-EFFORT: returns non-zero when nothing was staged. Note the tarballs are
# fetched BEFORE the build proper but AFTER the preflight, so a host that fails
# the preflight has neither binary nor sources, while one that fails mid-compile
# still has sources to pass on — which is the useful case, since a target with the
# build toolchain can then produce what the packing host could not.
#   args: <destdir> <srccache>
airgap_ship_ffmpeg_sources() {
    local dest="$1" cache="$2" n=0 f
    [ -d "$cache" ] || { airgap_warn "no ffmpeg source cache at $cache — not shipping sources."; return 1; }
    mkdir -p "$dest" || return 1
    for f in "$cache"/*.tar.*; do
        [ -f "$f" ] || continue
        cp -p "$f" "$dest/" && n=$((n + 1))
    done
    if [ "$n" = 0 ]; then
        rmdir "$dest" 2>/dev/null || true
        airgap_warn "no ffmpeg source tarballs in $cache — not shipping sources."
        return 1
    fi
    airgap_ok "shipped $n ffmpeg source tarballs ($(du -sh "$dest" | cut -f1)) -> ${dest##*/}"
    return 0
}

# Fetch the pinned codec tarballs into <cachedir> WITHOUT building anything, for
# a pack that ships sources and defers the binary (see airgap_augment_ffmpeg).
# Delegates to the builder's --fetch-only so the pinned versions and URLs stay in
# exactly one place; that mode also skips the build-host preflight, so a packing
# host with no C toolchain can still stage sources.
#   args: <cachedir>
airgap_fetch_ffmpeg_sources() {
    local cache="$1" builder="$AIRGAP_LIB_DIR/build-static-ffmpeg.sh"
    [ -x "$builder" ] || { airgap_warn "build-static-ffmpeg.sh not found at $builder."; return 1; }
    airgap_step "fetch ffmpeg codec sources (no build)"
    "$builder" --src-dir "$cache" --fetch-only >/dev/null || {
        airgap_warn "could not fetch the ffmpeg sources."; return 1; }
    airgap_ok "ffmpeg sources staged in ${cache##*/}"
    return 0
}

# ---- ffmpeg sidecars --------------------------------------------------------
# A sidecar is the alternative to re-packing: instead of rewriting a ~900 MB
# bundle to insert one 21 MB binary — which invalidates the packer's signature —
# the augment step emits the binary as its OWN small signed artefact, and the
# bundle travels untouched.
#
# WHY THAT IS BETTER, not just cheaper. Re-packing forces one signature to cover
# work done by two parties: whoever assembled the payload and whoever compiled the
# encoder. Whoever signs last vouches for both, so the augment host must hold a key
# the target trusts. With a sidecar each party signs exactly what it produced, and
# the chain composes to arbitrary length:
#
#   pack -> [re-sign] -> augment(sidecar) -> [sign sidecar] -> verify both -> apply -> provision
#
# The packer's signature stays valid forever because its artefact never changes,
# and the augment host needs no release key at all — only a key the target is
# willing to accept for encoders.
#
# THE BINDING. A sidecar must not be applicable to an arbitrary bundle, and the
# thing that actually determines the binary is the sources it was built from plus
# the architecture. So a sidecar records a digest over the bundle's codec tarballs
# and its own arch, and applying re-computes that digest from the bundle in hand.
# Binding to the *sources* rather than to the bundle's bytes is deliberate: the
# same encoder is legitimately correct for any bundle carrying those same pinned
# sources for that arch, which is exactly when you want to reuse it.

# Stable digest over the codec tarballs in <srcdir>: sha256 of the sorted
# "<sha256>  <name>" listing, so it is independent of directory order and of any
# build leftovers sitting beside them.
#   args: <srcdir>
airgap_ffmpeg_sources_digest() {  # <srcdir>
    local d="$1" listing
    listing="$( cd "$d" 2>/dev/null && sha256sum ./*.tar.* 2>/dev/null | sort -k2 )" || true
    [ -n "$listing" ] || { airgap_warn "no codec tarballs under $d — cannot digest sources."; return 1; }
    printf '%s\n' "$listing" | sha256sum | cut -d' ' -f1
}

# Copy a bundle's codec tarballs into a scratch build directory.
#
# NOT a nicety. build-static-ffmpeg.sh uses --src-dir as its WORKING directory:
# it extracts the trees there and creates _b/, _prefix/ and _logs/ beside them. So
# building straight out of _airgap/ffmpeg-src would leave several hundred MB of
# host-specific output inside the bundle — which breaks the sidecar's whole promise
# that the bundle is untouched, and for the re-pack path would bloat the result and
# violate the "tarballs only" rule the pack asserts. 90 MB of copying buys a
# read-only bundle.
#   args: <srcdir> <builddir>
airgap_stage_ffmpeg_build_dir() {  # <srcdir> <builddir>
    local src="$1" dst="$2" n=0 f
    mkdir -p "$dst" || return 1
    for f in "$src"/*.tar.*; do
        [ -f "$f" ] || continue
        cp -p "$f" "$dst/" && n=$((n + 1))
    done
    [ "$n" -gt 0 ] || { airgap_warn "no codec tarballs under $src."; return 1; }
    return 0
}

# Build the encoder from a bundle's own sources, verify every lane natively, and
# package it as a signed-able sidecar tarball. The bundle is NOT modified.
#   args: <bundle-root> <out.tar.zst> [extra build-static-ffmpeg.sh args...]
airgap_emit_ffmpeg_sidecar() {
    local root="$1" out="$2"; shift 2
    local man="$root/_airgap/MANIFEST" src="$root/_airgap/ffmpeg-src"
    local builder="" verifier="" d work dig ver

    [ -f "$man" ] || { airgap_warn "no MANIFEST at $man."; return 1; }
    [ -d "$src" ] || { airgap_warn "no ffmpeg sources at $src — only a bundle packed with sources can produce a sidecar."; return 1; }
    for d in "$root/boxer/scripts/dev" "$root/scripts/dev"; do
        [ -x "$d/build-static-ffmpeg.sh" ] && { builder="$d/build-static-ffmpeg.sh"; verifier="$d/verify-ffmpeg-lanes.sh"; break; }
    done
    [ -n "$builder" ] || { airgap_warn "build-static-ffmpeg.sh not found inside $root."; return 1; }

    dig="$(airgap_ffmpeg_sources_digest "$src")" || return 1

    airgap_step "sidecar: check this host can build ffmpeg"
    # --preflight-only exits before creating any directory, so this cannot write
    # into the bundle even though it names it.
    "$builder" --src-dir "$src" --preflight-only "$@" || {
        airgap_warn "this host cannot build ffmpeg — see the missing prerequisites above."
        return 1; }

    work="$(mktemp -d)"
    mkdir -p "$work/ffmpeg-sidecar"
    airgap_stage_ffmpeg_build_dir "$src" "$work/build" || { rm -rf -- "$work"; return 1; }
    airgap_step "sidecar: build static ffmpeg from the bundle's sources (offline)"
    # Built in scratch, so the bundle stays byte-for-byte as it arrived — that is
    # the sidecar's entire point. No --fetch either: the tarballs came from the
    # bundle, so this touches no network.
    "$builder" --src-dir "$work/build" --out "$work/ffmpeg-sidecar/ffmpeg" "$@" || {
        rm -rf -- "$work"; airgap_warn "the ffmpeg build failed — no sidecar written."; return 1; }

    if [ -x "$verifier" ]; then
        airgap_step "sidecar: verify every codec lane natively"
        "$verifier" "$work/ffmpeg-sidecar/ffmpeg" || {
            rm -rf -- "$work"
            airgap_warn "the freshly built ffmpeg FAILED lane verification — no sidecar written."
            return 1; }
    else
        airgap_warn "verify-ffmpeg-lanes.sh not in this bundle — packaging unverified."
    fi

    ver="$("$work/ffmpeg-sidecar/ffmpeg" -hide_banner -version 2>/dev/null | head -1)"
    {
        echo "kind=ffmpeg"
        echo "arch=$(uname -m)"
        echo "goarch=$(airgap_arch)"
        echo "built=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        echo "built_by=airgap-augment"
        echo "ffmpeg=$ver"
        echo "lanes_verified=$([ -x "$verifier" ] && echo yes || echo no)"
        # The binding. Applying re-computes this from the bundle it is given.
        echo "sources_digest=$dig"
        # ...and the per-tarball detail, so a mismatch can be diagnosed rather
        # than merely reported.
        ( cd "$src" && sha256sum ./*.tar.* | sort -k2 | sed 's|^|source_tarball=|' )
        # Informational provenance about the bundle this was built from.
        sed -n 's/^\(boxer_head\|boxer_tag\|hackathon_head\|scope\)=/bundle_\1=/p' "$man"
    } > "$work/ffmpeg-sidecar/SIDECAR"

    airgap_pick_compressor
    tar -C "$work" -cf - ffmpeg-sidecar | airgap_compress "$out" || {
        rm -rf -- "$work"; airgap_warn "could not write $out."; return 1; }
    rm -rf -- "$work"
    airgap_ok "sidecar -> ${out##*/} ($(du -h "$out" | cut -f1)); the bundle is untouched"
    return 0
}

# Install a sidecar's encoder into an extracted bundle, refusing any sidecar that
# was not built for THIS bundle's sources and architecture.
#
# Needs no build toolchain: this is the step that runs on the airgapped target.
#   args: <bundle-root> <sidecar.tar.zst>
airgap_apply_ffmpeg_sidecar() {
    local root="$1" side="$2"
    local man="$root/_airgap/MANIFEST" src="$root/_airgap/ffmpeg-src"
    local work sm dig want_arch got_arch kind

    [ -f "$man" ] || { airgap_warn "no MANIFEST at $man."; return 1; }
    [ -f "$side" ] || { airgap_warn "no such sidecar: $side"; return 1; }

    work="$(mktemp -d)"
    case "$side" in
        *.zst) command -v zstd >/dev/null 2>&1 || { rm -rf -- "$work"; airgap_warn "zstd not found."; return 1; }
               tar -I zstd -xf "$side" -C "$work" ;;
        *.gz)  tar -xzf "$side" -C "$work" ;;
        *)     rm -rf -- "$work"; airgap_warn "unrecognised sidecar extension: $side"; return 1 ;;
    esac
    sm="$work/ffmpeg-sidecar/SIDECAR"
    [ -f "$sm" ] && [ -f "$work/ffmpeg-sidecar/ffmpeg" ] || {
        rm -rf -- "$work"; airgap_warn "$side does not look like an ffmpeg sidecar."; return 1; }

    kind="$(sed -n 's/^kind=//p' "$sm" | head -1)"
    [ "$kind" = ffmpeg ] || { rm -rf -- "$work"; airgap_warn "sidecar kind is '$kind', expected 'ffmpeg'."; return 1; }

    # Architecture: the sidecar's, the bundle's, and this host's must all agree.
    got_arch="$(sed -n 's/^arch=//p' "$sm" | head -1)"
    want_arch="$(sed -n 's/^arch=//p' "$man" | head -1)"
    if [ -n "$want_arch" ] && [ "$(airgap_normalize_arch "$got_arch")" != "$(airgap_normalize_arch "$want_arch")" ]; then
        rm -rf -- "$work"
        airgap_warn "sidecar was built for $got_arch but this bundle is for $want_arch."
        return 1
    fi
    if [ "$(airgap_normalize_arch "$got_arch")" != "$(airgap_arch)" ]; then
        rm -rf -- "$work"
        airgap_warn "sidecar was built for $got_arch but this host is $(uname -m) — it would not run here."
        return 1
    fi

    # The binding: this encoder must have been built from THESE sources.
    if [ -d "$src" ]; then
        dig="$(airgap_ffmpeg_sources_digest "$src")" || { rm -rf -- "$work"; return 1; }
        # Read the sidecar's claim into a variable BEFORE any cleanup: the whole
        # value of this diagnostic is showing both sides of the mismatch, and
        # reading $sm after removing $work printed an empty one.
        local claimed; claimed="$(sed -n 's/^sources_digest=//p' "$sm" | head -1)"
        if [ "$dig" != "$claimed" ]; then
            rm -rf -- "$work"
            airgap_warn "sidecar was built from DIFFERENT codec sources than this bundle carries."
            airgap_warn "  bundle:  $dig"
            airgap_warn "  sidecar: ${claimed:-<absent>}"
            airgap_warn "  Use the sidecar built from this bundle, or re-emit one from it."
            return 1
        fi
        airgap_ok "sidecar matches this bundle's codec sources"
    else
        airgap_warn "this bundle carries no codec sources, so the sidecar's binding cannot be checked."
        airgap_warn "  Proceeding on the strength of its signature alone (verify it first!)."
    fi

    mkdir -p "$root/_airgap/bin"
    install -m 0755 "$work/ffmpeg-sidecar/ffmpeg" "$root/_airgap/bin/ffmpeg"
    local ver stamp
    ver="$(sed -n 's/^ffmpeg=//p' "$sm" | head -1)"
    stamp="applied from sidecar, built by $(sed -n 's/^built_by=//p' "$sm" | head -1) on $got_arch $(sed -n 's/^built=//p' "$sm" | head -1)"
    sed -i "s|^ffmpeg=.*|ffmpeg=$ver|" "$man"
    if grep -q '^ffmpeg_augmented=' "$man"; then
        sed -i "s|^ffmpeg_augmented=.*|ffmpeg_augmented=$stamp|" "$man"
    else
        printf 'ffmpeg_augmented=%s\n' "$stamp" >> "$man"
    fi
    rm -rf -- "$work"
    airgap_ok "installed _airgap/bin/ffmpeg from the sidecar and recorded its provenance"
    return 0
}

# ---- augmenting a packed bundle ---------------------------------------------
# Build the static ffmpeg from the sources ALREADY INSIDE an extracted bundle and
# install it as that bundle's binary, then verify it with the bundle's own lane
# verifier.
#
# WHY THIS EXISTS — the third step. A bundle is native-only because nothing in it
# cross-compiles, which normally means the packing host must BE the target's
# architecture. ffmpeg is the one piece that escapes: its sources are
# architecture-independent, they now travel, and the build needs no network at
# all (--fetch is never passed; the tarballs are right there). So the native build
# can be deferred to a THIRD host that is neither the packing host nor the
# airgapped destination:
#
#   1. pack       connected host, any architecture   --ffmpeg-source-only
#   2. augment    offline host, TARGET architecture  this function
#   3. provision  offline target, no C toolchain     airgap-unbundle.sh
#
# Step 2 needs a C/C++ build toolchain (cmake, make, cc, a static libc, this CPU's
# assembler) and nothing else — no network, no dhall, no Go, no Rust. That is what
# separates it from packing, and why it can happen on a transit box, a build
# runner of the right arch, or the target itself before it is isolated.
#
# It VERIFIES what it builds, natively, with the verify-ffmpeg-lanes.sh the bundle
# already carries: 9 checks over the four codec lanes plus the Annex-B file sink.
# That is stronger than the pack-time check it replaces, because it runs on the
# architecture that will actually encode. A binary that fails is NOT installed.
#
# The MANIFEST is updated so provenance stays visible: the ffmpeg line reflects the
# new binary and ffmpeg_augmented records where and when it was built, because a
# bundle whose binary did not come from its packing host should say so.
#
#   args: <extracted-bundle-root> [extra build-static-ffmpeg.sh args...]
airgap_augment_ffmpeg() {
    local root="$1"; shift
    local man="$root/_airgap/MANIFEST" src="$root/_airgap/ffmpeg-src"
    local builder="" verifier="" d out

    [ -f "$man" ] || { airgap_warn "no MANIFEST at $man — is this an extracted bundle?"; return 1; }
    [ -d "$src" ] || { airgap_warn "no ffmpeg sources at $src.
  Only a bundle packed with sources can be augmented; re-pack with
  --ffmpeg-source-only (or without --no-ffmpeg) to carry them."; return 1; }

    # The bundle layout differs between repos: boxer's root IS the boxer tree,
    # a downstream bundle nests it under boxer/. Try both rather than assume.
    for d in "$root/boxer/scripts/dev" "$root/scripts/dev"; do
        [ -x "$d/build-static-ffmpeg.sh" ] && { builder="$d/build-static-ffmpeg.sh"; verifier="$d/verify-ffmpeg-lanes.sh"; break; }
    done
    [ -n "$builder" ] || { airgap_warn "build-static-ffmpeg.sh not found inside $root."; return 1; }

    # Refuse early and with the package list rather than dying mid-compile: this
    # host may be a transit box that was never meant to be a build host.
    airgap_step "augment: check this host can build ffmpeg"
    "$builder" --src-dir "$src" --preflight-only "$@" || {
        airgap_warn "this host cannot build ffmpeg — see the missing prerequisites above."
        airgap_warn "  Augmenting needs a C/C++ toolchain (but no network). Run it somewhere else,"
        airgap_warn "  or let the airgapped target supply its own ffmpeg."
        return 1; }

    local work; work="$(mktemp -d)"
    airgap_stage_ffmpeg_build_dir "$src" "$work/build" || { rm -rf -- "$work"; return 1; }
    out="$work/ffmpeg-augmented"
    airgap_step "augment: build static ffmpeg from the bundled sources (offline)"
    # Built in scratch rather than in _airgap/ffmpeg-src: the builder works in its
    # --src-dir, and leaving extracted trees plus _b/_prefix/_logs inside the
    # bundle would both bloat the re-pack and break the "tarballs only" rule the
    # pack asserts. No --fetch, so this touches no network.
    "$builder" --src-dir "$work/build" --out "$out" "$@" || {
        rm -rf -- "$work"
        airgap_warn "the ffmpeg build failed — the bundle is left unchanged."; return 1; }

    if [ -x "$verifier" ]; then
        airgap_step "augment: verify every codec lane natively"
        "$verifier" "$out" || {
            airgap_warn "the freshly built ffmpeg FAILED lane verification — NOT installing it."
            airgap_warn "  The bundle keeps whatever it had. Investigate before shipping this."
            rm -rf -- "$work"; return 1; }
    else
        airgap_warn "verify-ffmpeg-lanes.sh not in this bundle — installing unverified."
    fi

    mkdir -p "$root/_airgap/bin"
    install -m 0755 "$out" "$root/_airgap/bin/ffmpeg"
    rm -rf -- "$work"

    # Rewrite the two MANIFEST lines in place, appending ffmpeg_augmented if absent.
    local ver; ver="$("$root/_airgap/bin/ffmpeg" -hide_banner -version 2>/dev/null | head -1)"
    local stamp; stamp="built by airgap-augment on $(uname -m) $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    sed -i "s|^ffmpeg=.*|ffmpeg=$ver|" "$man"
    if grep -q '^ffmpeg_augmented=' "$man"; then
        sed -i "s|^ffmpeg_augmented=.*|ffmpeg_augmented=$stamp|" "$man"
    else
        printf 'ffmpeg_augmented=%s\n' "$stamp" >> "$man"
    fi
    airgap_ok "installed _airgap/bin/ffmpeg ($(du -h "$root/_airgap/bin/ffmpeg" | cut -f1)) and recorded provenance"
    return 0
}

# Emit the env line pointing at the shipped ffmpeg sources, so a rebuild on the
# target does not have to know the bundle layout. No-op when none were staged.
#   args: <ffmpeg-src-dir>
airgap_ffmpeg_src_env_lines() {  # <srcdir>
    [ -d "$1" ] || return 0
    echo "export IMZERO2_FFMPEG_SRC=\"$1\""
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

# Preflight exactly the target-side requirements the bundle's render head
# actually creates, and say so for the ones it does not.
#
# WHY THIS EXISTS. These three checks used to be keyed on the wrong thing, and
# the result was a deploy contract wrong in both directions at once:
#
#   * the C compiler + pkg-config check ran for `--scope full` and blamed
#     `libmimalloc-sys` — a crate that is NOT in the graph the airgap flow
#     builds. mimalloc sits behind imzero2's `fast_alloc`, which lives only in
#     `default`, and every airgap build passes `--no-default-features`. Measured:
#     `--features headless` and `--features headless_soft` both build and LINK
#     with CC=/nonexistent, producing a binary whose entire dynamic contract is
#     libc/libm/libgcc_s/ld.so.
#   * the Vulkan check ran unconditionally, and the docs promised an ICD was
#     needed "for the wgpu render head" — but the lean heads carry no wgpu at
#     all. Measured: no `libvulkan` reference in either binary.
#   * ffmpeg was treated as merely nice to have, when a rasterizing head has the
#     encoder lane COMPILED IN and spawns it the moment the carrier or the H264
#     sink is configured.
#
# Asking an airgapped operator for a Vulkan ICD nothing loads, while calling the
# one genuinely required binary optional, is the worst way for this to be wrong:
# a missing ICD is exactly the thing that gets chased for hours on an isolated
# host. So the requirements are now DERIVED from the feature set the MANIFEST
# records, and each is reported as required, satisfied, or not applicable.
#
#   args: <feature-string> <ffmpeg-path> <target-builds-the-head: yes|no> [ffmpeg-src-dir]
airgap_preflight_render_head() {
    local features="$1" ffmpeg="$2" builds="${3:-no}" ffsrc="${4:-}" caps
    caps="$(airgap_render_head_caps "$features")"
    if [ "$caps" = unknown ]; then
        airgap_warn "unrecognised render-head feature set '$features' — cannot derive what this"
        airgap_warn "  target needs, so EVERY requirement is checked below. Teach"
        airgap_warn "  airgap_render_head_caps about it to get an accurate report."
        caps="$(airgap_render_head_caps_effective "$features")"
    else
        airgap_ok "render head: --no-default-features --features $features${caps:+ ($caps)}"
    fi

    if airgap_caps_have "$caps" wgpu; then
        airgap_preflight_vulkan
    else
        airgap_ok "no wgpu in this build — needs no Vulkan loader/ICD"
    fi

    # Only a target that COMPILES the head can need a C toolchain for it, so this
    # is silent for a bundle that ships the render head prebuilt.
    if [ "$builds" = yes ]; then
        if airgap_caps_have "$caps" cc; then
            airgap_preflight_c_compiler
        else
            airgap_ok "this Rust graph is pure Rust — needs no C compiler or pkg-config to build"
        fi
    fi

    if airgap_caps_have "$caps" raster; then
        if ! airgap_preflight_ffmpeg "$ffmpeg"; then
            airgap_warn "ffmpeg is NOT bundled, and this build requires one: it rasterizes frames,"
            airgap_warn "  so the encoder lane is compiled in and spawns ffmpeg as soon as"
            airgap_warn "  IMZERO2_HEADLESS_LISTEN or IMZERO2_HEADLESS_H264_OUT is set."
            # A source-only bundle has a better answer than "find one yourself":
            # the sources are right here. Saying so is the difference between a
            # solvable situation and an apparent dead end on an isolated host.
            if [ -n "$ffsrc" ] && [ -d "$ffsrc" ]; then
                airgap_warn "  BUT this bundle carries the sources, so you can build one here:"
                airgap_warn "    <bundle>/hackathon_2026/scripts/dev/airgap-augment.sh <bundle-root>"
                airgap_warn "  (needs a C/C++ toolchain on this host, no network). Otherwise:"
            else
                airgap_warn "  The environment must supply it:"
            fi
            airgap_preflight_services ffmpeg
        fi
    else
        airgap_ok "mesh draw-stream lane only (no raster) — this build never spawns ffmpeg"
    fi
    return 0
}

# ---- pinned upstream prebuilt tools -----------------------------------------
# Everything else in a bundle is either vendored source, compiled here from
# source (ffmpeg), or a copy of a toolchain the packing operator already
# installed and trusts (the Go SDK, the Rust sysroot). TinyGo fits none of those
# routes — building it means building LLVM — so it is taken as an upstream
# **prebuilt release artefact**.
#
# That is weaker provenance than the rest of the bundle, so the pin carries the
# whole of the guarantee: the artefact is fetched from an exact-version URL and
# refused unless its SHA-256 matches the constant here. Bump a version and its
# hash together — a bump with a stale hash fails closed (nothing is staged) and
# says so. Downloads are cached across runs, and the MANIFEST records what the
# bundle actually got.
#
# The fetch primitive is deliberately general: a future tool that cannot be built
# in a packing script belongs here too, on the same terms.

# Fetch <url> into <cachedir> unless already cached, verify its SHA-256 against
# <sha256>, and echo the cached path. A file that fails to verify is deleted, so
# a corrupted or truncated cache entry re-fetches on the next run rather than
# failing forever. Returns non-zero, with the reason on stderr, when the artefact
# could not be obtained or did not verify.
#   args: <url> <sha256> <cachedir>
airgap_fetch_pinned() {
    local url="$1" want="$2" cache="$3"
    local file="${url##*/}" path got
    mkdir -p "$cache" || return 1
    path="$cache/$file"
    if [ ! -f "$path" ]; then
        command -v curl >/dev/null 2>&1 || { airgap_warn "curl not found — cannot fetch $file."; return 1; }
        # Download to .part and rename, so an interrupted transfer is never left
        # behind as a complete-looking cache entry (build-static-ffmpeg.sh does
        # the same).
        if ! curl -sSL --retry 3 -o "$path.part" "$url"; then
            rm -f "$path.part"
            airgap_warn "download failed: $url"
            return 1
        fi
        mv -f "$path.part" "$path"
    fi
    got="$(sha256sum "$path" | cut -d' ' -f1)"
    if [ "$got" != "$want" ]; then
        airgap_warn "SHA-256 mismatch for $file — discarding it."
        airgap_warn "  expected $want"
        airgap_warn "  actual   $got"
        rm -f "$path"
        return 1
    fi
    echo "$path"
}

# TinyGo — pinned upstream release (github.com/tinygo-org/tinygo).
AIRGAP_TINYGO_VERSION=0.41.1
AIRGAP_TINYGO_SHA256_amd64=e156d1d93a376eef639a4143d13be07e8c463fb6cf2d7d447698ed4474d23e91
AIRGAP_TINYGO_SHA256_arm64=789733bc3b5bace0bd1835a267b3ea267804a7ef1cfe69bc522c295f5226d624

# Stage a whole TinyGo distribution — a TINYGOROOT: bin/ (tinygo + wasm-opt),
# lib/, src/, targets/ — into <destdir>, which becomes the root itself. TinyGo
# derives its root from the location of its own executable, so the tree must stay
# intact and nothing has to export TINYGOROOT on the target.
#
# Unlike the shipped Go SDK and rustc, the tinygo binary is statically linked, so
# it constrains the target's CPU architecture but not its libc. It does still need
# a `go` on PATH; the bundle's shipped GOROOT supplies that.
#
# BEST-EFFORT, like airgap_ship_ffmpeg: an architecture with no pinned release
# (see airgap_pinned_sha_for_arch), a missing curl, or a hash mismatch warns and
# stages nothing, leaving the target to supply its own tinygo. Returns non-zero
# when nothing was staged.
#   args: <destdir> <cachedir>
airgap_ship_tinygo() {
    local dest="$1" cache="$2"
    local arch sha url tarball work
    arch="$(airgap_arch)"
    sha="$(airgap_pinned_sha_for_arch AIRGAP_TINYGO_SHA256 TinyGo)" || return 1
    url="https://github.com/tinygo-org/tinygo/releases/download/v${AIRGAP_TINYGO_VERSION}/tinygo${AIRGAP_TINYGO_VERSION}.linux-${arch}.tar.gz"
    airgap_step "stage TinyGo ${AIRGAP_TINYGO_VERSION} (linux-${arch})"
    tarball="$(airgap_fetch_pinned "$url" "$sha" "$cache")" || return 1
    # Unpack beside the destination rather than in /tmp: the tree is ~1.2 GB, and
    # a same-filesystem move afterwards is a rename instead of a second copy.
    work="$(dirname "$dest")/.tinygo-unpack.$$"
    rm -rf -- "$work"
    mkdir -p "$work"
    if ! tar -xzf "$tarball" -C "$work"; then
        rm -rf -- "$work"
        airgap_warn "could not extract ${tarball##*/} — not bundling tinygo."
        return 1
    fi
    if [ ! -x "$work/tinygo/bin/tinygo" ]; then
        rm -rf -- "$work"
        airgap_warn "unexpected TinyGo tarball layout (no tinygo/bin/tinygo) — not bundling tinygo."
        return 1
    fi
    rm -rf -- "$dest"
    mv "$work/tinygo" "$dest"
    rm -rf -- "$work"
    airgap_ok "shipped TinyGo ($(du -sh "$dest" | cut -f1)) -> ${dest##*/}"
    return 0
}

# Compile a trivial package to wasm with the *bundled* tinygo and the *bundled*
# Go SDK, proving the pair the bundle ships can actually build — the TinyGo
# analogue of the Go vendor build and the Rust offline compile. It is the one
# check that catches a TinyGo release that has not caught up with the Go version
# in GOROOT, a mismatch whose first symptom would otherwise appear on the far
# side of the air gap. Cheap: a few seconds cold, under a second warm.
#
# Warns and returns non-zero on failure; the caller decides whether that is fatal.
#   args: <tinygoroot> <goroot> [target]
airgap_verify_tinygo() {
    local root="$1" goroot="$2" target="${3:-wasi}"
    local tmp out
    [ -x "$root/bin/tinygo" ] || return 1
    tmp="$(mktemp -d)"
    printf 'module airgapsmoke\n\ngo 1.23\n'      > "$tmp/go.mod"
    printf 'package main\n\nfunc main() {}\n'     > "$tmp/main.go"
    # A deliberately minimal env: the repo's GOFLAGS=-mod=vendor would look for a
    # vendor/ tree this throwaway module does not have, GOWORK=off keeps any
    # ambient workspace out of it, and GOPROXY=off proves the build reaches for
    # nothing.
    if out="$( cd "$tmp" && env -u GOFLAGS \
                 PATH="$goroot/bin:$PATH" GOROOT="$goroot" \
                 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off \
                 "$root/bin/tinygo" build -target="$target" -o "$tmp/out.wasm" . 2>&1 )"; then
        rm -rf -- "$tmp"
        airgap_ok "bundled tinygo compiles for -target=$target with the bundled Go SDK"
        return 0
    fi
    rm -rf -- "$tmp"
    airgap_warn "bundled tinygo failed to build -target=$target:"
    printf '%s\n' "$out" | head -5 >&2
    return 1
}

# Emit the env lines for a bundled TinyGo. Two hooks, deliberately:
#   BOXER_TINYGO  — extbin's declared override for the `tinygo` program, so
#                   boxer's own resolution is pinned at the bundled copy and does
#                   not depend on PATH order.
#   PATH          — so an operator can just run `tinygo`. ffmpeg is kept off PATH
#                   because it is a common system tool the bundle must not shadow
#                   for everything else; tinygo is not, and an airgapped target
#                   has no other one for this to displace.
#   args: <tinygoroot>
airgap_tinygo_env_lines() {
    [ -x "$1/bin/tinygo" ] || return 0
    echo "export BOXER_TINYGO=\"$1/bin/tinygo\""
    echo "export PATH=\"$1/bin:\$PATH\""
}

# Report a bundled tinygo, running it to prove the target can. Returns 0 when the
# bundle supplies a working one, so the caller can skip preflighting a host copy.
#   args: <tinygoroot> [goroot]
airgap_preflight_tinygo() {
    local bin="$1/bin/tinygo" v
    [ -x "$bin" ] || return 1
    if v="$(PATH="${2:+$2/bin:}$PATH" "$bin" version 2>&1 | head -1)"; then
        airgap_ok "tinygo bundled ($v); BOXER_TINYGO points at it"
        return 0
    fi
    airgap_warn "bundled tinygo will not run here: $v"
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
