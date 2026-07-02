// Package identgen groups the concrete identifier.IdGeneratorI implementations —
// seq/ (per-tag monotonic counters) and internalized/ (natural-key interners),
// each with an in-memory and a Badger backend — and the errors they share.
package identgen

import "github.com/stergiotis/boxer/public/observability/eh"

// ErrIdSpaceExhausted is returned by any generator once every id in a tag's body
// range has been assigned. Backends wrap it with structured context, so callers
// can match it with errors.Is regardless of which implementation produced it.
var ErrIdSpaceExhausted = eh.Errorf("surrogate id space exhausted for tag")

// ErrEmptyNaturalKey is returned by an internalizing generator when a nil or
// zero-length natural key is passed; an interner must dedupe by key. Sequential
// generators ignore the key and never return this.
var ErrEmptyNaturalKey = eh.Errorf("natural key is empty")
