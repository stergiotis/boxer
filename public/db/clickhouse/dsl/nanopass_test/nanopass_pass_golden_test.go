package nanopass_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"iter"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// The pass golden pins what every pass, the scope builder, the analyses,
// highlighting and the canonical AST produce for each fixture, so a change
// underneath them — a grammar edit, a regenerated parser, a runtime swap —
// has to reproduce their behaviour exactly or say which output moved.
//
// Each cell is a short digest of the output, or of the error text. The parse
// tree itself is deliberately absent: the tree is free to change shape as long
// as nothing that reads it does. On a mismatch the test prints the current
// output in full; rerun with -update-pass-golden only once the change is
// understood.

var updatePassGolden = flag.Bool("update-pass-golden", false, "rewrite testdata/pass_golden.txt from the current behaviour")

const passGoldenPath = "testdata/pass_golden.txt"

type goldenSchema struct{}

func (goldenSchema) GetColumns(db, table string) (iter.Seq[string], int, bool) {
	cols := []string{"id", "a", "b", "c", "x", "ts", "name", "value"}
	return slices.Values(cols), len(cols), true
}

type goldenResolver struct{}

func (goldenResolver) Resolve(db, table, handle string) passes.ResolveResult {
	if strings.HasPrefix(handle, "h_") {
		return passes.ResolveResult{Kind: passes.ResolveOK, Physical: []string{"phys_" + handle}}
	}
	return passes.ResolveResult{Kind: passes.ResolveNotAHandle}
}

type goldenPass struct {
	name string
	mk   func() nanopass.Pass
}

func goldenPasses() []goldenPass {
	return []goldenPass{
		{"CanonicalizeEquals", func() nanopass.Pass { return passes.CanonicalizeEquals }},
		{"CanonicalizeCasts", func() nanopass.Pass { return passes.CanonicalizeCasts }},
		{"CanonicalizeCaseConditionals", func() nanopass.Pass { return passes.CanonicalizeCaseConditionals }},
		{"CanonicalizeMultiIf", func() nanopass.Pass { return passes.CanonicalizeMultiIf }},
		{"RemoveRedundantParens", func() nanopass.Pass { return passes.RemoveRedundantParens }},
		{"CanonicalizeInsertWrapper", func() nanopass.Pass { return passes.CanonicalizeInsertWrapper }},
		{"CanonicalizeKeywordCase", func() nanopass.Pass { return passes.CanonicalizeKeywordCase }},
		{"CanonicalizeSugar", func() nanopass.Pass { return passes.CanonicalizeSugar }},
		{"CanonicalizeIdentifiers", func() nanopass.Pass { return passes.CanonicalizeIdentifiers }},
		{"CanonicalizeJoin", func() nanopass.Pass { return passes.CanonicalizeJoin }},
		{"StripComments", func() nanopass.Pass { return passes.StripComments }},
		{"CanonicalizeTernary", func() nanopass.Pass { return passes.CanonicalizeTernary }},
		{"CanonicalizeWhitespace", func() nanopass.Pass { return passes.CanonicalizeWhitespace }},
		{"CanonicalizeWhitespaceSingleLine", func() nanopass.Pass { return passes.CanonicalizeWhitespaceSingleLine }},
		{"CanonicalizeConstructorsFunction", func() nanopass.Pass { return passes.CanonicalizeConstructors(passes.ConstructorFormFunction) }},
		{"CanonicalizeConstructorsLiteral", func() nanopass.Pass { return passes.CanonicalizeConstructors(passes.ConstructorFormLiteral) }},
		{"CanonicalizeFull", func() nanopass.Pass { return passes.CanonicalizeFull(16) }},
		{"QualifyTables", func() nanopass.Pass { return passes.QualifyTables("dflt") }},
		{"SetFormat", func() nanopass.Pass { return passes.SetFormat("JSONEachRow") }},
		{"WrapColumnsWithDynamic", func() nanopass.Pass { return passes.WrapColumnsWithDynamic(".*_id$") }},
		{"ValidateColumnNames", func() nanopass.Pass { return passes.ValidateColumnNames("^[a-z_][a-z0-9_]*$") }},
		{"ExtractLiterals", func() nanopass.Pass { return passes.ExtractLiterals(passes.NewExtractLiteralsConfig(1)) }},
		{"PruneUnreferencedParams", func() nanopass.Pass { return passes.PruneUnreferencedParams("p_") }},
		{"WriteSettings", func() nanopass.Pass { return passes.WriteSettings(map[string]any{"max_threads": int64(4)}) }},
		{"ExpandColumns", func() nanopass.Pass { return passes.ExpandColumns(goldenSchema{}, "dflt") }},
		{"ResolveColumnNames", func() nanopass.Pass {
			return passes.ResolveColumnNames(goldenResolver{}, "dflt", func(passes.ColumnDiagnostic) {})
		}},
		{"ExposeSelectionConditions", func() nanopass.Pass {
			return passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{Schema: goldenSchema{}, DefaultDatabase: "dflt"})
		}},
		{"InjectParamsAsCTE", func() nanopass.Pass {
			return passes.InjectParamsAsCTE("", nil, func(canonicaltypes.PrimitiveAstNodeI) (string, error) { return "String", nil })
		}},
		{"EvaluateFunctions", func() nanopass.Pass {
			fe := passes.NewFunctionEvaluator()
			fe.RegisterBuiltins()
			return fe.Pass()
		}},
	}
}

