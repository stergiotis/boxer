//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lexAllGrammar1(sql string) ([]antlr.Token, *grammar1.ClickHouseLexer) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	tokens := make([]antlr.Token, stream.Size())
	for i := range tokens {
		tokens[i] = stream.Get(i)
	}
	return tokens, lexer
}

func lexAllGrammar2(sql string) ([]antlr.Token, *grammar2.ClickHouseLexer) {
	lexer := grammar2.NewClickHouseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	tokens := make([]antlr.Token, stream.Size())
	for i := range tokens {
		tokens[i] = stream.Get(i)
	}
	return tokens, lexer
}

func assertTokenStreamsIdentical(t *testing.T, sql string, tokens1 []antlr.Token, tokens2 []antlr.Token) {
	t.Helper()
	require.Equal(t, len(tokens1), len(tokens2),
		"token count: grammar1=%d grammar2=%d\nsql: %s", len(tokens1), len(tokens2), sql)
	for i := range tokens1 {
		a, b := tokens1[i], tokens2[i]
		if a.GetTokenType() != b.GetTokenType() || a.GetText() != b.GetText() ||
			a.GetChannel() != b.GetChannel() || a.GetStart() != b.GetStart() || a.GetStop() != b.GetStop() {
			t.Errorf("token[%d] mismatch: type(%d/%d) text(%q/%q) channel(%d/%d) start(%d/%d) stop(%d/%d)\nsql: %s",
				i, a.GetTokenType(), b.GetTokenType(), a.GetText(), b.GetText(),
				a.GetChannel(), b.GetChannel(), a.GetStart(), b.GetStart(), a.GetStop(), b.GetStop(), sql)
		}
	}
}

// TestLexerSymbolicNamesIdentical compares the full symbolic name table
// between both lexers via ANTLR runtime introspection.
func TestLexerSymbolicNamesIdentical(t *testing.T) {
	_, l1 := lexAllGrammar1("")
	_, l2 := lexAllGrammar2("")
	names1 := l1.GetSymbolicNames()
	names2 := l2.GetSymbolicNames()
	require.Equal(t, len(names1), len(names2),
		"symbolic names count: grammar1=%d grammar2=%d", len(names1), len(names2))
	for i := range names1 {
		assert.Equal(t, names1[i], names2[i],
			"symbolic name[%d]: grammar1=%q grammar2=%q", i, names1[i], names2[i])
	}
}

// TestLexerRuleNamesIdentical compares the lexer rule name tables.
func TestLexerRuleNamesIdentical(t *testing.T) {
	_, l1 := lexAllGrammar1("")
	_, l2 := lexAllGrammar2("")
	rules1 := l1.GetRuleNames()
	rules2 := l2.GetRuleNames()
	require.Equal(t, len(rules1), len(rules2),
		"rule names count: grammar1=%d grammar2=%d", len(rules1), len(rules2))
	for i := range rules1 {
		assert.Equal(t, rules1[i], rules2[i],
			"rule name[%d]: grammar1=%q grammar2=%q", i, rules1[i], rules2[i])
	}
}

// TestLexerCorpusIdentical lexes every corpus entry with both lexers and
// compares every token attribute.
func TestLexerCorpusIdentical(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			t1, _ := lexAllGrammar1(entry.SQL)
			t2, _ := lexAllGrammar2(entry.SQL)
			assertTokenStreamsIdentical(t, entry.SQL, t1, t2)
		})
	}
}

// TestLexerEdgeCasesIdentical covers lexer edge cases not in the corpus.
func TestLexerEdgeCasesIdentical(t *testing.T) {
	cases := []string{
		"SELECT `col` FROM `tbl`",
		`SELECT "col" FROM "tbl"`,
		"SELECT col FROM tbl",
		"SELECT a + b - c * d / e % f || g, a = b, a == b, a != b, a <> b, a < b, a > b, a <= b, a >= b, a :: UInt64, a -> b",
		"SELECT a[1], {p: UInt64}",
		"SELECT true, false, NULL, inf, nan",
		"SELECT a -- line\nFROM t /* block */ WHERE 1=1",
		"SELECT\t\ta\r\nFROM\n\nt",
		"SELECT 1",
		"   SELECT   1   ",
		"SELECT ''",
		"SELECT 1 SETTINGS s = [1, 2, 3]",
	}
	for _, sql := range cases {
		name := sql
		if len(name) > 40 {
			name = name[:40]
		}
		t.Run(name, func(t *testing.T) {
			t1, _ := lexAllGrammar1(sql)
			t2, _ := lexAllGrammar2(sql)
			assertTokenStreamsIdentical(t, sql, t1, t2)
		})
	}
}
