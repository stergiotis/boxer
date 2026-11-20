#!/bin/bash
# Compares the tree's release contracts against the committed baseline
# snapshot (ADR-0388): DDL of production tables, membership assignments, the
# leeway SQL surface, environment variables, the IDL fingerprint, wire and
# format versions, and the command tree.
#
# Fails on a non-additive difference that no note under doc/migration names
# on a `contract: <kind>/<item>` line. contracts/BASELINE names the snapshot
# directory, which is named for the commit it was taken at; re-baselining
# (after a release) is
#
#   sha=$(git rev-parse --short=9 HEAD)
#   ./boxer.sh gov contracts snapshot --out "contracts/$sha"
#   printf '%s\n' "$sha" > contracts/BASELINE
#
# and removing the previous snapshot directory.
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
baseline="contracts/$(tr -d '[:space:]' < contracts/BASELINE)"
./boxer.sh gov contracts diff "$baseline" "$@"
