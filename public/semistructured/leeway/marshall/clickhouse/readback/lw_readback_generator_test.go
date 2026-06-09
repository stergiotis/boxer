package readback

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// mapLookup is a tiny IdLookup for tests: membership name -> ref id.
type mapLookup map[string]uint64

func (m mapLookup) LookupMembership(name string) (uint64, error) {
	id, ok := m[name]
	if !ok {
		return 0, eh.Errorf("no id registered for membership %q", name)
	}
	return id, nil
}

// buildTestIR builds a minimal leeway schema (plain id/naturalKey; a scalar
// `symbol` section; a homogenous-array `u64Array` section; a two-sub-column
// scalar `timeRange` section), all carrying lr/lv/hr channels, and returns it
// loaded into an InformationRetrieval. Built inline to avoid importing the
// heavy anchor package.
func buildTestIR(t *testing.T) *InformationRetrieval {
	t.Helper()
	manip, err := common.NewTableManipulator()
	if err != nil {
		t.Fatalf("NewTableManipulator: %v", err)
	}
	manip.SetTableName("test")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectDeltaEncoding, easp.AspectLightGeneralCompression)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "naturalKey", ctabb.Y).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	membership := []common.MembershipSpecE{
		common.MembershipSpecLowCardRef,
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecHighCardRef,
	}
	sym := manip.TaggedValueSection("symbol").SectionStreamingGroup("data").AddSectionMembership(membership...)
	sym.TaggedValueColumn("value", ctabb.S).AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	u64a := manip.TaggedValueSection("u64Array").SectionStreamingGroup("data").AddSectionMembership(membership...)
	u64a.TaggedValueColumn("value", canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	tr := manip.TaggedValueSection("timeRange").SectionStreamingGroup("data").AddSectionMembership(membership...)
	tr.TaggedValueColumn("beginIncl", ctabb.Z64).AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	tr.TaggedValueColumn("endExcl", ctabb.Z64).AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	u32s := manip.TaggedValueSection("u32Set").SectionStreamingGroup("data").AddSectionMembership(membership...)
	u32s.TaggedValueColumn("value", canonicaltypes.PromoteScalarPrim(ctabb.U32, canonicaltypes.ScalarModifierSet)).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	pair := manip.TaggedValueSection("pair").SectionStreamingGroup("data").AddSectionMembership(membership...)
	pair.TaggedValueColumn("lo", ctabb.U64).AddColumnEncodingHints(easp.AspectLightGeneralCompression)
	pair.TaggedValueColumn("hi", ctabb.U64).AddColumnEncodingHints(easp.AspectLightGeneralCompression)

	tblDesc, err := manip.BuildTableDesc()
	if err != nil {
		t.Fatalf("BuildTableDesc: %v", err)
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		t.Fatalf("NewHumanReadableNamingConvention: %v", err)
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tblDesc, tech); err != nil {
		t.Fatalf("LoadFromTable: %v", err)
	}
	info := NewInformationRetrieval(conv)
	if err = info.LoadTable(ir, common.TableRowConfigMultiAttributesPerRow); err != nil {
		t.Fatalf("InformationRetrieval.LoadTable: %v", err)
	}
	return info
}

func TestResolver(t *testing.T) {
	r := NewLookupResolver(mapLookup{"foo": 99})

	res, err := r.Resolve("bar", common.MembershipSpecLowCardVerbatim)
	if err != nil {
		t.Fatalf("verbatim resolve: %v", err)
	}
	if got := res.Identity(); got.Role != common.ColumnRoleLowCardVerbatim || got.Literal != "'bar'" {
		t.Errorf("verbatim: got role=%s literal=%q, want lv/'bar'", got.Role, got.Literal)
	}

	res, err = r.Resolve("foo", common.MembershipSpecLowCardRef)
	if err != nil {
		t.Fatalf("ref resolve: %v", err)
	}
	if got := res.Identity(); got.Role != common.ColumnRoleLowCardRef || got.Literal != "99" {
		t.Errorf("ref: got role=%s literal=%q, want lr/99", got.Role, got.Literal)
	}

	if _, err = r.Resolve("unknown", common.MembershipSpecHighCardRef); err == nil {
		t.Errorf("expected error resolving unknown ref membership")
	}
}

