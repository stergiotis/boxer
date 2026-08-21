#!/bin/bash
# fsbrowser-widget-scene.sh — assert the file browser widget's interaction,
# headless. The scene itself lives in the tour, as `34_fsbrowser`; this is the
# name ADR-0200 §M1 gives it.
#
# ADR-0200's verification plan asks for a headless scene over the browser
# widget (ADR-0154): that a row click selects, that Enter on a selected
# directory enters it, that Backspace goes back up, and that the outline mode
# renders. The scene asserts the demo's READOUT lines, not the picture — a
# wrong read-back would still look right — and leaves two captures behind for
# a human to look at.
#
# It moved into scripts/dev/play-screenshot-tour.sh on 2026-08-21 so that one
# runner owns the build, the FFFI staleness guard, the port teardown and the
# capture index for every headless scene. Edit the trace THERE.
#
# Exit status is the assertion: a `wait` that never resolves fails the run, so
# a broken selection path exits non-zero rather than producing a wrong picture.
#
# Usage:
#   scripts/dev/fsbrowser-widget-scene.sh
#   FSSCENE_BUILD=0 scripts/dev/fsbrowser-widget-scene.sh  # reuse rust/imzero2/ binaries
#
# The two captures land in the tour's output directory (tmp/play-tour unless
# FSSCENE_OUT says otherwise), as 34_fsbrowser_list.png and
# 34_fsbrowser_outline.png, beside the index the tour writes.
#
# Equivalent to `scripts/dev/play-screenshot-tour.sh 34_fsbrowser`, which takes
# the tour's own PLAYSHOT_* knobs. The five below are forwarded for the
# invocations this script documented before the move; FSSCENE_SIZE is not among
# them, because the scene now pins its own viewport to the gallery's window.
#
# FSSCENE_DRY=1 is forwarded too but does not pass, and did not before the move
# either: a dry run resolves anchors WITHOUT actuating, and most of this trace
# waits on state that only a gesture produces ("selected: internal"), so it
# stops at the first such wait. Dry run suits a trace whose anchors are all
# present at mount; this one's are not.
set -uo pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")

env=()
[[ -n "${FSSCENE_OUT:-}" ]]     && env+=("PLAYSHOT_OUT=$FSSCENE_OUT")
[[ -n "${FSSCENE_TIMEOUT:-}" ]] && env+=("PLAYSHOT_TIMEOUT=$FSSCENE_TIMEOUT")
[[ -n "${FSSCENE_BUILD:-}" ]]   && env+=("PLAYSHOT_BUILD=$FSSCENE_BUILD")
[[ -n "${FSSCENE_DRY:-}" ]]     && env+=("PLAYSHOT_DRY=$FSSCENE_DRY")
[[ -n "${FSSCENE_PORT:-}" ]]    && env+=("PLAYSHOT_PORT=$FSSCENE_PORT")

exec env "${env[@]}" "$here/play-screenshot-tour.sh" 34_fsbrowser
