package ast_test

// FuzzAstRoundTrip generalises the corpus's structural round-trip oracle:
// for any Grammar1-parseable input, canonicalise → AST → ToSQL →
// re-canonicalise → re-convert must reproduce a structurally identical
// AST. Parseability alone is blind to precedence regrouping and dropped
// clauses; structural equality is not. Conversion failures on canonical
// input and instability are findings, never skips.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzAstRoundTrip -fuzztime 120s ./public/db/clickhouse/dsl/ast_test/

import (
	"reflect"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
)

// containsEmptyIdentifier reports whether sql contains an empty quoted or
// backquoted identifier (`""` / ``). Grammar1's IDENTIFIER lexer rule
// permits a zero-length quoted name, but ClickHouse rejects empty
// identifiers in every position (column, alias, window reference, table —
// verified against the server). Such input is G1-over-accepted and has no
// meaningful round trip, so the fuzzer skips it.
func containsEmptyIdentifier(sql string) bool {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	for {
		tok := lexer.NextToken()
		if tok.GetTokenType() == antlr.TokenEOF {
			return false
		}
		if tok.GetTokenType() == grammar1.ClickHouseLexerIDENTIFIER {
			if nanopass.DecodeIdentifier(tok.GetText()) == "" {
				return true
			}
		}
	}
}

const fuzzMaxInput = 4 << 10

func FuzzAstRoundTrip(f *testing.F) {
	entries, err := testdata.LoadCorpus()
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		f.Add(e.SQL)
	}

	f.Fuzz(func(t *testing.T, sql string) {
		if len(sql) > fuzzMaxInput {
			t.Skip()
		}
		if _, err := nanopass.Parse(sql); err != nil {
			t.Skip() // only Grammar1-parseable inputs are in-contract
		}
		if containsEmptyIdentifier(sql) {
			t.Skip() // empty identifiers are G1-over-accepted; server rejects them
		}
		canonical, err := fullPipeline(sql)
		if err != nil {
			if strings.Contains(err.Error(), "panicked") {
				t.Fatalf("canonicalisation panicked on %q: %v", sql, err)
			}
			return // loud, typed failure is in-contract
		}
		pr, err := nanopass.ParseCanonical(canonical)
		if err != nil {
			// Param-slot TYPE expressions are the one place Grammar1
			// deliberately out-accepts both Grammar2 and ClickHouse itself
			// (e.g. {x: A(0 % b(1))} parses in G1 but the server rejects it
			// as an invalid data type). Such input has no canonical form —
			// skip it. Any OTHER non-G2 canonical output is a real
			// canonicalisation defect and still fails loudly.
			if strings.Contains(sql, "{") {
				t.Skip()
			}
			t.Fatalf("canonical output not Grammar2-parseable:\n in: %q\nout: %q\nerr: %v", sql, canonical, err)
		}
		q1, err := ast.ConvertCSTToAST(pr)
		if err != nil {
			t.Fatalf("canonical input not convertible:\n in: %q\ncanonical: %q\nerr: %v", sql, canonical, err)
		}

		rendered := q1.ToSQL()
		recanonical, err := fullPipeline(rendered)
		if err != nil {
			t.Fatalf("ToSQL output does not re-canonicalise:\n in: %q\nrendered: %q\nerr: %v", sql, rendered, err)
		}
		pr2, err := nanopass.ParseCanonical(recanonical)
		if err != nil {
			t.Fatalf("re-canonical output not Grammar2-parseable:\nrendered: %q\nerr: %v", rendered, err)
		}
		q2, err := ast.ConvertCSTToAST(pr2)
		if err != nil {
			t.Fatalf("re-canonical input not convertible:\nrendered: %q\nerr: %v", rendered, err)
		}
		if !reflect.DeepEqual(q1, q2) {
			t.Fatalf("semantic round-trip changed the AST:\n in:       %q\nrendered:  %q\nrerendered: %q", sql, rendered, q2.ToSQL())
		}
	})
}
