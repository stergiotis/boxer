#!/bin/bash
# Regenerates the lexer and parser of grammar0, grammar1 and grammar2 from the
# .g4 files beside them. Needs a JRE and the ANTLR 4.13.2 "complete" jar from
# Maven Central (org.antlr:antlr4:4.13.2, classifier complete); ANTLR4_JAR
# names it. The jar is checked against the digest below before it runs.
# Every generated file (*.out.go, *.interp, *.tokens) is committed with the
# .g4 change that produced it; an unchanged grammar regenerates them byte for
# byte.
set -ev
# shellcheck disable=SC2128
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f ./*.out.*
export ANTLR4_TOOLS_ANTLR_VERSION="4.13.2"
jar="${ANTLR4_JAR:-$HOME/Downloads/antlr-4.13.2-complete.jar}"
echo "eae2dfa119a64327444672aff63e9ec35a20180dc5b8090b7a6ab85125df4d76  $jar" | sha256sum --check --quiet
antlr4="java -jar $jar"

compile() {
  rm -f *.out.go
  $antlr4 -Werror -Dlanguage=Go -visitor -no-listener -package "grammar$1" -o . ClickHouseLexer.g4 "ClickHouseParser$2.g4"
  # Rename only the ANTLR outputs (clickhouse*.go): a bare ./*.go would also
  # sweep hand-written co-located files (package_props.go, ADR-0080) into
  # .out.go and mislabel them as generated.
  for f in ./clickhouse*.go; do
    case "$f" in *.out.go) continue ;; esac
    mv "$f" "${f%.go}.out.go"
  done
}
compile "0" "Grammar0"
cd ../grammar1 || exit 1
compile "1" "Grammar1"
cd ../grammar2 || exit 1
compile "2" "Grammar2"
