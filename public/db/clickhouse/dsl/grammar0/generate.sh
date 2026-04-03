#!/bin/bash
set -ev
# shellcheck disable=SC2128
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f ./*.out.*
export ANTLR4_TOOLS_ANTLR_VERSION="4.13.2"
antlr4="java -jar $HOME/Downloads/antlr-4.13.2-complete.jar"

compile() {
  rm -f *.out.go
  $antlr4 -Werror -Dlanguage=Go -visitor -no-listener -package "grammar$1" -o . ClickHouseLexer.g4 "ClickHouseParser$2.g4"
  rename .go .out.go ./*.go
}
compile "0" "Grammar0"
cd ../grammar1 || exit 1
compile "1" "Grammar1"
cd ../grammar2 || exit 1
compile "2" "Grammar2"