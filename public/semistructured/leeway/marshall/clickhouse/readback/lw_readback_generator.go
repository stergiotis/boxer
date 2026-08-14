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
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
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
// Presence and the non-const Validator / Filter use ClickHouse built-ins only
// (has / hasAll / countEqual) — the property that lets a single-statement
// executor embed them with no UDF install (ADR-0100 S2). Only the Projection,
// and a Validator carrying const fields, reference the leeway DQL helper UDFs
// (HelperUDFsSQL). See ADR-0066 and EXPLANATION.md.
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
//
// Every artefact assumes the flat one-attribute-per-membership-per-row shape,
// so a Plan carrying a tuple / nested attribute section is rejected rather than
// mis-generated (see locate).
func (g *Generator) Generate(plan *mappingplan.Plan) (a Artefacts, err error) {
	a.Kind = plan.KindName
	if err = g.validate(plan); err != nil {
		return
	}

	var exprs, slotTypes []string
	presence := newPresenceSet()
	optPresence := newPresenceSet()
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

	// The read contract is the single statement of how many attributes each
	// slot may carry (ADR-0146 D1); Presence and Validator are its two halves
	// rendered as SQL, rather than a second copy of the rule.
	contract, err := mappingplan.DeriveReadContract(plan)
	if err != nil {
		return
	}

	for i := range plan.Fields {
		f := &plan.Fields[i]
		fa, ferr := g.field(f, contract)
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
		optPresence.add(fa.optPresence)
		validator.add(fa.validator)
	}

	a.Projection = "CAST(tuple(" + strings.Join(exprs, ", ") + "), " + marshalling.EscapeString("Tuple("+strings.Join(slotTypes, ", ")+")") + ")"
	presenceTerms := presence.terms()
	if len(presenceTerms) == 0 {
		// No slot is required, so no conjunction of literals can say the row
		// carries this kind. The necessary condition is then the DISJUNCTION —
		// at least one of the kind's slots is populated — which is exactly what
		// mappingplan.ReadContract.Verdict calls `populated`, and what stops a
		// container-only kind's Filter from matching every row. When some slot
		// IS required the conjunction implies the disjunction, so it is omitted.
		if anyTerms := optPresence.anyTerms(); len(anyTerms) > 0 {
			presenceTerms = []string{joinOr(anyTerms)}
		}
	}
	a.Presence = joinAnd(presenceTerms)
	a.Validator = joinAnd(validator.terms)
	a.Filter = joinAnd(slices.Concat(presenceTerms, validator.terms))
	return
}

// newValidator returns a Generator usable for validate only — it carries no
// MembershipResolver, so calling Generate on it would fail at the first field.
// Kept private so the resolver-less form cannot escape the package.
func newValidator(ir *InformationRetrieval) *Generator {
	return NewGenerator(ir, nil)
}

