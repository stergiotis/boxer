#!/usr/bin/env bash
# Opt-in browser regression runner for the imzero2 remote viewer page
# (ADR-0242 "Verification plan"). Not part of `cargo test` or `go test`: it
# needs an installed Chromium and is run by hand or from an integration lane.
#
# What it does
#   1. composes a temporary page from the REAL viewer index.html by inserting
#      one harness script before the page's own inline script (transport and
#      codec stubs) and the test scripts after it — the page's CSS, layout,
#      listeners, protobuf codec and GL code are untouched;
#   2. serves the temporary directory over http on 127.0.0.1 (a secure context,
#      and the only way a confined snap Chromium can read files this harness
#      generated outside its own home);
#   3. runs Chromium headless with --dump-dom under --virtual-time-budget, so
#      the page's timers run to completion before the DOM is serialised;
#   4. reads the TAP report the suites wrote into the dumped DOM.
#
# Chromium is required: the report transport is --dump-dom, which Firefox has
# no equivalent for (headless Firefox can only screenshot), so a
# Firefox-only machine is reported as a skip rather than silently passing.
#
# Usage
#   ./run.sh [options] [-- extra chromium args]
#     --page PATH      viewer page to test (default: ../index.html beside this
#                      directory, i.e. the checkout the harness lives in)
#     --suite NAME     suite to run, repeatable; NAME matches suite-NAME*.js
#                      (default: phase1)
#     --all            run every suite-*.js
#     --list           list available suites and exit
#     --budget MS      Chromium virtual-time budget (default 15000)
#     --watchdog MS    in-page report deadline (default: 60% of --budget)
#     --port N         http port (default 0 = pick a free one)
#     --chromium PATH  browser binary (default: $CHROMIUM, else discovery)
#     --swiftshader    add --enable-unsafe-swiftshader. Only for the mesh/GL
#                      suites on a machine where headless WebGL2 is otherwise
#                      unavailable: it opts into the software rasteriser with
#                      lower security guarantees, so it is off by default and
#                      named in the report when used. It is NOT a sandbox
#                      switch — the browser sandbox stays on either way.
#     --keep           keep the composed page / dumped DOM and print the path
#     --verbose        print the full TAP report and browser stderr
#
# Exit codes: 0 all cases passed · 1 case failures or page errors ·
#             2 harness failure (no report, or a suite that never finished) ·
#             77 skipped (no usable browser or no python3)
set -uo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
page="$here/../index.html"
budget=15000
watchdog=""
port=0
keep=0
verbose=0
list=0
browser="${CHROMIUM:-}"
suites=()
extra=()

die() { printf 'run.sh: %s\n' "$*" >&2; exit 2; }

while [ $# -gt 0 ]; do
  case "$1" in
    --page) page="${2:-}"; shift 2 ;;
    --suite) suites+=("${2:-}"); shift 2 ;;
    --all) suites+=("*"); shift ;;
    --list) list=1; shift ;;
    --budget) budget="${2:-}"; shift 2 ;;
    --watchdog) watchdog="${2:-}"; shift 2 ;;
    --port) port="${2:-}"; shift 2 ;;
    --chromium) browser="${2:-}"; shift 2 ;;
    --swiftshader) extra+=(--enable-unsafe-swiftshader); shift ;;
    --keep) keep=1; shift ;;
    --verbose|-v) verbose=1; shift ;;
    -h|--help) sed -n '2,/^set -/p' "${BASH_SOURCE[0]}" | sed -e '$d' -e 's/^# \{0,1\}//'; exit 0 ;;
    --) shift; extra+=("$@"); break ;;
    *) die "unknown argument: $1" ;;
  esac
done

available=()
for f in "$here"/suite-*.js; do
  [ -e "$f" ] || continue
  b="${f##*/}"; b="${b#suite-}"; b="${b%.js}"
  available+=("$b")
done

if [ "$list" = 1 ]; then
  printf 'suites in %s:\n' "$here"
  for s in "${available[@]}"; do printf '  %s\n' "$s"; done
  exit 0
fi

[ "${#suites[@]}" -gt 0 ] || suites=(phase1)

