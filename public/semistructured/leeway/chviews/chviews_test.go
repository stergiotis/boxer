package chviews

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stretchr/testify/require"
)

// fixtureNames composes the physical column names of a schema that carries
// both arities — the in-tree schema catalog has plain backbone columns and
// four tagged sections — through the real composer, so the decode is checked
// against names a generator produced rather than names a test author typed.
func fixtureNames(t *testing.T) (names []string, conv *ddl.HumanReadableNamingConvention) {
	t.Helper()
	manip, err := common.GetSystemTableColumnsManipulator()
	require.NoError(t, err)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "observedAt", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeOpaque, "blob", ctabb.S)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)

	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))

	conv, err = ddl.NewHumanReadableNamingConvention(ddl.DefaultSeparator)
	require.NoError(t, err)

	phys := make([]common.PhysicalColumnDesc, 0, 64)
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, common.SystemTableColumnsTableRowConfig)
		require.NoError(t, err)
	}
	names = make([]string, 0, len(phys))
	for _, p := range phys {
		names = append(names, p.String())
	}
	require.NotEmpty(t, names)
	return
}

// layoutOf applies the view's own layout rule — field count plus prefix — to
// a raw name, so the test exercises the rule the generated SQL carries rather
// than a second reading of it.
func layoutOf(parts []string) (layout ddl.NameLayoutE, ok bool) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == ddl.NameLayoutTagged.FieldCount() && parts[0] == ddl.TaggedValuePrefix {
		return ddl.NameLayoutTagged, true
	}
	prefixes, _ := plainPrefixTables()
	if len(parts) != ddl.NameLayoutPlain.FieldCount() {
		return
	}
	for _, p := range prefixes {
		if p == parts[0] {
			return ddl.NameLayoutPlain, true
		}
	}
	return
}

// TestFieldPositionsMatchTheComposer is the drift guard the views rest on: the
// field index the generator splices into SQL must select, out of a raw split
// of the name, exactly what the convention's own extractor reports. A
// component added to either arity — which shifts every later field — fails
// here without a ClickHouse in the loop (ADR-0226 §Verification).
func TestFieldPositionsMatchTheComposer(t *testing.T) {
	names, conv := fixtureNames(t)
	phys, err := conv.ParseColumns(names)
	require.NoError(t, err)
	require.Len(t, phys, len(names))

	parser := canonicaltypes.NewParser()
	seenTagged, seenPlain := false, false
	for i, name := range names {
		parts := strings.Split(name, ddl.DefaultSeparator)
		layout, ok := layoutOf(parts)
		require.True(t, ok, "composed name did not classify: %s", name)
		require.Len(t, parts, layout.FieldCount(), "name %s", name)
		switch layout {
		case ddl.NameLayoutTagged:
			seenTagged = true
		case ddl.NameLayoutPlain:
			seenPlain = true
		}

		field := func(component string) (value string, present bool) {
			idx, present := layout.FieldIndex(component)
			if !present {
				return
			}
			return parts[idx-1], true
		}

		if v, present := field(ddl.ComponentSectionName); present {
			section, e := conv.ExtractSectionName(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(section), v, "section of %s", name)
		}
		if v, present := field(ddl.ComponentColumnName); present {
			column, e := conv.ExtractLeewayColumnName(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(column), v, "column of %s", name)
		}
		if v, present := field(ddl.ComponentRole); present {
			role, e := conv.ExtractColumnRole(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(role), v, "role of %s", name)
		}
		if v, present := field(ddl.ComponentCanonicalType); present {
			ct, e := conv.ExtractCanonicalType(phys[i])
			require.NoError(t, e)
			reparsed, e := parser.ParsePrimitiveTypeOrGroupAst(v)
			require.NoError(t, e, "canonical type of %s", name)
			require.Equal(t, ct.String(), reparsed.String(), "canonical type of %s", name)
		}
		if v, present := field(ddl.ComponentEncodingHints); present {
			hints, e := conv.ExtractEncodingHints(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(hints), v, "encoding hints of %s", name)
		}
		if v, present := field(ddl.ComponentUseAspects); present {
			use, e := conv.ExtractUseAspects(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(use), v, "use aspects of %s", name)
		}
		if v, present := field(ddl.ComponentValueSemantics); present {
			sem, e := conv.ExtractValueSemantics(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(sem), v, "value semantics of %s", name)
		}
		if v, present := field(ddl.ComponentTableRowConfig); present {
			cfg, e := conv.ExtractTableRowConfig(phys[i])
			require.NoError(t, e)
			codes, _ := rowConfigTables()
			idx := -1
			for j, c := range common.AllTableRowConfigs {
				if c == cfg {
					idx = j
				}
			}
			require.GreaterOrEqual(t, idx, 0)
			require.Equal(t, codes[idx], v, "row config of %s", name)
		}
		if v, present := field(ddl.ComponentCoSectionGroup); present {
			key, e := conv.ExtractCoSectionGroup(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(key), v, "co-section group of %s", name)
		}
		if v, present := field(ddl.ComponentStreamingGroup); present {
			key, e := conv.ExtractStreamingGroup(phys[i])
			require.NoError(t, e)
			require.Equal(t, string(key), v, "streaming group of %s", name)
		}
	}
	require.True(t, seenTagged, "fixture must exercise the tagged arity")
	require.True(t, seenPlain, "fixture must exercise the plain arity")
}

