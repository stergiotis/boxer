package passes

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"

// CanonicalizeFull returns a Sequence pass running the full canonicalisation
// pipeline. The maxIter parameter overrides the default fixpoint cap on the
// passes that declare NeedsFixedPoint; passes already declared idempotent
// run once.
func CanonicalizeFull(maxIter int) nanopass.Pass {
	p := nanopass.Sequence(
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
	// Sequence carries no properties of its own; declare the composite's so
	// catalog surfaces (passreg, keelson.sql_passes) describe it truthfully.
	// Idempotency of the whole pipeline is corpus-checked in
	// TestFullPipelineIdempotent and TestAssertProperties.
	p.Properties = nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	}
	return p
}
