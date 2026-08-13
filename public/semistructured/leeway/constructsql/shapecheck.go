package constructsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// LwShapeCheck (ADR-0181 §SD5) — the static half of the transform contract's
// validation. It checks that a SELECT's output column set forms a coherent
// leeway table: every output name parses under the naming convention, the
// discovered table passes the validator, and section completeness holds per
// channel. All of it is decidable from names alone; the invariants that need
// data (co-length equality, cardinality positivity) are the audit-query
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
		err = eb.Build().Errorf("%s: %w", ShapeCheckPassName, err)
		return
	}
	scopes, err := nanopass.BuildScopes(pr, "")
	if err != nil {
		err = eb.Build().Errorf("%s: %w", ShapeCheckPassName, err)
		return
	}
	for _, root := range scopes {
		var names []string
		names, err = outputNames(pr, root)
		if err == nil {
			err = CheckOutputColumns(names)
		}
		if err != nil {
			err = eb.Build().Errorf("%s: %w", ShapeCheckPassName, err)
			return
		}
	}
	result = sql // analytical: the body is never rewritten
	return
}

// outputNames derives the statement's output column names from the top-level
// projection. An aliased item contributes its alias; a bare identifier rides
// through under its own name; anything else — an expression without a minted
// name — breaks the closure rule and is rejected, as is `*`, which cannot be
// checked without a schema.
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
			name = nanopass.DecodeIdentifier(strings.TrimSpace(nanopass.NodeText(pr, child)))
			return
		}
	}
	err = eb.Build().Str("item", strings.TrimSpace(item.GetText())).Errorf("expression without a minted name breaks the closure rule (ADR-0181): wrap it in a constructor or alias it to a physical name")
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

// membershipLaneRoles are the membership identity/payload lane roles; their
// cardinality companions are the `<role>card` spellings.
var membershipLaneRoles = map[common.ColumnRoleE]bool{
	common.ColumnRoleHighCardRef:                     true,
	common.ColumnRoleHighCardRefParametrized:         true,
	common.ColumnRoleHighCardVerbatim:                true,
	common.ColumnRoleLowCardRef:                      true,
	common.ColumnRoleLowCardRefParametrized:          true,
	common.ColumnRoleLowCardVerbatim:                 true,
	common.ColumnRoleMixedLowCardRef:                 true,
	common.ColumnRoleMixedLowCardVerbatim:            true,
	common.ColumnRoleMixedVerbatimHighCardParameters: true,
	common.ColumnRoleMixedRefHighCardParameters:      true,
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
}

// CheckOutputColumns is the name-level shape check over one output column
// set: parse, discover, validate, and enforce per-channel section
// completeness plus co-section-group wholeness.
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
		key := string(secName)
		sec := sections[key]
		if sec == nil {
			sec = &sectionShape{
				display:   key,
				membLanes: make(map[common.ColumnRoleE]bool, 2),
				membCards: make(map[common.ColumnRoleE]bool, 2),
			}
			sections[key] = sec
			order = append(order, key)
			sec.coGroup, err = conv.ExtractCoSectionGroup(phy)
			if err != nil {
				return
			}
		}
		var role common.ColumnRoleE
		role, err = conv.ExtractColumnRole(phy)
		if err != nil {
			err = eb.Build().Str("column", names[i]).Errorf("unable to classify: %w", err)
			return
		}
		switch {
		case role == common.ColumnRoleValue:
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
		case role == common.ColumnRoleLength:
			sec.hasLen = true
		case role == common.ColumnRoleCardinality:
			sec.hasCard = true
		case role == common.ColumnRoleCusumLength, role == common.ColumnRoleCusumCardinality:
			// materialized cumulative companions; nothing to enforce statically
		case membershipLaneRoles[role]:
			sec.membLanes[role] = true
		case strings.HasSuffix(string(role), "card"):
			base := common.ColumnRoleE(strings.TrimSuffix(string(role), "card"))
			sec.membCards[base] = true
		}
	}

	coGroups := make(map[naming.Key][]string, 4)
	for _, key := range order {
		sec := sections[key]
		if sec.valueCount > 0 && len(sec.membLanes) == 0 {
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
		if sec.coGroup != "" {
			coGroups[sec.coGroup] = append(coGroups[sec.coGroup], sec.display)
		}
	}
	for key, members := range coGroups {
		if len(members) < 2 {
			return eb.Build().Str("coSectionGroup", key.String()).Strs("sections", members).Errorf("dangling co-section-group half: the group must move whole under vertical subsetting")
		}
	}
	return
}
