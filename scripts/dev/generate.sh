#!/bin/bash
# Regenerate every derived source tree in boxer from its generator of record.
#
# Ported from the upstream repository's generate.sh, remapped to boxer's layout:
#
#   upstream                             boxer
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
# This is the one generate script; the generate.sh at the repository root
# execs it. Everything else regenerates from here. Generators reached by an explicit
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

# Keelson codec generator (ADR-0042). Every DTO under
# public/keelson/runtime/codec/<kind>/ is the source of a sibling <name>.out.go
# with the SoA Columns + Append + Marshal + ColumnList + ChlocalStructure
# surface, so the list is discovered from the .out.go files rather than kept by
# hand: a new codec package is regenerated as soon as its first output is
# committed. (factswrapper and kindcheck are libraries and have none.) Each
# package's TestGeneratorMatchesCheckedInOutput holds the same pair.
mapfile -t codec_sources < <(
    for out in public/keelson/runtime/codec/*/*.out.go; do
        printf '%s\n' "${out%.out.go}.go"
    done | LC_ALL=C sort
)
boxer keelsoncodec "${codec_sources[@]}"

# runtime/factsschema codegen (ADR-0026). Emits DDL / DML (Arrow and sparse
# CBOR) / read-access wrappers for the runtime facts schema. Driven from here
# rather than a //go:generate directive, so a change to the leeway aspect
# vocabularies that regenerates the gen-test-driven artifacts does NOT reach
# these four — run this script, not `go generate ./...` alone.
boxer runtimecodegen all

# Phosphor icon catalogue (ADR-0044 §SD3). SHA-verifies the vendored
# phosphor-icons.json and emits phosphor{,_lookup,_affordances}.out.go.
./public/keelson/runtime/icons/generate.sh

# canonicaltypes abbreviations table.
./public/semistructured/leeway/canonicaltypes/ctabb/generate.sh

# Leeway information_schema DML goldens — the facts / vcsmanaged mappings run
# through the dml driver, kept as testdata goldens (nothing in this module
# imports the generated classes).
go test -tags "$tags" -count=1 \
    -run '^TestInformationSchemaDmlMatchesGolden$' \
    ./public/semistructured/leeway/dml/ -update

# Leeway anchor table — test-driven regeneration of DDL / DML / read-access
# (.out.go + .out.sql). Filtered to the three generator tests so the package's
# ClickHouse tests do not run here.
go test -tags "$tags" -count=1 \
    -run '^(TestReadAccessGoClassBuilderGeneration|TestDmlGeneration|TestDdlClickHouseGeneration)$' \
    ./public/semistructured/leeway/anchor/

# Sweep //go:generate directives: envgen (doc/env-vars.md) and the
# leewaywidgets fixture_dml.out.go regen test. Tags pass-through so the
# build-tagged directives stay visible.
go generate -tags "$tags" ./...

echo "generate: done" 1>&2
