package passes

import "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"

func CanonicalizeFull(maxIter int) nanopass.Pass {
	c1 := nanopass.FixedPoint(CanonicalizeCaseConditionals, maxIter)
	c2 := nanopass.FixedPoint(CanonicalizeMultiIf, maxIter)
	c3 := nanopass.FixedPoint(CanonicalizeCasts(), maxIter)
	return func(sql string) (result string, err error) {
		return nanopass.Pipeline(sql,
			CanonicalizeWhitespaceSingleLine,
			CanonicalizeEquals,
			CanonicalizeSugar,
			CanonicalizeConstructors(ConstructorFormFunction),
			c1,
			c2,
			c3,
			CanonicalizeJoin,
			CanonicalizeTernary,
			// must be last
			CanonicalizeKeywordCase,
			CanonicalizeIdentifiers,
		)
	}
}
