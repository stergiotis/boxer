# shellcheck shell=bash
# Shared font resolution for the imzero2 launchers.
#
# Source this file, call imzero2_resolve_fonts, and pass the array it fills:
#
#     . "$(scripts/boxer-path.sh)/rust/imzero2/font-resolve.sh"
#     imzero2_resolve_fonts
#     main_go ... demo "${IMZERO2_FONT_ARGS[@]}"
#
# It is a library rather than four lines per launcher because the failures it
# prevents are silent ones. The Go host forwards a face to the client only when
# it is given one, and each unset face degrades without an error: no main font
# drops the whole UI to egui's built-in, and no mono font makes the Rust loader
# re-use the proportional main font as the FontFamily::Monospace primary, under
# which nothing fixed-width lines up — every row of a query result individually
# monospaced, no two rows the same width, and every box frame ragged. A
# launcher that quietly omits a flag therefore looks like a rendering bug in
# the client, which is a long way from where the cause is. Consumers outside
# this repository (shadow-boxer, sailing) resolve this file through their boxer
# pin, so a fix here reaches them at their next bump.
#
# The four variables are inputs as well as outputs: MAIN_FONT, MONO_FONT,
# PHOSPHOR_FONT and FALLBACK_FONT are honoured when already set, which is what
# hmi-fonts-pragmatapro.sh relies on.
#
# Safe to source under `set -euo pipefail`: every function returns 0, so a face
# that does not resolve leaves its variable empty instead of killing the
# caller.

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	echo "font-resolve.sh: source this file, do not run it" >&2
	exit 64
fi

# Phosphor is vendored here rather than installed, so it is resolved relative
# to this file — the one path that stays right for a launcher in another
# repository, which reaches this file through its boxer pin and has no reason
# to know the layout.
imzero2_font_lib_dir=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")

# Resolve a font file through fontconfig instead of hardcoding one distro's
# layout: Fedora ships Noto under google-noto-vf/, Arch under noto/ &
# noto-cjk/, Debian under truetype/noto/, each with its own directory and file
# names. fc-match is part of fontconfig and present on every mainstream
# desktop. It always answers with *some* font though, so the lookup is guarded
# by the matched family name to reject a silent fallback to an unrelated face.
imzero2_resolve_family() {
	local family="$1" want="$2" line file fam
	command -v fc-match >/dev/null 2>&1 || return 0
	line=$(fc-match -f '%{file}\t%{family}\n' "$family" 2>/dev/null) || return 0
	file="${line%%$'\t'*}"
	fam="${line#*$'\t'}"
	if [[ "$fam" == *"$want"* && -f "$file" ]]; then
		printf '%s' "$file"
	fi
	return 0
}

# The fixed-width face carries one requirement beyond being monospaced:
# BOX-DRAWING COVERAGE (U+2500…). Anything that renders a query result —
# ClickHouse's own `system.documentation` examples, a `clickhouse client`
# transcript pasted into a doc — draws its frame from that block, and a face
# without those glyphs sends exactly them down the fallback chain, where the
# advance is somebody else's. The result is a box whose corners do not meet. So
# the guard checks the charset, not just the family name.
imzero2_resolve_mono() {
	local want="$1" line file fam
	command -v fc-match >/dev/null 2>&1 || return 0
	line=$(fc-match -f '%{file}\t%{family}\n' "$want" 2>/dev/null) || return 0
	file="${line%%$'\t'*}"
	fam="${line#*$'\t'}"
	[ -f "$file" ] || return 0
	[[ "$fam" == *"$want"* ]] || return 0
	# fc-query prints the charset as hex ranges; 2500 falling inside one of
	# them is what makes the frame line up. Without fc-query, take the
	# family match rather than resolving nothing.
	if ! command -v fc-query >/dev/null 2>&1; then
		printf '%s' "$file"
		return 0
	fi
	fc-query --format='%{charset}\n' "$file" 2>/dev/null \
		| tr ' ' '\n' | grep -qE '^25[0-7][0-9a-f]' || return 0
	printf '%s' "$file"
	return 0
}

