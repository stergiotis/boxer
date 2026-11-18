#!/bin/bash
# Regenerate the Golay(24,12) lookup tables in public/fec/code/golay24.
# The C programs print Go source; gofmt brings it to the checked-in form.
set -e
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
out="$here/../../public/fec/code/golay24"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
gcc -o "$work/encoding" "$here/encoding.c"
gcc -o "$work/decoding" "$here/decoding.c"
"$work/encoding" | gofmt > "$out/encoding_table.go"
"$work/decoding" | gofmt > "$out/decoding_table.go"