func TestGenerator_Golden(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"myNums": 42, "window": 7}))
	plan := &mappingplan.Plan{
		KindName:  "myDto",
		PlainCols: []mappingplan.PlainCol{{Column: "id", GoField: "Id", Canonical: ctabb.U64}, {Column: "naturalKey", GoField: "NK", Canonical: ctabb.Y}},
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", Canonical: ctabb.S, LWMembership: "mySym", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "Nums", Canonical: canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray), LWMembership: "myNums", LWSection: "u64Array", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "WinBegin", Canonical: ctabb.Z64, LWMembership: "window", LWSection: "timeRange", LWColumn: "beginIncl", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "WinEnd", Canonical: ctabb.Z64, LWMembership: "window", LWSection: "timeRange", LWColumn: "endExcl", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
		},
	}
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.HasPrefix(a.Projection, "CAST(tuple(") {
		t.Errorf("projection should CAST a tuple to a named Tuple:\n%s", a.Projection)
	}
	for _, want := range []string{"Id UInt64", "NK String", "Sym String", "Nums Array(UInt64)", "WinBegin DateTime", "WinEnd DateTime"} {
		if !strings.Contains(a.Projection, want) {
			t.Errorf("projection missing named slot %q:\n%s", want, a.Projection)
		}
	}
	// Sym + WinBegin + WinEnd are scalar; Nums is the only list.
	if n := strings.Count(a.Projection, "LEEWAY_VALUE_BY_TAG_EQUAL("); n != 3 {
		t.Errorf("want 3 scalar extracts, got %d:\n%s", n, a.Projection)
	}
	if n := strings.Count(a.Projection, "LEEWAY_LIST_BY_TAG_EQUAL("); n != 1 {
		t.Errorf("want 1 list extract, got %d:\n%s", n, a.Projection)
	}
	// Three distinct memberships (mySym, myNums, window) — window dedup'd across
	// the two timeRange sub-columns.
	if n := strings.Count(a.Presence, "has("); n != 3 {
		t.Errorf("want 3 presence terms (window dedup'd), got %d:\n%s", n, a.Presence)
	}
	if n := strings.Count(a.Validator, "countEqual("); n != 3 {
		t.Errorf("want 3 validator terms, got %d:\n%s", n, a.Validator)
	}
	if !strings.Contains(a.Presence, "'mySym'") || !strings.Contains(a.Presence, "42") || !strings.Contains(a.Presence, "7") {
		t.Errorf("presence missing a resolved literal:\n%s", a.Presence)
	}
	// Filter is the WHERE embed: presence (index carrier) AND validator (exact).
	if want := a.Presence + " AND " + a.Validator; a.Filter != want {
		t.Errorf("filter = %q, want presence AND validator:\n%s", a.Filter, want)
	}
}

