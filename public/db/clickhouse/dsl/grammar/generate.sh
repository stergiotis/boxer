#!/bin/bash
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
antlr4 -Dlanguage=Go -visitor -no-listener -package grammar -o . *.g4
rename "s/\.go/.out.go/" *.go
rename "s/\.interp/.out.interp/" *.interp
rename "s/\.tokens/.out.tokens/" *.tokens
