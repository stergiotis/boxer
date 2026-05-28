#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here" || exit 1
../../../../../../build.sh
pebble="../../../../../../pebble_built.sh"
mkdir -p "facts"
$pebble leeway ddl table mappings informationschema facts --format cbor | $pebble leeway dml table generate go --packageName "facts" --tableName "facts" > facts/lw_info_facts.out.go
mkdir -p "vcsmanaged"
$pebble leeway ddl table mappings informationschema vcsmanaged --format cbor | $pebble leeway dml table generate go --packageName "vcsmanaged" --tableName "vcsmanaged" > vcsmanaged/lw_info_vcsmanaged.out.go
