#!/usr/bin/env bash
# The audio-lane-playout trial's runner (protocol: README.md beside this file).
#
#   measure.sh <run-dir> [arm ...]
#
# An arm is <carrier>-<target ms>: carrier `tab` (the lane inside a browser
# tab) or `headless` (the lane through the headless host's carrier); the
# target is BOXER_AUDIO_LANE_TARGET. With no arms named it runs the
# protocol's set.
#
# Environment:
#   SCRATCH     where the bundle and the binaries are built (default: a new
#               temporary directory, removed at the end)
#   PROBE_MS    how long each arm is observed (default 20000)
#   PORT        base port; an arm uses PORT..PORT+30 (default 47600)
#   CHROMIUM    the browser binary (default: chromium on the PATH)
#   SKIP_BUILD  1 reuses what SCRATCH already holds
#   KEEP_TRACE  1 keeps each arm's trace.jsonl, one line per played report
#
# Each arm leaves <run-dir>/<arm>/{report.json,row.json}; the rows are
# collected in <run-dir>/results.jsonl, and the machine and what was run on
# it in <run-dir>/env.txt.
set -uo pipefail

run=${1:?usage: measure.sh <run-dir> [arm ...]}
shift
arms=("$@")
[ "${#arms[@]}" -gt 0 ] || arms=(tab-40 tab-80 tab-150 tab-300 headless-40 headless-80 headless-150 headless-300)

here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../../.." && pwd)
mkdir -p "$run"
run=$(cd "$run" && pwd)
own_scratch=0
if [ -z "${SCRATCH:-}" ]; then SCRATCH=$(mktemp -d); own_scratch=1; fi
mkdir -p "$SCRATCH"
PROBE_MS=${PROBE_MS:-20000}
PORT=${PORT:-47600}
CHROMIUM=${CHROMIUM:-chromium}
app='github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets'
quote() { python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=""))' "$1"; }
pids=()
cleanup() {
  for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  [ "$own_scratch" = 1 ] && rm -rf "$SCRATCH"
}
trap cleanup EXIT

cd "$root"
tags=$(tr -d '\n' < ./tags)

# ---- the machine, without its name ----------------------------------------
{
  echo "date: $(date -u +%Y-%m-%dT%H:%MZ)"
  echo "commit: $(git rev-parse HEAD)$(git diff --quiet || echo ' (working tree modified)')"
  echo "cpu: $(lscpu 2>/dev/null | sed -n 's/^Model name: *//p' | head -1) x$(nproc)"
  echo "memory: $(awk '/MemTotal/ {printf "%.0f GiB", $2/1048576}' /proc/meminfo)"
  echo "kernel: $(uname -sr)"
  echo "go: $(go version | cut -d' ' -f3-)"
  echo "rustc: $(cd rust/imzero2 && rustc --version)"
  echo "browser: $("$CHROMIUM" --version 2>/dev/null | grep -v '^update\.go' | tail -1)"
  echo "browser flags: --headless=new --mute-audio --autoplay-policy=no-user-gesture-required --enable-unsafe-swiftshader"
  echo "probe_ms: $PROBE_MS"
} > "$run/env.txt"

# ---- builds ----------------------------------------------------------------
if [ "${SKIP_BUILD:-0}" != 1 ]; then
  echo "measure: building the tab bundle, the tab server, the host and the headless client" >&2
  go run -tags="$tags" ./public/thestack/cmd/imzero2tabdemo bundle --out "$SCRATCH/tab" > "$SCRATCH/bundle.log" 2>&1 || { tail -5 "$SCRATCH/bundle.log"; exit 2; }
  go build -tags="$tags" -o "$SCRATCH/tabserve" ./public/thestack/cmd/imzero2tabdemo || exit 2
  CGO_ENABLED=0 go build -tags "$tags,binary_log" -o "$SCRATCH/host" ./public/thestack/cmd/imzero2/ || exit 2
  ( cd rust/imzero2 && source "$root/scripts/dev/rust-repro-env.sh" &&
    cargo build --release --locked --no-default-features --features headless --target-dir target/headless_mesh > "$SCRATCH/cargo.log" 2>&1 ) || { tail -5 "$SCRATCH/cargo.log"; exit 2; }
fi
client="$root/rust/imzero2/target/headless_mesh/release/imzero2"
# What was run, so a row can be tied to a build: with SKIP_BUILD the commit
# above is the tree's at the run, not necessarily the binaries'.
{
  echo "skip_build: ${SKIP_BUILD:-0}"
  for f in "$SCRATCH/tab/imzero2tab.wasm" "$SCRATCH/tab/imzero2_browser.wasm" "$SCRATCH/tabserve" "$SCRATCH/host" "$client"; do
    echo "sha256 $(basename "$f"): $(sha256sum "$f" 2>/dev/null | cut -d' ' -f1) built $(date -u -r "$f" +%Y-%m-%dT%H:%MZ 2>/dev/null)"
  done
} >> "$run/env.txt"

