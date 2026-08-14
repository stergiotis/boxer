// Package persiststore is the generated record store behind the durable
// persist backend (ADR-0105 D3a). App state lives on its own store-owned
// table rather than on `boxer.facts`: the state verbs want a latest-wins
// view over a mutable key, which is the shape the generated state view
// gives directly and the append-only facts table only gives through
// hand-written `argMax` SQL — the code class ADR-0105 exists to delete.
//
// One row per Set, keyed "<appId>/<key>" (the pushoutstore namespacing
// pattern). A Delete appends a tombstone; the newest row for a key wins,
// and a tombstone reads as absent. The previous value stays queryable, so
// the row trail is still the history of that key — the one property the
// facts-backed predecessor had that was worth keeping.
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

// GetPersistSchemaInManipulator builds the persist-state table. Envelope
// roles: id (string Key — "<appId>/<key>"), ts (Order — the write time),
// lifecycle (Lifecycle — the Delete tombstone, which is what makes the
// state view emit at all).
func GetPersistSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableName)
	manip.SetTableComment("app persist state, keyed <appId>/<key> (ADR-0105 D3a)")
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

	// The payload, opaque to this layer: persist stores bytes the app chose.
	section("stateBlob", ctabb.Y, easp.AspectLightGeneralCompression)
	// appId and key are carried denormalised beside the composite entity id
	// so "every key this app owns" is a WHERE on a column rather than a
	// prefix match on the id string. appId repeats across every row an app
	// ever writes, which is what the inter-record hint is for.
	section("stateAppId", ctabb.S,
		easp.AspectInterRecordLowCardinality, easp.AspectLightGeneralCompression)
	section("stateKey", ctabb.S, easp.AspectLightGeneralCompression)
}
