#!/bin/bash
# fetcher-discipline.sh — Enforce the imzero2 fetcher-invocation rule.
#
# Background: ImZero2 fetchers (Fetcher.Fetch* methods) must only be
# called from StateManager.Sync at frame end. Calling them inline from
# a widget render body works at top scope but deadlocks the render
# loop the moment that body is wrapped in a deferred-block capture
# (a dock.Tab body, an etable row body, …). See
# doc/skills/imzero2-fetchers/SKILLS.md for the full explanation.
#
# This script greps the imzero2 Go tree for `.Fetch[A-Z]` references
# that are not in the allowlist below, and exits non-zero if any are
# found. New legitimate call sites must update the allowlist; if you
# can't justify why a fetcher should run outside StateManager.Sync,
# that's the lint doing its job.
#
# Scope note: the rule is specific to the egui2 Fetcher type, so the
# scan is confined to public/thestack/imzero2. This keeps two unrelated
# matches out of the picture — the caching read-through fetcher under
# public/caching, and the fffi2 fetcher-IDL codegen builder under
# public/thestack/fffi2 (which constructs IDL, it does not invoke a
# Fetcher at runtime).
#
# Usage: scripts/ci/fetcher-discipline.sh

set -euo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
repo_root=$(cd "$here/../.." && pwd)
cd "$repo_root"

scope="public/thestack/imzero2"

# Files allowed to contain Fetch* references.
#
# - egui2_statemanagement.go : the canonical home of fetch calls.
# - fetchers.out.go          : the generated fetcher methods themselves.
# - egui2_globals.go         : defines the Fetcher type + its invoke().
# - definition/              : codegen IDL sources that name fetchers.
# - *_test.go                : tests are exempt; they may exercise
#                              fetchers directly for fixture purposes.
# - EXPLANATION.md, SKILLS.md: doc strings naming the fetchers.
# - videooutput.go           : ADR-0088 status-bar codec indicator.
#                              ShowStatus refreshes the capability /
#                              stream model with a per-frame fetch, but
#                              it renders at top-level bottom-panel
#                              scope (host/chrome.go) — never inside a
#                              deferred-block capture — so the calls are
#                              the safe "works at top scope" case
#                              (ADR-0088 SD1/SD10). If ShowStatus is
#                              ever moved into a deferred body, migrate
#                              its fetches into StateManager.Sync and
#                              drop this entry.
#
# To add a new allowed site: add it here, and explain why in the PR.
# The default answer when the lint trips is "move the call into
# StateManager.Sync and read from the cache", not "extend the
# allowlist".
allowlist_pattern='egui2_statemanagement\.go|fetchers\.out\.go|egui2_globals\.go|definition/|_test\.go|EXPLANATION\.md|SKILLS\.md|videooutput\.go'

echo "fetcher-discipline: scanning imzero2 tree for inline Fetch* calls..."

# Match Fetcher.Fetch* method invocations specifically. Three patterns:
#
#   Fetcher()\.Fetch[A-Z]      — calling Fetch on a `.Fetcher()` accessor,
#                                e.g. StateManager.Fetcher().FetchR15…
#   \bfetcher\.Fetch[A-Z]      — variable named `fetcher` (conventional).
#   NewFetcher\(\)\.Fetch[A-Z] — fresh Fetcher constructed inline.
#
# The trailing `://` filter drops URLs in comments/docs that happen to
# contain a `Fetch`-shaped path segment.
findings=$(grep -rnE '(Fetcher\(\)|\bfetcher|NewFetcher\(\))\.Fetch[A-Z]' "$scope" 2>/dev/null \
    | grep -Ev "$allowlist_pattern" \
    | grep -v '^[^:]*:[^:]*://' \
    || true)

if [ -n "$findings" ]; then
    echo "fetcher-discipline: VIOLATIONS — inline fetcher calls outside the allowlist:" >&2
    echo "" >&2
    echo "$findings" >&2
    echo "" >&2
    echo "Each match calls a Fetcher.Fetch* method from a render body."
    echo "Migrate the call into StateManager.Sync and expose a Get* getter"
    echo "on the StateManager — see doc/skills/imzero2-fetchers/SKILLS.md."
    echo ""
    echo "If a new site genuinely needs to bypass the rule, add the file"
    echo "to the allowlist in this script and justify the exception in"
    echo "the PR description."
    exit 1
fi

echo "fetcher-discipline: ok (no inline fetcher calls outside the allowlist)"
