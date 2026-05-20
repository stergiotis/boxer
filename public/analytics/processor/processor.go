package processor

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/ph"
)

func NewProcessor[K comparable, V EntityItem[K]](
	consumer ConsumerI[K, V],
	cfg Config,
	opts ...Option[K, V],
) *Processor[K, V] {
	p := &Processor[K, V]{
		consumer:  consumer,
		cfg:       cfg,
		chunkPool: NewSlicePool[V](cfg.ChunkPoolCap),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithPool overrides the default chunk pool. Useful for tests that want to
// observe Get/Put behavior, or for callers that want to share a pool across
// processors.
func WithPool[K comparable, V EntityItem[K]](pool ChunkPoolI[V]) Option[K, V] {
	return func(p *Processor[K, V]) {
		p.chunkPool = pool
	}
}

// Run streams batches from source, partitions rows by entity ID, and invokes
// the consumer on each entity's row stream in a dedicated goroutine.
//
// Consumer contract: Process MUST honor ctx. On cancellation Run closes the
// row channel and waits for the consumer goroutine to exit before returning,
// so a consumer that ignores ctx will block Run indefinitely.
//
// A consumer that returns nil before consuming all of its rows signals
// "done with this entity"; remaining rows for that entity are dropped and
// Run continues with the next entity. A non-nil return aborts the pipeline.
//
// Rows are assumed grouped by entity ID; a non-contiguous reappearance of
// the same ID is treated as a new lifecycle (a fresh consumer goroutine).
func (inst *Processor[K, V]) Run(ctx context.Context, source BatchReaderI[K, V]) (err error) {
	var (
		currentID   K
		currentCh   chan []V
		currentDone chan error
		isActive    bool
	)

	// closeCurrent closes the active row channel, joins the consumer
	// goroutine, and returns any buffered chunks back to the pool. It does
	// not select on ctx.Done — joining the goroutine is required to avoid
	// leaks, so consumers must honor ctx themselves.
	closeCurrent := func() (consumerErr error) {
		if !isActive {
			return nil
		}
		close(currentCh)
		consumerErr = <-currentDone
		for chunk := range currentCh {
			inst.chunkPool.Put(chunk)
		}
		isActive = false
		return
	}

	// Ensure the consumer goroutine is joined on every exit path (including
	// ctx cancellation and panic), and surface ctx.Err() if the input loop
	// terminated due to upstream cancellation.
	defer func() {
		if isActive {
			if cerr := closeCurrent(); err == nil && cerr != nil {
				err = cerr
			}
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()

	for batch, batchErr := range source.StreamBatches(ctx) {
		if batchErr != nil {
			err = eh.Errorf("stream read failure: %w", batchErr)
			return
		}

		if len(batch) == 0 {
			continue
		}

		i := 0
		for i < len(batch) {
			row := batch[i]
			rowID := row.GetEntityID()

			// Entity change: drain previous consumer.
			if isActive && rowID != currentID {
				if err = closeCurrent(); err != nil {
					return
				}
			}

			// Start consumer for a new (or reappearing) entity.
			if !isActive {
				currentID = rowID
				currentCh = make(chan []V, inst.cfg.BufferSize)
				currentDone = make(chan error, 1)
				isActive = true

				go inst.runConsumerSafe(ctx, currentID, currentCh, currentDone)
			}

			// Collect contiguous run of same entity ID.
			j := i + 1
			for j < len(batch) && batch[j].GetEntityID() == currentID {
				j++
			}

			rawChunk := batch[i:j]
			pooledChunk := inst.chunkPool.Get()
			pooledChunk = append(pooledChunk, rawChunk...)

			select {
			case currentCh <- pooledChunk:
				i = j
			case <-ctx.Done():
				inst.chunkPool.Put(pooledChunk)
				err = ctx.Err()
				return
			case consumerErr := <-currentDone:
				// Consumer exited before we could send this chunk. Discard
				// the in-flight chunk, drain the buffer, and decide whether
				// the exit was an error or a legitimate early-stop.
				inst.chunkPool.Put(pooledChunk)
				isActive = false
				close(currentCh)
				for chunk := range currentCh {
					inst.chunkPool.Put(chunk)
				}
				if consumerErr != nil {
					err = consumerErr
					return
				}
				// Consumer returned nil: skip remaining rows of currentID in
				// this batch and continue with the next entity.
				i = j
			}
		}
	}

	if isActive {
		if err = closeCurrent(); err != nil {
			return
		}
	}

	return nil
}

// runConsumerSafe executes the user logic with panic recovery. A recovered
// panic is both logged (so it is observable even if the caller drops the
// returned error) and returned via the done channel.
func (inst *Processor[K, V]) runConsumerSafe(ctx context.Context, id K, ch chan []V, done chan error) {
	var err error
	defer func() {
		if r := recover(); r != nil {
			err = ph.ConvertPanicToError(r)
			log.Error().Err(err).Interface("entity_id", id).Msg("panic recovered in consumer")
		}
		done <- err
		close(done)
	}()

	iterator := func(yield func(V) bool) {
		for chunk := range ch {
			for _, item := range chunk {
				if !yield(item) {
					inst.chunkPool.Put(chunk)
					return
				}
			}
			inst.chunkPool.Put(chunk)
		}
	}

	err = inst.consumer.Process(ctx, id, iterator)
}
