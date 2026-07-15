package lwsql

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// DefaultConditionSection is the leeway section an ExposeSelectionConditions
// rewrite puts its condition columns in (ADR-0121 §SD5).
const DefaultConditionSection = "conditions"

// conditionColumnPrefix names the condition columns *within* the section: the n-th
// condition is `c<n>`, 1-based, so it labels as `conditions:c1` — the leeway
// reading of the plain path's `cond_1`.
const conditionColumnPrefix = "c"

// ErrConditionSectionExists is returned when the table already carries a section
// by the configured condition-section name. Adding condition columns to it would merge
// synthesized columns into an authored section, so the namer refuses.
var ErrConditionSectionExists = eh.Errorf("table already has a condition section")

// ErrSeparatorInSectionName is returned when the configured section name
// contains the separator the table's physical names are joined with. Such a name
// would corrupt the column-name grammar — the name would re-split at the wrong
// position — so it is rejected rather than emitted.
var ErrSeparatorInSectionName = eh.Errorf("condition section name contains the table's name separator")

// conditionType is the canonical type of a condition column: a boolean, which takes
// no width (see the CT rules). It renders as `b`.
var conditionType = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBool}

// NameConditions implements passes.ConditionNamerI, naming an
// ExposeSelectionConditions
// rewrite's condition columns for a leeway table as physical columns of a declared
// section, so they are part of the data model rather than bolted beside it.
//
// For n=2 on a colon-separated table it returns:
//
//	tv:conditions:c1:val:b:0:0:0:0::
//	tv:conditions:c2:val:b:0:0:0:0::
//
// Feeding a result carrying these back through DiscoverTableFromColumnNames
// reconstructs a genuine `conditions` tagged section beside the table's real
// ones, so BuildLabels labels them `conditions:c1` and `conditions:*`
// resolves as a handle.
//
// ok is false for a table that is not leeway-shaped — the pass then falls back
// to its plain `cond_1` naming, which is the point of the interface. err refuses
// the rewrite: the table already has the section, or its separator makes the
// configured name unrepresentable.
func (inst *Resolver) NameConditions(dbName string, tableName string, n int) (names []string, ok bool, err error) {
	if n <= 0 {
		return
	}
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return // not leeway-shaped — let the caller name them plainly
	}
	_, exists := idx.sections[fold(string(inst.conditionSection))]
	if exists {
		err = eb.Build().Str("database", dbName).Str("table", tableName).Stringer("section", inst.conditionSection).Errorf("%w", ErrConditionSectionExists)
		return
	}
	// The section name is folded to LowerSpinalCase, which cannot contain '_',
	// so a '_'-separated table is already safe; this catches a pathological
	// separator rather than the common case.
	if strings.Contains(string(inst.conditionSection), idx.meta.separator) {
		err = eb.Build().Str("table", tableName).Stringer("section", inst.conditionSection).Str("separator", idx.meta.separator).Errorf("%w", ErrSeparatorInSectionName)
		return
	}

	conv, err := ddl.NewHumanReadableNamingConvention(idx.meta.separator)
	if err != nil {
		err = eb.Build().Str("separator", idx.meta.separator).Errorf("unable to build naming convention for condition columns: %w", err)
		return
	}

	// Compose through the same seam the DDL generator uses, so a condition column
	// name can never drift from the convention the table's own columns follow.
	cc := common.IntermediateColumnContext{
		Scope:       common.IntermediateColumnScopeTagged,
		SectionName: inst.conditionSection,
		UseAspects:  useaspects.EmptyAspectSet,
		// A condition belongs to no co-section and no streaming group, and is a
		// tagged column (PlainItemTypeNone is what makes it one).
		PlainItemType: common.PlainItemTypeNone,
	}
	cp := common.IntermediateColumnProps{
		Names:          make([]naming.StylableName, 0, n),
		Roles:          make([]common.ColumnRoleE, 0, n),
		CanonicalType:  make([]canonicaltypes.PrimitiveAstNodeI, 0, n),
		EncodingHints:  make([]encodingaspects.AspectSet, 0, n),
		ValueSemantics: make([]valueaspects.AspectSet, 0, n),
	}
	for i := 1; i <= n; i++ {
		var name naming.StylableName
		name, err = naming.MakeStylableName(conditionColumnPrefix + strconv.Itoa(i))
		if err != nil {
			err = eb.Build().Int("condition", i).Errorf("unable to build condition column name: %w", err)
			return
		}
		cp.Names = append(cp.Names, name)
		cp.Roles = append(cp.Roles, common.ColumnRoleValue)
		cp.CanonicalType = append(cp.CanonicalType, conditionType)
		cp.EncodingHints = append(cp.EncodingHints, encodingaspects.EmptyAspectSet)
		cp.ValueSemantics = append(cp.ValueSemantics, valueaspects.EmptyAspectSet)
	}

	// tableRowConfig is a table-wide property read back from every column, so
	// the condition columns must carry the table's own, not a fixed default.
	phys, err := conv.MapIntermediateToPhysicalColumns(cc, cp, nil, idx.meta.tableRowConfig)
	if err != nil {
		err = eb.Build().Str("table", tableName).Errorf("unable to compose condition columns: %w", err)
		return
	}
	names = make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, p.String())
	}
	ok = true
	return
}

var _ passes.ConditionNamerI = (*Resolver)(nil)
