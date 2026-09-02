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
#   - boxerstaging/spinnaker/generate.sh
#   - boxerstaging/leeway/schema/generate.sh
#
# Everything else regenerates from here. Generators reached by an explicit
# invocation below are the ones taking a repo-wide argument list; anything whose
# regeneration is package-local carries a `//go:generate` directive next to the
# artifact and is swept by the `go generate ./...` at the end. That includes the
# anchor codecdemo codecs (`keelsoncodec --target=anchor`), the test-driven
# recordstore / ecsdemo-stage2 store regenerations, and the two apps codec
# goldens. Those four families used to be outside this script, which is how they
# drifted from their emitters unnoticed — see ADR-0146's Scope note.
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
# shellcheck source=/dev/null
source "$(dirname "$(readlink -f "$BASH_SOURCE")")/go-build-env.sh"
# shellcheck disable=SC2086 # deliberate word splitting of the flag list
go build $BOXER_GO_FLAGS -tags "$tags" -o "$appfile" ./public/app 1>&2
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
    public/keelson/runtime/codec/inflightsnapshotreply/inflightsnapshotreply.go \
    public/keelson/runtime/codec/launchrequest/launchrequest.go \
    public/keelson/runtime/codec/launchreply/launchreply.go

# runtime/factsschema codegen (ADR-0026). Emits DDL / DML (Arrow and sparse
# CBOR) / read-access wrappers for the runtime facts schema. Driven from here
# rather than a //go:generate directive, so a change to the leeway aspect
# vocabularies that regenerates the gen-test-driven artifacts does NOT reach
# these four — run this script, not `go generate ./...` alone.
boxer runtimecodegen all

# Phosphor icon catalogue (ADR-0044 §SD3). SHA-verifies the vendored
# phosphor-icons.json and emits phosphor{,_lookup,_affordances}.out.go.
./public/keelson/runtime/icons/generate.sh

# Sweep //go:generate directives: envgen (doc/env-vars.md) and the
# leewaywidgets fixture_dml.out.go regen test. Tags pass-through so the
# build-tagged directives stay visible.
go generate -tags "$tags" ./...

echo "generate: done" 1>&2
