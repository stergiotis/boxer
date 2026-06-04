package dql

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Artefacts are the three ClickHouse read-back fragments generated for one DTO
// kind (a mappingplan.Plan):
//
//   - Presence — a boolean expression over the physical columns: a cheap
//     necessary-but-not-sufficient prefilter (no false negatives). Embed in WHERE.
//   - Projection — a CAST to a named Tuple extracting every field, so a
//     downstream UDF can address slots by name (t.<GoFieldName>). Embed in SELECT.
//   - Validator — a boolean expression: the exact conformance check. Embed in WHERE.
//
// All three reference the leeway DQL helper UDFs (HelperUDFsSQL). See ADR-0066
// and EXPLANATION.md.
type Artefacts struct {
	Kind       string
	Presence   string
	Projection string
	Validator  string
}

// Generator turns a mappingplan.Plan into Artefacts by joining the Plan's
// logical fields against the physical schema (an InformationRetrieval already
// loaded with the kind's table) and resolving membership identities through a
// MembershipResolver.
type Generator struct {
	resolver MembershipResolver
	tech     *clickhouse.TechnologySpecificCodeGenerator // canonical type -> ClickHouse type

	plain   map[string]colInfo                       // plain item name -> column
	value   map[string]map[string]colInfo            // section -> sub-column -> value column
	support map[string]map[common.ColumnRoleE]string // section -> role -> escaped physical column
}

type colInfo struct {
	col           string // escaped physical column name
	subType       common.IntermediateColumnSubTypeE
	canonicalType canonicaltypes.PrimitiveAstNodeI
}

// NewGenerator indexes the physical columns of a loaded InformationRetrieval so
// each Plan field can be resolved to its value / membership / support columns.
func NewGenerator(ir *InformationRetrieval, resolver MembershipResolver) *Generator {
	g := &Generator{
		resolver: resolver,
		tech:     clickhouse.NewTechnologySpecificCodeGenerator(),
		plain:    make(map[string]colInfo, 8),
		value:    make(map[string]map[string]colInfo, 16),
		support:  make(map[string]map[common.ColumnRoleE]string, 16),
	}
	for r := range ir.IterateAll() {
		cc := r.ColumnContext
		info := colInfo{col: marshalling.EscapeIdentifier(r.PhysicalColumn.String()), subType: cc.SubType, canonicalType: r.CanonicalType}
		if cc.PlainItemType != common.PlainItemTypeNone {
			g.plain[string(r.Name)] = info
			continue
		}
		sec := string(cc.SectionName)
		if r.Role == common.ColumnRoleValue {
			if g.value[sec] == nil {
				g.value[sec] = make(map[string]colInfo, 4)
			}
			g.value[sec][string(r.Name)] = info
		} else {
			if g.support[sec] == nil {
				g.support[sec] = make(map[common.ColumnRoleE]string, 8)
			}
			g.support[sec][r.Role] = info.col
		}
	}
	return g
}

// Generate emits the three artefacts for plan. Plain columns project directly
// and are assumed present; tagged fields are located by membership. Mandatory
// fields (non-Option) contribute a presence term and a "exactly once"
// validator term; Option fields contribute only an "at most once" term; const
// fields additionally pin the value. Presence/validator terms are deduplicated
// so multi-sub-column sections (one membership, several value columns) count
// the membership once.
func (g *Generator) Generate(plan *mappingplan.Plan) (a Artefacts, err error) {
	a.Kind = plan.KindName

	var exprs, slotTypes []string
	presence := newTermSet()
	validator := newTermSet()

	addSlot := func(name, expr string, ct canonicaltypes.PrimitiveAstNodeI) error {
		chType, terr := g.chType(ct)
		if terr != nil {
			return eb.Build().Str("slot", name).Errorf("unable to render ClickHouse type: %w", terr)
		}
		exprs = append(exprs, expr)
		slotTypes = append(slotTypes, slotName(name)+" "+chType)
		return nil
	}

	for _, pc := range plan.PlainCols {
		pi, ok := g.plain[pc.Column]
		if !ok {
			err = eb.Build().Str("plainColumn", pc.Column).Str("kind", plan.KindName).Errorf("plain column not found in schema")
			return
		}
		if err = addSlot(pc.GoField, pi.col, pi.canonicalType); err != nil {
			return
		}
	}

	for i := range plan.Fields {
		f := &plan.Fields[i]
		fa, ferr := g.field(f)
		if ferr != nil {
			err = eb.Build().Str("field", f.GoFieldName).Str("section", f.LWSection).Str("membership", f.LWMembership).Errorf("unable to generate field: %w", ferr)
			return
		}
		if fa.valExpr != "" {
			if err = addSlot(f.GoFieldName, fa.valExpr, fa.canonicalType); err != nil {
				return
			}
		}
		presence.add(fa.presence)
		validator.add(fa.validator)
	}

	a.Projection = "CAST(tuple(" + strings.Join(exprs, ", ") + "), " + marshalling.EscapeString("Tuple("+strings.Join(slotTypes, ", ")+")") + ")"
	a.Presence = joinAnd(presence.terms)
	a.Validator = joinAnd(validator.terms)
	return
}