# The viewer page with the probe placed before its own script.
compose() {
  awk -v stub='<script src="audio-probe.js"></script>' '!ins && /^[[:space:]]*<script>[[:space:]]*$/ { print stub; ins = 1 } { print }' \
    "$root/rust/imzero2/src/imzero2/viewer/index.html" > "$1/index.html"
  cp "$here/harness/audio-probe.js" "$1/"
}
browse() { # url logfile
  local profile; profile=$(mktemp -d /tmp/audio-lane-profile.XXXXXX)
  "$CHROMIUM" --headless=new --user-data-dir="$profile" --no-first-run --no-default-browser-check --disable-extensions \
    --mute-audio --autoplay-policy=no-user-gesture-required --enable-unsafe-swiftshader --window-size=1200,800 "$1" > "$2" 2>&1 &
  br=$!; pids+=("$br"); brprofile=$profile
}
# Reduces one arm's evidence to a row. It reads report.json (the page probe's
# summary) and trace.jsonl (the Go side's "audio lane report" lines, one per
# played report of a playing stream): how many there were, in how many the
# device had played everything it was sent, and the target in force.
reduce() { # arm-dir
  python3 - "$1" <<'PY'
import json
import sys


def quantile(sorted_values, p):
    if not sorted_values:
        return None
    return sorted_values[min(len(sorted_values) - 1, int(p * len(sorted_values)))]


arm = sys.argv[1]
with open(f"{arm}/report.json") as f:
    report = json.loads(f.read().strip().splitlines()[-1])

dry, reports, targets = 0, 0, []
with open(f"{arm}/trace.jsonl") as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        r = json.loads(line)
        if not r.get("playing") or r.get("sent", 0) == 0:
            continue
        reports += 1
        targets.append(r["target"] * 1000.0 / r["deviceRate"])
        if r["reported"] >= r["sent"]:
            dry += 1
targets.sort()
seconds = report["contextSeconds"] or 1
row = {
    "arm": arm.rstrip("/").split("/")[-1],
    "device_rate": report["rate"],
    "context_s": round(report["contextSeconds"], 2),
    "scheduled_s": round(report["scheduledSeconds"], 2),
    "first_audio_ms": report["firstAudioMs"],
    "buffers": report["buffers"],
    "underruns": report["underruns"],
    "underruns_per_min": round(report["underruns"] * 60.0 / seconds, 2),
    "overlaps": report["overlaps"],
    "late_starts": report["lateStarts"],
    "lead_min_ms": report["leadMinMs"],
    "lead_p05_ms": report["leadP05Ms"],
    "lead_median_ms": report["leadMedianMs"],
    "go_reports": reports,
    "go_reports_dry": dry,
    "target_min_ms": round(targets[0], 1) if targets else None,
    "target_median_ms": round(quantile(targets, 0.5), 1) if targets else None,
    "target_max_ms": round(targets[-1], 1) if targets else None,
}
print(json.dumps(row))
PY
}
await() { # pid: wait for it to exit, at most the probe time plus a margin
  for _ in $(seq 1 $(( PROBE_MS / 100 + 400 ))); do kill -0 "$1" 2>/dev/null || return 0; sleep 0.1; done
  return 1
}

for arm in "${arms[@]}"; do
  carrier=${arm%%-*}; target=${arm#*-}
  out="$run/$arm"; mkdir -p "$out"
  echo "measure: arm $arm" >&2
  case "$carrier" in
    tab)
      compose "$SCRATCH/tab"
      "$SCRATCH/tabserve" serve --dir "$SCRATCH/tab" --listen 127.0.0.1:0 --exitOnReport > "$out/serve.out" 2> "$out/serve.err" &
      srv=$!; pids+=("$srv")
      for _ in $(seq 1 300); do grep -q '^PORT ' "$out/serve.out" 2>/dev/null && break; sleep 0.1; done
      port=$(sed -n 's/^PORT //p' "$out/serve.out" | head -1)
      [ -n "$port" ] || { echo "measure: the tab server did not start" >&2; exit 2; }
      inner="app=$(quote "$app")&log=1&env=BOXER_WAVEFORM_DEMO_AUTOPLAY%3D1&env=BOXER_AUDIO_LANE_TRACE%3D1&env=BOXER_AUDIO_LANE_TARGET%3D${target}ms"
      browse "http://127.0.0.1:$port/index.html?probe_ms=$PROBE_MS&worker=$(quote "worker.mjs?$inner")" "$out/browser.log"
      await "$srv" || echo "measure: arm $arm produced no report" >&2
      kill "$br" "$srv" 2>/dev/null; wait "$br" "$srv" 2>/dev/null; rm -rf "$brprofile"
      grep -v '^PORT ' "$out/serve.out" > "$out/report.json"
      # The module's log lines arrive wrapped in the server's own.
      python3 - "$out/serve.err" > "$out/trace.jsonl" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    try:
        inner = json.loads(line).get("module", "")
        j = json.loads(inner) if inner.startswith("{") else None
    except ValueError:
        continue
    if j and j.get("message") == "audio lane report":
        print(json.dumps(j))
