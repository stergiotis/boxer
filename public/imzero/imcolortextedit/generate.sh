#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

cd ".."
outfile="./imcolortextedit/api.out.go"
rm -f "$outfile"

mkdir -p "$IMZERO_CPP_BINDING_DIR/imcolortextedit"
./build.sh
./main generateFffiCode --idlBuildTag fffi_idl_code \
	                --idlPackagePattern github.com/stergiotis/boxer/public/imzero/imcolortextedit \
	                --goOutputFile "$outfile" \
	                --interfaceOutputFile "imcolortextedit.interface.out.cbor" \
	                --funcProcIdOffset 3000 \
			            --goCodeProlog $'import "github.com/stergiotis/boxer/public/imzero/imgui"\n' \
			            --cppOutputFile "$IMZERO_CPP_BINDING_DIR/imcolortextedit/dispatch.h" 2>&1 | ./main cbor diag
