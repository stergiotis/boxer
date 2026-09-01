package constructsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// LwShapeCheck (ADR-0181 §SD5) — the static half of the transform contract's
// validation. It checks that a SELECT's output column set forms a coherent
// leeway table: every output name parses under the naming convention, the
// discovered table passes the validator, section-level segments agree across
// each section's lanes, and section completeness holds per channel. All of
// it is decidable from names alone; the invariants that need data
// (co-length equality, cardinality positivity) are the audit-query
// generator's half (lwsql.AuditQueries).
//
// The pass is analytical: on success the body rides through unchanged; any
// violation is a hard error. It is opt-in — never in the standard set —
// because it is only meaningful on transform-shaped queries (a projection of
// ordinary SQL expressions is not a defect, it is just not a leeway table).

// ShapeCheckPassName is the registered nanopass name of the shape check.
const ShapeCheckPassName = "LwShapeCheck"

// ShapeCheckPass is LwShapeCheck as an opt-in pass.
var ShapeCheckPass = nanopass.LiftBodyPass(ShapeCheckPassName, shapeCheckImpl, nanopass.PassProperties{
	Idempotent: true,
	Reads:      nanopass.RegionBody,
	Writes:     nanopass.RegionBody,
})

func shapeCheckImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf(ShapeCheckPassName+": %w", err)
		return
	}
	scopes, err := nanopass.BuildScopes(pr, "")
	if err != nil {
		err = eb.Build().Errorf(ShapeCheckPassName+": %w", err)
		return
	}
	for _, root := range scopes {
		var names []string
		names, err = outputNames(pr, root)
		if err == nil {
			err = CheckOutputColumns(names)
		}
		if err != nil {
			err = eb.Build().Errorf(ShapeCheckPassName+": %w", err)
			return
		}
	}
	result = sql // analytical: the body is never rewritten
	return
}

// outputNames derives the statement's output column names from the top-level
// projection. An aliased item contributes its alias; a bare identifier rides
// through under its own name (a table qualifier does not reach the result
// column name and is dropped); anything else — an expression without a
// minted name — breaks the closure rule and is rejected, as is `*`, which
// cannot be checked without a schema.
func outputNames(pr *nanopass.ParseResult, scope *nanopass.SelectScope) (names []string, err error) {
	projection := scope.Node.ProjectionClause()
	if projection == nil {
		err = eb.Build().Errorf("select has no projection clause")
		return
	}
	projectionCtx, ok := projection.(*grammar1.ProjectionClauseContext)
	if !ok {
		err = eb.Build().Errorf("unexpected projection clause shape")
		return
	}
	listCtx, ok := projectionCtx.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !ok {
		err = eb.Build().Errorf("unexpected projection list shape")
		return
	}
	items := listCtx.AllColumnsExpr()
	names = make([]string, 0, len(items))
	for _, item := range items {
		switch it := item.(type) {
		case *grammar1.ColumnsExprAsteriskContext:
			err = eb.Build().Errorf("`*` cannot be shape-checked: the output names are not in the statement")
			return
		case *grammar1.ColumnsExprColumnContext:
			var name string
			name, err = itemOutputName(pr, it)
			if err != nil {
				return
			}
			names = append(names, name)
		default:
			err = eb.Build().Str("item", strings.TrimSpace(item.GetText())).Errorf("unsupported projection item shape")
			return
		}
	}
	return
}

func itemOutputName(pr *nanopass.ParseResult, item *grammar1.ColumnsExprColumnContext) (name string, err error) {
	for i := 0; i < item.GetChildCount(); i++ {
		switch child := item.GetChild(i).(type) {
		case *grammar1.ColumnExprAliasContext:
			name = aliasName(child)
			if name == "" {
				err = eb.Build().Str("item", strings.TrimSpace(item.GetText())).Errorf("unable to extract alias")
			}
			return
		case *grammar1.ColumnExprIdentifierContext:
			name, err = bareIdentifierName(pr, child)
			return
		}
	}
	err = eb.Build().Str("item", strings.TrimSpace(item.GetText())).Errorf("expression without a minted name breaks the closure rule (ADR-0181): wrap it in a constructor or alias it to a physical name")
	return
}

