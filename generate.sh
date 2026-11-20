#!/bin/bash
# Kept so `./generate.sh` still works from the repository root; the script of
# record is scripts/dev/generate.sh.
exec "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/scripts/dev/generate.sh" "$@"
