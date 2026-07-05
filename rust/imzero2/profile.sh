#!/bin/bash
# =============================================================================
# profile.sh — Launch the imzero2 demo under a profiler
# =============================================================================
#
# Usage:
#   APP=<appCode> ./profile.sh [MODE] [CAPTURE_SECONDS]
#
# Modes:
#   go-cpu       Go CPU profile        (pprof HTTP, captures for CAPTURE_SECONDS)
#   go-allocs    Go allocation profile (pprof HTTP)
#   go-heap      Go heap snapshot      (pprof HTTP)
#   go-trace     Go execution trace    (needs `go tool trace`)
#   rust-cpu     Rust CPU flamegraph   (cargo-flamegraph + perf)
#   rust-samply  Rust CPU profile via samply (sampling, no perf install needed)
#   rust-puffin  Rust frame profiler   (client exposes 127.0.0.1:8585)
#   launch       Just launch with the pprof endpoint, capture manually
#
# CAPTURE_SECONDS defaults to 15; ignored for modes that run interactively.
# APP selects the demo appCode passed to `--launch` (default 0).
# VSYNC defaults to on — set off to remove the frame-rate cap.
#
# Ported from pebble2impl/scripts/profile_{play,etable}.sh and generalised to
# any demo appCode (the pebble2impl-specific spinnaker SQL env was dropped).
# =============================================================================
set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
mode="${1:-launch}"
capture_s="${2:-15}"
app="${APP:-0}"
clientDir="$here/target/release"
VSYNC="${VSYNC:-on}"

# ---- Preflight for modes that need external tooling ----
preflight_rust_cpu() {
    local missing=""
    command -v flamegraph >/dev/null 2>&1 || missing="${missing} flamegraph"
    command -v perf       >/dev/null 2>&1 || missing="${missing} perf"
    local paranoid
    paranoid=$(cat /proc/sys/kernel/perf_event_paranoid 2>/dev/null || echo "?")
    if [ -n "$missing" ] || [ "$paranoid" = "?" ] || [ "$paranoid" -gt 1 ] 2>/dev/null; then
        echo "==> rust-cpu prerequisites missing:"
        [ -n "$missing" ] && echo "    - not in PATH:${missing}"
        echo "    - kernel.perf_event_paranoid = $paranoid (need <= 1)"
        echo
        echo "    To enable:"
        echo "      sudo dnf install perf          # or apt install linux-perf"
        echo "      cargo install flamegraph"
        echo "      echo 1 | sudo tee /proc/sys/kernel/perf_event_paranoid"
        echo
        echo "    Simpler alternatives on this host:"
        echo "      $0 rust-samply   # Firefox Profiler format, no perf install"
        echo "      $0 rust-puffin   # in-app frame profiler, no perf at all"
        return 1
    fi
    return 0
}
preflight_rust_samply() {
    if ! command -v samply >/dev/null 2>&1; then
        echo "==> samply not in PATH. Install with: cargo install samply"
        return 1
    fi
    local paranoid
    paranoid=$(cat /proc/sys/kernel/perf_event_paranoid 2>/dev/null || echo "?")
    if [ "$paranoid" = "?" ] || [ "$paranoid" -gt 1 ] 2>/dev/null; then
        echo "==> kernel.perf_event_paranoid = $paranoid (need <= 1)."
        echo "    echo 1 | sudo tee /proc/sys/kernel/perf_event_paranoid"
        return 1
    fi
    return 0
}
case "$mode" in
    rust-cpu)    preflight_rust_cpu    || exit 1 ;;
    rust-samply) preflight_rust_samply || exit 1 ;;
esac

# ---- Build Go + Rust via the sibling build scripts ----
echo "==> Building Rust..."
./build_rust.sh || exit 1
echo "==> Building Go..."
./build_go.sh || exit 1

# ---- Common launch ----
launch_app() {
    local debug_mode="${1:-}"
    if [ -n "$debug_mode" ]; then
        export BOXER_IMZERO_DEBUG_MODE="$debug_mode"
        echo "==> BOXER_IMZERO_DEBUG_MODE=$debug_mode"
    else
        unset BOXER_IMZERO_DEBUG_MODE
    fi
    echo "==> Launching demo appCode $app (pprof on :6060)..."
    "$here/main_go" --logFormat=console --logLevel=info \
        --pprofHttpListenAddress "localhost:6060" \
        imzero2 demo --clientBinary "$clientDir/imzero2" \
            --clientType "egui" \
            --clientBackgroundColorRGBA 8f8f8fff \
            --clientVsync "$VSYNC" \
            --clientFullscreen off \
            --clientInitialMainWindowWidth 1800 \
            --clientInitialMainWindowHeight 1024 \
            --launch "$app"
}

