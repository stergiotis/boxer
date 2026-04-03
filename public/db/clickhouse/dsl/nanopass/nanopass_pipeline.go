//go:build llm_generated_opus46

package nanopass

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Pass is a function that transforms SQL → SQL. Every pass receives valid SQL
// and must return valid SQL. This is the fundamental type for the nanopass pipeline.
type Pass = func(sql string) (result string, err error)

// Pipeline applies a sequence of passes to SQL, threading the result through each.
func Pipeline(sql string, passes ...Pass) (result string, err error) {
	result = sql
	for _, pass := range passes {
		result, err = pass(result)
		if err != nil {
			return
		}
	}
	return
}

var ErrNoFixPointReached = eh.Errorf("did not converge, no fix point reached")

// FixedPoint repeats a pass until its output stabilizes or maxIter is reached.
func FixedPoint(pass Pass, maxIter int) Pass {
	return func(sql string) (result string, err error) {
		result = sql
		for i := 0; i < maxIter; i++ {
			next, nextErr := pass(result)
			if nextErr != nil {
				err = nextErr
				return
			}
			if next == result {
				return
			}
			result = next
		}
		err = ErrNoFixPointReached
		return
	}
}

// FixedPointPipeline repeats an entire pipeline until its output stabilizes.
func FixedPointPipeline(maxIter int, passes ...Pass) Pass {
	combined := func(sql string) (string, error) {
		return Pipeline(sql, passes...)
	}
	return FixedPoint(combined, maxIter)
}

// Validate is a Pass that parses SQL with Grammar1 and returns an error if it
// fails. The SQL is returned unchanged on success. Useful as a pipeline step
// to verify that a preceding pass produced valid Grammar1 SQL.
func Validate(sql string) (result string, err error) {
	_, err = Parse(sql)
	if err != nil {
		err = eh.Errorf("Validate: %w", err)
		return
	}
	result = sql
	return
}

// ValidateCanonical is a Pass that parses SQL with Grammar2 and returns an
// error if it fails. The SQL is returned unchanged on success.
//
// Use this as the final step of a normalization pipeline to verify that
// the output conforms to Grammar2's canonical form. If this fails, one or
// more normalization passes are incomplete or missing.
//
// Grammar2 rejects:
//   - CASE, CAST(AS), expr::Type, DATE/TIMESTAMP sugar
//   - Array/tuple literal syntax, array/tuple access syntax
//   - Ternary operator (? :)
//   - ==, OUTER, comma join, unparenthesized USING
//   - Bare and backtick-quoted identifiers (must be double-quoted)
//   - INTO OUTFILE, WITH FILL
func ValidateCanonical(sql string) (result string, err error) {
	_, err = ParseCanonical(sql)
	if err != nil {
		err = eh.Errorf("ValidateCanonical: %w", err)
		return
	}
	result = sql
	return
}