// chType renders a canonical type as its ClickHouse type via the ddl/clickhouse
// generator (e.g. String, UInt64, Array(UInt64), DateTime64(9,'UTC')).
func (g *Generator) chType(ct canonicaltypes.PrimitiveAstNodeI) (string, error) {
	b := &strings.Builder{}
	g.tech.SetCodeBuilder(b)
	if err := g.tech.GenerateType(ct); err != nil {
		return "", eh.Errorf("GenerateType: %w", err)
	}
	return b.String(), nil
}

type fieldArtefacts struct {
	valExpr       string // "" for const fields (validation-only)
	canonicalType canonicaltypes.PrimitiveAstNodeI
	presence      string // "" for Option fields
	validator     string
}

func (g *Generator) field(f *mappingplan.TaggedField) (res fieldArtefacts, err error) {
	sec := f.LWSection
	subCol := f.LWColumn
	if subCol == "" {
		subCol = "value"
	}
	vinfo, ok := g.value[sec][subCol]
	if !ok {
		err = eb.Build().Str("section", sec).Str("subColumn", subCol).Errorf("value column not found in schema")
		return
	}
	res.canonicalType = vinfo.canonicalType

	spec, err := channelSpec(f.Flags.Channel)
	if err != nil {
		return
	}
	roles, err := membershipRoles(spec)
	if err != nil {
		return
	}
	idCol, ok := g.support[sec][roles.identity]
	if !ok {
		err = eb.Build().Str("section", sec).Stringer("role", roles.identity).Errorf("membership column not found in schema")
		return
	}
	cardCol, ok := g.support[sec][roles.card]
	if !ok {
		err = eb.Build().Str("section", sec).Stringer("role", roles.card).Errorf("membership cardinality column not found in schema")
		return
	}

	resolved, err := g.resolver.Resolve(f.LWMembership, spec)
	if err != nil {
		return
	}
	lit := resolved.Identity().Literal
	m2v := "LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(" + cardCol + ")"

	var valExpr string
	switch vinfo.subType {
	case common.IntermediateColumnsSubTypeScalar:
		valExpr = "LEEWAY_VALUE_BY_TAG_EQUAL(" + vinfo.col + ", " + idCol + ", " + lit + ", " + m2v + ")"
	case common.IntermediateColumnsSubTypeHomogenousArray:
		lenCol, ok := g.support[sec][common.ColumnRoleLength]
		if !ok {
			err = eb.Build().Str("section", sec).Errorf("homogenous-array length support column not found in schema")
			return
		}
		valExpr = "LEEWAY_LIST_BY_TAG_EQUAL(" + vinfo.col + ", " + lenCol + ", " + idCol + ", " + lit + ", " + m2v + ")"
	case common.IntermediateColumnsSubTypeSet:
		setCardCol, ok := g.support[sec][common.ColumnRoleCardinality]
		if !ok {
			err = eb.Build().Str("section", sec).Errorf("set cardinality support column not found in schema")
			return
		}
		valExpr = "LEEWAY_LIST_BY_TAG_EQUAL(" + vinfo.col + ", " + setCardCol + ", " + idCol + ", " + lit + ", " + m2v + ")"
	default:
		err = eb.Build().Stringer("subType", vinfo.subType).Str("section", sec).Errorf("unsupported value subtype")
		return
	}

	hasExpr := "has(" + idCol + ", " + lit + ")"
	countExpr := "countEqual(" + idCol + ", " + lit + ")"

	switch {
	case f.IsConst:
		// Fixed value: the membership is present exactly once and carries the
		// constant. Const fields are validation-only (no projected slot).
		res.presence = hasExpr
		res.validator = countExpr + " = 1 AND " + valExpr + " = " + marshalling.EscapeString(f.ConstValue)
	case f.IsOption:
		// Optional: no presence requirement; if present, the membership must
		// identify a single attribute.
		res.valExpr = valExpr
		res.validator = countExpr + " <= 1"
	default:
		res.valExpr = valExpr
		res.presence = hasExpr
		res.validator = countExpr + " = 1"
	}
	return
}

// termSet collects boolean terms, dropping duplicates while preserving order.
type termSet struct {
	seen  map[string]struct{}
	terms []string
}

func newTermSet() *termSet { return &termSet{seen: make(map[string]struct{}, 8)} }

func (s *termSet) add(term string) {
	if term == "" {
		return
	}
	if _, ok := s.seen[term]; ok {
		return
	}
	s.seen[term] = struct{}{}
	s.terms = append(s.terms, term)
}

func joinAnd(terms []string) string {
	if len(terms) == 0 {
		return "1"
	}
	return strings.Join(terms, " AND ")
}

func slotName(goField string) string {
	if goField == "" {
		return "_"
	}
	return goField
}
