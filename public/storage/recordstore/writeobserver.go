package recordstore

import (
	"context"
	"iter"

	"github.com/stergiotis/boxer/public/identity/callident"
)

// WrittenKey is one committed row as a store's write observer is told it
// (ADR-0295 §SD7). K is the store's key type and O its Order type.
type WrittenKey[K any, O any] struct {
	Key   K
	Order O
	// Lifecycle is the row's lifecycle marker, LifecycleLive on a store that
	// binds none.
	Lifecycle uint8
	// Tombstone is set on a row written by Delete — under a configured
	// tombstone pair the marker row's Lifecycle reads LifecycleLive, so this
	// is the flag to read.
	Tombstone bool
	// Identity is the call identity on the context the row was committed
	// under (BeginCtx, DeleteCtx, Ingest<Kind>Ctx) — who wrote it. The Flush
	// that ships the row stamps its insert with its own context's identity,
	// which may be someone else's; this field is the per-row attribution.
	Identity callident.CallIdentity
}

// WriteObserverI is the optional write observer of a generated store
// (<Store>StoreConfig.WriteObserver; ADR-0295 §SD7). It lets a domain layer
// say which entities a batch made durable without decoding Arrow. It is
// notification-only: it returns nothing and cannot fail or alter a write, and
// it is independent of the store's cache-view hooks.
//
// Every committed row is reported to Committed once, then exactly once to
// either Durable or Discarded. A failed Flush keeps its rows; the Flush that
// lands them reports them under its own batch id. The store detaches its
// buffer before calling Durable or Discarded, so an observer may commit into
// the store from the callback — those rows are reported with a later flush —
// and an observer that panics loses that one report rather than having it
// repeated: the guarantee is at most once under a panic. Like the store, the
// observer is called on the store's one goroutine.
type WriteObserverI[K any, O any] interface {
	// Committed is told a row a Commit or Delete buffered.
	Committed(w WrittenKey[K, O])
	// Durable is told, after a successful Flush, the batch id of the insert
	// that made keys durable and every key it carried. ctx is the Flush's
	// context, carrying the batch id.
	Durable(ctx context.Context, batch BatchIdT, keys iter.Seq[WrittenKey[K, O]])
	// Discarded is told the keys DiscardPending (and so Close) dropped
	// before they became durable.
	Discarded(keys iter.Seq[WrittenKey[K, O]])
}
