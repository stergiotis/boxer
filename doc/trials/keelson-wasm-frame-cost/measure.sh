#!/bin/bash
# measure.sh — one run of the keelson-wasm-frame-cost trial (ADR-0077 SD2).
#
# Builds the stub peer (rust/fffi2stub) natively and for wasm32, the Go frame
# producer (public/thestack/imzero2/egui2/demo/wasmspike) for native, js and
# wasip1, then runs every arm and writes results.tsv + environment.md into the
# run directory. Binaries stay in a temporary directory; only text lands in the
# run.
#
# Usage:
#   doc/trials/keelson-wasm-frame-cost/measure.sh <run-dir>
#   doc/trials/keelson-wasm-frame-cost/measure.sh --pack <dir>
#   PREBUILT=<dir> doc/trials/keelson-wasm-frame-cost/measure.sh <run-dir>
#
# `--pack` only builds, into <dir>, everything a run needs (both stubs, the
# three Go binaries, wasm_exec.js, the fetch table, the harness) plus a
# build-info.txt naming the commit. A copy of this trial directory and that
# <dir> on another host, with Node and a browser but no toolchain and no
# repository, then runs the same arms with PREBUILT=<dir>. Native arms need
# the same OS and architecture as the packing host; they are skipped
# otherwise.
#
# Environment:
#   SCENES    "gallery labels"        scenes to run
#   ROWS      1700                    rows of the labels scene (~300 KB/frame)
#   FRAMES    300                     measured frames per arm
#   WARMUP    30                      frames discarded before measuring
#   TARGETS   "wasip1 js"             wasm targets
#   BROWSERS  "chromium firefox"      browsers to run the bridge in (headless)
#   CHROMIUM / FIREFOX                browser launch commands (default: the
#                                     flatpak ids, else the PATH binaries)
#   PROFILE_DIR  ~/kwfc-browser-profiles  where the browsers' throwaway
#                                     profiles go (removed again when empty).
#                                     A sandboxed browser must be able to see
#                                     it: a snap sees the home directory but
#                                     not its hidden entries, the Firefox
#                                     flatpak only ~/Downloads, so set
#                                     PROFILE_DIR=~/Downloads/kwfc there. A
#                                     profile the browser cannot open shows up
#                                     as "never loaded the page".
#
# Prerequisites: Go (from go.mod), the Rust toolchain rust/fffi2stub pins with
# its wasm32 target, Node 20+, and an otherwise idle machine. The protocol is
# in README.md.
set -euo pipefail
here=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")
root=$(cd "$here/../../.." 2>/dev/null && pwd || true)
pack=""
if [[ "${1:-}" == "--pack" ]]; then
	pack=${2:?usage: measure.sh --pack <dir>}
	mkdir -p "$pack"; pack=$(cd "$pack" && pwd)
	run=""
else
	run=${1:?usage: measure.sh <run-dir>}
	mkdir -p "$run/raw"
	run=$(cd "$run" && pwd)
fi

SCENES=${SCENES-"gallery labels"}
ROWS=${ROWS:-1700}
FRAMES=${FRAMES:-300}
WARMUP=${WARMUP:-30}
TARGETS=${TARGETS-"wasip1 js"}
BROWSERS=${BROWSERS-"chromium firefox"}
STAGE=1024x600
PROFILE_DIR=${PROFILE_DIR:-$HOME/kwfc-browser-profiles}

