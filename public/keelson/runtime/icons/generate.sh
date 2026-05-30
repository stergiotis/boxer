#!/bin/bash
# Regenerate phosphor.out.go and phosphor_lookup.out.go from the
# vendored Phosphor catalogue. See ADR-0044 §SD3.
#
# Pipeline:
#   1. SHA-verify phosphor-icons.json (the vendored input).
#   2. iconsgen reads the JSON and emits the two Go source files.
#
# Source of the JSON: this file is downloaded from a stergiotis/ids-fonts
# release ('phosphor-icons.json' alongside Phosphor.ttf). The TS→JSON
# conversion runs in that repo's CI; see
# https://github.com/stergiotis/ids-fonts/blob/main/scripts/ts-to-json.py
#
# Bumping the upstream pin:
#   - bump PHOSPHOR_VERSION in ../../../ids-fonts and tag a release
#   - re-vendor the released phosphor-icons.json here
#   - regenerate SHA256SUMS (sha256sum phosphor-icons.json > SHA256SUMS)
#   - re-run this script
#
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

# Re-verify the SHA of the vendored JSON. CI runs the same check via
# scripts/ci/lint.sh; running it here catches drift before regenerating
# downstream artefacts against an out-of-date input.
sha256sum -c SHA256SUMS

# Regenerate phosphor.out.go and phosphor_lookup.out.go from the JSON.
# boxer.sh builds ./public/app with the repo tag set, then runs the iconsgen
# subcommand (the former cmd/iconsgen main, folded into app per the
# entry-point standard).
../../../../boxer.sh iconsgen generate \
	--iconsJson ./phosphor-icons.json \
	--constsOut ./phosphor.out.go \
	--lookupOut ./phosphor_lookup.out.go \
	--package icons

gofmt -l -w .
