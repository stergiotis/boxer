#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
# Generate to a temp file first, then move into place only on success.
# `leeway ct abbrevs --astPackage canonicaltypes` loads the canonicaltypes
# package tree; redirecting `>` straight into canonicaltypes_abbrevs.out.go
# truncates it before the load runs, which makes the package unparseable
# ("expected 'package', found 'EOF'") and the generator emits nothing. The
# temp+mv also means a failed run can never clobber the committed file.
out=$(mktemp)
trap 'rm -f -- "$out"' EXIT
../../../../../boxer.sh leeway ct abbrevs \
	--packageName ctabb \
	--import github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes \
	--astPackage canonicaltypes > "$out"
gofmt -w "$out"
mv -- "$out" "$here/canonicaltypes_abbrevs.out.go"