PY
      rm -f "$out/serve.out" "$out/serve.err" "$out/browser.log"
      ;;
    headless)
      page="$SCRATCH/page"; mkdir -p "$page" "$SCRATCH/dump"; compose "$page"
      main_font=$(fc-match -f '%{file}' 'Noto Sans'); mono_font=$(fc-match -f '%{file}' 'DejaVu Sans Mono')
      env -u DISPLAY -u WAYLAND_DISPLAY \
        IMZERO2_HEADLESS_LISTEN="127.0.0.1:$PORT" IMZERO2_HEADLESS_CODEC=mesh \
        IMZERO2_HEADLESS_DUMP_DIR="$SCRATCH/dump" IMZERO2_HEADLESS_DUMP_EVERY=1000000 \
        IMZERO2_HEADLESS_FPS=20 IMZERO2_SCREENSHOT_SIZE=1200x800 \
        KEELSON_INTROSPECT_HTTP_LISTEN="127.0.0.1:$((PORT + 9))" \
        BOXER_COMPONENT=audio-lane-trial BOXER_WAVEFORM_DEMO_AUTOPLAY=1 \
        BOXER_AUDIO_LANE_TRACE=1 BOXER_AUDIO_LANE_TARGET="${target}ms" \
        "$SCRATCH/host" --logFormat=json --logLevel=info imzero2 demo \
          --clientBinary "$client" \
          --clientInitialMainWindowWidth 1200 --clientInitialMainWindowHeight 800 \
          --mainFontTTF "$main_font" --monoFontTTF "$mono_font" \
          --phosphorFontTTF rust/imzero2/assets/fonts/phosphor/Phosphor.ttf \
          --launch widgets > "$out/host.log" 2>&1 &
      host=$!; pids+=("$host")
      for _ in $(seq 1 120); do
        (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && { exec 3<&-; break; }
        kill -0 "$host" 2>/dev/null || { echo "measure: the host died" >&2; tail -5 "$out/host.log" >&2; exit 2; }
        sleep 0.5
      done
      # The carrier serves its page from the binary, so the probed copy comes
      # from the tab binary's dev server, which also has the report sink.
      "$SCRATCH/tabserve" serve --dir "$page" --listen "127.0.0.1:$((PORT + 20))" --exitOnReport > "$out/serve.out" 2> /dev/null &
      srv=$!; pids+=("$srv")
      for _ in $(seq 1 300); do grep -q '^PORT ' "$out/serve.out" 2>/dev/null && break; sleep 0.1; done
      browse "http://127.0.0.1:$((PORT + 20))/index.html?probe_ms=$PROBE_MS&ws=$(quote "ws://127.0.0.1:$PORT/ws")" "$out/browser.log"
      await "$srv" || echo "measure: arm $arm produced no report" >&2
      kill "$br" "$srv" "$host" 2>/dev/null; sleep 0.5; kill -9 "$host" 2>/dev/null; wait "$br" "$srv" "$host" 2>/dev/null; rm -rf "$brprofile"
      grep -v '^PORT ' "$out/serve.out" > "$out/report.json"
      grep -a '"audio lane report"' "$out/host.log" > "$out/trace.jsonl"
      rm -f "$out/host.log" "$out/browser.log" "$out/serve.out"
      ;;
    *) echo "measure: unknown carrier in arm '$arm'" >&2; exit 2 ;;
  esac
  reduce "$out" > "$out/row.json" || echo "measure: arm $arm did not reduce" >&2
  # The per-report trace is bulk; the row holds what the protocol reads off it.
  [ "${KEEP_TRACE:-0}" = 1 ] || rm -f "$out/trace.jsonl"
  cat "$out/row.json" >&2
done
cat "$run"/*/row.json > "$run/results.jsonl" 2>/dev/null
echo "measure: done — $run/results.jsonl" >&2
