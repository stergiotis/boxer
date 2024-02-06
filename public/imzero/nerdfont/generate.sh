#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

./main_go nerdfont generate --glyphJson ./glyphnames.json \
	--staticGlyphsGoPackage "nerdfont" \
	--staticGlyphsGoFile "../public/nerdfont/staticGlyphs.go" \
	--dynamicGlyphsGoPackage "nerdfont" \
	--dynamicGlyphsGoFile "../public/nerdfont/dynamicGlyphs.go"
