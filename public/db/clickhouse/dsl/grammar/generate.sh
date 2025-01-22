#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f *.out.*
export ANTLR4_TOOLS_ANTLR_VERSION="4.13.2" 
antlr4 -Dlanguage=Go -visitor -no-listener -package grammar -o . ClickHouseLexer.g4 ClickHouseParser.g4
rename "s/\.go/.out.go/" *.go
rename "s/\.interp/.out.interp/" *.interp
rename "s/\.tokens/.out.tokens/" *.tokens
