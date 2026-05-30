#!/bin/bash
# Regenerate every derived source tree in boxer from its generator of record.
#
# Ported from ../pebble2impl/generate.sh, remapped to boxer's layout:
#
#   pebble2impl                          boxer
#   --------------------------------------------------------------------
#   src/go/public/...                    public/...
#   src/rust/src/imzero2/                rust/imzero2/src/imzero2/
#   ./egui2gen.sh generate ...           ./boxer egui2gen generate ...   (folded into public/app)
#   go run ./src/go/cmd/<gen> ...        ./boxer <gen> ...               (folded into public/app)
#
# Dropped — no targets in boxer (boxerstaging was not migrated here). Re-add
# when/if boxerstaging lands:
#   - keelsoncodec --target=anchor   boxerstaging/leeway/anchor/codecdemo
#   - boxerstaging/spinnaker/generate.sh
#   - boxerstaging/leeway/schema/generate.sh
#   - boxerstaging/leeway/anchor   test-driven DDL/DML/read-access regen
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."

tags=$(cat tags | tr -d $'\n')

# Build the boxer binary once; every Go generator below is a subcommand of it
# (build-once / run-many, mirroring the retired egui2gen.sh launcher).
appfile=$(mktemp)
cleanup() {
    rv=$?
    rm -f -- "$appfile"
    exit $rv
}
trap cleanup EXIT
echo "generate: building ./public/app …" 1>&2
go build -buildvcs=true -tags "$tags" -o "$appfile" ./public/app 1>&2
boxer() { "$appfile" --logFormat console "$@"; }

# egui2gen — FFFI2 Rust + Go bindings + API reference doc.
boxer egui2gen generate rust --rustOutputBasePath=./rust/imzero2/src/imzero2/
boxer egui2gen generate go   --goOutputBasePath=./public/thestack/imzero2/egui2/bindings
boxer egui2gen generate doc  --docOutputPath=./doc/skills/imzero2/assets/egui2_api_reference.md

# Keelson codec generator (ADR-0042). For each DTO under
# public/keelson/runtime/codec/<kind>/<kind>.go produce a sibling
# <kind>.out.go with the SoA Columns + Append + Marshal + ColumnList +
# ChlocalStructure surface.
boxer keelsoncodec \
    public/keelson/runtime/codec/m1fixture/fixture.go \
    public/keelson/runtime/codec/capabilitygrant/capabilitygrant.go \
    public/keelson/runtime/codec/errkind/errkind.go \
    public/keelson/runtime/codec/taskprogress/taskprogress.go \
    public/keelson/runtime/codec/taskcreated/taskcreated.go \
    public/keelson/runtime/codec/taskcancel/taskcancel.go \
    public/keelson/runtime/codec/taskerror/taskerror.go \
    public/keelson/runtime/codec/taskdone/taskdone.go \
    public/keelson/runtime/codec/grantrequest/grantrequest.go \
    public/keelson/runtime/codec/grantreply/grantreply.go \
    public/keelson/runtime/codec/dialogreply/dialogreply.go \
    public/keelson/runtime/codec/watchrequest/watchrequest.go \
    public/keelson/runtime/codec/watchreply/watchreply.go \
    public/keelson/runtime/codec/watchevent/watchevent.go \
    public/keelson/runtime/codec/persistreply/persistreply.go \
    public/keelson/runtime/codec/inflightsnapshotreply/inflightsnapshotreply.go

# runtime/factsschema codegen (ADR-0026). Emits DDL / DML (plain, rowbinary,
# cbor, sparserb) / read-access wrappers for the runtime facts schema.
boxer runtimecodegen all

# Phosphor icon catalogue (ADR-0044 §SD3). SHA-verifies the vendored
# phosphor-icons.json and emits phosphor{,_lookup,_affordances}.out.go.
./public/keelson/runtime/icons/generate.sh

# Sweep //go:generate directives: envgen (doc/env-vars.md) and the
# leewaywidgets fixture_dml.out.go regen test. Tags pass-through so the
# build-tagged directives stay visible.
go generate -tags "$tags" ./...

echo "generate: done" 1>&2
