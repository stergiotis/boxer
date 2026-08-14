package constructsql_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwextract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const extractTable = "events"

// f64Array is a homogenous-array value type: the flattened element lane
// plus the `len` support column that partitions it.
var f64Array = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ScalarModifier: canonicaltypes.ScalarModifierHomogenousArray}

// extractFixture builds a leeway table whose sections cover what the
// extraction family has to tell apart, and returns its physical column
// names — the same names a real ClickHouse table would carry.
func extractFixture(t *testing.T) []string {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(extractTable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)

	// A scalar section on a verbatim channel: memberships are spelled by
	// name, so no registry is needed to read one.
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	// A scalar section on a ref channel: memberships are registry ids.
	manip.MergeTaggedValueColumn("metric", "value", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardRef, "", "")
	// A two-value-column section, so the sub-column token has something to
	// disambiguate.
	manip.MergeTaggedValueColumn("geoPoint", "lat", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	manip.MergeTaggedValueColumn("geoPoint", "lon", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	// An array section, for the list form.
	manip.MergeTaggedValueColumn("samples", "value", f64Array, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")

	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, tech))
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names := make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, strings.Join(p.NameComponents, ""))
	}
	require.NotEmpty(t, names)
	return names
}

func extractResolver(t *testing.T) *lwsql.Resolver {
	t.Helper()
	return lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{extractTable: extractFixture(t)}))
}

func expandExtract(t *testing.T, sql string) (out string, err error) {
	t.Helper()
	return constructsql.ExtractExpandPass(extractResolver(t), "").Run(sql)
}

// TestLanesResolveFromColumnNamesAlone pins the schema half: the lanes come
// out of the physical names, with no IR and no server round-trip beyond the
// column list (ADR-0181 §SD3).
//
// Cross-checked against the handle path — a different code path over the
// same names — so agreement here is not the classifier agreeing with itself.
func TestLanesResolveFromColumnNamesAlone(t *testing.T) {
	r := extractResolver(t)

	lanes, ok := r.ExtractLanesFor("", extractTable, "symbol")
	require.True(t, ok)
	require.Equal(t, "symbol", lanes.Section)

	handle := r.Resolve("", extractTable, "symbol:value")
	require.Equal(t, passes.ResolveOK, handle.Kind)
	value, err := lanes.ValueColumnFor("")
	require.NoError(t, err)
	require.Equal(t, handle.Physical[0], value.Physical, "the value lane is the column the handle path resolves")

	ch, err := lanes.ChannelFor("")
	require.NoError(t, err)
	require.Equal(t, "low-card-verbatim", ch.Name)
	require.True(t, ch.Verbatim)
	require.Contains(t, ch.Ident, ":lv:")
	require.Contains(t, ch.Card, ":lvcard:")

	// A scalar column has no element-count lane, which is how its shape is
	// decided — no separate flag to disagree with the columns.
	require.Equal(t, lwextract.ShapeScalar, value.Shape)
	require.Empty(t, value.Length)

	arr, ok := r.ExtractLanesFor("", extractTable, "samples")
	require.True(t, ok)
	arrCol, err := arr.ValueColumnFor("")
	require.NoError(t, err)
	require.Equal(t, lwextract.ShapeList, arrCol.Shape)
	require.NotEmpty(t, arrCol.Length, "an array column carries the len support column")

	// Folding: sections resolve however the caller spells them.
	_, ok = r.ExtractLanesFor("", extractTable, "geo_point")
	require.True(t, ok, "section names fold")
	_, ok = r.ExtractLanesFor("", extractTable, "nosuch")
	require.False(t, ok)
}

// TestExpandsScalarExtraction is the family's whole point: a section and a
// membership in, the locate-and-extract expression out, with no physical
// name typed by a human.
func TestExpandsScalarExtraction(t *testing.T) {
	r := extractResolver(t)
	lanes, ok := r.ExtractLanesFor("", extractTable, "symbol")
	require.True(t, ok)
	value, err := lanes.ValueColumnFor("")
	require.NoError(t, err)
	ch, err := lanes.ChannelFor("")
	require.NoError(t, err)

	out, err := constructsql.ExtractExpandPass(r, "").Run("SELECT LW_GET('symbol', 'ticker') FROM events")
	require.NoError(t, err)
	require.Equal(t,
		"SELECT LW_VALUE_BY_TAG_EQUAL("+
			nanopass.QuoteIdentifier(value.Physical)+", "+
			nanopass.QuoteIdentifier(ch.Ident)+", 'ticker', "+
			"LW_RAGGED_PARENT_IDS("+nanopass.QuoteIdentifier(ch.Card)+")) FROM events",
		out)
	require.NotContains(t, out, "LW_GET")
}

