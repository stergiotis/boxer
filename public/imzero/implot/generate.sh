#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

cd ".."
outfile="./implot/api.out.go"
rm -f main
rm -f "$outfile"
go build -tags binary_log,bootstrap main.go

function applyTemplates() {
  export plotName=$1
  export typeName=$2
  export typeGo=$3
  cat "$here/plot${plotName}.idl.go.tmpl" | envsubst > "$here/plot${plotName}.${typeName}.out.idl.go"
}
function applyTemplates2() {
  applyTemplates $1 Float32 float32
  applyTemplates $1 Float64 float64
  applyTemplates $1 Int int
  applyTemplates $1 Int8 int8
  applyTemplates $1 Int16 int16
  applyTemplates $1 Int32 int32
  applyTemplates $1 UInt uint
  applyTemplates $1 UInt8 uint8
  applyTemplates $1 UInt16 uint16
  applyTemplates $1 UInt32 uint32
}
applyTemplates2 "line"
applyTemplates2 "scatter"
applyTemplates2 "stairs"
applyTemplates2 "shaded"
applyTemplates2 "bars"
applyTemplates2 "bargroups"
applyTemplates2 "errorbar"
applyTemplates2 "stems"
applyTemplates2 "inflines"
applyTemplates2 "piechart"
applyTemplates2 "heatmap"
applyTemplates2 "histogram"
applyTemplates2 "histogram2d"
applyTemplates2 "digital"

mkdir -p "$IMZERO_CPP_BINDING_DIR/implot"
./build.sh
./main generateFffiCode --idlBuildTag fffi_idl_code \
	                --idlPackagePattern github.com/stergiotis/boxer/public/imzero/implot \
	                --goOutputFile "$outfile" \
	                --funcProcIdOffset 2000 \
			--goCodeProlog $'import "github.com/stergiotis/boxer/public/imzero/imgui"\n' \
			--cppOutputFile "$IMZERO_CPP_BINDING_DIR/implot/dispatch.h" 2>&1 | ./main cbor diag
sed -i "s/ ImVec2/ imgui.ImVec2/g" "$outfile"
sed -i "s/ ImVec4/ imgui.ImVec4/g" "$outfile"
sed -i "s/ ImRect/ imgui.ImRect/g" "$outfile"
sed -i "s/ ImTextureID/ imgui.ImTextureID/g" "$outfile"
sed -i "s/ ImGui/ imgui.ImGui/g" "$outfile"
sed -i "s/ImGuiCond_/imgui.ImGuiCond_/g" "$outfile"
