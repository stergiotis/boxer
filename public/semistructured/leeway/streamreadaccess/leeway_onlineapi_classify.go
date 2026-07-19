package streamreadaccess

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// ColumnRoleClassE is the coarse display bucket of a physical Arrow column in a
// leeway-shaped result. It collapses the fine-grained common.ColumnRoleE — and
// the IR sub-type that carries it — into the three categories a reader toggles
// independently: the payload, the machine-readable structure that supports it,
// and the set-membership encoding. A UI that reshapes a leeway result (the play
// Table pane's display modes) filters columns on this bucket, not on the raw
// role, so it never has to enumerate the dozen membership variants itself.
type ColumnRoleClassE uint8

const (
	// ColumnRoleClassValue is a payload value column (role val): the data a
	// human came to read. Always shown.
	ColumnRoleClassValue ColumnRoleClassE = iota
	// ColumnRoleClassSupport is a machine-readable structural column — the
	// cardinality / length columns (len, card, cusum*, and the per-membership
	// *card variants) that let the decoder partition collections. Normally
	// hidden; a reader opts in to see the encoding. These columns are always
	// consumed to lay out a value view correctly regardless of whether they are
	// themselves shown.
	ColumnRoleClassSupport
	// ColumnRoleClassMembership is a set-membership encoding column (the
	// ref / verbatim / parametrized roles): which set(s) or tag(s) a tagged
	// value belongs to. Normally hidden; a reader opts in.
	ColumnRoleClassMembership
)

var _ fmt.Stringer = ColumnRoleClassE(0)

func (inst ColumnRoleClassE) String() string {
	switch inst {
	case ColumnRoleClassValue:
		return "value"
	case ColumnRoleClassSupport:
		return "support"
	case ColumnRoleClassMembership:
		return "membership"
	}
	return "invalid"
}

// ColumnClass is the leeway classification of one physical Arrow column,
// resolved against a specific result schema. For a column at ArrowIdx it
// answers: which section it belongs to (a tagged SectionName, or a backbone
// PlainItemType), what the leeway attribute is called (LeewayName), which
// display bucket it falls in (Class), and its collection shape (SubType — used
// to pack set/array values). It is produced by ClassifyArrowColumns and is the
// per-column half of the play Table pane's leeway display modes.
//
// Only columns that resolve to a schema field appear. A column present in the
// IR but absent from the schema (a projected-away attribute) is omitted, so a
// caller keys by ArrowIdx and treats un-listed schema columns as un-classified
// (non-leeway, implicit, or projected-in).
type ColumnClass struct {
	ArrowIdx      int
	Physical      string
	Class         ColumnRoleClassE
	SectionName   naming.StylableName   // tagged section; empty for a backbone column
	PlainItemType common.PlainItemTypeE // backbone item type; None for a tagged column
	LeewayName    naming.StylableName   // the leeway column / attribute name
	SubType       common.IntermediateColumnSubTypeE
	// CanonicalType is the column's leeway value type (nil when the group carried
	// none, e.g. a membership column resolves to its referenced scalar type). It
	// lets a display layer ask type questions — "is this a datetime?" — that the
	// physical name and the Arrow wire type answer only fragilely: a width-32
	// DateTime('UTC') arrives as a bare uint32, indistinguishable from a
	// cardinality support column by Arrow type alone. See Temporal.
	CanonicalType canonicaltypes.PrimitiveAstNodeI
}

// Temporal reports whether the column carries a datetime/time value — the
// type-driven signal a display layer uses to find temporal columns, in place of
// matching physical-name prefixes. Derived from the canonical type, it holds
// regardless of the Arrow width the column arrives as (a width-32 DateTime on
// the wire is a uint32, not an Arrow Timestamp) and regardless of collection
// shape (scalar, array, or set of datetimes are all temporal). A support or
// membership column is never Temporal: its canonical type is the structural
// integer or the referenced scalar, not a datetime.
func (inst ColumnClass) Temporal() bool {
	return inst.CanonicalType != nil && inst.CanonicalType.IsTemporalNode()
}

// Backbone reports whether the column is a plain/backbone column (an entity id,
// timestamp, routing, lifecycle, transaction, or opaque column) rather than a
// tagged-section value.
func (inst ColumnClass) Backbone() bool {
	return inst.PlainItemType != common.PlainItemTypeNone
}

// NonScalar reports whether the column carries a collection (a homogeneous
// array or a set) rather than a scalar — the case where a one-row-per-DB-row
// view must pack multiple items into one cell and a one-row-per-attribute view
// fans them out.
func (inst ColumnClass) NonScalar() bool {
	switch inst.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArray,
		common.IntermediateColumnsSubTypeSet:
		return true
	}
	return false
}

