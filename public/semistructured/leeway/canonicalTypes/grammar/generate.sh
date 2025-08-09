#!/bin/bash
set -ev
# shellcheck disable=SC2128
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f ./*.out.*
export ANTLR4_TOOLS_ANTLR_VERSION="4.13.2"
antlr4 -Werror -Dlanguage=Go -visitor -no-listener -package grammar -o . CanonicalTypeSignatureLexer.g4 CanonicalTypeSignatureParser.g4
rename .go .out.go ./*.go
rm -rf gen