# Resolve each requested name to a suite file (exact, then prefix match).
resolved=()
for want in "${suites[@]}"; do
  if [ "$want" = "*" ]; then
    for s in "${available[@]}"; do resolved+=("$s"); done
    continue
  fi
  hit=""
  for s in "${available[@]}"; do [ "$s" = "$want" ] && hit="$s"; done
  if [ -z "$hit" ]; then
    for s in "${available[@]}"; do case "$s" in "$want"*) [ -z "$hit" ] && hit="$s" ;; esac; done
  fi
  [ -n "$hit" ] || die "no suite matches '$want' (available: ${available[*]})"
  resolved+=("$hit")
done

[ -f "$page" ] || die "viewer page not found: $page"
page="$(cd -- "$(dirname -- "$page")" && pwd -P)/$(basename -- "$page")"

# ---- environment report -------------------------------------------------
printf '# harness: %s\n' "$here"
printf '# page: %s\n' "$page"
printf '# page-bytes: %s\n' "$(wc -c <"$page" | tr -d ' ')"
printf '# suites: %s\n' "${resolved[*]}"

if ! command -v python3 >/dev/null 2>&1; then
  printf '# SKIP: python3 is required to serve the composed page (no other dependency is)\n'
  exit 77
fi
printf '# python: %s\n' "$(python3 -V 2>&1)"

find_browser() {
  if [ -n "$browser" ]; then
    if command -v "$browser" >/dev/null 2>&1; then command -v "$browser"; return 0; fi
    printf 'run.sh: --chromium/$CHROMIUM not executable: %s\n' "$browser" >&2
    return 1
  fi
  for c in chromium chromium-browser google-chrome-stable google-chrome chrome; do
    if command -v "$c" >/dev/null 2>&1; then command -v "$c"; return 0; fi
  done
  return 1
}

if ! bin="$(find_browser)"; then
  printf '# SKIP: no Chromium/Chrome found (tried $CHROMIUM, chromium, chromium-browser, google-chrome-stable, google-chrome, chrome).\n'
  if command -v firefox >/dev/null 2>&1; then
    printf '# note: firefox is installed but cannot run this harness: the report is read back\n'
    printf '#       through Chromium --dump-dom, which has no headless-Firefox equivalent.\n'
  fi
  exit 77
fi
printf '# browser: %s\n' "$bin"
bver="$("$bin" --version 2>/dev/null | grep -v '^update\.go:' | tail -1)"
printf '# browser-version: %s\n' "${bver:-unknown}"
case "$bin" in /snap/*) printf '# browser-packaging: snap (confined; the page is served over http, not file://)\n' ;; esac
[ "${#extra[@]}" -gt 0 ] && printf '# extra-browser-flags: %s\n' "${extra[*]}"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/imzero2-viewer-harness.XXXXXX")" || die "mktemp failed"
profile="${TMPDIR:-/tmp}/imzero2-viewer-harness-profile.$$"
srvpid=""
cleanup() {
  [ -n "$srvpid" ] && kill "$srvpid" 2>/dev/null
  if [ "$keep" = 1 ]; then
    printf '# kept: %s\n' "$tmp"
  else
    rm -rf "$tmp" "$profile" 2>/dev/null
  fi
}
trap cleanup EXIT

cp "$here"/harness-stub.js "$here"/harness.js "$tmp"/ || die "cannot stage harness scripts"
for s in "${resolved[@]}"; do cp "$here/suite-$s.js" "$tmp"/ || die "cannot stage suite $s"; done

# Compose one page per suite: stub before the viewer's inline <script>, the
# test runtime and the suite after it. Refuse (rather than silently produce a
# page that tests nothing) if either anchor is missing.
compose() {
  awk -v stub='<script src="harness-stub.js"></script>' -v tail="$2" '
    !ins1 && /^[[:space:]]*<script>[[:space:]]*$/ { print stub; ins1 = 1 }
    !ins2 && /^[[:space:]]*<\/body>[[:space:]]*$/ { print tail; ins2 = 1 }
    { print }
    END { if (!ins1) exit 3; if (!ins2) exit 4 }
  ' "$page" >"$1"
}

[ -n "$watchdog" ] || watchdog=$(( budget * 6 / 10 ))

worst=0
total_pass=0
total_fail=0

for s in "${resolved[@]}"; do
  tail_tags="<script src=\"harness.js\"></script><script src=\"suite-$s.js\"></script>"
  compose "$tmp/page-$s.html" "$tail_tags"
  rc=$?
  if [ "$rc" != 0 ]; then
    case "$rc" in
      3) die "no '<script>' line found in $page — cannot place the transport stub before the page script" ;;
      4) die "no '</body>' line found in $page — cannot place the test scripts after the page script" ;;
      *) die "composing the harness page failed (awk exit $rc)" ;;
    esac
  fi
done

# -u: the "Serving HTTP on ... port N" line is how the runner learns the port
# it was given when --port 0 asked for a free one, and a buffered stdout never
# reaches the log file.
python3 -u -m http.server "$port" --bind 127.0.0.1 --directory "$tmp" >"$tmp/http.log" 2>&1 &
srvpid=$!
realport=""
for _ in $(seq 1 100); do
  realport="$(sed -n 's/.*port \([0-9]\{1,\}\).*/\1/p' "$tmp/http.log" | head -1)"
  [ -n "$realport" ] && break
  kill -0 "$srvpid" 2>/dev/null || break
  sleep 0.1
