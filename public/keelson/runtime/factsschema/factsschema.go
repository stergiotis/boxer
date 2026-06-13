// Package factsschema defines the runtime.facts leeway table per ADR-0026
// §SD6. Modelled after spinnaker.facts
// (public/boxerstaging/spinnaker/schema/spinnaker_schema.go):
// plain-value identity columns + per-canonical-type tagged-value sections
// in a single "data" streaming group, with foreignKey for cross-fact
// references. Fact "kind" is a membership (see memberships.go), so the
// same row can carry several kind memberships without schema duplication.
//
// No live ClickHouse interaction lives in this package — only the schema
// definition. Hosts that need an actual table execute the CREATE TABLE
// SQL emitted by the generated [factsschema/ddl] package (via
// `ddl.ComposeCreateTableSql`) against a live CH connection. The
// generated package bakes the column block at codegen time so no
// leeway pipeline runs at process startup.
package factsschema

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// TableRowConfig mirrors spinnaker — multi-attributes-per-row encoding lets
// one fact carry several tagged values across sections without splitting
// into multiple physical rows.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// DatabaseName / TableName are the conventional CH coordinates: the runtime
// expects CREATE TABLE runtime.facts (...).
const (
	DatabaseName = "runtime"
	TableName    = "facts"
)

// GetSchemaInManipulator returns a freshly-constructed TableManipulator with
// the runtime.facts schema loaded. Consumers proceed to BuildTableDesc plus
// the leeway DDL/DML/RA pipelines; the generated artefacts under dml/,
// ra/ and ddl/ are the runtime entrypoints. The codegen helpers at
// factsschema/codegen build those artefacts on demand.
func GetSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("factsschema: unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName(TableName)
	manip.SetTableComment("ADR-0026 §SD6 runtime.facts: app state, capability grants, and audit records as leeway memberships")
	LoadRuntimeFactsMapping(manip)
	return
}

// LoadRuntimeFactsMapping populates manip with the runtime.facts schema.
// Exposed separately so a composite mapping (a future host combining
// runtime facts with private columns) can extend without forking.
func LoadRuntimeFactsMapping(manip common.TableManipulatorFluidI) {
	ctId := ctabb.U64
	u64 := ctabb.U64
	u32 := ctabb.U32
	str := ctabb.S
	bytes := ctabb.Y
	dt := ctabb.Z64
	boolType := ctabb.B

	// Plain values: per-row identity, observation timestamp, eviction
	// trigger. The runtime writes id + naturalKey at row insert; ts at
	// every write; expiresAt only when a TTL applies (grants, audit
	// retention).
	{
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", u64).
			AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", bytes).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", dt).
			AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
		manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "expiresAt", dt).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}

	// Relation: cross-fact references — e.g., an audit fact's link to the
	// grant fact that authorised it, or a state-write's link back to the
	// grant for the persistence subject.
	{
		sec := manip.TaggedValueSection("foreignKey").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionStreamingGroup("foreignKey").
			AddSectionUseAspects(useaspects.AspectLinking)
		sec.TaggedValueColumn("value", ctId).
			AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	}

	// Data: per-canonical-type tagged value sections, all in one streaming
	// group. Every section accepts the spinnaker triple of membership
	// specs so any tagged value can carry a low-cardinality name, a
	// high-cardinality reference, or a mixed name + parameters address.
	membershipSpec := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecHighCardRef,
		common.MembershipSpecMixedLowCardRefHighCardParameters,
	}
	{
		strh := canonicaltypes.PromoteScalarPrim(str, canonicaltypes.ScalarModifierHomogenousArray)
		{
			sec := manip.TaggedValueSection("textArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", strh).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection("stringArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", strh).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		// symbol kept as scalar (interned/low-cardinality — 1-element-array
		// wrapping would defeat the inter-record low-cardinality encoding
		// that audit/grant rows rely on).
		{
			sec := manip.TaggedValueSection("symbol").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", str).
				AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
				AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
					easp.AspectIntraRecordLowCardinality,
					easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection("symbolArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", strh).
				AddColumnValueSemantics(valueaspects.AspectCanonicalizedValue).
				AddColumnEncodingHints(easp.AspectInterRecordLowCardinality,
					easp.AspectIntraRecordLowCardinality,
					easp.AspectLightGeneralCompression)
		}
		{
			sec := manip.TaggedValueSection("blobArray").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", canonicaltypes.PromoteScalarPrim(bytes, canonicaltypes.ScalarModifierHomogenousArray)).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		for _, ct := range []canonicaltypes.PrimitiveAstNodeI{
			ctabb.U8, ctabb.U16, ctabb.U32, ctabb.U64,
			ctabb.I8, ctabb.I16, ctabb.I32, ctabb.I64,
		} {
			name := strings.ToLower(ct.String())
			{
				cth := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierHomogenousArray)
				sec := manip.TaggedValueSection(naming.StylableName(name + "Array")).
					SectionStreamingGroup("data").
					AddSectionMembership(membershipSpec...)
				sec.TaggedValueColumn("value", cth).
					AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			}
			switch ct {
			case ctabb.U32, ctabb.U64:
				{
					ctm := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierSet)
					sec := manip.TaggedValueSection(naming.StylableName(name + "Set")).
						SectionStreamingGroup("data").
						AddSectionMembership(membershipSpec...)
					sec.TaggedValueColumn("value", ctm).
						AddColumnEncodingHints(easp.AspectLightGeneralCompression)
				}
			}
		}
		for _, ct := range []canonicaltypes.PrimitiveAstNodeI{
			ctabb.F32, ctabb.F64,
		} {
			name := strings.ToLower(ct.String())
			{
				cth := canonicaltypes.PromoteScalarPrim(ct, canonicaltypes.ScalarModifierHomogenousArray)
				sec := manip.TaggedValueSection(naming.StylableName(name + "Array")).
					SectionStreamingGroup("data").
					AddSectionMembership(membershipSpec...)
				sec.TaggedValueColumn("value", cth).
					AddColumnEncodingHints(easp.AspectLightGeneralCompression, easp.AspectLightSlowlyChangingFloat)
			}
		}
		// u32Range: half-open integer range for grant validity windows.
		{
			sec := manip.TaggedValueSection("u32Range").
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("beginIncl", u32).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
			sec.TaggedValueColumn("endExcl", u32).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
		{
			dth := canonicaltypes.PromoteScalarPrim(dt, canonicaltypes.ScalarModifierHomogenousArray)
			sec := manip.TaggedValueSection(naming.StylableName("timeArray")).
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", dth).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}

		{
			sec := manip.TaggedValueSection(naming.StylableName("bool")).
				SectionStreamingGroup("data").
				AddSectionMembership(membershipSpec...)
			sec.TaggedValueColumn("value", boolType).
				AddColumnEncodingHints(easp.AspectLightGeneralCompression)
		}
	}
}
