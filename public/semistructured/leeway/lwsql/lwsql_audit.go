package lwsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The runtime audit-query generator (ADR-0181 §SD5) — the data half of the
// transform contract's validation. LwShapeCheck proves an output column set
// is table-shaped from names alone; these queries check the invariants only
// the data can violate: co-length equality across a section's lanes,
// cardinality positivity (card ≥ 1, len ≥ 1 — an empty list is absent, not a
// value), and descriptor sums consistent with flattened lane lengths.
//
// Emitted SQL is ClickHouse-shaped (arrayAll lambdas) and uses built-ins
// only; each query returns one row with a `violations` count, zero on a
// conforming table. Plain (backbone) lanes carry no descriptors and are not
// audited.

// AuditQuery is one invariant check over a leeway table.
type AuditQuery struct {
	Name string // "<section>: <check>"
	SQL  string
}

// auditLanes is one section's lane classification for audit purposes.
type auditLanes struct {
	display string
	coGroup naming.Key
	// iLanes are lanes on the instance axis: their per-row lengths must agree.
	iLanes []string
	// raggedSums are "arraySum(descriptor) = length(flattened)" pairs.
	raggedSums [][2]string // [descriptor, flattened]
	// positive are descriptor lanes whose every element must be >= 1.
	positive []string
}

// paramPartner maps a mixed membership's parameter lane to the identity lane
// whose `<role>card` descriptor covers both.
var paramPartner = map[common.ColumnRoleE]common.ColumnRoleE{
	common.ColumnRoleMixedVerbatimHighCardParameters: common.ColumnRoleMixedLowCardVerbatim,
	common.ColumnRoleMixedRefHighCardParameters:      common.ColumnRoleMixedLowCardRef,
}

