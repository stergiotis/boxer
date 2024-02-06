#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
rm -f slider.*.out.idl.go
rm -f drag.*.out.idl.go
function applyTemplates() {
  export widget=$1
  export typeName=$2
  export typeGo=$3
  export typeImGui=$4
  cat "$here/${widget,,}.idl.go.tmpl" | envsubst > "$here/${widget,,}.${typeName}.out.idl.go"
}
function applyTemplates2() {
  applyTemplates $1 Float32 float32 ImGuiDataType_Float
  applyTemplates $1 Float64 float64 ImGuiDataType_Double
  applyTemplates $1 Int int ImGuiDataType_S32
  applyTemplates $1 Int8 int8 ImGuiDataType_S8
  applyTemplates $1 Int16 int16 ImGuiDataType_S16
  applyTemplates $1 Int32 int32 ImGuiDataType_S32
  applyTemplates $1 UInt uint ImGuiDataType_U32
  applyTemplates $1 UInt8 uint8 ImGuiDataType_U8
  applyTemplates $1 UInt16 uint16 ImGuiDataType_U16
  applyTemplates $1 UInt32 uint32 ImGuiDataType_U32
}
applyTemplates2 "Slider"
applyTemplates2 "Drag"

cd ".."
outfile="./imgui/api.out.go"
rm -f main
rm -f "$outfile"
go build -tags binary_log,bootstrap main.go
mkdir -p "$IMZERO_CPP_BINDING_DIR/imgui"
./main generateFffiCode --idlBuildTag fffi_idl_code \
	                --idlPackagePattern github.com/stergiotis/boxer/public/imzero/imgui \
	                --goOutputFile "$outfile" \
			--runeCppType "ImWchar" \
			--cppOutputFile "$IMZERO_CPP_BINDING_DIR/imgui/dispatch.h" 2>&1 | cbor-diag
