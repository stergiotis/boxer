#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
../../../../../boxer.sh leeway ct abbrevs --packageName "ctabb" --import "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes" --astPackage="canonicaltypes" > "$here/canonicalTypes_abbrevs.out.go"
gofmt -w ./*.go
