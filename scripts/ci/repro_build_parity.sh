#!/bin/bash
# Reproducible-build parity gate (ADR-0215). Builds the Go host and the lean
# Rust render head twice each — from two copies of the tree at two different
# paths, into cold caches — and byte-compares the pair. Drift exits non-zero;
# scripts/ci/lint.sh wires this in.
#
# WHY TWO COPIES AT TWO PATHS. The failures ADR-0215 measured were all path
# leakage: a checkout path or $CARGO_HOME compiled into panic locations and
# generated-file names. Building the same checkout twice cannot see that class
# at all — both builds embed the same path. The copies here sit at paths of
# different lengths on purpose, so an embedded path shifts the string pool and
# every relocation after it, which is what made the original drift visible.
#
# WHY COLD CACHES. With a warm GOCACHE the second Go build reuses every compiled
# package and only re-links, so the comparison would be of the linker alone. Each
# copy gets its own GOCACHE and its own in-crate cargo target dir. The Go module
# cache and $CARGO_HOME are shared: they hold inputs, not outputs, and
# go-build-env.sh / rust-repro-env.sh remap them identically in both builds.
#
# WHAT IT TESTS. The tree as git sees it — tracked files plus untracked
# not-ignored ones — so a contributor can run it before committing. Copies have
# no .git, so `-buildvcs=auto` stamps nothing in either; that matches the airgap
# bundle shape, and for one commit the stamp is constant anyway.
#
# COST. Measured on an 8-thread laptop, 2026-09-02: the Go pair ~3 min, the Rust
# pair (`--features headless`, the mesh-only appliance head — the smallest graph
# that carries build.rs codegen, which is where the OUT_DIR leak lived) ~4 min.
# Disk: two tree copies (~90 MB each) plus two Go caches and two cargo target
# dirs, about 4 GB in all. The work dir is `mktemp -d`, so a small tmpfs /tmp wants
# TMPDIR pointed at real disk, or REPRO_PARITY_WORKDIR set outright.
#
#   --go-only               skip the Rust pair (a quick local run).
#   REPRO_PARITY_WORKDIR    use this directory instead of mktemp -d.
#   REPRO_PARITY_KEEP=1     leave the work dir behind, to diff a drift by hand.
#
# Graceful skip of the Rust pair when cargo or the pinned toolchain is absent,
# matching h3_wasm_parity.sh; the Go pair always runs, Go being mandatory.

set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

go_only=0
case "${1:-}" in
    --go-only) go_only=1 ;;
    "") ;;
    *) echo "usage: $0 [--go-only]" >&2; exit 2 ;;
esac

if [ -n "${REPRO_PARITY_WORKDIR:-}" ]; then
    work="$REPRO_PARITY_WORKDIR"
    mkdir -p "$work"
else
    work=$(mktemp -d)
fi
if [ -z "${REPRO_PARITY_KEEP:-}" ]; then
    trap 'rm -rf "$work"' EXIT
else
    echo "repro_build_parity: keeping work dir $work"
fi

# Two copies at paths of different lengths (see the header).
copy_a="$work/a/boxer"
copy_b="$work/bb/boxer-copy"

copy_tree() {  # <dst>
    mkdir -p "$1"
    # --ignore-failed-read: a tracked file deleted in the working tree is listed
    # by --cached and absent on disk; the copy should mirror the tree, not fail.
    git ls-files -z --cached --others --exclude-standard \
        | tar --null -T - -cf - --ignore-failed-read 2>/dev/null \
        | tar -xf - -C "$1"
}
copy_tree "$copy_a"
copy_tree "$copy_b"

rc=0

