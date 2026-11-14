#!/bin/bash
# profile-cell.sh — one imagebench cell on a profiling build of a headless
# host, with perf attached to the host's client process (the Rust side) and to
# the Go process that feeds it, while the cell re-uploads.
#
# Usage:
#   doc/trials/image-upload-cost/profile-cell.sh <out-dir> <host> <arm> <side>
#     host: cpu | wgpu     arm, side: as measure.sh names them
#
# The client is the profiling build — line tables and frame pointers, in a
# target directory of its own so it never replaces the build measure.sh uses:
#   cd rust/imzero2 && source ../../scripts/dev/rust-repro-env.sh &&
#   CARGO_PROFILE_RELEASE_DEBUG=line-tables-only RUSTFLAGS="$RUSTFLAGS -C force-frame-pointers=yes" \
#     cargo build --release --locked --no-default-features --features headless_soft \
#     --target-dir target/headless-soft-prof        # cpu
#   … --features headless_wgpu --target-dir target/headless-prof                 # wgpu
#
# perf must be allowed to sample the caller's own processes
# (kernel.perf_event_paranoid <= 2). At 2 only user space is sampled, call
# chains included (cycles:u, --user-callchains), so time spent in the kernel —
# the pipe's copies, page faults — is absent from the reports; the script
# therefore also records each process's user and system CPU time over the
# window from /proc, which says how large the unsampled share is. The reports
# are text; the perf.data files are left in a temporary directory and removed.
set -euo pipefail
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../../.." && pwd)
out=${1:?usage: profile-cell.sh <out-dir> <host> <arm> <side>}
host=${2:?host}; arm=${3:?arm}; side=${4:?side}
SECONDS_PROFILED=${SECONDS_PROFILED:-20}
FPS=${FPS:-240}
mkdir -p "$out"
out=$(cd "$out" && pwd)
name="$host-$arm-$side"

case "$host" in
cpu) client="$root/rust/imzero2/target/headless-soft-prof/release/imzero2" ;;
wgpu) client="$root/rust/imzero2/target/headless-prof/release/imzero2" ;;
*) echo "unknown host $host" >&2; exit 2 ;;
esac
[ -x "$client" ] || { echo "no profiling build at $client" >&2; exit 2; }

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
# shellcheck source=/dev/null
source "$root/scripts/dev/go-build-env.sh"
runner="$work/imzero2-scene"
# shellcheck disable=SC2086 # deliberate word splitting of the flag list
(cd "$root" && CGO_ENABLED=0 go build $BOXER_GO_FLAGS -tags "${BOXER_GO_TAGS:+$BOXER_GO_TAGS,}binary_log" \
	-o "$runner" ./public/thestack/cmd/imzero2/)

# The cell keeps re-uploading for as long as the trace sleeps: a window that
# completes only stops recording, not uploading.
sleeps=""
for _ in $(seq 1 $(((SECONDS_PROFILED + 12) / 5))); do
	sleeps="$sleeps{\"do\":\"sleep\",\"settleMs\":5000}
"
done
cat >"$work/cell.scene.md" <<SCENE
---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1100x900
  fps: ${FPS}
  stepSettleMs: 350
---

> **Status: draft — pre-human-review.** Generated for a host profile.

# imagebench profile cell

$arm at $side.

