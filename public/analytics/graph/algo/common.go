package algo

import (
	"context"

	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// LimitE names the budget that stopped an algorithm short.
type LimitE uint8

const (
	// LimitNone: the algorithm ran to completion.
	LimitNone LimitE = iota
	// LimitIterations: the iteration budget ran out before convergence.
	LimitIterations
	// LimitPivots: a sampled algorithm used its pivot budget rather than
	// every source.
	LimitPivots
	// LimitOutput: the output cap was reached; the listing is a prefix.
	LimitOutput
	// LimitContext: the context was cancelled or its deadline passed.
	LimitContext
	// LimitRows: a producer's row budget cut the input; the result covers a
	// uniform subsample of the rows (ADR-0230 §SD1).
	LimitRows
)

// Truncation records whether a result stopped short and why (ADR-0229 §SD4).
type Truncation struct {
	Truncated bool
	By        LimitE
}

func truncatedBy(by LimitE) Truncation {
	return Truncation{Truncated: true, By: by}
}

func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func engineOrDefault(e *engine.Engine) *engine.Engine {
	if e == nil {
		return engine.New(0)
	}
	return e
}

// fill sets every element of a to v.
func fill[T any](a []T, v T) {
	for i := range a {
		a[i] = v
	}
}
