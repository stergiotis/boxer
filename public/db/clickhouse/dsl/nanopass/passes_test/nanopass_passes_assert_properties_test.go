//go:build llm_generated_opus47

package passes_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
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
	"SELECT CAST(a::Int64 + 1 AS String) FROM t",
	"SELECT array(array(1, 2), array(3, 4)) FROM t",
	"SELECT [[1, 2], [3, 4]] FROM t",
	"SELECT SUBSTRING(DATE '2024-01-02' FROM 1 FOR 4) FROM t",
	"SELECT EXTRACT(DAY FROM DATE '2024-01-01') FROM t",
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

	// Factory-built passes need their registries configured; their corpora
	// must contain registered call sites or the property check is vacuous.
	macroExpander := nanopass.NewMacroExpander()
	macroExpander.Register("apTwo", func(args []nanopass.LiteralArg) (string, error) { return "2", nil })
	macroExpander.Register("apDouble", func(args []nanopass.LiteralArg) (string, error) {
		return "(" + args[0].Value + " * 2)", nil
	})
	macroCorpus := append(append([]string{}, nestedCorpus...),
		"SELECT apDouble(apTwo()) FROM t",
		"SELECT apDouble(3), apTwo() FROM t",
	)

	// FunctionEvaluator folds nested literal-only calls in a single Apply
	// (tryEval recurses), so plain nesting converges immediately. The
	// fixpoint flag is justified by the VerbatimSql escape hatch: spliced
	// SQL may itself contain registered calls, which only the next
	// iteration's re-parse can see.
	evaluator := passes.NewFunctionEvaluator()
	evaluator.Register("apSeven", func(args []any) (any, error) { return int64(7), nil }, true)
	evaluator.Register("apWrap", func(args []any) (any, error) {
		return marshalling.VerbatimSql{SQL: "apSeven(1)"}, nil
	}, true)
	evalCorpus := append(append([]string{}, nestedCorpus...),
		"SELECT apWrap(0) FROM t",
		"SELECT apSeven(apSeven(1)) FROM t",
	)

	expandSchema := passes.NewStaticSchemaProvider(map[string][]string{
		"t":  {"a", "b"},
		"t1": {"x", "y"},
	})

	cases := []struct {
		name   string
		pass   nanopass.Pass
		nested bool     // use nestedCorpus instead of baseCorpus
		corpus []string // explicit corpus override (wins over nested)
	}{
		{name: "StripComments", pass: passes.StripComments},
		{name: "CanonicalizeKeywordCase", pass: passes.CanonicalizeKeywordCase},
		{name: "CanonicalizeIdentifiers", pass: passes.CanonicalizeIdentifiers},
		{name: "CanonicalizeEquals", pass: passes.CanonicalizeEquals},
		{name: "CanonicalizeJoin", pass: passes.CanonicalizeJoin},

		{name: "CanonicalizeWhitespace", pass: passes.CanonicalizeWhitespace},
		{name: "CanonicalizeWhitespaceSingleLine", pass: passes.CanonicalizeWhitespaceSingleLine},
		{name: "CanonicalizeMultiIf", pass: passes.CanonicalizeMultiIf},
		{name: "RemoveRedundantParens", pass: passes.RemoveRedundantParens},

		{name: "CanonicalizeSugar", pass: passes.CanonicalizeSugar, nested: true},
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

		{name: "MacroExpander", pass: macroExpander.Pass(), corpus: macroCorpus},
		{name: "FunctionEvaluator", pass: evaluator.Pass(), corpus: evalCorpus},
		{name: "ExpandColumns", pass: passes.ExpandColumns(expandSchema, "db")},
		{name: "WriteSettings", pass: passes.WriteSettings(map[string]any{"max_threads": int64(8)})},

		{name: "ValidateGrammar1", pass: nanopass.ValidateGrammar1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			corpus := baseCorpus
			if c.nested {
				corpus = nestedCorpus
			}
			if c.corpus != nil {
				corpus = c.corpus
			}
			nanopass.AssertProperties(t, c.pass, corpus)
		})
	}

	// ValidateGrammar2 accepts only canonical SQL — exercise its declared
	// idempotency against the canonicalised corpus (entries the pipeline
	// cannot canonicalise are skipped, but at least one must survive).
	t.Run("ValidateGrammar2", func(t *testing.T) {
		full := passes.CanonicalizeFull(16)
		canonical := make([]string, 0, len(baseCorpus))
		for _, sql := range baseCorpus {
			out, err := full.Run(sql)
			if err != nil {
				continue
			}
			canonical = append(canonical, out)
		}
		require.NotEmpty(t, canonical, "no corpus entry canonicalised")
		nanopass.AssertProperties(t, nanopass.ValidateGrammar2, canonical)
	})
}
