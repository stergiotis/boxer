#!/bin/bash
set -e
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

export doxygen="$HOME/Downloads/doxygen-1.10.0/bin/doxygen"

"$here/generateImGui.sh"
"$here/generateSpinner.sh"

"$here/../generate.sh"
