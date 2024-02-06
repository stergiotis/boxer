#!/bin/bash
set -e
spinner_dir="../../../../../contrib/imgui_imspinner"
package="imgui"
dest_dir=".."

rm -f spinner.out.idl.go
if ! cd "${spinner_dir}" ; then
   echo "spinner_dir=${spinner_dir} not found"
   exit 1
fi
pwd

git reset --hard
git clean -fd

tee config.doxygen <<EOS
EXTRACT_ALL=yes
GENERATE_XML=yes
GENERATE_HTML=no
GENERATE_LATEX=no
GENERATE_SQLITE3=no
HAVE_DOT=no
CALL_GRAPH=no
CALLER_GRAPH=no
DIRECTORY_GRAPH=no
EOS

echo "namespace ImSpinner {" > out.h
cat imspinner.h | grep "inline void Spinner" | sed 's/$/;/g' | sed 's/inline void/IMGUI_API void/g' | tr -d '{' >> out.h
echo "}" >> out.h

"$IMZERO_DOXYGEN" config.doxygen

cd -
cp "$spinner_dir/xml/namespaceImSpinner.xml" .

rm -f "$dest_dir/spinner.out.idl.go"
xsltproc --stringparam apidefine "IMGUI_API" transform.xslt namespaceImSpinner.xml | xsltproc semantics.xslt - > namespaceImSpinner2.xml
xsltproc --stringparam file imspinner.h --stringparam package $package --stringparam tags fffi_idl_code --stringparam mode auto functions.xslt namespaceImSpinner2.xml > spinner.out.idl.go
#xsltproc --stringparam file imspinner.h --stringparam package $package --stringparam tags fffi_idl_code --stringparam mode manual functions.xslt namespaceImSpinner2.xml > spinner_manual.out.idl.go

cp spinner.out.idl.go "$dest_dir/"
#cp spinner_manual.out.idl.go "$dest_dir/"
