//go:build llm_generated_opus47

package passes_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/require"
)

// nestedFormsCorpus contains SQL that exercises nested CASE / ternary /
// cast / constructor forms. NeedsFixedPoint passes must show
// non-convergence on at least one of these — that justifies the flag.
var nestedFormsCorpus = []string{
	"SELECT CASE WHEN CASE WHEN a = 1 THEN 'inner' ELSE 'other' END = 'inner' THEN 'yes' ELSE 'no' END FROM t",
	"SELECT a ? b ? c : d : e FROM t",
	"SELECT CAST(CAST(a AS UInt64) AS UInt32) FROM t",
	"SELECT array(array(1, 2), array(3, 4)) FROM t",
	"SELECT [[1, 2], [3, 4]] FROM t",
}

// TestAssertProperties exercises every pass against the shared corpus and
// validates its declared PassProperties. Idempotent passes must converge in
// one Apply; NeedsFixedPoint passes must show non-convergence on at least
// one corpus entry; the two flags must not both be set.
func TestAssertProperties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	baseCorpus := make([]string, 0, len(entries))
	for _, entry := range entries {
		baseCorpus = append(baseCorpus, entry.SQL)
	}

	// nestedCorpus is baseCorpus plus the nested-forms set; used for any
	// pass that declares NeedsFixedPoint, so the justification check has
	// material to work with.
	nestedCorpus := make([]string, 0, len(baseCorpus)+len(nestedFormsCorpus))
	nestedCorpus = append(nestedCorpus, baseCorpus...)
	nestedCorpus = append(nestedCorpus, nestedFormsCorpus...)

	cases := []struct {
		name    string
		pass    nanopass.Pass
		nested  bool // use nestedCorpus instead of baseCorpus
	}{
		{name: "StripComments", pass: passes.StripComments},
		{name: "CanonicalizeKeywordCase", pass: passes.CanonicalizeKeywordCase},
		{name: "CanonicalizeIdentifiers", pass: passes.CanonicalizeIdentifiers},
		{name: "CanonicalizeEquals", pass: passes.CanonicalizeEquals},
		{name: "CanonicalizeJoin", pass: passes.CanonicalizeJoin},
		{name: "CanonicalizeSugar", pass: passes.CanonicalizeSugar},
		{name: "CanonicalizeWhitespace", pass: passes.CanonicalizeWhitespace},
		{name: "CanonicalizeWhitespaceSingleLine", pass: passes.CanonicalizeWhitespaceSingleLine},
		{name: "CanonicalizeMultiIf", pass: passes.CanonicalizeMultiIf},
		{name: "RemoveRedundantParens", pass: passes.RemoveRedundantParens},

		{name: "CanonicalizeCaseConditionals", pass: passes.CanonicalizeCaseConditionals, nested: true},
		{name: "CanonicalizeTernary", pass: passes.CanonicalizeTernary, nested: true},
		{name: "CanonicalizeCasts", pass: passes.CanonicalizeCasts, nested: true},
		{name: "CanonicalizeConstructorsFunction", pass: passes.CanonicalizeConstructors(passes.ConstructorFormFunction), nested: true},
		{name: "CanonicalizeConstructorsLiteral", pass: passes.CanonicalizeConstructors(passes.ConstructorFormLiteral), nested: true},

		{name: "ValidateColumnNamesMatchAll", pass: passes.ValidateColumnNames(`.*`)},
		{name: "WrapColumnsWithDynamicIdSuffix", pass: passes.WrapColumnsWithDynamic(`.*_id$`)},
		{name: "QualifyTablesDefault", pass: passes.QualifyTables("mydb")},
		{name: "SetFormatJSON", pass: passes.SetFormat("JSON")},
		{name: "RemoveFormat", pass: passes.RemoveFormat},
		{name: "PruneUnreferencedParams", pass: passes.PruneUnreferencedParams("")},
		{name: "InjectParamsAsCTE", pass: passes.InjectParamsAsCTE("", nil, nil)},
		{name: "ExtractLiterals", pass: passes.ExtractLiterals(passes.NewExtractLiteralsConfig(0))},

		{name: "ValidateGrammar1", pass: nanopass.ValidateGrammar1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			corpus := baseCorpus
			if c.nested {
				corpus = nestedCorpus
			}
			nanopass.AssertProperties(t, c.pass, corpus)
		})
	}
}
