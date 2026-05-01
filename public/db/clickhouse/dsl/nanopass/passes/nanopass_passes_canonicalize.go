//go:build llm_generated_opus47

package passes

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"

// CanonicalizeFull returns a Sequence pass running the full canonicalisation
// pipeline. The maxIter parameter overrides the default fixpoint cap on the
// passes that declare NeedsFixedPoint; passes already declared idempotent
// run once.
func CanonicalizeFull(maxIter int) nanopass.Pass {
	return nanopass.Sequence(
		"CanonicalizeFull",
		CanonicalizeWhitespaceSingleLine,
		CanonicalizeEquals,
		CanonicalizeSugar,
		nanopass.FixedPoint(CanonicalizeConstructors(ConstructorFormFunction), maxIter),
		nanopass.FixedPoint(CanonicalizeCaseConditionals, maxIter),
		CanonicalizeMultiIf,
		nanopass.FixedPoint(CanonicalizeCasts, maxIter),
		CanonicalizeJoin,
		nanopass.FixedPoint(CanonicalizeTernary, maxIter),
		// keyword and identifier case must be last
		CanonicalizeKeywordCase,
		CanonicalizeIdentifiers,
	)
}
