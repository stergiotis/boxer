#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "${BASH_SOURCE}")")
cd "$here"
rm -rf ./export
mkdir ./export
"$here/../../boxer.sh" code analysis golang stub --excludePathRegex "doxygen" --outputBaseDir ./export/ --dir ../../
"$here/../../boxer.sh" code analysis golang prompt --inputDir ./export/public/semistructured/leeway > ./export/public/semistructured/leeway/prompt.md
