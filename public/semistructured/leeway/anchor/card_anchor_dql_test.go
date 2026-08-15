package anchor

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stretchr/testify/require"
)

// dqlQueryFiles are the friendly-form sources; TestDqlPipelineGeneration
// rewrites each into its physical, canonical `.out.sql` neighbour, which is
// what the integration lane executes.
var dqlQueryFiles = []string{
	"card_anchor_dql_query1.sql",
	"card_anchor_dql_query2.sql",
	"card_anchor_dql_query3.sql",
	"card_anchor_dql_query4.sql",
	"card_anchor_dql_query5.sql",
	"card_anchor_dql_query6.sql",
	"card_anchor_dql_query7.sql",
	"card_anchor_dql_query8.sql",
}

// dqlDocHeader returns the front-matter stanza plus title for a generated
// showcase document (doclint DL001 requires the stanza on every .md; the
// `generated:`/`generator:` keys follow the doc/env-vars.md pattern). No
// generated-at timestamp on purpose: regeneration is content-stable, so an
// unchanged run leaves the working tree clean.
func dqlDocHeader(title string, generator string) string {
	return "---\ntype: reference\naudience: contributor\nstatus: draft\ngenerated: true\ngenerator: go test, " + generator + "\n---\n\n" +
		"> **Status: draft — pre-human-review.** Machine-generated demo artifact;\n> regenerate via `go test`, do not edit.\n\n" +
		"# " + title + "\n\n"
}

func readDqlSource(path string, t *testing.T) string {
	b, err := os.ReadFile(path)
	require.NoError(t, err, path)
	s := strings.TrimSpace(string(b))
	// grammar1 parses a single statement; a trailing ';' is not part of it.
	s = strings.TrimSuffix(s, ";")
	return s
}

// TestDqlPipelineGeneration drives every friendly query through the
// pre-execute stages, records each stage's output in
// card_anchor_dql_pipeline.out.md (the stage-by-stage tour), asserts every
// handle resolved, that the result is grammar2-canonical (the terminal
// ValidateGrammar2 stage) and that the chain is idempotent on its own output,
// then writes the executable form to card_anchor_dql_queryN.out.sql.
func TestDqlPipelineGeneration(t *testing.T) {
	resolver := NewDqlResolver()

	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor DQL — friendly source to executable SQL, stage by stage", "TestDqlPipelineGeneration"))
	doc.WriteString("Each query starts as\n")
	doc.WriteString("friendly-handle SQL (leeway `section:column` handles, unqualified tables)\n")
	doc.WriteString("and passes through the nanopass pre-execute chain; a stage that changed\n")
	doc.WriteString("nothing is omitted. The final stage's output is the committed\n")
	doc.WriteString("`card_anchor_dql_queryN.out.sql` artifact.\n\n")

	for i, p := range dqlQueryFiles {
		src := readDqlSource("./"+p, t)
		fmt.Fprintf(doc, "## %s\n\n### source (friendly handles)\n\n```sql\n%s\n```\n\n", p, src)

		var diags []passes.ColumnDiagnostic
		stages := DqlPreExecuteStages(resolver, func(d passes.ColumnDiagnostic) { diags = append(diags, d) })

		cur := src
		for _, st := range stages {
			out, err := st.Run(cur)
			require.NoError(t, err, "pass %s on %s", st.Name, p)
			if out != cur {
				fmt.Fprintf(doc, "### after %s\n\n```sql\n%s\n```\n\n", st.Name, out)
			}
			cur = out
		}
		require.Empty(t, diags, "unresolved handles in %s", p)
		requireNoLeftoverHandles(t, p, cur)

		// The chain must be idempotent on its own output: a second run (with a
		// fresh diagnostics sink) changes nothing and resolves nothing new.
		again := cur
		for _, st := range DqlPreExecuteStages(resolver, func(d passes.ColumnDiagnostic) { diags = append(diags, d) }) {
			var err error
			again, err = st.Run(again)
			require.NoError(t, err, "idempotence rerun, pass %s on %s", st.Name, p)
		}
		require.Equal(t, cur, again, "pipeline not idempotent on %s", p)
		require.Empty(t, diags, "second run reported diagnostics on %s", p)

		writeFile(fmt.Sprintf("./card_anchor_dql_query%d.out.sql", i+1), cur+"\n", t)
	}
	writeFile("./card_anchor_dql_pipeline.out.md", doc.String(), t)
}

