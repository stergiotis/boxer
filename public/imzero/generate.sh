#!/bin/bash
set -ev
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

if [ -z "${IMZERO_CPP_BINDING_DIR}" ]; then
   echo "IMZERO_CPP_BINDING_DIR env variable is not set"
   exit 1
fi
if [ -z "${IMZERO_DOXYGEN}" ]; then
   echo "IMZERO_DOXYGEN env variable is not set"
   exit 1
fi

find . -type f -name "*.out.go" -delete
./build.sh
./imgui/doxygen/generate.sh
./implot/doxygen/generate.sh
./imcolortextedit/generate.sh
rm -f "$here/main"

cd "$here"

find . -name "*.go" -perm /200 -not -name "*manual*" -exec gofmt -w {} \;
