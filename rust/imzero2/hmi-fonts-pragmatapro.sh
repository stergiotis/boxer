# shellcheck shell=bash
# Source this file before running ./src/rust/hmi.sh to use PragmataPro as the
# monospace UI font. Per ADR-0030 §SD11, PragmataPro is a personal-install
# override only — the font is never shipped with the repo or binary.
#
# Scope: this script sets MONO_FONT only, so only FontFamily::Monospace
# (TextStyle::Monospace — code, hex dumps, fixed-width data) picks up
# PragmataPro. The proportional UI (Body/Heading/Button/Small) keeps
# whatever MAIN_FONT points at (Noto Sans by default). To override the
# proportional side too, set MAIN_FONT explicitly before sourcing.
#
# Usage:
#   source ./src/rust/hmi-fonts-pragmatapro.sh
#   ./src/rust/hmi.sh
#
# Or in one shot, without polluting the parent shell:
#   ( source ./src/rust/hmi-fonts-pragmatapro.sh && ./src/rust/hmi.sh )

PRAGMATA_DIR="${PRAGMATA_DIR:-$HOME/.local/share/fonts/pragmatapro}"

# Variants available in $PRAGMATA_DIR (R=Regular, B=Bold, I=Italic, Z=Bold Italic):
#   PragmataPro_Mono_R_liga_0903.ttf   <-- default: strict mono + programming ligatures
#   PragmataPro_Mono_R_0903.ttf            strict mono, no ligatures
#   PragmataProR_liga_0903.ttf             "narrow" PragmataPro + ligatures
#   PragmataProR_0903.ttf                  "narrow" PragmataPro, no ligatures
# Override MONO_FONT before sourcing to pick a different file.
MONO_FONT="${MONO_FONT:-$PRAGMATA_DIR/PragmataPro_Mono_R_liga_0903.ttf}"

if [[ ! -f "$MONO_FONT" ]]; then
	echo "hmi-fonts-pragmatapro.sh: font not found: $MONO_FONT" >&2
	echo "  install PragmataPro under \$PRAGMATA_DIR (currently $PRAGMATA_DIR)" >&2
	echo "  or set MONO_FONT to an absolute .ttf path before sourcing." >&2
	# Be safe whether sourced or executed.
	return 1 2>/dev/null || exit 1
fi

export MONO_FONT
# Leave MAIN_FONT, PHOSPHOR_FONT, FALLBACK_FONT at hmi.sh defaults —
# PragmataPro is a mono override, not a proportional one, and it
# carries neither icon glyphs nor CJK coverage.
echo "hmi-fonts-pragmatapro.sh: MONO_FONT=$MONO_FONT" >&2
