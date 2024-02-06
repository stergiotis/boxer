#!/bin/bash
set -e
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

implot_dir="../../../../../contrib/imgui_implot"
package="implot"
dest_dir=".."
xslt_dir="$here/../../imgui/doxygen"

if ! cd "${implot_dir}" ; then
   echo "implot_dir=${implot_dir} not found"    
   exit 1
fi


git reset --hard
git clean -fd

tee config.doxygen <<EOS
EXTRACT_ALL=yes
GENERATE_HTML=no
GENERATE_XML=yes
GENERATE_LATEX=no
GENERATE_SQLITE3=no
HAVE_DOT=no
CALL_GRAPH=no
CALLER_GRAPH=no
DIRECTORY_GRAPH=no
EOS

function comment_preparation() {
  sed -i -e 's,;\([\\t ]*\)//,;\1///<,g' "$1"
  sed -i -e 's.,\([\\t ]*\)//.,\1///<.g' "$1"
  sed -i -e 's,///< \(.*\)$,///<\\verbatim \1\\endverbatim,g' "$1"
  sed -i -e 's,^// \([^[]\),/// \1,g' "$1"
  sed -i -e 's,^//$,///,g' "$1"
}
comment_preparation implot.h
comment_preparation implot_internal.h
"$IMZERO_DOXYGEN" config.doxygen

cd -
cp "$implot_dir/xml/namespaceImPlot.xml" .
cp "$implot_dir/xml/implot_8h.xml" .
cp "$implot_dir/xml/implot__internal_8h.xml" .

#rm -f "$dest_dir/enums_api.out.idl.go"
#rm -f "$dest_dir/enums_internal.out.idl.go"
#rm -f "$dest_dir/functions_auto_api.out.idl.go"
#rm -f "$dest_dir/functions_auto_internal.out.idl.go"

enum_blacklist='ImPlotMarker'

goImports=$'import . "github.com/stergiotis/boxer/public/imzero/imgui"\nvar _ = ImVec2(0)\n'
xsltproc --stringparam apidefine "IMPLOT_API" "$xslt_dir/transform.xslt" namespaceImPlot.xml | xsltproc "$xslt_dir/semantics.xslt" - > namespaceImPlot2.xml
xsltproc --stringparam blacklist "$enum_blacklist" --stringparam autoValueValue -1 --stringparam autoValueNameSuffix "_AUTO" --stringparam package $package --stringparam goImports "$goImports" --stringparam tags fffi_idl_code "$xslt_dir/enums.xslt" implot_8h.xml > enums_api.out.go
xsltproc --stringparam blacklist "$enum_blacklist" --stringparam autoValueValue -1 --stringparam autoValueNameSuffix "_AUTO" --stringparam package $package --stringparam goImports "$goImports" --stringparam tags fffi_idl_code "$xslt_dir/enums.xslt" implot__internal_8h.xml > enums_internal.out.go
xsltproc --stringparam package $package --stringparam goImports "$goImports" --stringparam tags fffi_idl_code          --stringparam mode auto "$xslt_dir/functions.xslt" namespaceImPlot2.xml > functions_auto_api.out.idl.go
xsltproc --stringparam package $package --stringparam goImports "$goImports" --stringparam tags "fffi_idl_code && disabled" --stringparam mode manual "$xslt_dir/functions.xslt" namespaceImPlot2.xml > functions_manual_api.out.idl.go
xsltproc --stringparam package $package --stringparam goImports "$goImports" --stringparam tags fffi_idl_code          --stringparam mode auto "$xslt_dir/functions.xslt" namespaceImPlot2.xml > functions_auto_internal.out.idl.go
xsltproc --stringparam package $package --stringparam goImports "$goImports" --stringparam tags "fffi_idl_code && disabled" --stringparam mode manual "$xslt_dir/functions.xslt" namespaceImPlot2.xml > functions_manual_internal.out.idl.go

# resolve naming conflincts (overloads)
function renameFunction() {
	sed -i "s#$2#$3#g" "$1"
}
renameFunction functions_auto_api.out.idl.go "ItemIcon(col uint32" "ItemIconUint32(col uint32"
renameFunction functions_auto_api.out.idl.go "PixelsToPlot(pix ImVec2" "PixelsToPlotImVec2(pix ImVec2"
renameFunction functions_auto_api.out.idl.go "PlotToPixels(plt ImPlotPoint" "PlotToPixelsImPlotPoint(plt ImPlotPoint"
renameFunction functions_auto_api.out.idl.go "PushStyleColor(idx ImPlotCol,col ImVec4)" "PushStyleColorImVec4(idx ImPlotCol,col ImVec4)"
renameFunction functions_auto_api.out.idl.go "PushStyleVar(idx ImPlotStyleVar,val int)" "PushStyleVarInt(idx ImPlotStyleVar,val int)"
renameFunction functions_auto_api.out.idl.go "PushStyleVar(idx ImPlotStyleVar,val ImVec2)" "PushStyleVarImVec2(idx ImPlotStyleVar,val ImVec2)"
renameFunction functions_auto_api.out.idl.go "PushColormap(colormap ImPlotColormap)" "PushColormapById(colormap ImPlotColormap)"

function count_functions() {
        echo -n "$1: " >> stat.txt
        cat "$1" | grep "func (inst " | wc -l >> stat.txt
}
function count_functions_header() {
        echo -n "$1 ($2): " >> stat.txt
        cat "$1" | grep "$2" | wc -l >> stat.txt
}
rm -f stat.txt
count_functions "functions_auto_api.out.idl.go"
count_functions "functions_auto_internal.out.idl.go"
count_functions "functions_manual_api.out.idl.go"
count_functions "functions_manual_internal.out.idl.go"
count_functions_header "$implot_dir/implot.h" "IMPLOT_API"
count_functions_header "$implot_dir/implot.h" "IMPLOT_TMP"

cp functions_auto_api.out.idl.go "$dest_dir/"
#cp functions_auto_internal.out.idl.go "$dest_dir/"
cp enums_api.out.go "$dest_dir/"
#cp enums_internal.out.go "$dest_dir/"
rm -f enums_internal.out.go
"$dest_dir/generate.sh"
