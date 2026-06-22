#!/bin/bash
# Launch the imzero2 demo carousel through the headless remote-access host
# (ADR-0024): no window, renders offscreen, encodes H.264, serves a browser
# viewer. Mirrors hmi.sh's font resolution; builds both sides first.
#
#   ./hmi_headless.sh                          # widgets demo on 127.0.0.1:8089
#   ./hmi_headless.sh --launch play            # any demo selector
#   IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089 ...   # off-loopback bind is REFUSED (host has no auth/TLS yet,
#                                              # ADR-0082): front it with an authenticating TLS reverse
#                                              # proxy, or set IMZERO2_HEADLESS_INSECURE_EXPOSE=1 to
#                                              # override on a trusted / air-gapped network
#
# Viewer page: http://<host>:<port+1>/  (WebCodecs-capable browser required)
#
# Encoder defaults to VAAPI (ADR-0024 SD3). On boxes without VAAPI H.264
# encode (e.g. Fedora's mesa without the freeworld drivers), override:
#   IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libopenh264 -rc_mode off -bf 0 -g 120"
# (-g 120 = periodic IDR, matching the built-in default: a late-joining passive
# viewer can start at the next scheduled key frame, ADR-0086 SD5, and the active
# view pays a tunable refresh, SD10. Use -g 100000 only for a known single
# viewer — an infinite GOP starves any passive joiner of a key frame.)
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

# --sandbox (ours, NOT a main_go flag): run the demo inside a transient systemd
# unit whose filesystem + syscall sandbox mirrors the deployed
# imzero2-demo.service drop-in (showcase/onbox/20-hardening.conf), so the box's
# "limited filesystem access" is reproducible ad-hoc. Strip it from the args
# before launch-detection or main_go ever see it; the build still runs on the
# host (the sandbox is read-only) — see the wrapped exec at the end.
SANDBOX=0
_sbx_args=()
for _a in "$@"; do
	if [ "$_a" = "--sandbox" ]; then SANDBOX=1; else _sbx_args+=("$_a"); fi
done
set -- "${_sbx_args[@]}"

resolve_noto() {
    local family="$1" want="$2" line file fam
    command -v fc-match >/dev/null 2>&1 || return 0
    line=$(fc-match -f '%{file}\t%{family}\n' "$family" 2>/dev/null) || return 0
    file="${line%%$'\t'*}"; fam="${line#*$'\t'}"
    [[ "$fam" == *"$want"* && -f "$file" ]] && printf '%s' "$file"
}
MAIN_FONT="${MAIN_FONT:-$(resolve_noto 'Noto Sans' 'Noto Sans')}"
PHOSPHOR_FONT="${PHOSPHOR_FONT:-$here/assets/fonts/phosphor/Phosphor.ttf}"
FALLBACK_FONT="${FALLBACK_FONT:-$(resolve_noto 'Noto Sans Mono CJK JP' 'CJK')}"

export IMZERO2_HEADLESS_LISTEN="${IMZERO2_HEADLESS_LISTEN:-127.0.0.1:8089}"
export IMZERO2_HEADLESS_FPS="${IMZERO2_HEADLESS_FPS:-30}"
WINDOW_W="${WINDOW_W:-1280}"
WINDOW_H="${WINDOW_H:-800}"

# Bind-address gate (ADR-0082). The headless carrier has no authentication or TLS
# yet — ADR-0082 is accepted but not implemented in wscarrier.rs — so an
# off-loopback bind serves an unauthenticated, unencrypted stream, and because the
# same WebSocket carries input, anyone who can reach it gets full remote control.
# Fail closed: refuse a non-loopback IMZERO2_HEADLESS_LISTEN unless the operator
# explicitly accepts the risk (trusted/air-gapped LAN, or behind an authenticating
# TLS-terminating reverse proxy). Drop this gate once the carrier enforces token +
# TLS itself.
listen_addr="$IMZERO2_HEADLESS_LISTEN"
if [[ "$listen_addr" == \[*\]:* ]]; then
	bind_host="${listen_addr#\[}"; bind_host="${bind_host%%\]*}"   # [ipv6]:port
else
	bind_host="${listen_addr%:*}"                                  # host:port (or bare host)
fi
case "$bind_host" in
	127.*|::1|localhost) ;;                                        # loopback — always allowed
	*)
		if [[ "$IMZERO2_HEADLESS_INSECURE_EXPOSE" == 1 ]]; then
			echo "hmi_headless.sh: WARNING — binding non-loopback '${bind_host:-<all interfaces>}' with NO auth/TLS (ADR-0082 unimplemented);" >&2
			echo "hmi_headless.sh:           the stream AND its input channel are exposed to anyone who can reach this address." >&2
		else
			echo "hmi_headless.sh: refusing non-loopback bind '${bind_host:-<all interfaces>}' — the headless host has no auth/TLS yet (ADR-0082)." >&2
			echo "hmi_headless.sh: an off-loopback stream is unauthenticated and grants remote input control. Front it with an" >&2
			echo "hmi_headless.sh: authenticating TLS reverse proxy, or set IMZERO2_HEADLESS_INSECURE_EXPOSE=1 to override on a trusted network." >&2
			exit 1
		fi
		;;
esac