# Report one pair. Prints ok with the hash, or DRIFT with sizes, the count of
# differing bytes and the first differing strings — the clue that pointed at
# mimalloc's __TIME__ and at the leaked paths in the original audit.
compare() {  # <label> <file_a> <file_b>
    local label="$1" a="$2" b="$3" ha hb
    if [ ! -f "$a" ] || [ ! -f "$b" ]; then
        echo "repro_build_parity: $label: ERROR: build produced no artifact ($a, $b)" >&2
        return 1
    fi
    ha=$(sha256sum "$a" | awk '{print $1}')
    hb=$(sha256sum "$b" | awk '{print $1}')
    if [ "$ha" = "$hb" ]; then
        echo "repro_build_parity: $label ok ($ha)"
        return 0
    fi
    {
        echo "repro_build_parity: $label DRIFT"
        echo "  a: $ha  ($(wc -c < "$a") bytes)  $a"
        echo "  b: $hb  ($(wc -c < "$b") bytes)  $b"
        echo "  differing bytes: $(cmp -l "$a" "$b" 2>/dev/null | wc -l)"
        if command -v strings >/dev/null 2>&1; then
            echo "--- strings only in a (first 20) ---"
            comm -23 <(strings "$a" | sort -u) <(strings "$b" | sort -u) | head -20
            echo "--- strings only in b (first 20) ---"
            comm -13 <(strings "$a" | sort -u) <(strings "$b" | sort -u) | head -20
        fi
    } >&2
    return 1
}

# ---- Go: ./public/app --------------------------------------------------------
# Built exactly as boxer.sh does: the sourced env file, nothing else. GOCACHE is
# per copy (cold); the module cache is shared. GOWORK=off so a workspace file
# above the checkout — which the copies are not under anyway — cannot change
# what resolves.
go_build() {  # <copy> <out>
    (
        cd "$1"
        # shellcheck source=/dev/null
        source scripts/dev/go-build-env.sh
        # shellcheck disable=SC2086 # deliberate word splitting of BOXER_GO_FLAGS
        GOCACHE="$1/.gocache" GOWORK=off \
            go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" -o "$2" ./public/app
    )
}
go_build "$copy_a" "$work/app.a" >/dev/null 2>&1 || { echo "repro_build_parity: go: build a failed" >&2; go_build "$copy_a" "$work/app.a"; }
go_build "$copy_b" "$work/app.b" >/dev/null 2>&1 || { echo "repro_build_parity: go: build b failed" >&2; go_build "$copy_b" "$work/app.b"; }
compare "go ./public/app" "$work/app.a" "$work/app.b" || rc=1

# ---- Rust: rust/imzero2 --features headless --------------------------------
if [ "$go_only" = 1 ]; then
    echo "repro_build_parity: rust: skipped (--go-only)"
    exit $rc
fi
if ! command -v cargo >/dev/null 2>&1; then
    echo "repro_build_parity: rust: skipped (cargo not installed)"
    exit $rc
fi
if ! (cd rust/imzero2 && cargo --version >/dev/null 2>&1); then
    echo "repro_build_parity: rust: skipped (pinned toolchain for rust/imzero2 unavailable)"
    exit $rc
fi

# Built as build_rust_headless_mesh.sh does: cd into the crate (rustup reads the
# pin from the CWD), source the remap file with the crate as its root, relative
# in-crate target dir (the one constraint rust-repro-env.sh states). A caller
# who sourced rust-repro-env.sh in their own shell would otherwise hand us a
# RUSTFLAGS remapping THEIR checkout, so the three variables are cleared first.
rust_build() {  # <copy>
    (
        cd "$1/rust/imzero2"
        unset RUSTFLAGS RUST_REPRO_ENV_APPLIED RUST_REPRO_ROOT
        # shellcheck source=/dev/null
        source ../../scripts/dev/rust-repro-env.sh
        cargo build --release --locked --no-default-features --features headless \
            --target-dir target/repro
    )
}
rust_build "$copy_a" >/dev/null 2>&1 || { echo "repro_build_parity: rust: build a failed" >&2; rust_build "$copy_a"; }
rust_build "$copy_b" >/dev/null 2>&1 || { echo "repro_build_parity: rust: build b failed" >&2; rust_build "$copy_b"; }
compare "rust imzero2 --features headless" \
    "$copy_a/rust/imzero2/target/repro/release/imzero2" \
    "$copy_b/rust/imzero2/target/repro/release/imzero2" || rc=1

exit $rc