// TestExpandsNullForm covers the member that exists because absent and
// present-with-the-default are different questions (ADR-0066 decision 4).
func TestExpandsNullForm(t *testing.T) {
	out, err := expandExtract(t, "SELECT LW_GET_NULL('symbol', 'ticker') FROM events")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(strings.TrimPrefix(out, "SELECT "), "if(has("), out)
	require.Contains(t, out, ", NULL)")
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(")
}

// TestExpandsListForm pins the array member, and that the two forms are not
// interchangeable: reading an array section with the scalar member, or a
// scalar section with the list member, is a mistake the pass names rather
// than a cast the server attempts.
func TestExpandsListForm(t *testing.T) {
	out, err := expandExtract(t, "SELECT LW_GET_LIST('samples', 'run') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, "LW_LIST_BY_TAG_EQUAL(")

	_, err = expandExtract(t, "SELECT LW_GET('samples', 'run') FROM events")
	require.ErrorContains(t, err, constructsql.NameGetList)

	_, err = expandExtract(t, "SELECT LW_GET_LIST('symbol', 'ticker') FROM events")
	require.ErrorContains(t, err, constructsql.NameGet)
}

// TestSubColumnToken covers a section with more than one value column: there
// is no obvious "the" value, so the pass asks instead of guessing, and lists
// what it has.
func TestSubColumnToken(t *testing.T) {
	_, err := expandExtract(t, "SELECT LW_GET('geoPoint', 'here') FROM events")
	require.ErrorContains(t, err, "more than one value column")
	require.ErrorContains(t, err, "lat")
	require.ErrorContains(t, err, "lon")

	out, err := expandExtract(t, "SELECT LW_GET('geoPoint', 'here', 'col:lat') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, ":geoPoint:lat:")
	require.NotContains(t, out, ":geoPoint:lon:")

	_, err = expandExtract(t, "SELECT LW_GET('geoPoint', 'here', 'col:altitude') FROM events")
	require.ErrorContains(t, err, "no such value column")

	_, err = expandExtract(t, "SELECT LW_GET('symbol', 'ticker', 'nonsense') FROM events")
	require.ErrorContains(t, err, "unknown token")
}

// TestRefChannelNeedsAnId is the honest edge of a client-side expansion: a
// ref channel identifies memberships by registry id, and this pass holds no
// registry. The error says so and says where the id comes from, rather than
// escaping the name into a literal that would match nothing.
func TestRefChannelNeedsAnId(t *testing.T) {
	_, err := expandExtract(t, "SELECT LW_GET('metric', 'cpuLoad') FROM events")
	require.ErrorContains(t, err, "registry id")
	require.ErrorContains(t, err, "leeway id")

	out, err := expandExtract(t, "SELECT LW_GET('metric', '6917529027641081861') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, "6917529027641081861")
	require.NotContains(t, out, "'6917529027641081861'", "a ref id is a number, not a string")
}

// TestUnresolvableSectionNamesWhatWasSearched pins the diagnostics ADR-0181
// §SD3 asks for: a section no table carries reports the tables it looked in
// and the sections they do have.
func TestUnresolvableSectionNamesWhatWasSearched(t *testing.T) {
	_, err := expandExtract(t, "SELECT LW_GET('nosuch', 'x') FROM events")
	require.ErrorContains(t, err, "no table in scope carries that section")
	require.ErrorContains(t, err, extractTable)
	require.ErrorContains(t, err, "symbol")
}

// TestExtractionIsAnOrdinaryExpression covers the difference from the
// constructor family: an extraction produces a value, so it is legal
// wherever a value is — WHERE included, which is where a BI-shaped filter
// on a leeway attribute actually lands.
func TestExtractionIsAnOrdinaryExpression(t *testing.T) {
	out, err := expandExtract(t, "SELECT 1 FROM events WHERE LW_GET('symbol', 'ticker') = 'AAPL'")
	require.NoError(t, err)
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(")
	require.Contains(t, out, "= 'AAPL'")

	out, err = expandExtract(t, "SELECT count() FROM events GROUP BY LW_GET('symbol', 'ticker')")
	require.NoError(t, err)
	require.Contains(t, out, "GROUP BY LW_VALUE_BY_TAG_EQUAL(")

	out, err = expandExtract(t, "SELECT upper(LW_GET('symbol', 'ticker')) FROM events")
	require.NoError(t, err)
	require.Contains(t, out, "upper(LW_VALUE_BY_TAG_EQUAL(")
}

// TestJoinQualifiesLanes covers the ambiguity a second source introduces:
// the expansion splices physical column names, and an unqualified name in a
// join is the server's problem to complain about, far from the call.
func TestJoinQualifiesLanes(t *testing.T) {
	out, err := expandExtract(t,
		"SELECT LW_GET('symbol', 'ticker') FROM events AS e JOIN other AS o ON e.id = o.id")
	require.NoError(t, err)
	require.Contains(t, out, `"e"."tv:symbol:value`, "lanes are qualified by the carrying table's alias")
}

// TestSectionInMoreThanOneSourceIsAmbiguous is the other half of the
// binding rule: two tables carrying the section cannot be told apart from
// the call, so the pass refuses rather than picking.
func TestSectionInMoreThanOneSourceIsAmbiguous(t *testing.T) {
	names := extractFixture(t)
	r := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{
		extractTable: names,
		"replica":    names,
	}))
	_, err := constructsql.ExtractExpandPass(r, "").Run(
		"SELECT LW_GET('symbol', 'ticker') FROM events AS e JOIN replica AS r ON e.id = r.id")
	require.ErrorContains(t, err, "more than one table in scope")
}

// TestSubqueryScopesBindIndependently pins that a call is resolved against
// ITS enclosing SELECT, not the outermost one — the scope rule the pass
// borrows from BuildScopes.
func TestSubqueryScopesBindIndependently(t *testing.T) {
	out, err := expandExtract(t,
		"SELECT x FROM (SELECT LW_GET('symbol', 'ticker') AS x FROM events)")
	require.NoError(t, err)
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(")

	// The outer scope reads a subquery, which has no catalog schema to ask.
	_, err = expandExtract(t,
		"SELECT LW_GET('symbol', 'ticker') FROM (SELECT 1 AS x FROM events)")
	require.ErrorContains(t, err, "no table in scope carries that section")
}

// TestUntouchedAndIdempotent covers the two properties registration relies
// on: a query with no call is returned unchanged (the marker pre-scan is
// what makes standard-set registration near-free), and expanding twice is
// expanding once, because expansion leaves no call behind.
func TestUntouchedAndIdempotent(t *testing.T) {
	const plain = "SELECT a, b FROM events WHERE a > 1"
	out, err := expandExtract(t, plain)
	require.NoError(t, err)
	require.Equal(t, plain, out)
	require.False(t, constructsql.HasExtractMarker(plain))
	require.True(t, constructsql.HasExtractMarker("select lw_get('s','m')"))

	once, err := expandExtract(t, "SELECT LW_GET('symbol', 'ticker') FROM events")
	require.NoError(t, err)
	twice, err := expandExtract(t, once)
	require.NoError(t, err)
	require.Equal(t, once, twice)
}

// TestUnboundPassIsInert pins the Factory contract: a pass built without a
// schema binding must pass SQL through, not fail every query carrying a
// call. A consumer with no resolver simply does not get the sugar.
func TestUnboundPassIsInert(t *testing.T) {
	const sql = "SELECT LW_GET('symbol', 'ticker') FROM events"
	out, err := constructsql.ExtractExpandPass(nil, "").Run(sql)
	require.NoError(t, err)
	require.Equal(t, sql, out)
}

// TestExtractFunctionsRoster pins the panel-facing declaration: every
// member is listed with a doc line, and the expansion dependencies name
// functions the read-back family actually declares — a dependency on a name
// that does not exist would mark the family missing on every server
// (ADR-0174 §SD6).
func TestExtractFunctionsRoster(t *testing.T) {
	fns := constructsql.ExtractFunctions()
	require.Len(t, fns, 3)
	names := make([]string, 0, len(fns))
	for _, f := range fns {
		require.NotEmpty(t, f.Doc, f.Name)
		require.NotEmpty(t, f.Params, f.Name)
		names = append(names, f.Name)
	}
	require.ElementsMatch(t, []string{constructsql.NameGet, constructsql.NameGetNull, constructsql.NameGetList}, names)

	// Every declared dependency must be a function some expansion can
	// actually emit. The list is hand-written beside a builder that decides
	// which form to render, so a name kept here after the builder stopped
	// emitting it would mark the family missing on servers that are fine.
	deps := constructsql.ExtractExpansionDependencies()
	require.NotEmpty(t, deps)
	general := lwextract.Lanes{Value: "v", Ident: "i", Card: "c", Length: "l"}
	fast := lwextract.Lanes{Value: "v", Ident: "i", Length: "l"}
	var emitted strings.Builder
	for _, lanes := range []lwextract.Lanes{general, fast} {
		for _, shape := range []lwextract.ShapeE{lwextract.ShapeScalar, lwextract.ShapeList} {
			expr, err := lwextract.Value(lwextract.Request{Lanes: lanes, Shape: shape, Membership: "'m'"})
			require.NoError(t, err)
			emitted.WriteString(expr)
			emitted.WriteString(" ")
		}
	}
	for _, dep := range deps {
		require.Containsf(t, emitted.String(), dep,
			"%s is declared as an expansion dependency but no form emits it", dep)
	}
}

// TestExpansionAgreesWithTheReadBackPath is the drift test ADR-0181 §SD3's
// one-builder decision exists for.
//
// The two consumers reach the same expression by different roads: the
// read-back generator resolves lanes from an IR loaded off a TableDesc,
// while LwExtractExpand resolves them by parsing physical column NAMES. If
// those disagree — a role mapped differently, a support column picked from
// the wrong section — the generated artefacts and the sugar read different
// columns while both look right in isolation.
//
// Comparing the lanes rather than running SQL is deliberate: the expression
// they feed is the same function call, pinned by lwextract's own tests and
// by the generator's goldens. What is unpinned, and only checkable here, is
// that the two resolvers agree on WHICH columns.
func TestExpansionAgreesWithTheReadBackPath(t *testing.T) {
	names := extractFixture(t)

	// The IR road: build the same table, load it the way the read-back
	// generator does, and pick the section's lanes out by role.
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(extractTable)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("symbol", "value", ctabb.S, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	manip.MergeTaggedValueColumn("metric", "value", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardRef, "", "")
	manip.MergeTaggedValueColumn("geoPoint", "lat", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	manip.MergeTaggedValueColumn("geoPoint", "lon", ctabb.F64, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	manip.MergeTaggedValueColumn("samples", "value", f64Array, hints,
		valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	info := readback.NewInformationRetrieval(conv)
	require.NoError(t, info.LoadTable(ir, common.TableRowConfigMultiAttributesPerRow))

	type irLanes struct {
		value map[string]string
		roles map[common.ColumnRoleE]string
	}
	bySection := make(map[string]*irLanes, 4)
	for r := range info.IterateAll() {
		cc := r.ColumnContext
		if cc.PlainItemType != common.PlainItemTypeNone {
			continue
		}
		sec := string(cc.SectionName)
		l := bySection[sec]
		if l == nil {
			l = &irLanes{value: make(map[string]string, 2), roles: make(map[common.ColumnRoleE]string, 4)}
			bySection[sec] = l
		}
		if r.Role == common.ColumnRoleValue {
			l.value[string(r.Name)] = r.PhysicalColumn.String()
		} else {
			l.roles[r.Role] = r.PhysicalColumn.String()
		}
	}
	require.NotEmpty(t, bySection)

	// The names road, and the comparison.
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{extractTable: names}))
	for _, tc := range []struct {
		section  string
		subCol   string
		identRol common.ColumnRoleE
		cardRole common.ColumnRoleE
	}{
		{"symbol", "value", common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality},
		{"metric", "value", common.ColumnRoleLowCardRef, common.ColumnRoleLowCardRefCardinality},
		{"geoPoint", "lat", common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality},
		{"geoPoint", "lon", common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality},
		{"samples", "value", common.ColumnRoleLowCardVerbatim, common.ColumnRoleLowCardVerbatimCardinality},
	} {
		t.Run(tc.section+"."+tc.subCol, func(t *testing.T) {
			want := bySection[tc.section]
			require.NotNil(t, want, "the IR road lost the section")

			lanes, ok := resolver.ExtractLanesFor("", extractTable, tc.section)
			require.True(t, ok)
			got, err := lanes.ValueColumnFor(tc.subCol)
			require.NoError(t, err)
			require.Equal(t, want.value[tc.subCol], got.Physical, "value lane")

			ch, err := lanes.ChannelFor("")
			require.NoError(t, err)
			require.Equal(t, want.roles[tc.identRol], ch.Ident, "membership identity lane")
			require.Equal(t, want.roles[tc.cardRole], ch.Card, "membership cardinality lane")

			if got.Length != "" {
				length, hasLen := want.roles[common.ColumnRoleLength]
				if !hasLen {
					length = want.roles[common.ColumnRoleCardinality]
				}
				require.Equal(t, length, got.Length, "element-count lane")
			} else {
				require.NotContains(t, want.roles, common.ColumnRoleLength)
				require.NotContains(t, want.roles, common.ColumnRoleCardinality)
			}
		})
	}
}

// u64Set is a set-valued type: the flattened element lane plus the `card`
// support column that partitions it.
var u64Set = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ScalarModifier: canonicaltypes.ScalarModifierSet}

// TestMixedShapeSectionPairsEachColumnWithItsOwnCounts is the regression for
// a section holding value columns of different scalar modifiers.
//
// Such a section carries BOTH a `len` and a `card` support column — the
// write path buckets by modifier and emits each independently, and the shape
// check permits it. Deciding the shape or the element-count lane per SECTION
// therefore pairs a set's flattened lane with an array's per-attribute
// counts: wrong rows, no error, and the scalar column in the same section
// becomes unreadable by the scalar call. Each column must carry its own.
func TestMixedShapeSectionPairsEachColumnWithItsOwnCounts(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("mixed")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	hints := encodingaspects.EncodeAspectsMustValidate(encodingaspects.AspectLightGeneralCompression)
	for _, c := range []struct {
		name string
		ct   canonicaltypes.PrimitiveAstNodeI
	}{
		{"scal", ctabb.S},
		{"arr", f64Array},
		{"st", u64Set},
	} {
		manip.MergeTaggedValueColumn("mix", naming.StylableName(c.name), c.ct, hints,
			valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common.MembershipSpecLowCardVerbatim, "", "")
	}
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	phys := make([]common.PhysicalColumnDesc, 0, 32)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
	}
	names := make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, strings.Join(p.NameComponents, ""))
	}

	// Precondition: the section really does carry both support columns.
	// Without this the test would pass for the wrong reason on a schema
	// that never produced the mixed shape.
	var hasLen, hasCard bool
	for _, n := range names {
		hasLen = hasLen || strings.Contains(n, ":mix:len:")
		hasCard = hasCard || strings.Contains(n, ":mix:card:")
	}
	require.True(t, hasLen && hasCard, "precondition: a mixed section carries both len and card: %v", names)

	r := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{"mixed": names}))
	lanes, ok := r.ExtractLanesFor("", "mixed", "mix")
	require.True(t, ok)

	scal, err := lanes.ValueColumnFor("scal")
	require.NoError(t, err)
	require.Equal(t, lwextract.ShapeScalar, scal.Shape)
	require.Empty(t, scal.Length, "a scalar column has nothing to partition")

	arr, err := lanes.ValueColumnFor("arr")
	require.NoError(t, err)
	require.Equal(t, lwextract.ShapeList, arr.Shape)
	require.Contains(t, arr.Length, ":mix:len:", "an array is partitioned by len")

	st, err := lanes.ValueColumnFor("st")
	require.NoError(t, err)
	require.Equal(t, lwextract.ShapeList, st.Shape)
	require.Contains(t, st.Length, ":mix:card:", "a set is partitioned by card")

	// End to end: each call reaches its own counts, and the scalar column is
	// readable by the scalar call rather than refused for the section.
	pass := constructsql.ExtractExpandPass(r, "")
	out, err := pass.Run("SELECT LW_GET('mix', 'm', 'col:scal') FROM mixed")
	require.NoError(t, err, "a scalar column in a mixed section must stay readable")
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(")

	out, err = pass.Run("SELECT LW_GET_LIST('mix', 'm', 'col:st') FROM mixed")
	require.NoError(t, err)
	require.Contains(t, out, ":mix:card:", "the set must be sliced by its own counts")
	require.NotContains(t, out, ":mix:len:")

	out, err = pass.Run("SELECT LW_GET_LIST('mix', 'm', 'col:arr') FROM mixed")
	require.NoError(t, err)
	require.Contains(t, out, ":mix:len:")
	require.NotContains(t, out, ":mix:card:")
}

// fakeIds is a membership registry for the test: one name, one id, and an
// error for anything else — the same total behaviour a real registry has.
type fakeIds map[string]uint64

func (inst fakeIds) LookupMembership(name string) (id uint64, err error) {
	id, ok := inst[name]
	if !ok {
		err = eh.Errorf("no such membership")
	}
	return
}

// TestRefChannelTakesANameWhenBound is ADR-0171 §SD4 seen from the authoring
// side: with a registry bound, a ref channel names its membership instead of
// carrying the uint64, which was the last place ADR-0181's "no Go, no
// physical names" criterion still failed.
//
// Resolution happens at expansion time, so the emitted SQL still carries a
// constant — the property ADR-0066 chose over a query-time lookup.
func TestRefChannelTakesANameWhenBound(t *testing.T) {
	r := extractResolver(t)
	const id = 6917529027641081861
	pass := constructsql.ExtractExpandPassWithIds(r, fakeIds{"cpuLoad": id}, "")

	out, err := pass.Run("SELECT LW_GET('metric', 'cpuLoad') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, strconv.FormatUint(id, 10), "the name resolves to the wire id")
	require.NotContains(t, out, "'cpuLoad'", "a ref channel carries the id, not the name")

	// An unknown name is an error naming where to look, not a predicate that
	// matches nothing.
	_, err = pass.Run("SELECT LW_GET('metric', 'noSuchThing') FROM events")
	require.ErrorContains(t, err, "keelson('memberships')")
}

// TestRefChannelKeepsTakingAnId pins that §SD4 is additive: every query
// written before it still expands, with or without a binding, because the
// decimal form is checked before the registry is consulted.
func TestRefChannelKeepsTakingAnId(t *testing.T) {
	r := extractResolver(t)
	const sql = "SELECT LW_GET('metric', '6917529027641081861') FROM events"

	bound, err := constructsql.ExtractExpandPassWithIds(r, fakeIds{}, "").Run(sql)
	require.NoError(t, err)
	unbound, err := constructsql.ExtractExpandPass(r, "").Run(sql)
	require.NoError(t, err, "the id form needs no registry")
	require.Equal(t, bound, unbound)
}

// TestRefChannelWithoutABindingSaysSo keeps the unbound host's error
// actionable: it names both ways to find the id rather than reporting a
// parse failure.
func TestRefChannelWithoutABindingSaysSo(t *testing.T) {
	_, err := expandExtract(t, "SELECT LW_GET('metric', 'cpuLoad') FROM events")
	require.ErrorContains(t, err, "bound no membership registry")
	require.ErrorContains(t, err, "leeway id")
	require.ErrorContains(t, err, "keelson('memberships')")
}

// TestVerbatimChannelIgnoresTheRegistry pins that a verbatim channel is
// unaffected: its membership IS the name, so a registry must not be
// consulted and a name that happens to look like a number must not be
// silently turned into one.
func TestVerbatimChannelIgnoresTheRegistry(t *testing.T) {
	r := extractResolver(t)
	pass := constructsql.ExtractExpandPassWithIds(r, fakeIds{"ticker": 42}, "")
	out, err := pass.Run("SELECT LW_GET('symbol', 'ticker') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, "'ticker'", "a verbatim channel carries the name")
	require.NotContains(t, out, "42")
}
