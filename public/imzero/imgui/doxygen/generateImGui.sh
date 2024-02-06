#!/bin/bash
imgui_dir="../../../../../contrib/imgui"
package="imgui"
dest_dir=".."

rm -f enums_api.out.go
rm -f enums_internal.out.go
rm -f functions_auto_api.out.idl.go
rm -f functions_auto_internal.out.idl.go
rm -f functions_manual_api.out.idl.go
rm -f functions_manual_internal.out.idl.go
if ! cd "${imgui_dir}" ; then
   echo "imgui_dir=${imgui_dir} not found"    
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

function imgui_comment_preparation() {
  sed -i -e 's,;\([\\t ]*\)//,;\1///<,g' "$1"
  sed -i -e 's,///< \(.*\)$,///<\\verbatim \1\\endverbatim,g' "$1"
}
imgui_comment_preparation imgui.h
imgui_comment_preparation imgui_internal.h
$doxygen config.doxygen

cd -
cp "$imgui_dir/xml/namespaceImGui.xml" .
cp "$imgui_dir/xml/imgui_8h.xml" .
cp "$imgui_dir/xml/imgui__internal_8h.xml" .
cp "$imgui_dir/xml/structImDrawList.xml" .

rm -f "$dest_dir/enums_api.out.idl.go"
rm -f "$dest_dir/enums_internal.out.idl.go"
rm -f "$dest_dir/functions_auto_api.out.idl.go"
rm -f "$dest_dir/functions_auto_internal.out.idl.go"
xsltproc --stringparam apidefine "IMGUI_API" transform.xslt namespaceImGui.xml | xsltproc semantics.xslt - > namespaceImGui2.xml
xsltproc --stringparam package $package enums.xslt imgui_8h.xml > enums_api.out.go
xsltproc --stringparam package $package enums.xslt imgui__internal_8h.xml > enums_internal.out.go
xsltproc --stringparam file imgui.h --stringparam package $package --stringparam tags fffi_idl_code          --stringparam mode auto functions.xslt namespaceImGui2.xml > functions_auto_api.out.idl.go
xsltproc --stringparam file imgui.h --stringparam package $package --stringparam tags "fffi_idl_code && disabled" --stringparam mode manual functions.xslt namespaceImGui2.xml > functions_manual_api.out.idl.go
xsltproc --stringparam file imgui_internal.h --stringparam package $package --stringparam tags fffi_idl_code          --stringparam mode auto functions.xslt namespaceImGui2.xml > functions_auto_internal.out.idl.go
xsltproc --stringparam file imgui_internal.h --stringparam package $package --stringparam tags "fffi_idl_code && disabled" --stringparam mode manual functions.xslt namespaceImGui2.xml > functions_manual_internal.out.idl.go

xsltproc --stringparam apidefine "IMGUI_API" transform.xslt structImDrawList.xml | xsltproc semantics.xslt - > structImDrawList2.xml
xsltproc --stringparam file imgui.h --stringparam package $package --stringparam instvar "foreignptr ImDrawListPtr" --stringparam tags fffi_idl_code      --stringparam mode auto functions.xslt structImDrawList2.xml > drawlist_auto_api.out.idl.go


# resolve naming conflicts (overloads)
function renameFunction() {
        sed -i "s#$2#$3#g" "$1"
}

sed -i "s.BeginChild(id ImGuiID.BeginChildID(id ImGuiID.g" functions_auto_api.out.idl.go
sed -i "s.BeginChildV(id ImGuiID.BeginChildVID(id ImGuiID.g" functions_auto_api.out.idl.go
sed -i "s.PushID(int_id int.PushIDInt(int_id int.g" functions_auto_api.out.idl.go
sed -i "s.PushStyleColor(idx ImGuiCol,col ImVec4).PushStyleColorImVec4(idx ImGuiCol,col ImVec4).g" functions_auto_api.out.idl.go
sed -i "s.PushStyleVar(idx ImGuiStyleVar,val ImVec2).PushStyleVarImVec2(idx ImGuiStyleVar,val ImVec2).g" functions_auto_api.out.idl.go
sed -i "s.GetColorU32(col ImVec4).GetColorU32ImVec4(col ImVec4).g" functions_auto_api.out.idl.go
sed -i "s.GetColorU32(idx ImGuiCol).GetColorU32ImGuiCol(idx ImGuiCol).g" functions_auto_api.out.idl.go
sed -i "s.OpenPopup(id ImGuiID.OpenPopupID(id ImGuiID.g" functions_auto_api.out.idl.go
sed -i "s.OpenPopupV(id ImGuiID.OpenPopupVID(id ImGuiID.g" functions_auto_api.out.idl.go
sed -i "s.IsRectVisible(rect_min ImVec2,rect_max ImVec2).IsRectVisible2(rect_min ImVec2,rect_max ImVec2).g" functions_auto_api.out.idl.go
sed -i "s.ImageButtonV(user_texture_id ImTextureID.ImageButtonVOld(user_texture_id ImTextureID.g" functions_auto_api.out.idl.go
sed -i "s.ImageButton(user_texture_id ImTextureID.ImageButtonOld(user_texture_id ImTextureID.g" functions_auto_api.out.idl.go

function count_functions() {
	echo -n "$1: " >> stat.txt
	cat "$1" | grep "func (inst " | wc -l >> stat.txt
}
rm -f stat.txt
count_functions "functions_auto_api.out.idl.go"
count_functions "functions_auto_internal.out.idl.go"
count_functions "functions_manual_api.out.idl.go"
count_functions "functions_manual_internal.out.idl.go"

cp functions_auto_api.out.idl.go "$dest_dir/"
#cp functions_auto_internal.out.idl.go "$dest_dir/"
cp enums_api.out.go "$dest_dir/"
#cp enums_internal.out.go "$dest_dir/"
rm -f enums_internal.out.go
cp drawlist_auto_api.out.idl.go "$dest_dir/"