// bareIdentifierName is the result-column name of an unaliased identifier
// item: the nested identifier without any table qualifier, which ClickHouse
// drops when naming the column.
func bareIdentifierName(pr *nanopass.ParseResult, identExpr *grammar1.ColumnExprIdentifierContext) (name string, err error) {
	colIdCtx, ok := identExpr.ColumnIdentifier().(*grammar1.ColumnIdentifierContext)
	if !ok {
		err = eb.Build().Str("item", strings.TrimSpace(nanopass.NodeText(pr, identExpr))).Errorf("unexpected identifier shape")
		return
	}
	nestedCtx, ok := colIdCtx.NestedIdentifier().(*grammar1.NestedIdentifierContext)
	if !ok {
		err = eb.Build().Str("item", strings.TrimSpace(nanopass.NodeText(pr, identExpr))).Errorf("unexpected identifier shape")
		return
	}
	name = nanopass.DecodeIdentifier(nestedCtx.GetText())
	return
}

func aliasName(ctx *grammar1.ColumnExprAliasContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		switch child := ctx.GetChild(i).(type) {
		case *grammar1.IdentifierContext:
			return nanopass.DecodeIdentifier(child.GetText())
		case *grammar1.AliasContext:
			return nanopass.DecodeIdentifier(child.GetText())
		}
	}
	return ""
}

type sectionShape struct {
	display    string
	valueCount int
	arrayVals  []string
	setVals    []string
	hasLen     bool
	hasCard    bool
	membLanes  map[common.ColumnRoleE]bool
	membCards  map[common.ColumnRoleE]bool // keyed by the base lane role
	coGroup    naming.Key
	streaming  naming.Key
	useAspects string // the section's use-aspects segment, verbatim
}

