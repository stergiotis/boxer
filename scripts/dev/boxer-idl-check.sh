#!/bin/bash
# Fail when boxer's generated egui2 sources disagree with the egui2 IDL.
#
# The Rust imzero2 client and the Go host speak FFFI2 through code that
# `boxer egui2gen` generates from the IDL in
# public/thestack/imzero2/egui2/definition: enums_out.rs and the marker-fenced
# dispatch block of interpreter.rs on the Rust side, bindings/*.out.go on the Go
# side. Both are committed, so cargo and go only see the IDL through them. An IDL
# edit that was not regenerated, or was regenerated for one side only, therefore
# still compiles, and the client and the host then disagree about the wire with
# nothing saying so.
#
# This re-runs the generator into a scratch copy and compares the generated
# parts with the committed ones. It never writes to boxer's source tree: on
# drift it prints what differs and how to regenerate, and exits 1.
#
# A passing result is remembered as a stamp under the crate's target/ directory,
# keyed on the IDL, the generator and the generated parts only — hand-written
# Rust outside the marker fences does not invalidate it. Until one of those
# changes, the check costs a few sha256sums; after a change it builds a minimal
# generator binary (seconds on a warm Go cache).
#
#   scripts/dev/boxer-idl-check.sh                 # check (cached)
#   scripts/dev/boxer-idl-check.sh --force         # ignore the stamp
#   scripts/dev/boxer-idl-check.sh --state NAME    # own state directory
#
# Downstream repositories call this from their launch and build scripts. Each
# passes its own --state NAME (default boxer-idl-check), so concurrent callers
# never share a scratch generator or a stamp.
#
# BOXER_IDL_CHECK=0 skips the check. BOXER_ROOT overrides which boxer is checked
# (default: the checkout this script lives in). A boxer that is not writable —
# the Go module cache — is a published commit with nothing to regenerate, and is
# skipped.

set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")

force=0
state=boxer-idl-check
while [ $# -gt 0 ]; do
    case "$1" in
        --force) force=1; shift ;;
        --state)
            [ $# -ge 2 ] || { echo "ERROR: --state needs a name" >&2; exit 2; }
            state=$2; shift 2 ;;
        -h|--help) grep '^#' "$BASH_SOURCE" | sed '1d; s/^# \?//'; exit 0 ;;
        *) echo "ERROR: unknown argument: $1" >&2; exit 2 ;;
    esac
done
case "$state" in
    ''|*[!A-Za-z0-9._-]*|.*) echo "ERROR: --state must match [A-Za-z0-9._-]+ and not start with '.': $state" >&2; exit 2 ;;
esac

if [ "${BOXER_IDL_CHECK:-1}" = 0 ]; then
    echo "boxer-idl-check: skipped (BOXER_IDL_CHECK=0)" >&2
    exit 0
fi

BOXER_ROOT="${BOXER_ROOT:-$here/../..}"
BOXER_ROOT=$(cd "$BOXER_ROOT" && pwd) || { echo "ERROR: BOXER_ROOT not found" >&2; exit 1; }
CRATE_REL="rust/imzero2"
RUST_GEN_REL="$CRATE_REL/src/imzero2"
GO_GEN_REL="public/thestack/imzero2/egui2/bindings"
STATE_REL="$CRATE_REL/target/$state"
STATE_DIR="$BOXER_ROOT/$STATE_REL"
STAMP="$STATE_DIR/verified.sha256"

if [ ! -w "$BOXER_ROOT/$CRATE_REL" ]; then
    echo "boxer-idl-check: skipped ($BOXER_ROOT is read-only: a published boxer)" >&2
    exit 0
fi

# The generated part of a Rust file: all of enums_out.rs, and in any other file
# only the blocks the generator splices between its marker fences
# (/*----- //IMZERO2_… -----*/ … /*===== //IMZERO2_… =====*/). Empty for a file
# with no fences.
generated_rust_part() {
    if [ "$(basename "$1")" = enums_out.rs ]; then
        cat -- "$1"
    else
        awk '/\/\*-+ \/\/IMZERO2_/ {on = 1} on {print} /\/\*=+ \/\/IMZERO2_/ {on = 0}' "$1"
    fi
}

