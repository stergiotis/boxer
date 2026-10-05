#!/bin/bash
# One landbench cell on the profiling build of the cpu host, perf attached to
# the client and to the Go host during the measured window.
#
# Usage: OUT=<dir> BOXER_CHECKOUT=<boxer tree under test> \
#        CLIENT_CHECKOUT=<checkout holding target/headless-soft-prof> profile-cell.sh <view> <arm>
# The client is built with line tables and frame pointers:
#   CARGO_PROFILE_RELEASE_DEBUG=line-tables-only RUSTFLAGS="-C force-frame-pointers=yes" \
#   cargo build --release --locked --no-default-features --features headless_soft \
#     --target-dir target/headless-soft-prof
set -euo pipefail
view=${1:-world-z0}; arm=${2:-fill}
S=${OUT:?}
out=$S/$view-$arm; rm -rf "$out"; mkdir -p "$out"
root=${BOXER_CHECKOUT:?}
cat >"$out/cell.scene.md" <<SCENE
---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x900
  stepSettleMs: 350
---

> **Status: draft — pre-human-review.** Generated for a host profile.

# landbench profile cell

\`\`\`jsonl trace
{"do":"wait","role":"text_input"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"land bench"}
{"do":"wait","contains":"land bench","settleMs":400}
{"do":"click","contains":"land bench"}
{"do":"click","name":"$view","role":"button"}
{"do":"click","name":"$arm","role":"button"}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"sleep","settleMs":5000}
{"do":"tree","text":"bench ","role":"label"}
\`\`\`
SCENE
cd "$root"
scripts/dev/scene.sh --out "$out/scene" --timeout 10m --clientBinary ${CLIENT_CHECKOUT:?}/rust/imzero2/target/headless-soft-prof/release/imzero2 "$out/cell.scene.md" >"$out/stdout" 2>&1 &
runner=$!
want=${CLIENT_CHECKOUT:?}/rust/imzero2/target/headless-soft-prof/release/imzero2
client=""
for _ in $(seq 1 600); do
	for p in $(pgrep -x imzero2 || true); do
		[ "$(readlink /proc/$p/exe 2>/dev/null)" = "$want" ] && client=$p
	done
	[ -n "$client" ] && break; sleep 0.2
done
[ -n "$client" ] || { echo "no client"; exit 1; }
host=$(ps -o ppid= -p "$client" | tr -d ' ')
echo "client=$client host=$host $(ps -o args= -p "$host" | cut -c1-100)" >"$out/pids.txt"
perf record -D 6000 -F 997 --call-graph fp -p "$client" -o "$out/client.data" 2>"$out/perf-client.err" &
perf record -D 6000 -F 997 -g -p "$host" -o "$out/host.data" 2>"$out/perf-host.err" &
wait "$runner" || true
sleep 2
grep -a 'label ="bench' "$out/stdout" | sed 's/ #[0-9]* @.*//' || true