// validate reports the first plain column or tagged-field column the Plan
// references that the schema lacks, or the first field whose shape the
// generator cannot express — the conformance subset of Generate, with no SQL
// emission and no membership resolution.
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
// exists in the schema loaded into ir, and whether every field is a shape the
// generator can express (see locate) — the conformance check the readback
// generator runs before emitting SQL, exposed so a consumer can verify a DTO
// Plan against a schema at plan-build time without generating ClickHouse
// artefacts. It resolves no membership ids (needs no MembershipResolver).
func ValidatePlanAgainstIR(plan *mappingplan.Plan, ir *InformationRetrieval) error {
	return newValidator(ir).validate(plan)
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
	presence      []presenceTerm // the slot must be populated; empty for a slot that may carry zero attributes
	optPresence   []presenceTerm // the slot MAY be populated — used only to build the "carries anything at all" disjunction
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

// lanes renders the locators as lwextract's view of them.
//
// Card is always populated: locate errors when the schema lacks the
// cardinality column, so this generator never takes lwextract's
// one-membership-per-attribute fast path. That is deliberate — a Plan
// reaching a schema without the column is a conformance failure worth
// reporting, not a shape to silently optimise.
func (inst fieldLocators) lanes() lwextract.Lanes {
	return lwextract.Lanes{
		Value:  inst.vinfo.col,
		Ident:  inst.idCol,
		Card:   inst.cardCol,
		Length: inst.subtypeCol,
	}
}

// shape maps the value column's IR subtype onto lwextract's. locate has
// already rejected every other subtype.
func (inst fieldLocators) shape() lwextract.ShapeE {
	if inst.vinfo.subType == common.IntermediateColumnsSubTypeScalar {
		return lwextract.ShapeScalar
	}
	return lwextract.ShapeList
}

// locate resolves a tagged field to its physical columns, erroring on the first
// one the schema lacks — and on the field shapes the generator cannot express
// at all. Shared by field (Generate) and validate (ValidatePlanAgainstIR) so
// the conformance rules cannot drift between them.
func (g *Generator) locate(f *mappingplan.TaggedField) (loc fieldLocators, err error) {
	if f.TupleField != "" {
		// A tuple-family section (a dynamic-membership tuple, ADR-0103/0109, or
		// a nested attribute section, ADR-0113) maps MANY attributes per row
		// through one Go field. Every artefact below assumes the flat
		// one-attribute-per-membership shape: the validator pins
		// `countEqual(...) = 1` and the projection extracts a single value slot.
		// Emitting them for a tuple is silently wrong rather than merely
		// incomplete — a dynamic tuple's per-element memberships are not on the
		// TaggedField at all (LWMembership is ""), so a verbatim channel would
		// resolve to the empty literal and match nothing, and a static nested
		// `[]S` would assert exactly one attribute for an N-attribute section.
		// Reject here so both Generate and ValidatePlanAgainstIR say so.
		err = eb.Build().Str("tupleField", f.TupleField).Str("section", f.LWSection).Errorf("tuple / nested attribute sections are not supported by the read-back generator — it maps one attribute per membership per row")
		return
	}
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

func (g *Generator) field(f *mappingplan.TaggedField, contract mappingplan.ReadContract) (res fieldArtefacts, err error) {
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

	// A `,unit` field is a container column carrying exactly ONE element per
	// attribute, authored and read back as the scalar element type (the
	// BeginAttributeSingle shape). The physical column stays an array, so the
	// located list is indexed to its single element and the slot's canonical is
	// demoted to the element's. Without this the projection emits Array(T) for
	// a T DTO field — a slot that does not round-trip the field it is named
	// after. On a scalar value column `,unit` is already the projected shape,
	// so it is a no-op there.
	unit := f.Flags.Unit && vinfo.subType != common.IntermediateColumnsSubTypeScalar
	if unit {
		res.canonicalType = canonicaltypes.DemoteToScalarPrim(vinfo.canonicalType)
	}

	// The expression itself is lwextract's (ADR-0181 §SD3): one builder, so
	// this generator and the LW_GET family cannot drift on what "locate the
	// attribute tagged X" means. Card is always populated here — locate
	// requires the column — so the general helper form is what renders, and
	// the golden tests pin that it is byte-identical to what this emitted
	// when the strings lived inline.
	lanes := loc.lanes()
	valExpr, err := lwextract.Value(lwextract.Request{
		Lanes:      lanes,
		Shape:      loc.shape(),
		Membership: lit,
		Unit:       unit,
	})
	if err != nil {
		return
	}
	// Whether the slot this field projects is a scalar — true for a scalar
	// value column and for the indexed `,unit` shape above. Drives the Option
	// treatment below: only a scalar slot can carry Nullable(T).
	projectsScalar := vinfo.subType == common.IntermediateColumnsSubTypeScalar || unit

	countExpr := lwextract.CountEqual(lanes, lit)

	switch {
	case f.IsConst:
		// Fixed value: the membership is present exactly once and carries the
		// constant. Const fields are validation-only (no projected slot).
		//
		// ConstValue is a single string and the write path marshals it through
		// the scalar lane, so the value-equality check is a scalar comparison.
		// On a non-scalar value column valExpr is an array (LW_LIST_BY_TAG_EQUAL)
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
	case !slotRequired(contract, f):
		// The slot may legitimately carry zero attributes, so it contributes no
		// presence requirement; if present, the membership must identify a
		// single attribute. Two shapes reach here:
		//
		//   - Option[T], absent when Has=false.
		//   - a CONTAINER ([]T / *roaring.Bitmap), which the write path splices
		//     to zero attributes when empty (marshalContainer). Treating it as
		//     mandatory — the pre-ADR-0146 behaviour — made a row with a
		//     legitimately empty container fail the Presence and Validator its
		//     own kind generates, while both Go read paths accepted it.
		//
		// A scalar Option projects as Nullable(T) returning NULL when the
		// membership is absent, so an absent optional is distinguishable from
		// one present with the type default (ADR-0066 decision 4). Array/set
		// slots cannot: ClickHouse forbids Nullable(Array(...)), so they keep
		// the empty-array sentinel. For a container that is not a limitation —
		// absent and present-empty are the same thing on the write side.
		// A `,unit` Option projects a scalar slot, so it gets the Nullable
		// treatment even though its value column is an array.
		if projectsScalar {
			res.valExpr = lwextract.NullWhenAbsent(valExpr, lanes, lit)
			res.nullableSlot = true
		} else {
			res.valExpr = valExpr
		}
		res.optPresence = []presenceTerm{{col: idCol, lit: lit}}
		res.validator = countExpr + " <= 1"
	default:
		res.valExpr = valExpr
		res.presence = []presenceTerm{{col: idCol, lit: lit}}
		res.validator = countExpr + " = 1"
	}
	return
}

// slotRequired reports whether the contract requires the field's slot to be
// populated. A Plan the contract cannot describe (a hand-built one, or a shape
// DeriveReadContract does not model) falls back to the field's own shape, which
// is the same rule the contract applies.
func slotRequired(contract mappingplan.ReadContract, f *mappingplan.TaggedField) bool {
	if slot, ok := contract.Slot(f.LWSection, f.LWMembership); ok {
		return slot.Required()
	}
	return !f.IsOption && !f.IsMulti()
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

// anyTerms renders the set as one index-eligible expression per column with OR
// semantics — has(col, lit) for a single literal, hasAny(col, [lits...]) for
// several. The caller ORs the per-column results together.
func (s *presenceSet) anyTerms() []string {
	out := make([]string, 0, len(s.cols))
	for _, col := range s.cols {
		lits := s.lits[col]
		if len(lits) == 1 {
			out = append(out, "has("+col+", "+lits[0]+")")
		} else {
			out = append(out, "hasAny("+col+", ["+strings.Join(lits, ", ")+"])")
		}
	}
	return out
}

// joinOr ORs terms into one parenthesised expression, so the result composes
// with joinAnd without changing precedence.
func joinOr(terms []string) string {
	if len(terms) == 0 {
		return "1"
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return "(" + strings.Join(terms, " OR ") + ")"
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
