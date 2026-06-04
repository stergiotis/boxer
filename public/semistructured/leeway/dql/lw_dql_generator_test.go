package dql

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
		PlainCols: []mappingplan.PlainCol{{Column: "id", GoField: "Id", GoType: "uint64"}, {Column: "naturalKey", GoField: "NK", GoType: "[]byte"}},
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", GoType: "string", LWMembership: "mySym", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "Nums", GoType: "uint64", IsSlice: true, LWMembership: "myNums", LWSection: "u64Array", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "WinBegin", GoType: "time.Time", LWMembership: "window", LWSection: "timeRange", LWColumn: "beginIncl", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
			{GoFieldName: "WinEnd", GoType: "time.Time", LWMembership: "window", LWSection: "timeRange", LWColumn: "endExcl", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
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
		PlainCols: []mappingplan.PlainCol{{Column: "id", GoField: "Id", GoType: "uint64"}},
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", GoType: "string", LWMembership: "mySym", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			{GoFieldName: "Nums", GoType: "uint64", IsSlice: true, LWMembership: "myNums", LWSection: "u64Array", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef}},
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