// TestGenerator_FilterArtefact pins the Filter composition at the edges: no
// presence terms (Option-only) degrades to the bare validator, an empty plan
// to the trivial filter.
func TestGenerator_FilterArtefact(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{}))

	optPlan := &mappingplan.Plan{
		KindName: "optOnly",
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", Canonical: ctabb.S, LWMembership: "maybe", LWSection: "symbol", IsOption: true, Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
		},
	}
	a, err := g.Generate(optPlan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.Presence != "1" {
		t.Errorf("Option-only presence = %q, want 1", a.Presence)
	}
	if a.Filter != a.Validator {
		t.Errorf("Option-only filter = %q, want bare validator %q", a.Filter, a.Validator)
	}

	a, err = g.Generate(&mappingplan.Plan{KindName: "empty"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.Presence != "1" || a.Validator != "1" || a.Filter != "1" {
		t.Errorf("empty plan artefacts = %q/%q/%q, want 1/1/1", a.Presence, a.Validator, a.Filter)
	}
}

// TestGenerator_HasAllGrouping checks that several mandatory memberships on
// one physical column group into a single hasAll term (one array scan, one
// skip-index condition) and that the grouped form evaluates correctly.
func TestGenerator_HasAllGrouping(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{}))
	plan := &mappingplan.Plan{
		KindName: "grouped",
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "A", Canonical: ctabb.S, LWMembership: "alpha", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "B", Canonical: ctabb.S, LWMembership: "beta", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
		},
	}
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	symLv := g.support["symbol"][common.ColumnRoleLowCardVerbatim]
	if want := "hasAll(" + symLv + ", ['alpha', 'beta'])"; a.Presence != want {
		t.Errorf("presence = %q, want single grouped term %q", a.Presence, want)
	}
	if n := strings.Count(a.Validator, "countEqual("); n != 2 {
		t.Errorf("want 2 validator terms, got %d:\n%s", n, a.Validator)
	}

	symVal := g.value["symbol"]["value"].col
	symLvCard := g.support["symbol"][common.ColumnRoleLowCardVerbatimCardinality]
	both := map[string]string{
		symVal:    "['x','y']",
		symLv:     "['alpha','beta']",
		symLvCard: "[1,1]",
	}
	proj, pres, valid := runExec(t, both, a.Projection, a.Presence, a.Validator)
	if pres != "1" || valid != "1" {
		t.Errorf("both-present row: presence=%q validator=%q, want 1/1", pres, valid)
	}
	if !strings.Contains(proj, "x") || !strings.Contains(proj, "y") {
		t.Errorf("projection = %q, want both x and y", proj)
	}

	// beta absent: hasAll must reject without a false negative on alpha.
	oneMissing := map[string]string{
		symVal:    "['x','y']",
		symLv:     "['alpha','other']",
		symLvCard: "[1,1]",
	}
	_, pres, valid = runExec(t, oneMissing, a.Projection, a.Presence, a.Validator)
	if pres != "0" || valid != "0" {
		t.Errorf("beta-missing row: presence=%q validator=%q, want 0/0", pres, valid)
	}
}