// ClassifyArrowColumns resolves every leeway column in ir against schema and
// returns its per-column classification (see ColumnClass). It mirrors the
// index resolution the schema-driven Driver does in prepareFromSchema — the
// same IR walk, the same physical-name mapping, the same canonicalized resolver
// (newArrowColumnResolver) — but keeps every resolved column, including the
// cardinality / length support columns the Driver consumes internally as
// cursors and never surfaces. tableRowConfig must be the value
// DiscoverTableFromColumnNames returned for this schema.
//
// A column group that cannot be mapped to physical names is skipped rather than
// failing the whole classification: a partial map is more useful to a display
// layer than none. Columns present in the IR but absent from schema are omitted
// (their resolved index is -1).
func ClassifyArrowColumns(
	ir *common.IntermediateTableRepresentation,
	conv common.NamingConventionFwdI,
	schema *arrow.Schema,
	tableRowConfig common.TableRowConfigE,
) (classes []ColumnClass) {
	if ir == nil || conv == nil || schema == nil {
		return nil
	}
	resolver := newArrowColumnResolver(schema, conv)
	var physBuf []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		class, ok := classifyColumnSubType(cc.SubType)
		if !ok {
			continue
		}
		var err error
		physBuf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, physBuf[:0], tableRowConfig)
		if err != nil {
			continue
		}
		for j, name := range cp.Names {
			if j >= len(physBuf) {
				break
			}
			arrowIdx := resolver.resolve(physBuf[j])
			if arrowIdx < 0 {
				continue
			}
			// CanonicalType is index-aligned with Names within a column-props
			// group; guard the length in case a group carries fewer types than
			// names (a malformed group leaves the type nil, i.e. untyped).
			var ct canonicaltypes.PrimitiveAstNodeI
			if j < len(cp.CanonicalType) {
				ct = cp.CanonicalType[j]
			}
			classes = append(classes, ColumnClass{
				ArrowIdx:      arrowIdx,
				Physical:      schema.Field(arrowIdx).Name,
				Class:         class,
				SectionName:   cc.SectionName,
				PlainItemType: cc.PlainItemType,
				LeewayName:    name,
				SubType:       cc.SubType,
				CanonicalType: ct,
			})
		}
	}
	return
}

// classifyColumnSubType maps an IR column sub-type to its display bucket: the
// three value sub-types (scalar, homogeneous array, set) are payload; the three
// *Support sub-types are the machine-readable cardinality / length columns; the
// membership sub-type is the set-membership encoding. ok is false for any
// sub-type outside this set, so an unrecognised group is skipped rather than
// mis-bucketed.
func classifyColumnSubType(st common.IntermediateColumnSubTypeE) (class ColumnRoleClassE, ok bool) {
	switch st {
	case common.IntermediateColumnsSubTypeScalar,
		common.IntermediateColumnsSubTypeHomogenousArray,
		common.IntermediateColumnsSubTypeSet:
		return ColumnRoleClassValue, true
	case common.IntermediateColumnsSubTypeHomogenousArraySupport,
		common.IntermediateColumnsSubTypeSetSupport,
		common.IntermediateColumnsSubTypeMembershipSupport:
		return ColumnRoleClassSupport, true
	case common.IntermediateColumnsSubTypeMembership:
		return ColumnRoleClassMembership, true
	}
	return 0, false
}

// arrowColumnResolver maps a leeway PhysicalColumnDesc to its column index in a
// specific Arrow schema. It indexes the schema by both the raw field name and
// its canonicalized form: a section or column authored in a non-canonical style
// (camelCase, PascalCase, snake_case) round-trips through the IR re-styled, so
// both sides must be canonicalised before comparison or every lookup for such a
// column silently fails (index -1). See NamingConventionFwdI.CanonicalizeSchemaName.
type arrowColumnResolver struct {
	nameToIdx map[string]int
}

func newArrowColumnResolver(schema *arrow.Schema, conv common.NamingConventionFwdI) arrowColumnResolver {
	nFields := schema.NumFields()
	nameToIdx := make(map[string]int, nFields*2)
	for i := range nFields {
		n := schema.Field(i).Name
		nameToIdx[n] = i
		if canon := conv.CanonicalizeSchemaName(n); canon != n {
			nameToIdx[canon] = i
		}
	}
	return arrowColumnResolver{nameToIdx: nameToIdx}
}

func (inst arrowColumnResolver) resolve(phy common.PhysicalColumnDesc) int {
	if idx, ok := inst.nameToIdx[phy.String()]; ok {
		return idx
	}
	return -1
}
