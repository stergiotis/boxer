package processor

import (
	"context"
	"iter"
)

// Prefetcher wraps a BatchReaderI and reads 'depth' batches ahead.
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
		// Create buffered channel to hold pre-fetched batches
		ch := make(chan fetchResult[V], inst.depth)

		// 1. Background Producer
		go func() {
			defer close(ch)

			// Iterate over the upstream source
			for batch, err := range inst.source.StreamBatches(ctx) {
				select {
				case ch <- fetchResult[V]{batch, err}:
					// Pushed to buffer
				case <-ctx.Done():
					// Context cancelled, stop fetching
					return
				}

				if err != nil {
					// Stop fetching on error
					return
				}
			}
		}()

		// 2. Main Consumer (Yields to Processor)
		for res := range ch {
			if !yield(res.batch, res.err) {
				// Downstream consumer stopped
				return
			}
		}
	}
}
