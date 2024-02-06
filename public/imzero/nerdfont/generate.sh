#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

../build.sh
../main --logFormat console nerdfont generate --glyphJson ./glyphnames.json \
	--staticGlyphsGoPackage "nerdfont" \
	--staticGlyphsGoFile "./staticGlyphs.go" \
	--dynamicGlyphsGoPackage "nerdfont" \
	--dynamicGlyphsGoFile "./dynamicGlyphs.go"
gofmt -l -w .