// TestGenerator_ConstValuePresence checks that a const field contributes its
// pinned value as a presence term on the value column (a second necessary
// condition, skip-index-eligible there) — for scalar string columns only,
// since has() does not coerce a string literal to a numeric array — and that
// a row carrying the membership with the wrong value is pruned by presence.
func TestGenerator_ConstValuePresence(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"answer": 9}))
	plan := &mappingplan.Plan{
		KindName: "constKind",
		Fields: []mappingplan.TaggedField{
			{LWMembership: "appKind", LWSection: "symbol", IsConst: true, ConstValue: "production", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "Tag", Canonical: ctabb.S, LWMembership: "feature", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
		},
	}
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	symVal := g.value["symbol"]["value"].col
	symLv := g.support["symbol"][common.ColumnRoleLowCardVerbatim]
	symLvCard := g.support["symbol"][common.ColumnRoleLowCardVerbatimCardinality]
	// Memberships group on the lv column; the const value lands on the value column.
	if want := "hasAll(" + symLv + ", ['appKind', 'feature']) AND has(" + symVal + ", 'production')"; a.Presence != want {
		t.Errorf("presence = %q, want %q", a.Presence, want)
	}

	match := map[string]string{
		symVal:    "['production','edge']",
		symLv:     "['appKind','feature']",
		symLvCard: "[1,1]",
	}
	_, pres, valid := runExec(t, match, a.Projection, a.Presence, a.Validator)
	if pres != "1" || valid != "1" {
		t.Errorf("conforming row: presence=%q validator=%q, want 1/1", pres, valid)
	}

	// Membership present but the const value wrong: presence already rejects
	// (the value-column term), and the validator agrees — no false negative.
	wrongValue := map[string]string{
		symVal:    "['staging','edge']",
		symLv:     "['appKind','feature']",
		symLvCard: "[1,1]",
	}
	_, pres, valid = runExec(t, wrongValue, a.Projection, a.Presence, a.Validator)
	if pres != "0" || valid != "0" {
		t.Errorf("wrong-const-value row: presence=%q validator=%q, want 0/0", pres, valid)
	}

	// A const on a numeric value column must not emit the value-side term:
	// has(Array(UInt64), '42') is a ClickHouse type error (NO_COMMON_TYPE).
	numPlan := &mappingplan.Plan{
		KindName: "constNum",
		Fields: []mappingplan.TaggedField{
			{LWMembership: "answer", LWSection: "pair", LWColumn: "lo", IsConst: true, ConstValue: "42", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
		},
	}
	a, err = g.Generate(numPlan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pairLo := g.value["pair"]["lo"].col
	pairLr := g.support["pair"][common.ColumnRoleLowCardRef]
	if want := "has(" + pairLr + ", 9)"; a.Presence != want {
		t.Errorf("numeric-const presence = %q, want membership term only %q", a.Presence, want)
	}
	if strings.Contains(a.Presence, pairLo) {
		t.Errorf("numeric-const presence must not reference the value column %s:\n%s", pairLo, a.Presence)
	}
}

// runExec injects the helper UDFs, then runs proj/presence/validator over a
// single synthetic row whose columns are the (escaped) physical column names ->
// array-literal map, via clickhouse-local. Returns the three TSV cells.
func runExec(t *testing.T, cols map[string]string, proj, presence, validator string) (projOut, presOut, validOut string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(HelperUDFsSQL())
	sb.WriteString("\nSELECT ")
	sb.WriteString(proj)
	sb.WriteString(" AS proj, ")
	sb.WriteString(presence)
	sb.WriteString(" AS presence, ")
	sb.WriteString(validator)
	sb.WriteString(" AS valid FROM ( SELECT ")
	first := true
	for name, lit := range cols {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(lit)
		sb.WriteString(" AS ")
		sb.WriteString(name)
	}
	sb.WriteString(" )")
	out := runClickHouseLocal(t, sb.String())
	parts := strings.Split(strings.TrimRight(out, "\n"), "\t")
	if len(parts) != 3 {
		t.Fatalf("expected 3 TSV columns, got %d: %q\nscript:\n%s", len(parts), out, sb.String())
	}
	return parts[0], parts[1], parts[2]
}

// TestGenerator_Exec generates artefacts for a scalar-verbatim + array-ref Plan
// and runs them against a synthetic entity in clickhouse-local, checking the
// projected values plus the presence/validator booleans for a matching row and
// the presence boolean for a non-matching one (no false negatives).
func TestGenerator_Exec(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"myNums": 42}))
	plan := &mappingplan.Plan{
		KindName:  "exec",
		PlainCols: []mappingplan.PlainCol{{Column: "id", GoField: "Id", Canonical: ctabb.U64}},
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", Canonical: ctabb.S, LWMembership: "mySym", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "Nums", Canonical: canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray), LWMembership: "myNums", LWSection: "u64Array", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
		},
	}
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	idCol := g.plain["id"].col
	symVal := g.value["symbol"]["value"].col
	symLv := g.support["symbol"][common.ColumnRoleLowCardVerbatim]
	symLvCard := g.support["symbol"][common.ColumnRoleLowCardVerbatimCardinality]
	numVal := g.value["u64Array"]["value"].col
	numLen := g.support["u64Array"][common.ColumnRoleLength]
	numLr := g.support["u64Array"][common.ColumnRoleLowCardRef]
	numLrCard := g.support["u64Array"][common.ColumnRoleLowCardRefCardinality]
	for name, v := range map[string]string{"id": idCol, "symVal": symVal, "symLv": symLv, "symLvCard": symLvCard, "numVal": numVal, "numLen": numLen, "numLr": numLr, "numLrCard": numLrCard} {
		if v == "" {
			t.Fatalf("schema is missing the physical column for %s", name)
		}
	}

	// Matching entity: symbol attr1 'hello' tagged mySym; u64Array attr1 [10,20]
	// tagged 42 (id 42); attr2 [30] tagged 99.
	match := map[string]string{
		idCol:     "123",
		symVal:    "['hello','world']",
		symLv:     "['mySym','other']",
		symLvCard: "[1,1]",
		numVal:    "[10,20,30]",
		numLen:    "[2,1]",
		numLr:     "[42,99]",
		numLrCard: "[1,1]",
	}
	proj, pres, valid := runExec(t, match, a.Projection, a.Presence, a.Validator)
	if pres != "1" {
		t.Errorf("matching row presence = %q, want 1", pres)
	}
	if valid != "1" {
		t.Errorf("matching row validator = %q, want 1", valid)
	}
	if !strings.Contains(proj, "hello") || !strings.Contains(proj, "[10,20]") || !strings.Contains(proj, "123") {
		t.Errorf("projection = %q, want id 123, Sym hello, Nums [10,20]", proj)
	}

	// Non-matching entity: mySym absent from the symbol section.
	miss := map[string]string{
		idCol:     "123",
		symVal:    "['world','other']",
		symLv:     "['other','third']",
		symLvCard: "[1,1]",
		numVal:    "[10,20,30]",
		numLen:    "[2,1]",
		numLr:     "[42,99]",
		numLrCard: "[1,1]",
	}
	_, pres, valid = runExec(t, miss, a.Projection, a.Presence, a.Validator)
	if pres != "0" {
		t.Errorf("non-matching row presence = %q, want 0 (no false negative)", pres)
	}
	if valid != "0" {
		t.Errorf("non-matching row validator = %q, want 0", valid)
	}
}

