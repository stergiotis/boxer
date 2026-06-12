#!/bin/bash
# Launch the imzero2 demo carousel through the headless remote-access host
# (ADR-0024): no window, renders offscreen, encodes H.264, serves a browser
# viewer. Mirrors hmi.sh's font resolution; builds both sides first.
#
#   ./hmi_headless.sh                          # widgets demo on 127.0.0.1:8089
#   ./hmi_headless.sh --launch play            # any demo selector
#   IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089 ...   # expose beyond localhost (trusted networks only — no auth at v1)
#
# Viewer page: http://<host>:<port+1>/  (WebCodecs-capable browser required)
#
# Encoder defaults to VAAPI (ADR-0024 SD3). On boxes without VAAPI H.264
# encode (e.g. Fedora's mesa without the freeworld drivers), override:
#   IMZERO2_HEADLESS_ENCODER_ARGS="-c:v libopenh264 -b:v 4M -bf 0 -g 60"
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

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

./build_rust_headless.sh || exit 1
./build_go.sh || exit 1

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
echo "viewer page: http://$page_host:$((ws_port + 1))/" >&2

exec "$here/main_go" --logFormat=console --logLevel=info imzero2 demo \
    --clientBinary "$here/target/headless/release/imzero2" \
    --clientInitialMainWindowWidth "$WINDOW_W" \
    --clientInitialMainWindowHeight "$WINDOW_H" \
    ${MAIN_FONT:+--mainFontTTF "$MAIN_FONT"} \
    ${PHOSPHOR_FONT:+--phosphorFontTTF "$PHOSPHOR_FONT"} \
    ${FALLBACK_FONT:+--fallbackFontTTF "$FALLBACK_FONT"} \
    $launch \
    "$@"
