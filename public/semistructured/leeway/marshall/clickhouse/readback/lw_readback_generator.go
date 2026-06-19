package readback

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Artefacts are the ClickHouse read-back fragments generated for one DTO kind
// (a mappingplan.Plan):
//
//   - Presence — a boolean expression over the physical columns: a cheap
//     necessary-but-not-sufficient prefilter (no false negatives). Its
//     has/hasAll terms are the only index-eligible part of the artefacts:
//     ClickHouse prunes granules for them through a bloom_filter skip index,
//     which it never does for the validator's countEqual (verified on 26.5).
//   - Projection — a CAST to a named Tuple extracting every field, so a
//     downstream UDF can address slots by name (t.<GoFieldName>). Embed in SELECT.
//   - Validator — a boolean expression: the exact conformance check. It is
//     semantically complete on its own but index-blind; embedded without the
//     Presence terms it forces a full scan.
//   - Filter — Presence AND Validator, the form to embed in WHERE: still the
//     exact check, and the redundant-looking Presence conjuncts are what carry
//     skip-index pruning.
//
// All fragments reference the leeway DQL helper UDFs (HelperUDFsSQL). See
// ADR-0066 and EXPLANATION.md.
type Artefacts struct {
	Kind       string
	Presence   string
	Projection string
	Validator  string
	Filter     string
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

// Generate emits the artefacts for plan. Plain columns project directly and
// are assumed present; tagged fields are located by membership. Mandatory
// fields (non-Option) contribute a presence literal and a "exactly once"
// validator term; Option fields contribute only an "at most once" term; const
// fields additionally pin the value — and, on scalar string value columns,
// contribute the pinned value as a second presence literal. Presence literals
// are deduplicated and grouped per physical column — has(col, lit) for one
// literal, hasAll(col, [lits...]) for several — so each column costs one array
// scan and one skip-index condition; validator terms are deduplicated so
// multi-sub-column sections (one membership, several value columns) count the
// membership once.
func (g *Generator) Generate(plan *mappingplan.Plan) (a Artefacts, err error) {
	a.Kind = plan.KindName
	if err = g.validate(plan); err != nil {
		return
	}

	var exprs, slotTypes []string
	presence := newPresenceSet()
	validator := newTermSet()

	addSlot := func(name, expr string, ct canonicaltypes.PrimitiveAstNodeI, nullable bool) error {
		chType, terr := g.chType(ct)
		if terr != nil {
			return eb.Build().Str("slot", name).Errorf("unable to render ClickHouse type: %w", terr)
		}
		if nullable {
			chType = "Nullable(" + chType + ")"
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
		if err = addSlot(pc.GoField, pi.col, pi.canonicalType, false); err != nil {
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
			if err = addSlot(f.GoFieldName, fa.valExpr, fa.canonicalType, fa.nullableSlot); err != nil {
				return
			}
		}
		presence.add(fa.presence)
		validator.add(fa.validator)
	}

	a.Projection = "CAST(tuple(" + strings.Join(exprs, ", ") + "), " + marshalling.EscapeString("Tuple("+strings.Join(slotTypes, ", ")+")") + ")"
	presenceTerms := presence.terms()
	a.Presence = joinAnd(presenceTerms)
	a.Validator = joinAnd(validator.terms)
	a.Filter = joinAnd(slices.Concat(presenceTerms, validator.terms))
	return
}

// validate reports the first plain column or tagged-field column the Plan
// references that the schema lacks — the conformance subset of Generate, with
// no SQL emission and no membership resolution.
func (g *Generator) validate(plan *mappingplan.Plan) (err error) {
	for _, pc := range plan.PlainCols {
		if _, ok := g.plain[pc.Column]; !ok {
			err = eb.Build().Str("plainColumn", pc.Column).Str("kind", plan.KindName).Errorf("plain column not found in schema")
			return
		}
	}
	for i := range plan.Fields {
		f := &plan.Fields[i]
		if _, lerr := g.locate(f); lerr != nil {
			err = eb.Build().Str("field", f.GoFieldName).Str("section", f.LWSection).Str("membership", f.LWMembership).Errorf("plan does not conform to schema: %w", lerr)
			return
		}
	}
	return
}

// ValidatePlanAgainstIR reports whether every plain column, section, value
// sub-column, and per-channel membership support column the Plan references
// exists in the schema loaded into ir — the conformance check the readback
// generator runs before emitting SQL, exposed so a consumer can verify a DTO
// Plan against a schema at plan-build time without generating ClickHouse
// artefacts. It resolves no membership ids (needs no MembershipResolver).
func ValidatePlanAgainstIR(plan *mappingplan.Plan, ir *InformationRetrieval) error {
	return NewGenerator(ir, nil).validate(plan)
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
	nullableSlot  bool           // project as Nullable(T): scalar Option fields
	presence      []presenceTerm // empty for Option fields
	validator     string
}

// presenceTerm is one necessary-condition literal on a physical column;
// presenceSet groups terms per column into a has/hasAll expression.
type presenceTerm struct {
	col string
	lit string
}

// fieldLocators are the physical columns and channel spec a tagged field
// resolves to in the schema. locate produces them (reporting any column the
// Plan references but the schema lacks); field consumes them to build SQL,
// validate to check existence alone.
type fieldLocators struct {
	vinfo      colInfo
	spec       common.MembershipSpecE
	idCol      string
	cardCol    string
	subtypeCol string // length (homogenous array) / cardinality (set) support col; "" for scalar
}

// locate resolves a tagged field to its physical columns, erroring on the first
// one the schema lacks. Shared by field (Generate) and validate
// (ValidatePlanAgainstIR) so the conformance rules cannot drift between them.
func (g *Generator) locate(f *mappingplan.TaggedField) (loc fieldLocators, err error) {
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
	loc.vinfo = vinfo

	loc.spec, err = channelSpec(f.Flags.Channel)
	if err != nil {
		return
	}
	roles, err := membershipRoles(loc.spec)
	if err != nil {
		return
	}
	loc.idCol, ok = g.support[sec][roles.identity]
	if !ok {
		err = eb.Build().Str("section", sec).Stringer("role", roles.identity).Errorf("membership column not found in schema")
		return
	}
	loc.cardCol, ok = g.support[sec][roles.card]
	if !ok {
		err = eb.Build().Str("section", sec).Stringer("role", roles.card).Errorf("membership cardinality column not found in schema")
		return
	}

	switch vinfo.subType {
	case common.IntermediateColumnsSubTypeScalar:
		// no extra support column
	case common.IntermediateColumnsSubTypeHomogenousArray:
		loc.subtypeCol, ok = g.support[sec][common.ColumnRoleLength]
		if !ok {
			err = eb.Build().Str("section", sec).Errorf("homogenous-array length support column not found in schema")
			return
		}
	case common.IntermediateColumnsSubTypeSet:
		loc.subtypeCol, ok = g.support[sec][common.ColumnRoleCardinality]
		if !ok {
			err = eb.Build().Str("section", sec).Errorf("set cardinality support column not found in schema")
			return
		}
	default:
		err = eb.Build().Stringer("subType", vinfo.subType).Str("section", sec).Errorf("unsupported value subtype")
		return
	}
	return
}

func (g *Generator) field(f *mappingplan.TaggedField) (res fieldArtefacts, err error) {
	loc, err := g.locate(f)
	if err != nil {
		return
	}
	sec := f.LWSection
	vinfo := loc.vinfo
	idCol := loc.idCol
	res.canonicalType = vinfo.canonicalType

	resolved, err := g.resolver.Resolve(f.LWMembership, loc.spec)
	if err != nil {
		return
	}
	lit := resolved.Identity().Literal
	m2v := "LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(" + loc.cardCol + ")"

	var valExpr string
	switch vinfo.subType {
	case common.IntermediateColumnsSubTypeScalar:
		valExpr = "LEEWAY_VALUE_BY_TAG_EQUAL(" + vinfo.col + ", " + idCol + ", " + lit + ", " + m2v + ")"
	case common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
		valExpr = "LEEWAY_LIST_BY_TAG_EQUAL(" + vinfo.col + ", " + loc.subtypeCol + ", " + idCol + ", " + lit + ", " + m2v + ")"
	}

	countExpr := "countEqual(" + idCol + ", " + lit + ")"

	switch {
	case f.IsConst:
		// Fixed value: the membership is present exactly once and carries the
		// constant. Const fields are validation-only (no projected slot).
		//
		// ConstValue is a single string and the write path marshals it through
		// the scalar lane, so the value-equality check is a scalar comparison.
		// On a non-scalar value column valExpr is an array (LEEWAY_LIST_BY_TAG)
		// and `array = 'const'` is a query-time CANNOT_READ_ARRAY_FROM_TEXT —
		// reject at generation rather than emit SQL that fails when run. (The
		// tag parser admits const on array string sections, but it has no
		// well-defined read-back semantics; revisit with a write+read design.)
		if vinfo.subType != common.IntermediateColumnsSubTypeScalar {
			err = eb.Build().Str("section", sec).Stringer("subType", vinfo.subType).Errorf("const fields are only supported on scalar value sections")
			return
		}
		constLit := marshalling.EscapeString(f.ConstValue)
		res.presence = []presenceTerm{{col: idCol, lit: lit}}
		if _, isString := vinfo.canonicalType.(canonicaltypes.StringAstNode); isString && vinfo.subType == common.IntermediateColumnsSubTypeScalar {
			// The pinned value must occur somewhere in the value column — a
			// second necessary condition, skip-index-eligible there. String
			// columns only: has() does not coerce a string literal to a
			// numeric array (NO_COMMON_TYPE), unlike the validator's equality.
			res.presence = append(res.presence, presenceTerm{col: vinfo.col, lit: constLit})
		}
		res.validator = countExpr + " = 1 AND " + valExpr + " = " + constLit
	case f.IsOption:
		// Optional: no presence requirement; if present, the membership must
		// identify a single attribute. A scalar Option projects as
		// Nullable(T) returning NULL when the membership is absent, so an
		// absent optional is distinguishable from one present with the type
		// default (ADR-0066 decision 4). Array/set Options cannot: ClickHouse
		// forbids Nullable(Array(...)), so they keep the empty-array sentinel
		// — absent and present-empty are indistinguishable there (v1 concern).
		if vinfo.subType == common.IntermediateColumnsSubTypeScalar {
			res.valExpr = "if(has(" + idCol + ", " + lit + "), " + valExpr + ", NULL)"
			res.nullableSlot = true
		} else {
			res.valExpr = valExpr
		}
		res.validator = countExpr + " <= 1"
	default:
		res.valExpr = valExpr
		res.presence = []presenceTerm{{col: idCol, lit: lit}}
		res.validator = countExpr + " = 1"
	}
	return
}

// presenceSet collects presence literals, dropping duplicates while preserving
// order, grouped per physical column: a column with one literal emits
// has(col, lit), one with several emits hasAll(col, [lits...]). Both forms can
// prune granules through a bloom_filter skip index (countEqual/indexOf cannot),
// and grouping costs one array scan and one index condition per column instead
// of one per literal.
type presenceSet struct {
	seen map[presenceTerm]struct{}
	cols []string            // first-seen column order
	lits map[string][]string // column -> literals, first-seen order
}

func newPresenceSet() *presenceSet {
	return &presenceSet{seen: make(map[presenceTerm]struct{}, 8), lits: make(map[string][]string, 8)}
}

func (s *presenceSet) add(terms []presenceTerm) {
	for _, t := range terms {
		if _, ok := s.seen[t]; ok {
			continue
		}
		s.seen[t] = struct{}{}
		if _, ok := s.lits[t.col]; !ok {
			s.cols = append(s.cols, t.col)
		}
		s.lits[t.col] = append(s.lits[t.col], t.lit)
	}
}

func (s *presenceSet) terms() []string {
	out := make([]string, 0, len(s.cols))
	for _, col := range s.cols {
		lits := s.lits[col]
		if len(lits) == 1 {
			out = append(out, "has("+col+", "+lits[0]+")")
		} else {
			out = append(out, "hasAll("+col+", ["+strings.Join(lits, ", ")+"])")
		}
	}
	return out
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
