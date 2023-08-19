#!/bin/bash
set -e
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f encoding
rm -f decoding
rm -f encodingTable.go
rm -f decodingTable.go
gcc -o encoding encoding.c
gcc -o decoding decoding.c
./decoding > decodingTable.go
./encoding > encodingTable.go
rm -f encoding
rm -f decoding
mv -v *.go ../../public/fec/code/golay24/