// TestLayoutAgreesWithTheParser: the view decides plain-vs-tagged from the
// field count and the prefix alone, because that is all SQL has. The parser
// decides it from the item type. They must not disagree on a composed name.
func TestLayoutAgreesWithTheParser(t *testing.T) {
	names, conv := fixtureNames(t)
	phys, err := conv.ParseColumns(names)
	require.NoError(t, err)
	for i, name := range names {
		layout, ok := layoutOf(strings.Split(name, ddl.DefaultSeparator))
		require.True(t, ok, "composed name did not classify: %s", name)
		pit, e := conv.ExtractPlainItemType(phys[i])
		require.NoError(t, e)
		if pit == common.PlainItemTypeNone {
			require.Equal(t, ddl.NameLayoutTagged, layout, "name %s", name)
			continue
		}
		require.Equal(t, ddl.NameLayoutPlain, layout, "name %s", name)
	}
}

// TestHandleMatchesLabels pins the §SD3 claim that a handle read out of the
// view is a handle the playground accepts: the section spelling the plain
// prefix table produces must be the one BuildLabels renders, for every
// backbone column as well as every tagged one.
func TestHandleMatchesLabels(t *testing.T) {
	names, _ := fixtureNames(t)
	labels := lwsql.BuildLabels(names)
	require.NotEmpty(t, labels)

	prefixes, sections := plainPrefixTables()
	for _, name := range names {
		parts := strings.Split(name, ddl.DefaultSeparator)
		layout, ok := layoutOf(parts)
		require.True(t, ok)

		var section string
		if layout == ddl.NameLayoutTagged {
			idx, _ := layout.FieldIndex(ddl.ComponentSectionName)
			section = parts[idx-1]
		} else {
			for j, p := range prefixes {
				if p == parts[0] {
					section = sections[j]
				}
			}
		}
		require.NotEmpty(t, section, "no section for %s", name)
		colIdx, _ := layout.FieldIndex(ddl.ComponentColumnName)
		handle := section + ":" + parts[colIdx-1]
		require.Equal(t, labels[name], handle, "handle of %s", name)
	}
}

// TestLaneKindTableCoversEveryRole: an unlisted role decodes as unknown, which
// is correct but useless. Every role the model declares must be in the table,
// and none of them may land on unknown — that would mean the classifier grew a
// role the view cannot name.
func TestLaneKindTableCoversEveryRole(t *testing.T) {
	roles, kinds := laneKindTables()
	require.Len(t, kinds, len(roles))
	require.Len(t, roles, len(common.AllColumnRoles)-1) // ColumnRoleUnspecific is the default case
	for i, role := range roles {
		require.NotEmpty(t, role)
		require.NotEqual(t, lwsql.LaneKindUnknown.String(), kinds[i], "role %s classifies as unknown", role)
	}
}

