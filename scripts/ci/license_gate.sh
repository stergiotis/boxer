#!/bin/bash
# License compliance gate. Generates a CycloneDX 1.6 SBOM via
# cyclonedx-gomod and feeds it to internal/cmd/licensegate, which
# applies the forbidden/restricted policy in policy.go. Exits non-zero
# if any module's elected license falls into a violating category.
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

sbom=$(mktemp --suffix=.json)
trap 'rm -f "$sbom"' EXIT

# cyclonedx-gomod mod operates module-wide and does not honour build
# tags (ADR-0004 SD3, SD9). -licenses=true populates the per-component
# license evidence the gate consumes; -test=true broadens scope to
# include test-only transitive deps (SD8).
go tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod mod \
    -licenses=true \
    -test=true \
    -json \
    -output "$sbom"

go run ./internal/cmd/licensegate -sbom "$sbom"
