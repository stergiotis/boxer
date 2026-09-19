#!/bin/bash
# License compliance gate. Generates a CycloneDX 1.6 SBOM via
# cyclonedx-gomod for the Go module graph and `cargo metadata` for every
# Rust crate tree, and feeds both to `boxer gov license-gate`, which
# applies the forbidden/restricted policy in
# public/gov/licensegate/policy.go. Exits non-zero if any module's or
# crate's elected license falls into a violating category. The Rust half
# is ADR-0246.
# boxer is MIT-licensed and cannot accept copyleft inbound dependencies;
# the gate enforces this prospectively. See ADR-0004
# (doc/adr/0004-license-gate-cyclonedx.md) for the full rationale and
# THIRD_PARTY_NOTICES.md §3 for the policy contract.
#
# The gate intentionally does NOT fail on `unknown` classifications.
# Some upstream Go modules ship their LICENSE in a form the detector
# cannot classify (e.g. LICENSE.md instead of LICENSE, or an Apache
# header in a non-canonical layout). Such cases surface in a trailing
# advisory block for periodic manual review but do not block CI;
# see ADR-0004 SD5.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"

# The Rust half needs cargo, and cargo metadata resolves manifests, so a
# reachable registry too (ADR-0246 SD1). A gate that skipped the crate
# trees when cargo is absent would report them clean without looking.
if ! command -v cargo >/dev/null; then
    printf 'license gate: cargo is required to classify the Rust crate trees (ADR-0246)\n' >&2
    exit 1
fi

sbom=$(mktemp --suffix=.json)
cargo_dir=$(mktemp -d)
trap 'rm -f "$sbom"; rm -rf "$cargo_dir"' EXIT

# cyclonedx-gomod mod operates module-wide and does not honour build
# tags (ADR-0004 SD3, SD9). -licenses=true populates the per-component
# license evidence the gate consumes; -test=true broadens scope to
# include test-only transitive deps (SD8).
go tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod mod \
    -licenses=true \
    -test=true \
    -json \
    -output "$sbom"

# Every crate tree with its own lockfile, discovered rather than listed, so
# a new one is gated the day it lands (ADR-0246 SD5). --locked makes a
# lockfile that no longer matches its manifest a failure, not a silent
# re-resolution the gate would then classify.
cargo_flags=()
shopt -s nullglob
for lock in rust/*/Cargo.lock; do
    tree=$(dirname "$lock")
    out="$cargo_dir/$(basename "$tree").json"
    (cd "$tree" && cargo metadata --locked --format-version 1 >"$out")
    cargo_flags+=(--cargo-metadata "$out")
done
shopt -u nullglob
if [[ ${#cargo_flags[@]} -eq 0 ]]; then
    printf 'license gate: no rust/*/Cargo.lock found; refusing to report the Rust half clean\n' >&2
    exit 1
fi

go run -tags "$tags" ./public/app gov license-gate --sbom "$sbom" "${cargo_flags[@]}"