// goldenExtraInputs are the shapes the fixtures under-represent: every
// position a qualified name, the COLUMNS keyword or a parameter slot can take.
var goldenExtraInputs = []struct{ name, sql string }{
	{"qualified_projection", "SELECT t.a, t.b, db.t.c, t.n.f, n.f FROM db.t AS t"},
	{"qualified_everywhere", "SELECT t.a FROM t WHERE t.a = u.b AND t.c IN (SELECT u.c FROM u) GROUP BY t.a HAVING max(t.b) > 1 ORDER BY t.a LIMIT 1"},
	{"qualified_join", "SELECT t1.x, t2.y FROM t1 JOIN db2.t2 AS t2 ON t1.id = t2.id LEFT JOIN t3 ON t3.id = t1.id"},
	{"qualified_star", "SELECT t.*, db.t.* FROM db.t AS t"},
	{"columns_qualifier", "SELECT columns.name, system.columns.type FROM system.columns"},
	{"columns_upper", "SELECT COLUMNS.name FROM system.COLUMNS"},
	{"param_table", "SELECT a FROM {db:Identifier}.{t:Identifier} WHERE {t:Identifier}.a = {v:UInt8}"},
	{"handle_resolution", "SELECT t.h_x, h_y FROM t"},
	{"dynamic_columns", "SELECT x_id, t.x_id, y FROM t"},
	{"insert_qualified", "INSERT INTO db.t (a, n.b) SELECT s.a, s.b FROM s"},
	{"cast_between", "SELECT CAST(t.a AS UInt64) FROM t WHERE t.b BETWEEN 1 AND 10"},
	{"tuple_access", "SELECT f(x).field, LW_COMPONENT('a').MyField FROM t"},
	{"invalid_dangling_dot", "SELECT t. FROM t"},
	{"invalid_trailing", "SELECT a FROM t WHERE"},
}

func goldenDigest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

func goldenJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "json error: " + err.Error()
	}
	return string(b)
}

func goldenResult(s string, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return s
}

// goldenScopes renders the scope forest without pointers.
func goldenScopes(pr *nanopass.ParseResult) string {
	ss, err := nanopass.BuildScopes(pr, "dflt")
	if err != nil {
		return "error: " + err.Error()
	}
	var b strings.Builder
	for _, s := range nanopass.FlattenScopes(ss) {
		r := pr.SourceRangeOf(s.Node)
		fmt.Fprintf(&b, "[%d,%d) unions=%d subs=%d", r.Start, r.End, len(s.UnionMembers), len(s.Subqueries))
		for _, t := range s.Tables {
			fmt.Fprintf(&b, " table=%s|%s|%s|%v|%v|%v", t.Database, t.Table, t.Alias, t.IsCTE, t.IsSubquery, t.IsFunction)
		}
		for _, c := range s.CTEDefs {
			fmt.Fprintf(&b, " cte=%s|%d|%v", c.Name, len(c.Scopes), c.Recursive)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// goldenRecover turns a panic into an output, so a panicking consumer is
// pinned as such rather than aborting the run.
func goldenRecover(f func() string) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = fmt.Sprintf("panic: %v", r)
		}
	}()
	return f()
}

