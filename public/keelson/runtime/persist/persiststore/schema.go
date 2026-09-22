// Package persiststore is the generated record store behind every kind of
// durable app state (ADR-0105 D3a, and its Update of 2026-08-15 extending
// D3a to every state-shaped kind). State lives on its own store-owned table
// rather than on `boxer.facts`: the state verbs want a latest-wins view over
// a mutable key, which the generated state view gives directly and the
// append-only facts table only gives through hand-written `argMax` SQL — the
// code class ADR-0105 exists to delete. The rule the two tables now split
// on is *trail on `boxer.facts`, state on `boxer.persiststate`*.
//
// Three kinds share the table, each a component over the same small set of
// typed sections: persist state (State), workingsets (Workingset) and
// table column-width overrides (ColumnWidth). A kind is which component a
// row carries — no kind membership is written — and every live row also
// carries Owner, the app it belongs to and the process and window that
// wrote it. Adding a kind is a DTO plus vocabulary entries, never a schema
// change.
//
// One row per write, keyed by kind (keys.go): the newest row for a key
// wins, and a Delete appends a tombstone that reads as absent. The previous
// value stays queryable, so the row trail is still the history of that key.
//
// The table is this store's own: EnsureTable provisions it. That is the
// difference from a facts-bound store (ADR-0184 SD2), where `chstore` is
// the sole DDL author and the verb must be suppressed.
package persiststore

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// DatabaseName / TableName are the CH coordinates. The database matches
// factsschema's, so persist state sits beside the facts table it left
// rather than in a database of its own — D3a moves the substrate, not the
// deployment. ADR-0105 D3a wrote this table "runtime.persiststate"; the
// runtime database was since renamed to "boxer".
const (
	DatabaseName = "boxer"
	TableName    = "persiststate"
)

// TableRowConfig matches pushoutstore's: multiple attributes per row.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// GetPersistSchemaInManipulator builds the state table. Envelope roles: id
// (string Key — see keys.go), ts (Order — the write time), lifecycle
// (Lifecycle — the Delete tombstone, which is what makes the state view
// emit at all).
func GetPersistSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableName)
	manip.SetTableComment("app state of every kind, keyed <kind>/<appId>/... (ADR-0105 D3a, Update 2026-08-15)")
	loadPersistSchema(manip)
	return
}

func loadPersistSchema(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.S).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "lifecycle", ctabb.U8).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	channels := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	section := func(name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, hints ...easp.AspectE) {
		sec := manip.TaggedValueSection(name).
			SectionStreamingGroup("data").
			AddSectionMembership(channels...)
		sec.TaggedValueColumn("value", ct).
			AddColumnEncodingHints(hints...)
	}

	// A small typed set rather than a section per field, so a new kind of
	// state is a DTO plus vocabulary entries and never a schema change —
	// ADR-0026 §SD6's "why one table" argument, applied to state. Kinds
	// share these sections under distinct registry memberships; the ids
	// keep their attributes apart on read (ADR-0105 D2's id-level gate).
	//
	// symbol: low-cardinality strings that repeat across rows — app ids,
	// run ids, tier and kind labels — which is what the inter-record hint
	// is for.
	section("symbol", ctabb.S,
		easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	// string: free text that does not repeat — persist keys, table scopes,
	// column keys.
	section("string", ctabb.S, easp.AspectLightGeneralCompression)
	// blob: payloads opaque to this layer — a persist value, a workingset's
	// facts-CBOR launch config.
	section("blob", ctabb.Y, easp.AspectLightGeneralCompression)
	section("u64", ctabb.U64, easp.AspectLightGeneralCompression)
	section("f64", ctabb.F64, easp.AspectLightGeneralCompression)
}