// CheckOutputColumns is the name-level shape check over one output column
// set: parse, discover, validate, and enforce per-channel section
// completeness, section-segment agreement, and co-section-group wholeness.
func CheckOutputColumns(names []string) (err error) {
	if len(names) == 0 {
		return eb.Build().Errorf("empty output column set")
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return eb.Build().Str("column", n).Errorf("duplicate output column name — a result carrying it twice is not a leeway table")
		}
		seen[n] = true
	}
	separator := ":"
	if !strings.ContainsRune(strings.Join(names, ""), ':') {
		separator = "_"
	}
	conv, err := ddl.NewHumanReadableNamingConvention(separator)
	if err != nil {
		return
	}
	phys, err := conv.ParseColumns(names)
	if err != nil {
		err = eb.Build().Errorf("output name does not parse under the leeway naming convention: %w", err)
		return
	}
	table, _, err := conv.DiscoverTableFromColumnNames(names)
	if err != nil {
		err = eb.Build().Errorf("output columns do not form a leeway table: %w", err)
		return
	}
	err = common.NewTableValidator().ValidateTable(&table)
	if err != nil {
		err = eb.Build().Errorf("discovered table fails validation: %w", err)
		return
	}

	sections := make(map[string]*sectionShape, 8)
	order := make([]string, 0, 8)
	for i, phy := range phys {
		var secName naming.StylableName
		secName, err = conv.ExtractSectionName(phy)
		if err != nil {
			err = eb.Build().Str("column", names[i]).Errorf("unable to classify: %w", err)
			return
		}
		if secName == "" {
			continue // plain columns ride freely
		}
		var coGroup, streaming naming.Key
		var use string
		{
			coGroup, err = conv.ExtractCoSectionGroup(phy)
			if err != nil {
				return
			}
			streaming, err = conv.ExtractStreamingGroup(phy)
			if err != nil {
				return
			}
			var useSet useaspects.AspectSet
			useSet, err = conv.ExtractUseAspects(phy)
			if err != nil {
				return
			}
			use = useSet.String()
		}
		key := string(secName)
		sec := sections[key]
		if sec == nil {
			sec = &sectionShape{
				display:    key,
				membLanes:  make(map[common.ColumnRoleE]bool, 2),
				membCards:  make(map[common.ColumnRoleE]bool, 2),
				coGroup:    coGroup,
				streaming:  streaming,
				useAspects: use,
			}
			sections[key] = sec
			order = append(order, key)
		} else {
			// Section-level segments must agree across the section's lanes;
			// a first-seen latch would make the verdict depend on the
			// projection order, and a disagreement is a shape no generator
			// produces (the read-back side re-derives names WITH the
			// section's segments and would miss a divergent lane).
			switch {
			case coGroup != sec.coGroup:
				return eb.Build().Str("section", sec.display).Str("column", names[i]).Str("got", coGroup.String()).Str("want", sec.coGroup.String()).Errorf("lanes of one section disagree on the co-section group")
			case streaming != sec.streaming:
				return eb.Build().Str("section", sec.display).Str("column", names[i]).Str("got", streaming.String()).Str("want", sec.streaming.String()).Errorf("lanes of one section disagree on the streaming group")
			case use != sec.useAspects:
				return eb.Build().Str("section", sec.display).Str("column", names[i]).Str("got", use).Str("want", sec.useAspects).Errorf("lanes of one section disagree on the use aspects — every lane of a section carries the same use segment")
			}
		}
		var role common.ColumnRoleE
		role, err = conv.ExtractColumnRole(phy)
		if err != nil {
			err = eb.Build().Str("column", names[i]).Errorf("unable to classify: %w", err)
			return
		}
		kind, base := lwsql.ClassifyLaneRole(role)
		switch kind {
		case lwsql.LaneKindValue:
			sec.valueCount++
			var ct canonicaltypes.PrimitiveAstNodeI
			ct, err = conv.ExtractCanonicalType(phy)
			if err != nil {
				err = eb.Build().Str("column", names[i]).Errorf("unable to classify: %w", err)
				return
			}
			var mod canonicaltypes.ScalarModifierE
			mod, err = common.ExtractScalarModifier(ct)
			if err != nil {
				err = eb.Build().Str("column", names[i]).Errorf("unable to classify: %w", err)
				return
			}
			switch mod {
			case canonicaltypes.ScalarModifierHomogenousArray:
				sec.arrayVals = append(sec.arrayVals, names[i])
			case canonicaltypes.ScalarModifierSet:
				sec.setVals = append(sec.setVals, names[i])
			}
		case lwsql.LaneKindLength:
			sec.hasLen = true
		case lwsql.LaneKindSetCardinality:
			sec.hasCard = true
		case lwsql.LaneKindCusum:
			// materialized cumulative companions; nothing to enforce statically
		case lwsql.LaneKindMembership:
			sec.membLanes[role] = true
		case lwsql.LaneKindMembershipCardinality:
			sec.membCards[base] = true
		default:
			return eb.Build().Str("column", names[i]).Stringer("role", role).Errorf("unclassified column role — extend the lane classifier before shape-checking it")
		}
	}

	coGroups := make(map[naming.Key][]*sectionShape, 4)
	coGroupOrder := make([]naming.Key, 0, 4)
	for _, key := range order {
		sec := sections[key]
		if sec.coGroup != "" {
			if _, seenGroup := coGroups[sec.coGroup]; !seenGroup {
				coGroupOrder = append(coGroupOrder, sec.coGroup)
			}
			coGroups[sec.coGroup] = append(coGroups[sec.coGroup], sec)
		}
	}
	membershipReachable := func(sec *sectionShape) bool {
		if len(sec.membLanes) > 0 {
			return true
		}
		// A values-only co-section may lean on a membership-carrying
		// partner — that sharing is what co-section groups exist for.
		for _, partner := range coGroups[sec.coGroup] {
			if sec.coGroup != "" && len(partner.membLanes) > 0 {
				return true
			}
		}
		return false
	}
	for _, key := range order {
		sec := sections[key]
		if sec.valueCount > 0 && !membershipReachable(sec) {
			return eb.Build().Str("section", sec.display).Errorf("section has value lanes but no membership lane — an instance exists because it is tagged")
		}
		if len(sec.arrayVals) > 0 && !sec.hasLen {
			return eb.Build().Str("section", sec.display).Strs("columns", sec.arrayVals).Errorf("array value lanes without their `len` support lane")
		}
		if len(sec.setVals) > 0 && !sec.hasCard {
			return eb.Build().Str("section", sec.display).Strs("columns", sec.setVals).Errorf("set value lanes without their `card` support lane")
		}
		if sec.hasLen && len(sec.arrayVals) == 0 {
			return eb.Build().Str("section", sec.display).Errorf("dangling `len` support lane: no array value lane in the section")
		}
		if sec.hasCard && len(sec.setVals) == 0 {
			return eb.Build().Str("section", sec.display).Errorf("dangling `card` support lane: no set value lane in the section")
		}
		for base := range sec.membCards {
			if !sec.membLanes[base] {
				return eb.Build().Str("section", sec.display).Str("role", string(base)).Errorf("dangling membership cardinality lane: its membership lane is not in the set")
			}
		}
	}
	for _, key := range coGroupOrder {
		members := coGroups[key]
		if len(members) < 2 {
			return eb.Build().Str("coSectionGroup", key.String()).Str("section", members[0].display).Errorf("dangling co-section-group half: the group must move whole under vertical subsetting")
		}
	}
	return
}
