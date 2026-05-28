// Package codecdemo demonstrates the keelsoncodec --target=anchor
// emit mode: the same generator that produces the full facts bus-
// codec stack also produces a schema-agnostic codec for anchor's
// leeway DML / RA, with Go's type inference binding the schema at
// the call site.
//
// dronemission.out.go is regenerated from dronemission.go by:
//
//	go run -tags "$(cat tags)" ./cmd/keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/dronemission.go
//
// The generated output carries no facts-platform imports (no
// factsschema/dml_cbor, no buscodec.CodecI bridge, no cborarrow
// wiring) — just SoA columns, the F-bounded generic
// DroneMissionBuildEntities[…] / DroneMissionFillFromArrow[…]
// helpers, and the per-section / per-membership interface
// declarations they need.
package codecdemo

// DroneMission is a tiny demo DTO mirroring the lw: tag convention
// used elsewhere in the codec pipeline. Two tagged sections (symbol +
// u64Array) plus the plain `id` and optional `naturalKey` plain
// columns — enough to exercise both scalar (BeginAttribute(value))
// and non-scalar (BeginAttributeSingle / AddToContainer) write paths.
//
// The DTO targets anchor's `symbol` section (scalar) and `u64Array`
// section (non-scalar) — both already declared in
// `card_anchor_schema.go`'s LoadExampleSchema.
type DroneMission struct {
	_ struct{} `kind:"droneMission"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	// Status maps to anchor's symbol section (scalar string value).
	Status string `lw:"droneStatus,symbol"`

	// Battery maps to anchor's u64Array section (non-scalar uint64).
	// Single-value attribute via BeginAttributeSingle on the write
	// path; single-value read via GetAttrValueSingleOrDefault.
	Battery uint64 `lw:"battery,u64Array,unit"`
}
