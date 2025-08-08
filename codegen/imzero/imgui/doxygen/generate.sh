#!/bin/bash
set -e
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"

"$here/generateImGui.sh"
"$here/generateSpinner.sh"

"$here/../generate.sh"
