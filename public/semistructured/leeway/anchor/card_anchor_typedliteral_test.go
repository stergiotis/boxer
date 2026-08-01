package anchor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/require"
)

// TestDqlTypedLiteralGeneration shows the marshalling bridge between Go
// values and ClickHouse SQL literal text, in both directions, and records it
// in card_anchor_dql_typedliteral.out.md:
//
//   - Go → SQL: MarshalGoValueToSQL builds literal text (quoting and escaping
//     included) from plain Go values, so a query never concatenates a raw
//     value; with PreserveCasts the narrow types keep their width via the
//     same CAST(x, 'T') spelling the canonicalizer normalizes query 4/5's
//     hand-written casts into.
//   - SQL → Go: ExtractLiterals pulls every literal out of query 2's
//     executable form into named parameters (SET statements + a
//     parameterized body); each parameter deserializes to a TypedLiteral and
//     re-marshals to SQL — the lossless round-trip play's parameter
//     machinery builds on.
func TestDqlTypedLiteralGeneration(t *testing.T) {
	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor DQL — TypedLiteral: Go values to SQL literals and back", "TestDqlTypedLiteralGeneration"))

	doc.WriteString("## Go value → SQL literal\n\n")
	doc.WriteString("| Go value | SQL |\n|---|---|\n")
	type row struct {
		label string
		sql   string
		err   error
	}
	mustSQL := func(v any) string {
		s, err := marshalling.MarshalGoValueToSQL(v)
		require.NoError(t, err)
		return s
	}
	mustSQLCast := func(v any) string {
		s, err := marshalling.MarshalGoValueToSQLWithOptions(v, marshalling.MarshalOptions{PreserveCasts: true})
		require.NoError(t, err)
		return s
	}
	rows := []row{
		{label: "`uint64(61029384)` (the engineered H3 cell)", sql: mustSQL(uint64(61029384))},
		{label: "`\"leave quietly at the back door\"`", sql: mustSQL("leave quietly at the back door")},
		{label: "`\"it's urgent\"` (escaping)", sql: mustSQL("it's urgent")},
		{label: "`[]string{\"DDOS\", \"SQL_INJECTION\", \"PORT_SCAN\"}` (query 1's IN list)", sql: mustSQL([]string{"DDOS", "SQL_INJECTION", "PORT_SCAN"})},
		{label: "`[]uint64{22, 443, 8123}`", sql: mustSQL([]uint64{22, 443, 8123})},
		{label: "`float32(0)` with PreserveCasts (query 4/5's zeroed coordinate)", sql: mustSQLCast(float32(0))},
		{label: "`[]int32{-1, 0, 1}` with PreserveCasts", sql: mustSQLCast([]int32{-1, 0, 1})},
	}
	for _, r := range rows {
		fmt.Fprintf(doc, "| %s | `%s` |\n", r.label, r.sql)
	}
	doc.WriteString("\n")

	// SQL → TypedLiteral → SQL, over query 2's executable form.
	resolver := NewDqlResolver()
	final, err := NewDqlPreExecutePipeline(resolver, nil).Run(readDqlSource("./card_anchor_dql_query2.sql", t))
	require.NoError(t, err)

	cfg := passes.NewExtractLiteralsConfig(2)
	cfg.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	extracted, err := passes.ExtractLiterals(cfg).Run(final)
	require.NoError(t, err)

	doc.WriteString("## ExtractLiterals on query 2 — literals become named parameters\n\n")
	fmt.Fprintf(doc, "```sql\n%s\n```\n\n", extracted)
	doc.WriteString("Each parameter deserializes to a TypedLiteral and marshals back to the\n")
	doc.WriteString("same literal text (the round-trip asserted by this test):\n\n")
	doc.WriteString("| parameter | literal | context | round-trip |\n|---|---|---|---|\n")
	n := 0
	for _, info := range passes.IterateExtractedParams(extracted, "") {
		val, err := info.Value()
		require.NoError(t, err, info.FullName)
		back, err := marshalling.MarshalTypedLiteralToSQL(val)
		require.NoError(t, err, info.FullName)
		require.Equal(t, info.LiteralSQL, back, "TypedLiteral round-trip must be lossless for %s", info.FullName)
		fmt.Fprintf(doc, "| `%s` | `%s` | `%s` | `%s` |\n", info.FullName, info.LiteralSQL, info.FunctionName, back)
		n++
	}
	require.NotZero(t, n, "query 2 must yield extracted parameters")
	doc.WriteString("\n")

	writeFile("./card_anchor_dql_typedliteral.out.md", doc.String(), t)
}
