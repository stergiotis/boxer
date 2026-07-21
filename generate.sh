#!/bin/bash
# Regenerate every checked-in code-generation artefact in boxer.
#
# Recreated from ../pebble2impl/generate.sh after the keelson / imzero2 /
# egui2 / fffi2 import. Two adaptations vs. the pebble2impl original:
#   - flatter layout: src/go/public/… -> public/…, src/rust/… -> rust/…,
#     boxerstaging/leeway/… -> semistructured/leeway/…
#   - the standalone cmd/{egui2gen,keelsoncodec,iconsgen,runtimecodegen}
#     mains are now ./public/app subcommands (entry-point standard), so
#     `go run ./cmd/X` and `./egui2gen.sh` become `app X …`.
#
# Omitted vs. pebble2impl: spinnaker codegen (that package is not imported
# into boxer) and the antlr canonicaltypes/grammar regen (needs a
# hand-provisioned antlr jar; run it manually when the .g4 grammar changes).
set -ev
here=$(dirname "$(readlink -f "${BASH_SOURCE}")")
cd "$here"

tags=$(cat tags | tr -d $'\n')

# Build the aggregated app once and reuse it for the subcommand-backed steps
# below (egui2gen / keelsoncodec / runtimecodegen). The per-directory
# generate.sh scripts invoked further down build their own binary.
app=$(mktemp)
trap 'rm -f -- "$app"' EXIT
go build -buildvcs=true -tags "$tags" -o "$app" ./public/app
run() { "$app" --logFormat console "$@"; }

# 1. egui2gen — FFFI2 Rust + Go bindings + API reference doc.
run egui2gen generate rust --rustOutputBasePath=./rust/imzero2/src/imzero2/
run egui2gen generate go   --goOutputBasePath=./public/thestack/imzero2/egui2/bindings
run egui2gen generate doc  --docOutputPath=./doc/skills/imzero2/assets/egui2_api_reference.md

# 2. keelson codec generator (ADR-0042). For each DTO source emit a sibling
#    <name>.out.go with the SoA Columns + Append + Marshal + ColumnList +
#    ChlocalStructure surface. (factswrapper is the wrapper library, not a DTO.)
run keelsoncodec \
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

# 3. Anchor codecdemo — same generator with --target=anchor. Emits the
#    schema-agnostic surface only (no facts wrapper), proving the keelsoncodec
#    output is portable across leeway schemas.
run keelsoncodec --target=anchor \
    public/semistructured/leeway/anchor/codecdemo/dronemission.go \
    public/semistructured/leeway/anchor/codecdemo/sensorreading.go

# 4. runtime/factsschema codegen (ADR-0026). DDL / DML (plain, cbor) /
#    read-access wrappers for the runtime facts schema.
run runtimecodegen all

# 5. Phosphor icon catalogue (ADR-0044 §SD3). SHA-verifies the vendored
#    phosphor-icons.json and emits phosphor{,_lookup}.out.go.
./public/keelson/runtime/icons/generate.sh

# 6. Leeway information_schema codegen — test-driven regen of the facts /
#    vcsmanaged DML wrappers (mirrors the anchor step below). Reconstructs the
#    pebble2impl `ddl table mappings informationschema <name> --format cbor |
#    dml table generate go` pipeline, whose CBOR producer subcommand was not
#    ported to boxer; the gen tests feed mapping.NewInformationSchema*Mapping()
#    straight into the dml driver.
go test -tags "$tags" \
    -run '^(TestFactsInfoSchemaDmlGeneration|TestVcsManagedInfoSchemaDmlGeneration)$' \
    ./public/semistructured/leeway/schema/...

# 7. canonicaltypes abbreviations table.
./public/semistructured/leeway/canonicaltypes/ctabb/generate.sh

# 8. Leeway anchor table — test-driven regen of DDL / DML / read-access
#    (.out.go + .out.sql). Filtered to the three generator tests so the
#    package's CH-integration tests don't fire here.
go test -tags "$tags" \
    -run '^(TestReadAccessGoClassBuilderGeneration|TestDmlGeneration|TestDdlClickHouseGeneration)$' \
    ./public/semistructured/leeway/anchor/

# 9. Sweep //go:generate directives: envgen (doc/env-vars.md), the dsl
#    builder-test generator, and the leewaywidgets fixture_dml regen test.
go generate -tags "$tags" ./...
