#!/bin/bash
set -ev
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
find . -type f -name "*.out.go" -delete
./build.sh
./imgui/doxygen/generate.sh
./implot/doxygen/generate.sh
./imcolortextedit/generate.sh
rm -f "$here/main"
