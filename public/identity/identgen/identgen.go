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

// ErrTagInUse is returned by a store-backed factory when Create is called for
// a tag that already has a generator on the same store. Two generators would
// share the persistent state but not an in-process lock, so an interner could
// mint two different ids for one natural key in the get-or-assign window. A
// tag's slot is held until the factory closes: Release returns leased ids but
// keeps its generator usable, so it cannot free the slot. The in-memory
// backend is exempt — each of its generators is its own store.
var ErrTagInUse = eh.Errorf("tag already has a generator on this store")
