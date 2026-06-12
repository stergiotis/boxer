package nanopass_test

// Benchmarks for the nanopass core: parsing (the dominant per-pass cost),
// scope construction, CST traversal, token rewriting, the Pass runner, and
// the small hot helpers (discard scan, byte-range conversion, identifier
// codec). Run with:
//
//	go test -bench BenchmarkNanopass -benchmem -run xxx ./public/db/clickhouse/dsl/nanopass_test/
//
// Inputs are deterministic: three synthetic sizes plus the embedded corpus.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
)

const benchSmallSQL = "SELECT a FROM t WHERE x > 1"

const benchMediumSQL = `WITH active AS (SELECT id, tenant_id FROM accounts WHERE state = 'active'),
recent AS (SELECT account_id, max(ts) AS last_seen FROM events GROUP BY account_id)
SELECT a.id, a.tenant_id, r.last_seen, count(*) AS n,
  CASE WHEN r.last_seen > '2024-01-01' THEN 'fresh' ELSE 'stale' END AS freshness
FROM active AS a
JOIN recent AS r ON a.id = r.account_id
LEFT JOIN orders o ON o.account_id = a.id
WHERE a.tenant_id IN (1, 2, 3) AND o.amount BETWEEN 10 AND 1000
GROUP BY a.id, a.tenant_id, r.last_seen
HAVING n > 2
ORDER BY r.last_seen DESC
LIMIT 100 SETTINGS max_threads = 4`

// benchLargeSQL is generated deterministically: a 5-CTE, 6-branch union with
// wide projections — roughly the upper end of human-written queries.
var benchLargeSQL = func() string {
	var b strings.Builder
	b.WriteString("WITH ")
	for c := 0; c < 5; c++ {
		if c > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "cte%d AS (SELECT id, v%d FROM src%d WHERE v%d > %d)", c, c, c, c, c)
	}
	for u := 0; u < 6; u++ {
		if u > 0 {
			b.WriteString(" UNION ALL ")
		}
		b.WriteString("SELECT ")
		for col := 0; col < 40; col++ {
			if col > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "t.c%d + %d AS a%d", col, u, col)
		}
		fmt.Fprintf(&b, " FROM big%d AS t JOIN cte%d ON t.id = cte%d.id WHERE t.c0 IN (1, 2, 3, 4) AND t.c1 BETWEEN %d AND %d", u, u%5, u%5, u, u+100)
	}
	return b.String()
}()

var benchSizes = []struct {
	name string
	sql  string
}{
	{"small", benchSmallSQL},
	{"medium", benchMediumSQL},
	{"large", benchLargeSQL},
}

func BenchmarkNanopassParse(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(sz.sql)))
			for b.Loop() {
				if _, err := nanopass.Parse(sz.sql); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkNanopassParseCorpus sweeps the full embedded corpus per
// iteration — the cost profile of one corpus-wide pass application.
func BenchmarkNanopassParseCorpus(b *testing.B) {
	entries, err := testdata.LoadCorpus()
	if err != nil {
		b.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		total += len(e.SQL)
	}
	b.ReportAllocs()
	b.SetBytes(int64(total))
	for b.Loop() {
		for _, e := range entries {
			if _, err := nanopass.Parse(e.SQL); err != nil {
				b.Fatalf("%s: %v", e.Name, err)
			}
		}
	}
}

func BenchmarkNanopassParseCanonical(b *testing.B) {
	canonical, err := passes.CanonicalizeFull(16).Run(benchMediumSQL)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(canonical)))
	for b.Loop() {
		if _, err := nanopass.ParseCanonical(canonical); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNanopassBuildScopes(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			pr, err := nanopass.Parse(sz.sql)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				scopes, err := nanopass.BuildScopes(pr, "db")
				if err != nil {
					b.Fatal(err)
				}
				_ = nanopass.FlattenScopes(scopes)
			}
		})
	}
}

func BenchmarkNanopassWalkCST(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			pr, err := nanopass.Parse(sz.sql)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				n := 0
				nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
					n++
					return true
				})
				if n == 0 {
					b.Fatal("walk visited nothing")
				}
			}
		})
	}
}