// goldenOutputs computes every pinned output of one input, keyed by name.
func goldenOutputs(sql string) (keys []string, outputs map[string]string) {
	outputs = make(map[string]string)
	add := func(k string, f func() string) {
		keys = append(keys, k)
		outputs[k] = goldenRecover(f)
	}
	pr, err := nanopass.Parse(sql)
	add("parse", func() string { return goldenResult("ok", err) })
	add("kind", func() string { return fmt.Sprint(analysis.ClassifyStatementKind(sql)) })
	add("highlight", func() string { return goldenJSON(highlight.Highlight(sql)) })
	if err != nil {
		return
	}
	add("scopes", func() string { return goldenScopes(pr) })
	add("columns", func() string { return goldenJSON(analysis.ExtractColumns(pr)) })
	add("tables", func() string { return goldenJSON(analysis.ExtractTables(pr)) })
	add("functions", func() string { return goldenJSON(analysis.ExtractFunctions(pr)) })
	add("passthrough", func() string {
		r, err := analysis.ExtractPassthroughTables(pr, "dflt")
		return goldenResult(goldenJSON(r), err)
	})
	add("security", func() string {
		c, w, err := analysis.ClassifyQuerySecurity(pr)
		return goldenResult(goldenJSON([]any{c, w}), err)
	})
	for _, p := range goldenPasses() {
		add(p.name, func() string { return goldenResult(p.mk().Run(sql)) })
	}
	add("canonical_ast", func() string {
		c, err := passes.CanonicalizeFull(16).Run(sql)
		if err != nil {
			return "not canonicalisable"
		}
		pr2, err := nanopass.ParseCanonical(c)
		if err != nil {
			return "error: " + err.Error()
		}
		q, err := ast.ConvertCSTToAST(pr2)
		return goldenResult(goldenJSON(q), err)
	})
	return
}

func goldenInputs(t *testing.T) (names, sqls []string) {
	t.Helper()
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	for _, e := range entries {
		names = append(names, "corpus/"+e.Name)
		sqls = append(sqls, e.SQL)
	}
	for _, tc := range predictionCorpus() {
		names = append(names, "prediction/"+tc.name)
		sqls = append(sqls, tc.sql)
	}
	for _, tc := range goldenExtraInputs {
		names = append(names, "extra/"+tc.name)
		sqls = append(sqls, tc.sql)
	}
	return
}

func TestPassGolden(t *testing.T) {
	names, sqls := goldenInputs(t)
	var b strings.Builder
	got := make(map[string]map[string]string, len(names))
	for i, name := range names {
		keys, outputs := goldenOutputs(sqls[i])
		got[name] = outputs
		fmt.Fprintf(&b, "%s", name)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%s", k, goldenDigest(outputs[k]))
		}
		b.WriteString("\n")
	}
	if *updatePassGolden {
		require.NoError(t, os.WriteFile(passGoldenPath, []byte(b.String()), 0o644))
		return
	}
	raw, err := os.ReadFile(passGoldenPath)
	require.NoError(t, err, "no golden yet: run with -update-pass-golden")

	want := make(map[string]map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		cells := make(map[string]string, len(fields)-1)
		for _, f := range fields[1:] {
			k, v, _ := strings.Cut(f, "=")
			cells[k] = v
		}
		want[fields[0]] = cells
	}
	for i, name := range names {
		cells, ok := want[name]
		if !ok {
			t.Errorf("%s: not in the golden; run with -update-pass-golden", name)
			continue
		}
		for k, out := range got[name] {
			if w, ok := cells[k]; !ok || w != goldenDigest(out) {
				t.Errorf("%s: %s changed (golden %q)\ninput:  %s\noutput: %s", name, k, w, sqls[i], out)
			}
		}
		for k := range cells {
			if _, ok := got[name][k]; !ok {
				t.Errorf("%s: %s is in the golden but was not produced", name, k)
			}
		}
	}
}
