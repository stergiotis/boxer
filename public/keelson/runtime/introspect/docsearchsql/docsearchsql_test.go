package docsearchsql

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

func expandOK(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := expand(sql)
	if err != nil {
		t.Fatalf("expand(%q): %v", sql, err)
	}
	return
}

func TestPassthroughWithoutMacro(t *testing.T) {
	const sql = "SELECT 1 FROM system.one"
	if got := expandOK(t, sql); got != sql {
		t.Errorf("macro-free SQL must pass through byte-identical, got %q", got)
	}
}

func TestExpansionShape(t *testing.T) {
	out := expandOK(t, "SELECT * FROM docsearch('argMax dedup') ORDER BY score DESC LIMIT 20")
	for _, want := range []string{
		"keelson('helpsections')",
		"keelson('adrsections')",
		"system.documentation",
		`'(?i)argMax'`,
		`'(?i)dedup'`,
		"multiMatchAllIndices",
		"NOT has(w, 0)",
		"'chdoc://'",
		"ORDER BY score DESC LIMIT 20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expansion misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "docsearch") {
		t.Errorf("expansion must contain no macro call:\n%s", out)
	}
	// The emitted statement must parse — later passes in the pipeline
	// (identsql, column-handle resolution) walk it.
	if _, err := nanopass.Parse(out); err != nil {
		t.Errorf("expansion does not re-parse: %v\n%s", err, out)
	}
}

func TestIdempotent(t *testing.T) {
	once := expandOK(t, "SELECT ref FROM docsearch('window')")
	twice := expandOK(t, once)
	if once != twice {
		t.Errorf("second expansion changed the text")
	}
}

func TestThesaurusReachesTheExpansion(t *testing.T) {
	// `lcase` is a ClickHouse alias for `lower` (chaliases.gen.go, from
	// the pinned engine): the spliced pattern must carry the canonical
	// spelling as an alternation branch, so a docsearch for the alias
	// finds documentation that only writes the real name.
	out := expandOK(t, "SELECT * FROM docsearch('lcase')")
	if !strings.Contains(out, "(?:lcase|lower)") {
		t.Errorf("expansion should alternate the alias with its canonical name:\n%s", out)
	}
}

func TestInvalidRegexTokenDegradesToLiteral(t *testing.T) {
	out := expandOK(t, "SELECT * FROM docsearch('quantile(')")
	if !strings.Contains(out, `'(?i)quantile\\('`) {
		t.Errorf("unbalanced-paren token should splice quote-meta'd:\n%s", out)
	}
}

func TestQueryEscaping(t *testing.T) {
	// A quote inside the query must survive the round trip into the
	// pattern literal without breaking either statement.
	out := expandOK(t, `SELECT * FROM docsearch('o\'brien')`)
	if _, err := nanopass.Parse(out); err != nil {
		t.Fatalf("expansion with escaped quote does not re-parse: %v", err)
	}
}

func TestMultipleCallsExpandIndependently(t *testing.T) {
	out := expandOK(t, "SELECT * FROM docsearch('alpha') UNION ALL SELECT * FROM docsearch('beta')")
	if strings.Count(out, "keelson('helpsections')") != 2 {
		t.Errorf("each call must expand:\n%s", out)
	}
}

func TestScalarPositionIgnored(t *testing.T) {
	const sql = "SELECT docsearch('x') FROM system.one"
	if got := expandOK(t, sql); got != sql {
		t.Errorf("scalar docsearch is not the macro, got %q", got)
	}
}

func TestRejects(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM docsearch()",
		"SELECT * FROM docsearch('a', 'b')",
		"SELECT * FROM docsearch(name)",
		"SELECT * FROM docsearch('')",
		"SELECT * FROM docsearch('   ')",
	} {
		if _, err := expand(sql); err == nil {
			t.Errorf("expand(%q) should fail", sql)
		}
	}
}
