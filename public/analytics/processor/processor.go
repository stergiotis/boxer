package processor

import (
	"context"
	"runtime/debug"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func NewProcessor[K comparable, V EntityItem[K]](
	consumer ConsumerI[K, V],
	cfg Config,
) *Processor[K, V] {
	return &Processor[K, V]{
		consumer:  consumer,
		cfg:       cfg,
		chunkPool: NewSlicePool[V](cfg.ChunkPoolCap),
	}
}

func (inst *Processor[K, V]) Run(ctx context.Context, source BatchReaderI[K, V]) (err error) {
	var (
		currentID   K
		currentCh   chan []V
		currentDone chan error
		isActive    bool
	)

	// Helper to cleanly close the active entity stream
	closeCurrent := func() (closeErr error) {
		if !isActive {
			return nil
		}

		close(currentCh)

		select {
		case closeErr = <-currentDone:
			// Logic: We log errors in the consumer, but we bubble them up here too
		case <-ctx.Done():
			closeErr = ctx.Err()
		}

		isActive = false
		return
	}

	// Read from Source (Wrapped by Prefetcher likely)
	for batch, batchErr := range source.StreamBatches(ctx) {
		if batchErr != nil {
			err = eh.Errorf("stream read failure: %w", batchErr)
			return
		}

		if len(batch) == 0 {
			continue
		}

		// Process rows in batch
		i := 0
		for i < len(batch) {
			row := batch[i]
			rowID := row.GetEntityID()

			// 1. Detect Change
			if isActive && rowID != currentID {
				if err = closeCurrent(); err != nil {
					return // Logged in consumer or builder
				}
			}

			// 2. Initialize
			if !isActive {
				currentID = rowID
				currentCh = make(chan []V, inst.cfg.BufferSize)
				currentDone = make(chan error, 1)
				isActive = true

				go inst.runConsumerSafe(ctx, currentID, currentCh, currentDone)
			}

			// 3. Find Chunk
			j := i + 1
			for j < len(batch) && batch[j].GetEntityID() == currentID {
				j++
			}

			// 4. Allocation-Free Copy (using Pool)
			rawChunk := batch[i:j]
			pooledChunk := inst.chunkPool.Get()
			pooledChunk = append(pooledChunk, rawChunk...) // Zero-alloc append (if cap suffices)

			// 5. Hand-off
			select {
			case currentCh <- pooledChunk:
				i = j
			case <-ctx.Done():
				inst.chunkPool.Put(pooledChunk) // Clean up
				err = ctx.Err()
				return
			case consumerErr := <-currentDone:
				inst.chunkPool.Put(pooledChunk) // Clean up
				err = eb.Build().Errorf("consumer stopped early: %w", consumerErr)
				return
			}
		}
	}

	// Finalize last entity
	if isActive {
		if err = closeCurrent(); err != nil {
			return
		}
	}

	return nil
}

// runConsumerSafe executes the user logic with panic recovery
func (inst *Processor[K, V]) runConsumerSafe(ctx context.Context, id K, ch chan []V, done chan error) {
	var err error
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			// Log panic using standardized Error Builder
			err = eb.Build().
				Str("stack", stack).
				Errorf("panic recovered in consumer: %v", r)
		}
		done <- err
		close(done)
	}()

	// Iterator Bridge
	iterator := func(yield func(V) bool) {
		for chunk := range ch {
			for _, item := range chunk {
				if !yield(item) {
					// Stop signal received.
					// We must still Drain/Put the current chunk.
					inst.chunkPool.Put(chunk)
					return
				}
			}
			// Return chunk to pool immediately after iteration
			inst.chunkPool.Put(chunk)
		}
	}

	err = inst.consumer.Process(ctx, id, iterator)
	return
}