# Rebuild policy: compile only when launched interactively. The edit/run loop
# wants a fresh binary, but a non-interactive launcher (a systemd unit, the
# ansible deploy timer, CI) should run the already-built artifact rather than
# invoke the toolchain on the box. The probe is stdin, so piping the output —
# `./hmi_headless.sh | tee run.log` — still counts as interactive. HMI_BUILD=1/0
# forces the decision; a missing binary rebuilds regardless, so a launcher never
# starts nothing.
go_bin="$here/main_go"
rust_bin="$here/target/headless/release/imzero2"
if [[ "$HMI_BUILD" == 0 ]]; then
	do_build=0
elif [[ "$HMI_BUILD" == 1 || -t 0 ]]; then
	do_build=1
elif [[ ! -x "$go_bin" || ! -x "$rust_bin" ]]; then
	echo "hmi_headless.sh: pre-built binary missing — rebuilding despite non-interactive launch" >&2
	do_build=1
else
	echo "hmi_headless.sh: non-interactive launch — skipping rebuild (HMI_BUILD=1 to force)" >&2
	do_build=0
fi
if [[ "$do_build" == 1 ]]; then
	./build_rust_headless.sh || exit 1
	./build_go.sh || exit 1
fi

projectRoot="$here"
while [ "$projectRoot" != "/" ] && [ ! -f "$projectRoot/go.mod" ]; do
    projectRoot=$(dirname "$projectRoot")
done
cd "$projectRoot"

launch="--launch widgets"
if [[ "$*" == *"--launch"* ]]; then
    launch=""
fi

ws_port="${IMZERO2_HEADLESS_LISTEN##*:}"
page_host="${IMZERO2_HEADLESS_LISTEN%%:*}"
if [ "$page_host" = "0.0.0.0" ]; then
    # Binding all interfaces: print a routable address for the hint.
    page_host="$(hostname -I 2>/dev/null | awk '{print $1}')"
    page_host="${page_host:-<lan-ip>}"
fi
echo "viewer page: http://$page_host:$ws_port/ (also :$((ws_port + 1)))" >&2

cmd=("$here/main_go" --logFormat=console --logLevel=info imzero2 demo
    --clientBinary "$here/target/headless/release/imzero2"
    --clientInitialMainWindowWidth "$WINDOW_W"
    --clientInitialMainWindowHeight "$WINDOW_H")
[ -n "$MAIN_FONT" ]     && cmd+=(--mainFontTTF "$MAIN_FONT")
[ -n "$PHOSPHOR_FONT" ] && cmd+=(--phosphorFontTTF "$PHOSPHOR_FONT")
[ -n "$FALLBACK_FONT" ] && cmd+=(--fallbackFontTTF "$FALLBACK_FONT")
cmd+=($launch "$@")

if [ "$SANDBOX" != 1 ]; then
    exec "${cmd[@]}"
fi

# --sandbox: launch main_go inside a transient systemd unit whose FS + syscall
# sandbox is PARSED from the deployment's own drop-in, so the two never drift.
command -v systemd-run >/dev/null 2>&1 || { echo "hmi_headless.sh: --sandbox needs systemd-run (systemd)" >&2; exit 1; }
hardening="$projectRoot/showcase/onbox/20-hardening.conf"
[ -f "$hardening" ] || { echo "hmi_headless.sh: --sandbox: drop-in not found: $hardening" >&2; exit 1; }

# Replay every [Service] directive as a `systemd-run -p`, minus ProtectHome: the
# box runs from /opt so it hides all homes, but a dev checkout lives under /home
# — so hide every home EXCEPT this repo (read-only), the ad-hoc analogue of the
# box exposing only /opt/imzero2.
sandbox_props=()
while IFS= read -r kv; do
    sandbox_props+=(-p "$kv")
done < <(awk '
    /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
    /^\[/ { insvc = ($0 ~ /^\[Service\]/); next }
    insvc {
        k = $0; sub(/=.*/, "", k); gsub(/[[:space:]]/, "", k)
        if (k != "ProtectHome") print
    }' "$hardening")
sandbox_props+=(-p "ProtectHome=tmpfs" -p "BindReadOnlyPaths=$projectRoot")

# systemd-run does NOT inherit our environment: forward every IMZERO2_* the
# launcher set, plus a software encoder default (PrivateDevices hides /dev/dri,
# so VAAPI is unavailable in here) and a writable cache under the private /tmp.
: "${LIBGL_ALWAYS_SOFTWARE:=1}"
: "${IMZERO2_HEADLESS_ENCODER_ARGS:=-c:v libopenh264 -rc_mode off -bf 0 -g 100000}"
: "${XDG_CACHE_HOME:=/tmp/.cache}"
export LIBGL_ALWAYS_SOFTWARE IMZERO2_HEADLESS_ENCODER_ARGS XDG_CACHE_HOME
sandbox_env=()
for _v in "${!IMZERO2_@}"; do sandbox_env+=(--setenv="$_v=${!_v}"); done
sandbox_env+=(--setenv="LIBGL_ALWAYS_SOFTWARE=$LIBGL_ALWAYS_SOFTWARE" --setenv="XDG_CACHE_HOME=$XDG_CACHE_HOME")

echo "hmi_headless.sh: --sandbox → transient systemd unit (ProtectSystem=strict; homes hidden;" >&2
echo "hmi_headless.sh:   only $projectRoot visible, read-only). IPAddress* is parsed but NOT enforced" >&2
echo "hmi_headless.sh:   under --user — use 'sudo systemd-run -p User=\$USER …' for egress-deny fidelity." >&2
exec systemd-run --user --pty --collect "${sandbox_props[@]}" "${sandbox_env[@]}" -- "${cmd[@]}"
