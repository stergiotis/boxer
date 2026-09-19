#!/bin/bash
# Rasterises every non-ASCII glyph in a Go string literal against the real
# client font chains and fails on any that no face draws.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys — here a
# non-zero exit fails the gate.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."

# A glyph a control is drawn from has to come from a font the CLIENT loads, not
# from the fallback chain — see the imzero2 skill §12 "Oversized, Off-Centre
# Glyph". This rasterises every non-ASCII glyph in a Go string literal against
# the real family chains from apphost.rs and fails on any that no face draws:
# those render as empty boxes, and the failure is invisible to every automated
# lane we have, because a scene's font stack is thinner than a desktop's.
#
# Baselined (scripts/ci/glyph-baseline.txt) rather than absolute: some sites are
# terminal text or a deliberate fallback showcase. A new site fails; an accepted
# one passes and carries its reason in that file.
#
# Skipped, not failed, when Pillow or fontconfig is missing, or when fontconfig
# does not return the families this audits against — a contributor's font setup
# is not a finding about the repo, and the script says so and exits 0.
if ! command -v python3 >/dev/null 2>&1 || ! python3 -c "import PIL" >/dev/null 2>&1; then
    echo "skipped: needs python3 + Pillow (python3-pillow)"
    exit 0
fi
if ! command -v fc-match >/dev/null 2>&1; then
    echo "skipped: needs fontconfig (fc-match)"
    exit 0
fi
if glyph_out=$(./scripts/dev/glyph-audit.py --baseline scripts/ci/glyph-baseline.txt 2>&1); then
    echo "$glyph_out" | tail -3
    exit 0
fi
echo "$glyph_out"
exit 1
