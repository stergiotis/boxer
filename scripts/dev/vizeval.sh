#!/bin/bash
# vizeval.sh — build the imzero2 host once and run `imzero2 vizeval` with it.
#
# The same build as scene.sh, into the same launcher cache: the harness
# (ADR-0257, proposed) renders each candidate as a scene, and the scene
# launcher starts the host as a child of its own executable.
#
# Usage:
#   scripts/dev/vizeval.sh space apps/play/vizeval
#   scripts/dev/vizeval.sh score apps/play/vizeval
#   scripts/dev/vizeval.sh score --candidates c.jsonl apps/play/vizeval/10_host_metrics.vizeval.md
#
# Scoring needs ClickHouse (the scenario datasets run there) and the headless
# Rust client, which the scene launcher finds or names the script to build.
set -euo pipefail
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../.." && pwd)
# shellcheck source=/dev/null
source "$here/go-build-env.sh"

# Stable per checkout, built under a unique name and renamed into place: at most
# one binary on disk, never a half-written one exec'd, and two worktrees never
# run each other's build.
cache="${XDG_CACHE_HOME:-${HOME:-${TMPDIR:-/tmp}}/.cache}/boxer-launcher"
cache="$cache/imzero2-scene-$(printf '%s' "$root" | cksum | cut -d' ' -f1)"
mkdir -p "$cache"
app="$cache/app"
build=$(mktemp "$cache/build.XXXXXXXXXX")
trap 'rm -f -- "$build"' EXIT
find "$cache" -maxdepth 1 -name 'build.*' -mmin +60 -delete 2>/dev/null || true

tags="${BOXER_GO_TAGS:+$BOXER_GO_TAGS,}binary_log"
# shellcheck disable=SC2086 # deliberate word splitting of the flag list
( cd "$root" && CGO_ENABLED=0 go build $BOXER_GO_FLAGS -tags "$tags" \
	-o "$build" ./public/thestack/cmd/imzero2/ ) 1>&2
mv -f -- "$build" "$app"
exec "$app" --logFormat=console --logLevel="${SCENE_LOG_LEVEL:-warn}" imzero2 vizeval "$@"