// TestDqlAnalysisGeneration runs the nanopass analysis functions over each
// query's executable form and records the findings in
// card_anchor_dql_analysis.out.md: statement kind, security class and
// witnesses, referenced tables, columns and functions, and the ADR-0117
// passthrough-table triage. The UDF installer script is included to show the
// statement-kind classifier turning away a non-SELECT.
func TestDqlAnalysisGeneration(t *testing.T) {
	resolver := NewDqlResolver()
	pipeline := NewDqlPreExecutePipeline(resolver, nil)

	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor DQL — nanopass analysis of the executable queries", "TestDqlAnalysisGeneration"))

	for _, p := range dqlQueryFiles {
		final, err := pipeline.Run(readDqlSource("./"+p, t))
		require.NoError(t, err, p)

		pr, err := nanopass.Parse(final)
		require.NoError(t, err, p)

		fmt.Fprintf(doc, "## %s\n\n", p)

		kind := analysis.ClassifyStatementKind(final)
		class, witnesses, err := analysis.ClassifyQuerySecurity(pr)
		require.NoError(t, err, p)
		fmt.Fprintf(doc, "- statement kind: `%s`\n", kind)
		fmt.Fprintf(doc, "- security class: `%s`\n", class)
		for _, w := range witnesses {
			fmt.Fprintf(doc, "  - witness: %s `%s`\n", w.Kind, w.Name)
		}

		tables := analysis.ExtractTables(pr)
		names := make([]string, 0, len(tables))
		for _, tr := range tables {
			if tr.Database == "" {
				names = append(names, tr.Table)
				continue
			}
			names = append(names, tr.Database+"."+tr.Table)
		}
		fmt.Fprintf(doc, "- tables: %s\n", mdCodeList(names))

		passthrough, err := analysis.ExtractPassthroughTables(pr, "anchor")
		require.NoError(t, err, p)
		names = names[:0]
		for _, tr := range passthrough {
			names = append(names, tr.Database+"."+tr.Table)
		}
		fmt.Fprintf(doc, "- passthrough tables (ADR-0117): %s\n", mdCodeList(names))

		cols := analysis.ExtractColumns(pr)
		names = names[:0]
		for _, c := range cols {
			names = append(names, c.Column)
		}
		fmt.Fprintf(doc, "- columns (%d refs): %s\n", len(cols), mdCodeList(dedupeSorted(names)))

		funcs := analysis.ExtractFunctions(pr)
		names = names[:0]
		for _, f := range funcs {
			names = append(names, f.Name)
		}
		fmt.Fprintf(doc, "- functions: %s\n\n", mdCodeList(dedupeSorted(names)))
	}

	// A pack installer statement is CREATE FUNCTION — not a SELECT. The
	// classifier lands it on a non-read-only kind, which is how an
	// executor's read-only gate turns it away before parsing. Query 1
	// depends on the pack (ADR-0162), so its first installer statement is
	// the specimen.
	fmt.Fprintf(doc, "## co/ragged pack installer (ADR-0162)\n\n- statement kind: `%s`\n",
		analysis.ClassifyStatementKind(chpack.Statements()[0]))

	writeFile("./card_anchor_dql_analysis.out.md", doc.String(), t)
}