# imzero2_resolve_fonts fills MAIN_FONT, MONO_FONT, PHOSPHOR_FONT and
# FALLBACK_FONT (each only when unset), warns about what it could not find, and
# fills IMZERO2_FONT_ARGS with the flags for the faces it has. The array is the
# unit callers pass on: a face that resolved to nothing contributes no flag,
# rather than an empty one the client would reject.
imzero2_resolve_fonts() {
	local me="${0##*/}"

	# Proportional UI font: Noto Sans. The hardcoded path is the last
	# resort for a box without fontconfig.
	MAIN_FONT="${MAIN_FONT:-$(imzero2_resolve_family 'Noto Sans' 'Noto Sans')}"
	MAIN_FONT="${MAIN_FONT:-/usr/share/fonts/google-noto-vf/NotoSans[wght].ttf}"

	# Preference order: DejaVu Sans Mono is the widest-installed face that
	# covers the box-drawing block; Liberation Mono and Adwaita Mono are the
	# common alternatives on distros that ship neither.
	if [ -z "${MONO_FONT:-}" ]; then
		local want_mono
		for want_mono in 'DejaVu Sans Mono' 'Liberation Mono' 'Adwaita Mono'; do
			MONO_FONT=$(imzero2_resolve_mono "$want_mono")
			if [ -n "$MONO_FONT" ]; then
				break
			fi
		done
	fi

	# ADR-0044 iconography: one icon font, Phosphor regular, vendored from
	# the `stergiotis/ids-fonts` v0.2.4 release. No download fallback and no
	# fontconfig lookup — it is a file in this repository.
	PHOSPHOR_FONT="${PHOSPHOR_FONT:-$imzero2_font_lib_dir/assets/fonts/phosphor/Phosphor.ttf}"

	# CJK fallback: query a language-qualified family ('… CJK JP') because
	# the bare 'Noto Sans Mono CJK' is not a fontconfig family and silently
	# falls back to plain Noto Sans (no CJK glyphs); the 'CJK' guard rejects
	# that.
	FALLBACK_FONT="${FALLBACK_FONT:-$(imzero2_resolve_family 'Noto Sans Mono CJK JP' 'CJK')}"
	FALLBACK_FONT="${FALLBACK_FONT:-/usr/share/fonts/google-noto-sans-mono-cjk-vf-fonts/NotoSansMonoCJK-VF.ttc}"

	# Best-effort heads-up. Detection above keeps these pointing at real
	# files wherever the fonts are installed, so a warning here means a box
	# that lacks them — where the symptom is a rendering oddity nobody would
	# trace back to a launcher.
	if [ -n "$MAIN_FONT" ] && [ ! -f "$MAIN_FONT" ]; then
		echo "$me: MAIN_FONT not found: $MAIN_FONT" >&2
		echo "  install Noto Sans (Fedora: google-noto-sans-vf-fonts, Arch: noto-fonts," >&2
		echo "  Debian/Ubuntu: fonts-noto-core) or set MAIN_FONT to an absolute .ttf." >&2
	fi
	if [ -z "${MONO_FONT:-}" ]; then
		echo "$me: no monospace face with box-drawing coverage found — table frames" >&2
		echo "  will render ragged. Install DejaVu Sans Mono or set MONO_FONT." >&2
	fi
	if [ ! -f "$PHOSPHOR_FONT" ]; then
		echo "$me: PHOSPHOR_FONT not found: $PHOSPHOR_FONT" >&2
		echo "  icons render as tofu; this repository should carry the file." >&2
	fi

	IMZERO2_FONT_ARGS=()
	if [ -n "${MAIN_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--mainFontTTF "$MAIN_FONT"); fi
	if [ -n "${MONO_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--monoFontTTF "$MONO_FONT"); fi
	if [ -n "${PHOSPHOR_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--phosphorFontTTF "$PHOSPHOR_FONT"); fi
	if [ -n "${FALLBACK_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--fallbackFontTTF "$FALLBACK_FONT"); fi
	return 0
}