# Everything the generated code is a function of, plus the generated code
# itself: the IDL, the egui2 driver and the FFFI2 generator, the rustfmt
# configuration the Rust side is formatted with, and boxer's module graph.
fingerprint() {
    (
        cd "$BOXER_ROOT" || exit 1
        {
            find public/thestack/imzero2/egui2/definition \
                 public/thestack/imzero2/egui2/driver \
                 public/thestack/fffi2/compiletime \
                 -name '*.go' -not -name '*_test.go' -type f
            find "$GO_GEN_REL" -maxdepth 1 -name '*.out.go' -type f
            printf '%s\n' "$CRATE_REL/rustfmt.toml" "$CRATE_REL/rust-toolchain" go.mod go.sum tags
        } | LC_ALL=C sort | xargs sha256sum || exit 1
        for f in "$RUST_GEN_REL/enums_out.rs" $(grep -l '/\*-* //IMZERO2_' "$RUST_GEN_REL"/*.rs | LC_ALL=C sort); do
            printf '%s  %s\n' "$(generated_rust_part "$f" | sha256sum | cut -d' ' -f1)" "$f"
        done
    ) | sha256sum | cut -d' ' -f1
}

fp=$(fingerprint) || { echo "ERROR: unable to fingerprint the egui2 IDL inputs" >&2; exit 1; }
if [ "$force" = 0 ] && [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$fp" ]; then
    exit 0
fi

echo "boxer-idl-check: egui2 IDL or generated sources changed — verifying they agree" >&2
mkdir -p "$STATE_DIR/_gen" || exit 1
scratch=$(mktemp -d "$STATE_DIR/run.XXXXXX") || exit 1
trap 'rm -rf -- "$scratch"' EXIT

# A minimal generator: the egui2 driver alone, rather than all of boxer's
# public/app. It lives in the crate's gitignored target/, inside boxer's module
# so it builds against boxer's working tree; the leading underscore keeps it out
# of `go build ./...` there. The generator name is pinned to the one public/app
# stamps into the "Code generated by" headers, which is derived from the main
# package's path.
cat > "$STATE_DIR/_gen/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/driver"
)

func main() {
	if len(os.Args) != 3 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: gen <rustOutputBasePath> <goOutputBasePath>")
		os.Exit(2)
	}
	driver.CodeGeneratorName = "TheStack (github.com/stergiotis/boxer/public/app)"
	err := driver.GenerateRustFiles(os.Args[1])
	if err == nil {
		err = driver.GenerateGoFiles("bindings", os.Args[2])
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
EOF

# boxer's tags and build environment, as scripts/dev/generate.sh uses; in a
# subshell so neither leaks into the caller. GOWORK=off: the generator is
# boxer's alone, and a workspace above the checkout would pull every sibling
# module into its build. The exception is a vendored workspace (an extracted
# airgap bundle): boxer has no vendor/ of its own there, GOFLAGS=-mod=vendor and
# there is no module cache or proxy behind it, so outside the workspace the
# build fails with "inconsistent vendoring". The workspace's vendor/ is the only
# place its dependencies exist, so the build stays in it.
gen="$STATE_DIR/gen"
(
    cd "$BOXER_ROOT" || exit 1
    # shellcheck source=/dev/null
    source "$BOXER_ROOT/scripts/dev/go-build-env.sh"
    gowork=off
    ws="$(go env GOWORK 2>/dev/null)"
    if [ -n "$ws" ] && [ "$ws" != off ] && [ -f "$(dirname "$ws")/vendor/modules.txt" ]; then
        gowork="$ws"
    fi
    # shellcheck disable=SC2086 # deliberate word splitting of the flag list
    GOWORK="$gowork" go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" -o "$gen" "./$STATE_REL/_gen"
) || { echo "ERROR: unable to build the egui2 generator" >&2; exit 1; }

# The Rust generator splices into existing files at marker comments and runs
# rustfmt from the file's directory, so the scratch copy mirrors the crate
# layout: the pinned rust-toolchain and rustfmt.toml must be found walking up,
# and the whole src/ tree is copied because rustfmt follows `mod` declarations
# (interpreter.rs has children under src/imzero2/interpreter/).
# cp -R does not create missing parents of its destination (GNU coreutils
# fails with "cannot create directory"), so the whole path is made up front.
mkdir -p "$scratch/crate/src/imzero2" "$scratch/bindings" || exit 1
cp "$BOXER_ROOT/$CRATE_REL/rustfmt.toml" "$BOXER_ROOT/$CRATE_REL/rust-toolchain" "$scratch/crate/" || exit 1
# The whole module tree: rustfmt resolves `mod` declarations, and interpreter.rs
# has a child module under interpreter/ (capture_replay).
cp -R "$BOXER_ROOT/$RUST_GEN_REL"/. "$scratch/crate/src/imzero2/" || exit 1
"$gen" "$scratch/crate/src/imzero2/" "$scratch/bindings" 2> "$scratch/gen.log" || {
    cat "$scratch/gen.log" >&2
    echo "ERROR: egui2 generator failed" >&2
    exit 1
}
# The generator only warns when rustfmt fails and keeps the unformatted file,
# which would then be reported as drift against the formatted committed one.
if grep -q 'unable to format rust file' "$scratch/gen.log"; then
    grep 'unable to format rust file' "$scratch/gen.log" >&2
    echo "ERROR: rustfmt failed on the regenerated sources; the comparison would be meaningless" >&2
    exit 1
fi

drift=()
for f in "$scratch/crate/src/imzero2"/*.rs; do
    committed="$BOXER_ROOT/$RUST_GEN_REL/$(basename "$f")"
    cmp -s <(generated_rust_part "$f") <(generated_rust_part "$committed") ||
        drift+=("$RUST_GEN_REL/$(basename "$f")")
done
for f in "$scratch/bindings"/*; do
    cmp -s "$f" "$BOXER_ROOT/$GO_GEN_REL/$(basename "$f")" || drift+=("$GO_GEN_REL/$(basename "$f")")
done

if [ ${#drift[@]} -gt 0 ]; then
    rm -f -- "$STAMP"
    {
        echo "ERROR: the egui2 IDL and boxer's generated FFFI2 code disagree."
        echo "  Stale in $BOXER_ROOT:"
        printf '    %s\n' "${drift[@]}"
        echo "  A client built now would not match the host's bindings. Regenerate with"
        echo "    $BOXER_ROOT/scripts/dev/generate.sh"
        echo "  (or BOXER_IDL_CHECK=0 to skip this check)."
    } >&2
    exit 1
fi

# Re-fingerprint: the stamp must describe what was verified, and the tree may
# have moved underneath the generator run.
fp2=$(fingerprint) || exit 1
[ "$fp2" = "$fp" ] && printf '%s\n' "$fp" > "$STAMP"
echo "boxer-idl-check: egui2 generated sources match the IDL" >&2