// TestDqlLwsqlGeneration exercises the two lwsql directions that complete the
// friendly-handle story and records both in card_anchor_dql_lwsql.out.md:
//
//   - ExposeSelectionConditions (ADR-0121) on query 7 — the retrieval read —
//     so each returned row reports which OR disjunct admitted it. The pass
//     gates on the ADR-0117 passthrough triage (single table, verbatim
//     projection), which is why it applies to query 7 and to none of 1–6.
//     Shown twice: plain naming (`cond_N`, bolted beside the schema) and
//     leeway naming (the resolver as ConditionNamerI, condition columns as
//     physical members of a declared `conditions` section).
//   - BuildLabels — the result-side inverse of the resolver: physical column
//     names back to `section:column` display labels, covering value, support
//     and plain/backbone columns alike.
func TestDqlLwsqlGeneration(t *testing.T) {
	resolver := NewDqlResolver()
	pipeline := NewDqlPreExecutePipeline(resolver, nil)
	final, err := pipeline.Run(readDqlSource("./card_anchor_dql_query7.sql", t))
	require.NoError(t, err)

	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor DQL — lwsql selection conditions and result labels", "TestDqlLwsqlGeneration"))
	doc.WriteString("## selection conditions (ADR-0121) on query 7\n\n")
	doc.WriteString("The WHERE is two OR-ed disjuncts, so each returned row gains one boolean\n")
	doc.WriteString("column per disjunct reporting which alternative admitted it. The rewrite\n")
	doc.WriteString("applies because query 7 passes the ADR-0117 passthrough triage; the\n")
	doc.WriteString("computing queries 1-6 do not, and the pass declines them untouched.\n\n")
	fmt.Fprintf(doc, "### input (query 7, executable form)\n\n```sql\n%s\n```\n\n", final)

	plain, err := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
		Schema:          NewDqlSchemaProvider(),
		DefaultDatabase: "anchor",
	}).Run(final)
	require.NoError(t, err)
	require.NotEqual(t, final, plain, "conditions rewrite must apply")
	fmt.Fprintf(doc, "### plain naming (no namer) — `cond_N` beside the schema\n\n```sql\n%s\n```\n\n", plain)

	leeway, err := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
		Schema:          NewDqlSchemaProvider(),
		Namer:           resolver,
		DefaultDatabase: "anchor",
	}).Run(final)
	require.NoError(t, err)
	require.NotEqual(t, final, leeway, "conditions rewrite must apply")
	fmt.Fprintf(doc, "### leeway naming (resolver as ConditionNamerI) — a `conditions` section\n\n```sql\n%s\n```\n\n", leeway)

	condNames, ok, err := resolver.NameConditions("anchor", "facts", 2)
	require.NoError(t, err)
	require.True(t, ok, "anchor.facts must be leeway-shaped for the namer")
	doc.WriteString("The synthesized condition columns are physical leeway names, so a result\n")
	doc.WriteString("carrying them classifies back into the schema like any other column:\n\n")
	condLabels := lwsql.BuildLabels(condNames)
	for _, n := range condNames {
		fmt.Fprintf(doc, "- `%s` → label `%s`\n", n, condLabels[n])
	}
	doc.WriteString("\n")

	doc.WriteString("## result labels — BuildLabels over every anchor column\n\n")
	doc.WriteString("The SQL shipped to the server keeps physical names; a result surface shows\n")
	doc.WriteString("these labels instead (physical name on hover). Support columns and the\n")
	doc.WriteString("plain/backbone columns label the same way.\n\n")
	physical := DqlPhysicalColumnNames()
	labels := lwsql.BuildLabels(physical)
	require.Len(t, labels, len(physical), "every physical column must label")
	doc.WriteString("| physical | label |\n|---|---|\n")
	for _, n := range physical {
		fmt.Fprintf(doc, "| `%s` | `%s` |\n", n, labels[n])
	}

	writeFile("./card_anchor_dql_lwsql.out.md", doc.String(), t)
}

// requireNoLeftoverHandles fails if the executable SQL still contains a
// friendly handle — a quoted identifier with exactly one colon. Physical
// leeway names carry many colons, so one colon inside quotes is a handle the
// resolve pass never visited. This is a distinct check from the diagnostics
// sink: a handle in a region the pass does not walk (e.g. a query-level WITH
// expression clause) is neither resolved NOR reported, so only the output
// itself can prove full coverage.
func requireNoLeftoverHandles(t *testing.T, queryFile string, sql string) {
	t.Helper()
	for _, m := range regexp.MustCompile("[\"`]([^\"`]+)[\"`]").FindAllStringSubmatch(sql, -1) {
		if strings.Count(m[1], ":") == 1 {
			t.Fatalf("%s: unresolved handle %q survived the pipeline", queryFile, m[1])
		}
	}
}

func dedupeSorted(in []string) (out []string) {
	seen := make(map[string]struct{}, len(in))
	out = make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return
}

func mdCodeList(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}