// TestStatementsShape keeps the rendered family legible without pinning a
// whole body: the database comes first, every declared view is created once,
// and the decode's result columns are all aliased in it.
func TestStatementsShape(t *testing.T) {
	const stamp = Stamp("surface v99")
	stmts := Statements("", stamp)
	require.Len(t, stmts, len(AllViewNames())+1)
	require.Equal(t, "CREATE DATABASE IF NOT EXISTS "+DefaultDatabase, stmts[0])
	for i, name := range AllViewNames() {
		require.True(t, strings.HasPrefix(stmts[i+1], "CREATE OR REPLACE VIEW "+DefaultDatabase+"."+name+" AS"),
			"statement %d: %s", i+1, stmts[i+1])
		// The stamp is what makes a view built against an older vocabulary
		// visible, so every view must carry it — ClickHouse inlines the
		// function bodies and nothing else records which ones.
		require.True(t, strings.HasSuffix(stmts[i+1], "COMMENT "+stamp.Literal()),
			"statement %d carries no stamp: %s", i+1, stmts[i+1])
	}

	body := columnsBody()
	for _, c := range ColumnsColumns() {
		require.Contains(t, body, c, "columns view does not mention %s", c)
	}
	for _, fn := range []string{"LW_ASPECT_DECODABLE", "LW_ASPECT_NAMES_ENC", "LW_ASPECT_NAMES_USE", "LW_ASPECT_NAMES_SEM"} {
		require.Contains(t, body, fn)
	}
}

// TestAggregatesQualifySources: an aggregate aliased to the name of the column
// it consumes is a cyclic alias to the ClickHouse analyzer, and the aggregate
// views alias several that way on purpose. The guard is that every source
// reference is qualified.
func TestAggregatesQualifySources(t *testing.T) {
	for _, body := range []string{sectionsBody(""), tablesBody("")} {
		for _, col := range []string{"use_aspects", "lane_kind", "streaming_group", "table_row_config", "section"} {
			require.NotContains(t, body, "("+col+")", "unqualified source %s in\n%s", col, body)
			require.NotContains(t, body, "("+col+",", "unqualified source %s in\n%s", col, body)
		}
	}
}

// TestDropStatementsReverseInstallOrder: the aggregates select from the
// decode, so they go first — a DROP in install order would leave a view
// standing over a view that no longer exists.
func TestDropStatementsReverseInstallOrder(t *testing.T) {
	drops := DropStatements("")
	names := AllViewNames()
	require.Len(t, drops, len(names))
	for i, stmt := range drops {
		want := "DROP VIEW IF EXISTS " + DefaultDatabase + "." + names[len(names)-1-i]
		require.Equal(t, want, stmt)
	}
}

// TestTablesColumnsMatchTheAggregate: the outer select reads the aggregate by
// name, so a column added to one list and not the other renders SQL that
// fails at CREATE time on every endpoint rather than in this package.
func TestTablesColumnsMatchTheAggregate(t *testing.T) {
	body := tablesBody("")
	for _, c := range TablesColumns() {
		require.Contains(t, body, c, "tables view does not mention %s", c)
	}
	for _, c := range tableAggregateColumns {
		require.Contains(t, body, "a."+c+" AS "+c, "aggregate column %s is not projected", c)
		require.GreaterOrEqual(t, strings.Count(body, " AS "+c), 2,
			"aggregate column %s is projected but never produced", c)
	}
	for _, c := range SectionsColumns() {
		require.Contains(t, sectionsBody(""), c, "sections view does not mention %s", c)
	}
}

func TestTargetDatabaseValidate(t *testing.T) {
	require.NoError(t, TargetDatabase("").Validate())
	require.NoError(t, TargetDatabase("lw_views2").Validate())
	require.Error(t, TargetDatabase("2bad").Validate())
	require.Error(t, TargetDatabase("has space").Validate())
	require.Error(t, TargetDatabase("a';DROP").Validate())
	require.Equal(t, DefaultDatabase, TargetDatabase("").Name())
	require.Equal(t, "x.columns", TargetDatabase("x").Qualified(ViewColumns))
}

func TestSqlLiteralEscapes(t *testing.T) {
	require.Equal(t, "'plain'", sqlLiteral("plain"))
	require.Equal(t, `'it\'s'`, sqlLiteral("it's"))
	require.Equal(t, `'back\\slash'`, sqlLiteral(`back\slash`))
	require.Equal(t, "['a', 'b']", sqlStringArray([]string{"a", "b"}))
	require.Equal(t, "('a', 'b')", sqlStringTuple([]string{"a", "b"}))
}