// TestGenerator_ExecSetAndMultiSubcol covers the set value path (LIST_BY_TAG
// keyed on the section's `card` support column) and a multi-sub-column section
// (two value columns sharing one membership: located once, projected twice,
// the membership counted once in presence/validator).
func TestGenerator_ExecSetAndMultiSubcol(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"zones": 5, "span": 7}))
	plan := &mappingplan.Plan{
		KindName: "setmulti",
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Zones", Canonical: canonicaltypes.PromoteScalarPrim(ctabb.U32, canonicaltypes.ScalarModifierSet), LWMembership: "zones", LWSection: "u32Set", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "Lo", Canonical: ctabb.U64, LWMembership: "span", LWSection: "pair", LWColumn: "lo", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "Hi", Canonical: ctabb.U64, LWMembership: "span", LWSection: "pair", LWColumn: "hi", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
		},
	}
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// span is shared by Lo and Hi -> one membership term each in presence/validator.
	if n := strings.Count(a.Presence, "has("); n != 2 {
		t.Errorf("want 2 presence terms (zones + span), got %d:\n%s", n, a.Presence)
	}
	if n := strings.Count(a.Validator, "countEqual("); n != 2 {
		t.Errorf("want 2 validator terms (span dedup'd), got %d:\n%s", n, a.Validator)
	}

	setVal := g.value["u32Set"]["value"].col
	setCard := g.support["u32Set"][common.ColumnRoleCardinality]
	setLr := g.support["u32Set"][common.ColumnRoleLowCardRef]
	setLrCard := g.support["u32Set"][common.ColumnRoleLowCardRefCardinality]
	pairLo := g.value["pair"]["lo"].col
	pairHi := g.value["pair"]["hi"].col
	pairLr := g.support["pair"][common.ColumnRoleLowCardRef]
	pairLrCard := g.support["pair"][common.ColumnRoleLowCardRefCardinality]
	for name, v := range map[string]string{"setVal": setVal, "setCard": setCard, "setLr": setLr, "setLrCard": setLrCard, "pairLo": pairLo, "pairHi": pairHi, "pairLr": pairLr, "pairLrCard": pairLrCard} {
		if v == "" {
			t.Fatalf("schema is missing the physical column for %s", name)
		}
	}

	// One entity: u32Set attr holds the 3-element set tagged 5; pair attr holds
	// (lo=10, hi=20) tagged 7.
	row := map[string]string{
		setVal: "[100,200,300]", setCard: "[3]", setLr: "[5]", setLrCard: "[1]",
		pairLo: "[10]", pairHi: "[20]", pairLr: "[7]", pairLrCard: "[1]",
	}
	proj, pres, valid := runExec(t, row, a.Projection, a.Presence, a.Validator)
	if pres != "1" {
		t.Errorf("presence = %q, want 1", pres)
	}
	if valid != "1" {
		t.Errorf("validator = %q, want 1", valid)
	}
	for _, want := range []string{"[100,200,300]", "10", "20"} {
		if !strings.Contains(proj, want) {
			t.Errorf("projection = %q, want set [100,200,300] and lo/hi 10/20", proj)
		}
	}
}
