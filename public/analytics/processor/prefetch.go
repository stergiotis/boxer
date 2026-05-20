package processor

import (
	"context"
	"iter"
)

// Prefetcher wraps a BatchReaderI and reads 'depth' batches ahead.
//
// The producer goroutine is bound to the lifetime of each StreamBatches call:
// when the consumer stops iterating (via yield-false or normal completion),
// an internal sub-context is cancelled so the producer exits even if the
// outer ctx is still live. The upstream source must honor its ctx for this
// to terminate promptly.
func Prefetcher[K comparable, V EntityItem[K]](
	ctx context.Context,
	source BatchReaderI[K, V],
	depth int,
) BatchReaderI[K, V] {
	return &prefetchReader[K, V]{
		source: source,
		depth:  depth,
	}
}

type prefetchReader[K comparable, V EntityItem[K]] struct {
	source BatchReaderI[K, V]
	depth  int
}

type fetchResult[V any] struct {
	batch []V
	err   error
}

func (inst *prefetchReader[K, V]) StreamBatches(ctx context.Context) iter.Seq2[[]V, error] {
	return func(yield func([]V, error) bool) {
		// Sub-context cancelled when this iterator function returns
		// (whether via yield-false, source completion, or upstream ctx
		// cancel). Bounds the producer goroutine's lifetime to ours.
		subCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		ch := make(chan fetchResult[V], inst.depth)

		go func() {
			defer close(ch)
			for batch, err := range inst.source.StreamBatches(subCtx) {
				select {
				case ch <- fetchResult[V]{batch, err}:
				case <-subCtx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()

		for res := range ch {
			if !yield(res.batch, res.err) {
				return
			}
		}
	}
}