case "$mode" in
    go-cpu)
        echo "==> Will capture Go CPU profile for ${capture_s}s after 3s warmup."
        launch_app "" &
        app_pid=$!
        sleep 3
        out="demo_cpu.pprof"
        echo "==> Capturing → $out"
        curl -s -o "$out" "http://localhost:6060/debug/pprof/profile?seconds=${capture_s}"
        echo "==> Saved $out"
        echo "==> Top functions:"
        go tool pprof -top -cum -nodecount=30 "$out" 2>/dev/null | head -60
        echo ""
        echo "==> View interactively: go tool pprof -http :7654 $out"
        kill "$app_pid" 2>/dev/null || true
        wait "$app_pid" 2>/dev/null || true
        ;;

    go-allocs)
        echo "==> Will capture Go allocs profile for ${capture_s}s after 3s warmup."
        launch_app "" &
        app_pid=$!
        sleep 3
        out="demo_allocs.pprof"
        echo "==> Capturing → $out"
        curl -s -o "$out" "http://localhost:6060/debug/pprof/allocs?seconds=${capture_s}"
        echo "==> Saved $out"
        echo "==> Top allocators (cumulative):"
        go tool pprof -top -cum -nodecount=30 "$out" 2>/dev/null | head -60
        kill "$app_pid" 2>/dev/null || true
        wait "$app_pid" 2>/dev/null || true
        ;;

    go-heap)
        echo "==> Will take heap snapshot after 5s."
        launch_app "" &
        app_pid=$!
        sleep 5
        out="demo_heap.pprof"
        curl -s -o "$out" "http://localhost:6060/debug/pprof/heap"
        echo "==> Saved $out"
        go tool pprof -top -cum -nodecount=30 "$out" 2>/dev/null | head -60
        kill "$app_pid" 2>/dev/null || true
        wait "$app_pid" 2>/dev/null || true
        ;;

    go-trace)
        echo "==> Will capture Go execution trace for ${capture_s}s."
        launch_app "" &
        app_pid=$!
        sleep 3
        out="demo_trace.out"
        curl -s -o "$out" "http://localhost:6060/debug/pprof/trace?seconds=${capture_s}"
        echo "==> Saved $out"
        echo "==> View: go tool trace $out"
        kill "$app_pid" 2>/dev/null || true
        wait "$app_pid" 2>/dev/null || true
        ;;

    rust-cpu)
        launch_app "flamegraph"
        echo "==> flamegraph.svg in CWD"
        ;;

    rust-samply)
        # samply wraps the Rust client via a shim so the Go launcher runs it
        # transparently. build_rust.sh already builds with --features puffin;
        # that doesn't conflict with sampling.
        echo "==> Launching Rust client under samply record..."
        shim_dir=$(mktemp -d)
        cat >"$shim_dir/imzero2" <<EOF
#!/bin/bash
exec samply record -o /tmp/imzero2_samply.profile -- "$clientDir/imzero2" "\$@"
EOF
        chmod +x "$shim_dir/imzero2"
        orig_clientDir="$clientDir"
        clientDir="$shim_dir"
        trap 'rm -rf "$shim_dir"' EXIT
        launch_app ""
        clientDir="$orig_clientDir"
        echo "==> Profile at /tmp/imzero2_samply.profile"
        echo "    View: samply load /tmp/imzero2_samply.profile"
        ;;

    rust-puffin)
        # build_rust.sh already builds with --features puffin — no rebuild
        # needed. The client exposes 127.0.0.1:8585 automatically.
        if ! command -v puffin_viewer >/dev/null 2>&1; then
            echo "==> Note: puffin_viewer not in PATH (install: cargo install puffin_viewer)"
        fi
        launch_app ""
        echo "==> Connect viewer: puffin_viewer --url 127.0.0.1:8585"
        ;;

    launch)
        echo "==> Launching demo appCode $app with pprof at http://localhost:6060/debug/pprof/"
        launch_app ""
        ;;

    *)
        echo "Unknown mode: $mode"
        echo "Usage: APP=<appCode> $0 {go-cpu|go-allocs|go-heap|go-trace|rust-cpu|rust-samply|rust-puffin|launch} [capture_seconds]"
        exit 1
        ;;
esac