func BenchmarkNanopassFindAllIdentifiers(b *testing.B) {
	pr, err := nanopass.Parse(benchLargeSQL)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			_, ok := ctx.(*grammar1.IdentifierContext)
			return ok
		})
		if len(nodes) == 0 {
			b.Fatal("no identifiers found")
		}
	}
}

// BenchmarkNanopassRewriteCycle is the canonical pass shape: fresh rewriter,
// node replacements, text emission (parse hoisted — measured separately).
func BenchmarkNanopassRewriteCycle(b *testing.B) {
	pr, err := nanopass.Parse(benchMediumSQL)
	if err != nil {
		b.Fatal(err)
	}
	tables := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.TableIdentifierContext)
		return ok
	})
	if len(tables) == 0 {
		b.Fatal("no table identifiers")
	}
	b.ReportAllocs()
	for b.Loop() {
		rw := nanopass.NewRewriter(pr)
		for _, tid := range tables {
			nanopass.ReplaceNode(rw, tid, "db.x")
		}
		if out := nanopass.GetText(rw); len(out) == 0 {
			b.Fatal("empty rewrite")
		}
	}
}

func BenchmarkNanopassPassRun(b *testing.B) {
	b.Run("StripComments", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := passes.StripComments.Run(benchMediumSQL); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("QualifyTables", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := passes.QualifyTables("db").Run(benchMediumSQL); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("CanonicalizeFull", func(b *testing.B) {
		p := passes.CanonicalizeFull(16)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := p.Run(benchMediumSQL); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkNanopassIsDiscardOutput(b *testing.B) {
	// Worst case: no marker, full scan; quote-dense to exercise skipQuoted.
	clean := strings.Repeat("SELECT 'a''b' AS \"col\", `q` FROM t WHERE s = 'text' ", 64)
	marked := clean + nanopass.PassDiscardOutputMarker
	b.Run("clean", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(clean)))
		for b.Loop() {
			if nanopass.IsDiscardOutput(clean) {
				b.Fatal("false positive")
			}
		}
	})
	b.Run("marked", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(marked)))
		for b.Loop() {
			if !nanopass.IsDiscardOutput(marked) {
				b.Fatal("missed marker")
			}
		}
	})
}

func BenchmarkNanopassSourceRangeOf(b *testing.B) {
	sql := "SELECT 'héllø wörld', f(x), 'déjà vu' FROM t WHERE name LIKE '%ü%' AND marked(1) > 0"
	pr, err := nanopass.Parse(sql)
	if err != nil {
		b.Fatal(err)
	}
	var fn antlr.ParserRuleContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar1.ColumnExprFunctionContext); ok && fn == nil {
			fn = ctx
		}
		return true
	})
	if fn == nil {
		b.Fatal("no function context")
	}
	b.ReportAllocs()
	for b.Loop() {
		if r := pr.SourceRangeOf(fn); r.Empty() {
			b.Fatal("empty range")
		}
	}
}

func BenchmarkNanopassIdentifierCodec(b *testing.B) {
	spellings := []string{"bare_ident", `"quoted name"`, "`back``ticked`", `"esc\"aped"`}
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, s := range spellings {
				if nanopass.DecodeIdentifier(s) == "" {
					b.Fatal("empty decode")
				}
			}
		}
	})
	b.Run("quote", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if nanopass.QuoteIdentifier(`a"b\c`) == "" {
				b.Fatal("empty quote")
			}
		}
	})
}

func BenchmarkNanopassMacroExpand(b *testing.B) {
	me := nanopass.NewMacroExpander()
	me.Register("threshold", func(args []nanopass.LiteralArg) (string, error) {
		return "(" + args[0].Value + " * 100)", nil
	})
	me.Register("base", func(args []nanopass.LiteralArg) (string, error) { return "42", nil })
	p := me.Pass()
	sql := "SELECT a FROM t WHERE x > threshold(base()) AND y < threshold(5)"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := p.Run(sql); err != nil {
			b.Fatal(err)
		}
	}
}