done
[ -n "$realport" ] || { cat "$tmp/http.log" >&2; die "local http server did not start"; }
printf '# server: http://127.0.0.1:%s (serving the composed pages only)\n' "$realport"

extract_report() {
  awk '
    /<pre id="harness-results">/ { on = 1; sub(/^.*<pre id="harness-results">/, "") }
    on {
      if (match($0, /<\/pre>/)) { sub(/<\/pre>.*$/, ""); print; exit }
      print
    }
  ' "$1" | sed -e 's/&lt;/</g' -e 's/&gt;/>/g' -e 's/&quot;/"/g' -e "s/&#39;/'/g" -e 's/&amp;/\&/g'
}

for s in "${resolved[@]}"; do
  url="http://127.0.0.1:$realport/page-$s.html?harness_suite=$s&harness_watchdog=$watchdog"
  dom="$tmp/dom-$s.html"
  err="$tmp/stderr-$s.log"
  printf '\n### suite %s\n' "$s"
  "$bin" --headless --dump-dom "--virtual-time-budget=$budget" \
    "--user-data-dir=$profile" --no-first-run --no-default-browser-check \
    --disable-extensions --disable-component-update --disable-background-networking \
    --disable-search-engine-choice-screen --mute-audio --window-size=1024,768 \
    ${extra[@]+"${extra[@]}"} "$url" >"$dom" 2>"$err"
  brc=$?
  if [ "$brc" != 0 ]; then
    printf 'BROWSER-EXIT %s (see stderr below)\n' "$brc"
    grep -v '^update\.go:' "$err" | tail -20
    worst=2
    continue
  fi

  report="$(extract_report "$dom")"
  if [ -z "$report" ]; then
    printf 'HARNESS-FAILURE: no report in the dumped DOM (the page never reached the sink)\n'
    grep -v '^update\.go:' "$err" | tail -20
    [ "$verbose" = 1 ] && { printf -- '--- dumped DOM (head) ---\n'; head -40 "$dom"; }
    worst=2
    continue
  fi

  if [ "$verbose" = 1 ]; then
    printf '%s\n' "$report"
  else
    printf '%s\n' "$report" | grep -E '^# (suite|browser|webgl2|gl-counters|user-agent|page-error|status|cases)' || true
    printf '%s\n' "$report" | grep -E '^(not ok|  #)' || true
  fi

  pass=$(printf '%s\n' "$report" | grep -c '^ok ' || true)
  fail=$(printf '%s\n' "$report" | grep -c '^not ok ' || true)
  perr=$(printf '%s\n' "$report" | grep -c '^# page-error:' || true)
  status=$(printf '%s\n' "$report" | sed -n 's/^# status: //p' | tail -1)
  total_pass=$(( total_pass + pass ))
  total_fail=$(( total_fail + fail ))
  printf 'suite %s: %s passed, %s failed, page-errors %s, status %s\n' \
    "$s" "$pass" "$fail" "$perr" "${status:-none}"
  if [ "${status:-}" != "completed" ]; then worst=2
  elif [ "$fail" != 0 ] || [ "$perr" != 0 ]; then [ "$worst" -lt 1 ] && worst=1
  fi
  if [ "$verbose" = 1 ]; then
    printf -- '--- browser stderr (filtered) ---\n'
    grep -v '^update\.go:' "$err" | tail -20
  fi
done

printf '\n# total: %s passed, %s failed across %s suite(s)\n' "$total_pass" "$total_fail" "${#resolved[@]}"
exit "$worst"