# ---- build (or take a pack) -------------------------------------------------
if [[ -n "${PREBUILT:-}" ]]; then
	work=$(mktemp -d "${TMPDIR:-/tmp}/kwfc.XXXXXX")
	trap 'rm -rf "$work"' EXIT
	cp "$PREBUILT"/* "$work/"
	build_info=$(cat "$work/build-info.txt")
else
	if [[ -n "$pack" ]]; then work=$pack; else
		work=$(mktemp -d "${TMPDIR:-/tmp}/kwfc.XXXXXX")
		trap 'rm -rf "$work"' EXIT
	fi
	tags=$(cat "$root/tags")
	spike=./public/thestack/imzero2/egui2/demo/wasmspike
	(cd "$root/rust/fffi2stub" && cargo build --release --locked -q && cargo build --release --locked -q --lib --target wasm32-unknown-unknown)
	(cd "$root" && go build -tags="$tags" -o "$work/imzero2tab" ./public/thestack/cmd/imzero2tab \
		&& go build -tags="$tags" -o "$work/wasmspike_native" $spike \
		&& GOOS=js GOARCH=wasm go build -tags="$tags" -o "$work/wasmspike_js.wasm" $spike \
		&& GOOS=wasip1 GOARCH=wasm go build -tags="$tags" -o "$work/wasmspike_wasip1.wasm" $spike \
		&& GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -tags="$tags" -o "$work/wasmspike_wasip1_reactor.wasm" $spike)
	cp "$root/rust/fffi2stub/target/release/fffi2stub" "$work/fffi2stub"
	cp "$root/rust/fffi2stub/target/wasm32-unknown-unknown/release/fffi2stub.wasm" "$work/fffi2stub.wasm"
	# the real Rust host (M2), if its toolchain target is here
	if (cd "$root/rust/imzero2" && ./build_rust_browser.sh >/dev/null 2>&1); then
		cp "$root/rust/imzero2/target/browser/wasm32-unknown-unknown/release/imzero2_browser.wasm" "$work/imzero2.wasm"
	else
		echo "measure.sh: the browser host did not build; host arms skipped" >&2
	fi
	cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$work/wasm_exec.js"
	cp "$here/harness/"{index.html,worker.mjs} "$work/"
	cp "$root/public/thestack/imzero2/browserhost/web/"{bridge.js,package.json} "$work/"
	# the page's server for the browser arms (imzero2tab serve)
	"$work/wasmspike_native" -dumpFetchTable "$root/public/thestack/imzero2/egui2/bindings" > "$work/fetchtable.txt"
	build_info="boxer $(cd "$root" && git rev-parse --short HEAD) (dirty: $(cd "$root" && git status --porcelain | grep -c .) paths), go $(go version | cut -d' ' -f3), rustc $(cd "$root/rust/fffi2stub" && rustc --version | cut -d' ' -f2), native $(uname -sm)"
	echo "$build_info" > "$work/build-info.txt"
	if [[ -n "$pack" ]]; then echo "measure.sh: packed into $pack"; exit 0; fi
fi
cp "$work/fetchtable.txt" "$run/raw/fetchtable.txt"
table=$(cat "$work/fetchtable.txt")
native_ok=1
if ! "$work/wasmspike_native" -list >/dev/null 2>&1; then
	native_ok=0
	echo "measure.sh: the native binary does not run here ($(uname -sm)); native arms skipped" >&2
fi

# ---- browsers ---------------------------------------------------------------
browser_cmd() {
	case "$1" in
	chromium)
		if [[ -n "${CHROMIUM:-}" ]]; then echo "$CHROMIUM"
		elif flatpak info io.github.ungoogled_software.ungoogled_chromium >/dev/null 2>&1; then echo "flatpak run io.github.ungoogled_software.ungoogled_chromium"
		elif command -v chromium >/dev/null; then echo chromium
		fi ;;
	firefox)
		if [[ -n "${FIREFOX:-}" ]]; then echo "$FIREFOX"
		elif flatpak info org.mozilla.firefox >/dev/null 2>&1; then echo "flatpak run org.mozilla.firefox"
		elif command -v firefox >/dev/null; then echo firefox
		fi ;;
	esac
}
browser_version() {
	case "$1" in
	chromium) $2 --version 2>/dev/null | tail -1 ;;
	firefox) $2 --version 2>/dev/null | tail -1 ;;
	esac
}

# ---- results ----------------------------------------------------------------
results="$run/results.tsv"
printf 'scene\tarm\thost\ttarget\tconsumer\tflush\tframes\tbytes_frame\tmsgs_frame\trender_p50_us\trender_p90_us\trender_max_us\tsync_p50_us\tsync_p90_us\ttotal_p50_us\ttotal_p90_us\tbridge_us_frame\twall_ms\trust_us_frame\trust_interpret_us\tmesh_bytes_frame\n' > "$results"

# record <scene> <arm> <host> <file-with-RESULT-or-ARM-line>
record() {
	python3 - "$1" "$2" "$3" "$4" >> "$results" <<'EOF'
import sys, json
scene, arm, host, path = sys.argv[1:5]
line = next((l for l in open(path) if l.startswith('RESULT ') or l.startswith('ARM ')), None)
if line is None:
    print('\t'.join([scene, arm, host] + ['NA'] * 18)); sys.exit()
d = json.loads(line.split(' ', 1)[1])
r = d.get('result') or d
stub = d.get('stub') or d.get('host') or {}
bridge = d.get('bridge') or {}
hostd = d.get('host') or {}
frames = r['frames']
msgs = r['messages_per_frame'] or (stub.get('messages', 0) / max(1, stub.get('frames', 1)))
bridge_us = 1e3 * bridge.get('bridgeMs', 0) / max(1, stub.get('frames', 1)) if bridge else 0
q = lambda k, p: '%.0f' % r[k][p]
flush = 'n/a' if r['consumer'] != 'pipe' else ('host' if hostd else ('lazy' if r.get('lazy_flush') else 'eager'))
print('\t'.join([scene, arm, host, r['target'], r['consumer'], flush, str(frames), '%.0f' % r['bytes_per_frame'], '%.0f' % msgs,
                 q('render_us', 'p50'), q('render_us', 'p90'), q('render_us', 'max'), q('sync_us', 'p50'), q('sync_us', 'p90'),
                 q('total_us', 'p50'), q('total_us', 'p90'), '%.0f' % bridge_us, '%.0f' % d.get('wallMs', 0),
                 '%.0f' % (1e3 * hostd.get('stepMs', 0) / max(1, hostd.get('frames', 1))) if hostd else 'n/a',
                 str(hostd.get('lastInterpretUs', 'n/a')) if hostd else 'n/a',
                 '%.0f' % (hostd.get('meshBytes', 0) / max(1, hostd.get('frames', 1))) if hostd else 'n/a']))
EOF
}

scene_flags() {
	echo "-scene $1 -rows $ROWS -frames $FRAMES -warmup $WARMUP -stage $STAGE"
}

for scene in $SCENES; do
	flags=$(scene_flags "$scene")
	# native, in-process
	arm=native-inproc
	if [[ $native_ok == 1 ]]; then
	"$work/wasmspike_native" -consumer inproc -fetchTable "$table" $flags -target native -arm $arm 2> "$run/raw/$scene-$arm.txt" || true
	record "$scene" $arm native "$run/raw/$scene-$arm.txt"
	fi
	# native, pipe to the stub binary (stderr must never share the FIFO);
	# once flushing after every message, once flushing only before a read
	for flush in eager lazy; do
		[[ $native_ok == 1 ]] || break
		arm=native-pipe-$flush
		lazy=""; [[ $flush == eager ]] && lazy="-lazyFlush=false"
		rm -f "$work/a.fifo" "$work/b.fifo"; mkfifo "$work/a.fifo" "$work/b.fifo"
		timeout 600 "$work/fffi2stub" "$work/fetchtable.txt" 1024 600 < "$work/a.fifo" > "$work/b.fifo" 2> "$run/raw/$scene-$arm-stub.txt" &
		timeout 600 "$work/wasmspike_native" -consumer pipe $lazy $flags -target native -arm $arm > "$work/a.fifo" < "$work/b.fifo" 2> "$run/raw/$scene-$arm.txt" || true
		wait
		record "$scene" $arm native "$run/raw/$scene-$arm.txt"
	done
	# node, both targets: in-process, then the bridge with both flush modes,
	# then the bridge into the real Rust host (deferred flush, the default),
	# then — wasip1 only — the reactor build, the deployment shape: the host
	# calls a frame export per tick instead of the module blocking in main
	for target in $TARGETS; do
		for mode in inproc pipe-eager pipe-lazy host reactor-host; do
			arm=node-$target-$mode
			peer=(--stub "$work/fffi2stub.wasm")
			gomod="$work/wasmspike_$target.wasm"
			reactor=()
			if [[ $mode == host || $mode == reactor-host ]]; then
				[[ -f "$work/imzero2.wasm" ]] || continue
				peer=(--host "$work/imzero2.wasm")
			fi
			if [[ $mode == reactor-host ]]; then
				[[ $target == wasip1 ]] || continue
				gomod="$work/wasmspike_wasip1_reactor.wasm"
				reactor=(--reactor 1)
			fi
			consumer=${mode%%-*}; [[ $mode == host || $mode == reactor-host ]] && consumer=pipe
			lazy=""; [[ $mode == pipe-eager ]] && lazy="-lazyFlush=false"
			timeout 900 node "$here/harness/run_node.mjs" --target "$target" "${reactor[@]}" --go "$gomod" "${peer[@]}" \
				--table "$work/fetchtable.txt" --wasm-exec "$work/wasm_exec.js" -- -consumer $consumer $lazy $flags -target "$target" -arm $arm \
				> "$run/raw/$scene-$arm.txt" 2> "$run/raw/$scene-$arm-stderr.txt" || true
			record "$scene" $arm node "$run/raw/$scene-$arm.txt"
		done
	done
	# browsers, bridge arms only
	for browser in $BROWSERS; do
		cmd=$(browser_cmd "$browser")
		[[ -z "$cmd" ]] && { echo "measure.sh: no $browser found; skipped" >&2; continue; }
		for target in $TARGETS; do
		for flush in eager lazy host; do
			[[ $flush == host && ! -f "$work/imzero2.wasm" ]] && continue
			arm=$browser-$target-pipe-$flush
			out="$run/raw/$scene-$arm.txt"
			"$work/imzero2tab" serve --dir "$work" --listen 127.0.0.1:0 --exitOnReport > "$out" 2>&1 &
			srv=$!
			for _ in $(seq 1 50); do grep -q '^PORT ' "$out" 2>/dev/null && break; sleep 0.1; done
			port=$(sed -n 's/^PORT //p' "$out")
			hostq=""; [[ $flush == host ]] && hostq="&host=1"
			url="http://127.0.0.1:$port/index.html?target=$target&consumer=pipe&flush=$flush$hostq&scene=$scene&rows=$ROWS&frames=$FRAMES&warmup=$WARMUP&stage=$STAGE&arm=$arm"
			mkdir -p "$PROFILE_DIR"
			profile=$(mktemp -d "$PROFILE_DIR/$browser.XXXXXX")
			case "$browser" in
			chromium) $cmd --headless=new --disable-gpu --user-data-dir="$profile" "$url" > "$run/raw/$scene-$arm-browser.txt" 2>&1 & ;;
			firefox) $cmd --headless --new-instance --profile "$profile" "$url" > "$run/raw/$scene-$arm-browser.txt" 2>&1 & ;;
			esac
			bp=$!
			# a browser that has not even fetched the page after 30 s is not
			# going to; the report itself may take minutes
			for i in $(seq 1 900); do
				kill -0 $srv 2>/dev/null || break
				if (( i == 30 )) && ! grep -q '^GET /index.html' "$out"; then echo "measure.sh: $browser never loaded the page; arm $arm skipped" >&2; break; fi
				sleep 1
			done
			kill $bp 2>/dev/null || true; kill $srv 2>/dev/null || true; wait $bp 2>/dev/null || true
			rm -rf "$profile"
			record "$scene" $arm "$browser" "$out"
		done
		done
	done
done

rmdir "$PROFILE_DIR" 2>/dev/null || true

# ---- environment ------------------------------------------------------------
{
	echo "# Environment"
	echo
	echo "- date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
	echo "- build: $build_info"
	echo "- node: $(node --version)"
	for browser in $BROWSERS; do
		cmd=$(browser_cmd "$browser"); [[ -n "$cmd" ]] && echo "- $browser: $(browser_version "$browser" "$cmd")"
	done
	echo "- cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | sed 's/^ //'), $(nproc) threads"
	echo "- memory: $(awk '/MemTotal/{printf "%.0f GiB", $2/1048576}' /proc/meminfo)"
	echo "- kernel: $(uname -r)"
	echo "- load at start: $(cut -d' ' -f1-3 /proc/loadavg)"
	echo "- cpufreq: governor $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null || echo unknown), driver $(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_driver 2>/dev/null || echo unknown), epp $(cat /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference 2>/dev/null || echo n/a)$(command -v powerprofilesctl >/dev/null && echo ", profile $(powerprofilesctl get 2>/dev/null)")"
	echo "- settings: SCENES=\"$SCENES\" ROWS=$ROWS FRAMES=$FRAMES WARMUP=$WARMUP TARGETS=\"$TARGETS\" BROWSERS=\"$BROWSERS\" STAGE=$STAGE"
} > "$run/environment.md"
echo "measure.sh: results in $results"
