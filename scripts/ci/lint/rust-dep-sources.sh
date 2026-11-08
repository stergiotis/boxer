#!/bin/bash
# Where Rust dependencies may come from (ADR-0363 §M7, proposed): crates.io,
# or a vendored copy in this repository. zapfast's check-dependency-sources.py
# is the model; a dependency source that moves silently is a supply-chain
# change nobody reviewed.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.
#
# Over every tracked file:
#   Cargo.lock      every `source` is the crates.io index. A git or other
#                   registry source fails.
#   Cargo.toml      no `git =` and no `registry =` on a dependency, and every
#                   entry of a `[patch.*]` table is `{ path = "vendor/<dir>" }`
#                   with a VENDORING.md in that directory, which records the
#                   upstream commit and every local delta.
#   .cargo/config*  no `[source...]` replacement and no `[registries...]`.
#
# RUST_DEP_SOURCES_ROOT checks another tree instead of this repository; the
# step's own test uses it.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=${RUST_DEP_SOURCES_ROOT:-$(cd "$here/../../.." && pwd)}
cd "$root"

crates_io='registry+https://github.com/rust-lang/crates.io-index'
fail=0
report() {
    echo "rust-dep-sources: $1"
    fail=1
}

while IFS= read -r lock; do
    while IFS= read -r src; do
        report "$lock: dependency source $src is not crates.io"
    done < <(sed -n 's/^source = "\(.*\)"$/\1/p' "$lock" | grep -vxF "$crates_io" | sort -u)
done < <(git ls-files -- '*Cargo.lock')

while IFS= read -r manifest; do
    dir=$(dirname "$manifest")
    # Comments stripped, so a commented-out alternative ("# egui = { git = …
    # }") stays allowed as the note it is.
    body=$(sed 's/#.*$//' "$manifest")
    if grep -nE '(^|[{,[:space:]])git[[:space:]]*=' <<<"$body" >/dev/null; then
        report "$manifest: a git dependency"
    fi
    if grep -nE '(^|[{,[:space:]])registry[[:space:]]*=' <<<"$body" >/dev/null; then
        report "$manifest: a dependency from another registry"
    fi
    # Every key line inside a [patch.*] table.
    while IFS= read -r entry; do
        name=${entry%%=*}
        name=${name//[[:space:]]/}
        path=$(sed -n 's/.*path[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' <<<"$entry")
        if [[ -z $path || $path != vendor/* ]]; then
            report "$manifest: [patch] entry $name is not a path into vendor/"
            continue
        fi
        if [[ ! -f $dir/$path/VENDORING.md ]]; then
            report "$manifest: [patch] entry $name points at $path, which has no VENDORING.md"
        fi
    done < <(awk '
        /^[[:space:]]*\[/ { inpatch = ($0 ~ /^[[:space:]]*\[patch\./) ; next }
        inpatch && /=/ { print }
    ' <<<"$body")
done < <(git ls-files -- '*Cargo.toml')

while IFS= read -r config; do
    if grep -nE '^[[:space:]]*\[(source|registries)' "$config" >/dev/null; then
        report "$config: a source replacement or an extra registry"
    fi
done < <(git ls-files -- '*.cargo/config' '*.cargo/config.toml')

if [[ $fail -ne 0 ]]; then
    exit 1
fi
echo "passed"
