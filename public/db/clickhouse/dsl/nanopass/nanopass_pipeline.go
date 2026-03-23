//go:build llm_generated_opus46

package nanopass

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Pass is a SQL-to-SQL transformation.
// It receives syntactically valid ClickHouse SQL and returns modified SQL.
// The returned SQL must also be syntactically valid.
type Pass func(sql string) (string, error)

// Pipeline chains passes sequentially. Each pass receives the output of the previous.
func Pipeline(sql string, passes ...Pass) (result string, err error) {
	result = sql
	for i, pass := range passes {
		result, err = pass(result)
		if err != nil {
			err = eh.Errorf("pass %d failed: %w", i, err)
			return
		}
	}
	return
}

// FixedPoint runs a pass repeatedly until the output stabilizes (no change)
// or maxIterations is reached. Returns an error if maxIterations is exceeded
// without reaching a fixed point.
func FixedPoint(pass Pass, maxIterations int) Pass {
	return func(sql string) (result string, err error) {
		result = sql
		for i := 0; i < maxIterations; i++ {
			var next string
			next, err = pass(sql)
			if err != nil {
				err = eh.Errorf("FixedPoint iteration %d: %w", i, err)
				return
			}
			if next == result {
				return
			}
			result = next
			sql = next
		}
		err = eh.Errorf("FixedPoint: did not converge after %d iterations", maxIterations)
		return
	}
}

// FixedPointPipeline runs an entire pipeline repeatedly until the output stabilizes
// or maxIterations is reached.
func FixedPointPipeline(maxIterations int, passes ...Pass) Pass {
	combined := func(sql string) (string, error) {
		return Pipeline(sql, passes...)
	}
	return FixedPoint(combined, maxIterations)
}

// Validate parses the SQL and returns an error if it is invalid.
// Useful as a pipeline pass for debugging.
func Validate(sql string) (result string, err error) {
	_, err = Parse(sql)
	if err != nil {
		return
	}
	result = sql
	return
}

// LoggingPass wraps a pass with debug-level logging of input and output.
func LoggingPass(logger zerolog.Logger, name string, pass Pass) Pass {
	return func(sql string) (result string, err error) {
		logger.Debug().Str("pass", name).Str("input", sql).Msg("pass start")
		result, err = pass(sql)
		if err != nil {
			logger.Debug().Str("pass", name).Err(err).Msg("pass failed")
			return
		}
		logger.Debug().Str("pass", name).Str("output", result).Msg("pass done")
		return
	}
}