// AuditQueries derives the audit set from a table's physical column names.
// tableName is emitted verbatim into FROM.
func AuditQueries(tableName string, columnNames []string) (queries []AuditQuery, err error) {
	if len(columnNames) == 0 {
		err = eb.Build().Errorf("empty column set")
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(detectSeparator(columnNames))
	if err != nil {
		return
	}
	phys, err := conv.ParseColumns(columnNames)
	if err != nil {
		err = eb.Build().Errorf("column name does not parse under the leeway naming convention: %w", err)
		return
	}

	type rawSection struct {
		display    string
		coGroup    naming.Key
		scalarVals []string
		arrayVals  []string
		setVals    []string
		lenLane    string
		cardLane   string
		membLanes  map[common.ColumnRoleE]string
		membCards  map[common.ColumnRoleE]string // keyed by base lane role
	}
	sections := make(map[string]*rawSection, 8)
	order := make([]string, 0, 8)
	for i, phy := range phys {
		var secName naming.StylableName
		secName, err = conv.ExtractSectionName(phy)
		if err != nil {
			err = eb.Build().Str("column", columnNames[i]).Errorf("unable to classify: %w", err)
			return
		}
		if secName == "" {
			continue
		}
		key := string(secName)
		sec := sections[key]
		if sec == nil {
			sec = &rawSection{
				display:   key,
				membLanes: make(map[common.ColumnRoleE]string, 2),
				membCards: make(map[common.ColumnRoleE]string, 2),
			}
			sec.coGroup, err = conv.ExtractCoSectionGroup(phy)
			if err != nil {
				return
			}
			sections[key] = sec
			order = append(order, key)
		}
		var role common.ColumnRoleE
		role, err = conv.ExtractColumnRole(phy)
		if err != nil {
			err = eb.Build().Str("column", columnNames[i]).Errorf("unable to classify: %w", err)
			return
		}
		name := columnNames[i]
		switch {
		case role == common.ColumnRoleValue:
			var ct canonicaltypes.PrimitiveAstNodeI
			ct, err = conv.ExtractCanonicalType(phy)
			if err != nil {
				err = eb.Build().Str("column", name).Errorf("unable to classify: %w", err)
				return
			}
			var mod canonicaltypes.ScalarModifierE
			mod, err = common.ExtractScalarModifier(ct)
			if err != nil {
				return
			}
			switch mod {
			case canonicaltypes.ScalarModifierHomogenousArray:
				sec.arrayVals = append(sec.arrayVals, name)
			case canonicaltypes.ScalarModifierSet:
				sec.setVals = append(sec.setVals, name)
			default:
				sec.scalarVals = append(sec.scalarVals, name)
			}
		case role == common.ColumnRoleLength:
			sec.lenLane = name
		case role == common.ColumnRoleCardinality:
			sec.cardLane = name
		case role == common.ColumnRoleCusumLength, role == common.ColumnRoleCusumCardinality:
			// materialized cumulative companions; not audited
		case strings.HasSuffix(string(role), "card"):
			sec.membCards[common.ColumnRoleE(strings.TrimSuffix(string(role), "card"))] = name
		default:
			sec.membLanes[role] = name
		}
	}

	coGroups := make(map[naming.Key][]*auditLanes, 4)
	queries = make([]AuditQuery, 0, len(order)*3)
	for _, key := range order {
		sec := sections[key]
		lanes := auditLanes{display: sec.display, coGroup: sec.coGroup}

		lanes.iLanes = append(lanes.iLanes, sec.scalarVals...)
		if sec.lenLane != "" {
			lanes.iLanes = append(lanes.iLanes, sec.lenLane)
			lanes.positive = append(lanes.positive, sec.lenLane)
			for _, v := range sec.arrayVals {
				lanes.raggedSums = append(lanes.raggedSums, [2]string{sec.lenLane, v})
			}
		}
		if sec.cardLane != "" {
			lanes.iLanes = append(lanes.iLanes, sec.cardLane)
			lanes.positive = append(lanes.positive, sec.cardLane)
			for _, v := range sec.setVals {
				lanes.raggedSums = append(lanes.raggedSums, [2]string{sec.cardLane, v})
			}
		}
		for role, lane := range sec.membLanes {
			base := role
			if partner, isParam := paramPartner[role]; isParam {
				base = partner
			}
			if cardLane, repeating := sec.membCards[base]; repeating {
				lanes.raggedSums = append(lanes.raggedSums, [2]string{cardLane, lane})
			} else {
				// no cardinality lane: membership cardinality ≡ 1, the lane
				// sits on the instance axis
				lanes.iLanes = append(lanes.iLanes, lane)
			}
		}
		for _, cardLane := range sec.membCards {
			lanes.iLanes = append(lanes.iLanes, cardLane)
			lanes.positive = append(lanes.positive, cardLane)
		}

		if len(lanes.iLanes) >= 2 {
			terms := make([]string, 0, len(lanes.iLanes)-1)
			first := quoteAudit(lanes.iLanes[0])
			for _, l := range lanes.iLanes[1:] {
				terms = append(terms, "length("+first+") = length("+quoteAudit(l)+")")
			}
			queries = append(queries, violationQuery(sec.display+": co-length", tableName, terms))
		}
		if len(lanes.raggedSums) > 0 {
			terms := make([]string, 0, len(lanes.raggedSums))
			for _, p := range lanes.raggedSums {
				terms = append(terms, "arraySum("+quoteAudit(p[0])+") = length("+quoteAudit(p[1])+")")
			}
			queries = append(queries, violationQuery(sec.display+": ragged-sum", tableName, terms))
		}
		if len(lanes.positive) > 0 {
			terms := make([]string, 0, len(lanes.positive))
			for _, l := range lanes.positive {
				terms = append(terms, "arrayAll(c -> c >= 1, "+quoteAudit(l)+")")
			}
			queries = append(queries, violationQuery(sec.display+": positivity", tableName, terms))
		}
		if lanes.coGroup != "" && len(lanes.iLanes) > 0 {
			coGroups[lanes.coGroup] = append(coGroups[lanes.coGroup], &lanes)
		}
	}

	for key, members := range coGroups {
		if len(members) < 2 {
			continue // the static shape check flags dangling halves
		}
		terms := make([]string, 0, len(members)-1)
		first := quoteAudit(members[0].iLanes[0])
		for _, m := range members[1:] {
			terms = append(terms, "length("+first+") = length("+quoteAudit(m.iLanes[0])+")")
		}
		queries = append(queries, violationQuery("co-group "+key.String()+": co-length", tableName, terms))
	}
	return
}

// quoteAudit double-quotes a physical column name; validated name components
// cannot contain a double quote.
func quoteAudit(name string) string {
	return `"` + name + `"`
}

func violationQuery(name string, tableName string, terms []string) AuditQuery {
	return AuditQuery{
		Name: name,
		SQL:  "SELECT count() AS violations FROM " + tableName + " WHERE NOT (" + strings.Join(terms, " AND ") + ")",
	}
}
