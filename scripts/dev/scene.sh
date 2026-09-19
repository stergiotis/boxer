#!/bin/bash
# scene.sh — build the imzero2 host once and run `imzero2 scene` with it.
#
# The runner is the host binary (ADR-0248 §SD4): it launches the app a scene
# names as a child of its own executable, so there is nothing to build per
# scene. This script is only the build: one binary per checkout, at a stable
# path, in the launcher cache of ADR-0179.
#
# Usage:
#   scripts/dev/scene.sh apps/play/scenes                 # a directory is a tour
#   scripts/dev/scene.sh --only history apps/play/scenes
#   scripts/dev/scene.sh --dryRun public/…/scenes/x.scene.md
#   scripts/dev/scene.sh --help
#
# The headless Rust client is not built here; the runner says which script
# builds it when it finds none, or finds one older than the generated sources.
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
exec "$app" --logFormat=console --logLevel="${SCENE_LOG_LEVEL:-warn}" imzero2 scene --repoRoot "$root" "$@"