\`\`\`jsonl trace
{"do":"wait","role":"text_input"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"image bench"}
{"do":"wait","contains":"image bench","settleMs":400}
{"do":"click","contains":"image bench"}
{"do":"click","name":"$arm","role":"button"}
{"do":"click","name":"$side","role":"button"}
${sleeps}{"do":"tree","text":"bench ","role":"label"}
\`\`\`
SCENE

"$runner" --logFormat=console --logLevel=warn imzero2 scene --repoRoot "$root" \
	--out "$work/scene" --timeout 10m --clientBinary "$client" "$work/cell.scene.md" >"$work/stdout" 2>&1 &
scene_pid=$!

client_pid=""
for _ in $(seq 1 600); do
	for p in $(pgrep -x imzero2 || true); do
		[ "$(readlink "/proc/$p/exe" 2>/dev/null)" = "$client" ] && client_pid=$p
	done
	[ -n "$client_pid" ] && break
	sleep 0.2
done
[ -n "$client_pid" ] || { echo "no client process" >&2; cat "$work/stdout" >&2; exit 1; }
go_pid=$(ps -o ppid= -p "$client_pid" | tr -d ' ')

# -D: start after the gallery is up and the cell is selected.
perf record -e cycles:u -D 8000 -F 997 --call-graph fp --user-callchains -p "$client_pid" -o "$work/client.data" 2>"$work/perf-client.err" -- sleep "$((SECONDS_PROFILED + 8))" &
perf record -e cycles:u -D 8000 -F 997 --call-graph fp --user-callchains -m 8 -p "$go_pid" -o "$work/go.data" 2>"$work/perf-go.err" -- sleep "$((SECONDS_PROFILED + 8))" &
# User and system CPU time of both processes over the same window, in clock
# ticks (fields 14 and 15 of /proc/<pid>/stat).
ticks() { awk '{print $14, $15}' "/proc/$1/stat" 2>/dev/null || echo "0 0"; }
sleep 8
read -r cu0 cs0 <<<"$(ticks "$client_pid")"
read -r gu0 gs0 <<<"$(ticks "$go_pid")"
sleep "$SECONDS_PROFILED"
read -r cu1 cs1 <<<"$(ticks "$client_pid")"
read -r gu1 gs1 <<<"$(ticks "$go_pid")"
wait "$scene_pid" || true
wait || true
[ -s "$work/client.data" ] || { echo "perf recorded nothing for the client:" >&2; cat "$work/perf-client.err" >&2; exit 1; }
# The Go process has many threads and an unprivileged perf a small locked-
# memory budget shared by both recorders; its profile is the one to lose.
[ -s "$work/go.data" ] || echo "perf recorded nothing for the Go side: $(grep -m1 -i -E 'error|permission' "$work/perf-go.err")" >&2

{
	echo "# imagebench $name — what the demo reported"
	sed 's/\x1b\[[0-9;]*m//g' "$work/stdout" | grep -a 'label ="bench ' | sed 's/ #[0-9]* @.*//' || true
	hz=$(getconf CLK_TCK)
	echo "# CPU time over the ${SECONDS_PROFILED} s window, seconds (user, system) — perf below samples user time only"
	echo "client user=$(echo "scale=2; ($cu1-$cu0)/$hz" | bc) system=$(echo "scale=2; ($cs1-$cs0)/$hz" | bc)"
	echo "go     user=$(echo "scale=2; ($gu1-$gu0)/$hz" | bc) system=$(echo "scale=2; ($gs1-$gs0)/$hz" | bc)"
} >"$out/$name.bench.txt"
report() { # data title file children?
	{
		echo "# $2"
		echo "# perf report --stdio $4 --percent-limit 0.5, $(perf --version)"
		perf report -i "$1" --stdio "$4" --percent-limit 0.5 -g none --sort symbol 2>/dev/null |
			{ grep -v -e '^#' -e '^$' || true; } | sed 's/ *- *- *$//; s/ *$//' | cut -c1-200
	} >"$3"
}
report "$work/client.data" "host client ($host), $arm $side: inclusive" "$out/$name.client.inclusive.perf.txt" --children
report "$work/client.data" "host client ($host), $arm $side: self" "$out/$name.client.self.perf.txt" --no-children
[ -s "$work/go.data" ] && report "$work/go.data" "Go side, $arm $side: self" "$out/$name.go.self.perf.txt" --no-children
ls -la "$out" | sed -n '2,$p' | awk '{print $5, $9}'
