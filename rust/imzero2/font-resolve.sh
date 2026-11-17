# shellcheck shell=bash
# Font flags for the imzero2 launchers.
#
# Source this file, call imzero2_resolve_fonts, and pass the array it fills:
#
#     . "$(scripts/boxer-path.sh)/rust/imzero2/font-resolve.sh"
#     imzero2_resolve_fonts
#     main_go ... demo "${IMZERO2_FONT_ARGS[@]}"
#
# The Go host finds the text faces itself: a font flag left empty is filled
# through fontconfig when the client starts (Noto Sans, its bold face, a
# fixed-width face covering the box-drawing block, Noto Sans CJK), and a face
# it cannot find is logged. `imzero2 fonts` prints what it would choose. So
# this file only carries what the host cannot find on its own, and what a
# launcher pins:
#
#   PHOSPHOR_FONT   the icon font vendored in this repository, resolved relative
#                   to this file, so a launcher in another repository reaching
#                   it through its boxer pin needs no knowledge of the layout;
#   MAIN_FONT, BOLD_FONT, MONO_FONT, FALLBACK_FONT
#                   honoured when set, as hmi-fonts-pragmatapro.sh does, and
#                   otherwise left to the host.
#
# Safe to source under `set -euo pipefail`: the function returns 0.

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	echo "font-resolve.sh: source this file, do not run it" >&2
	exit 64
fi

imzero2_font_lib_dir=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")

# imzero2_resolve_fonts fills PHOSPHOR_FONT when unset and IMZERO2_FONT_ARGS
# with a flag for every face that is set.
imzero2_resolve_fonts() {
	# ADR-0044 iconography: one icon font, Phosphor regular, vendored from
	# the `stergiotis/ids-fonts` v0.2.4 release.
	PHOSPHOR_FONT="${PHOSPHOR_FONT:-$imzero2_font_lib_dir/assets/fonts/phosphor/Phosphor.ttf}"
	if [ ! -f "$PHOSPHOR_FONT" ]; then
		echo "${0##*/}: PHOSPHOR_FONT not found: $PHOSPHOR_FONT" >&2
		echo "  icons render as tofu; this repository should carry the file." >&2
	fi

	IMZERO2_FONT_ARGS=()
	if [ -n "${MAIN_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--mainFontTTF "$MAIN_FONT"); fi
	if [ -n "${MONO_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--monoFontTTF "$MONO_FONT"); fi
	if [ -n "${PHOSPHOR_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--phosphorFontTTF "$PHOSPHOR_FONT"); fi
	if [ -n "${FALLBACK_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--fallbackFontTTF "$FALLBACK_FONT"); fi
	if [ -n "${BOLD_FONT:-}" ]; then IMZERO2_FONT_ARGS+=(--boldFontTTF "$BOLD_FONT"); fi
	return 0
}
