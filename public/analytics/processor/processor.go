package processor

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/ph"
)

// NewProcessor constructs a Processor. BufferSize and ChunkPoolCap must be
// non-negative; negative values panic at construction time rather than
// causing confusing make-time crashes later. BufferSize == 0 is valid
// (unbuffered handoff between reader and consumer); ChunkPoolCap == 0 is
// valid (no preallocation, append grows from nil).
func NewProcessor[K comparable, V EntityItem[K]](
	consumer ConsumerI[K, V],
	cfg Config,
	opts ...Option[K, V],
) *Processor[K, V] {
	if cfg.BufferSize < 0 {
		panic(eh.Errorf("processor.NewProcessor: invalid BufferSize %d (must be >= 0)", cfg.BufferSize))
	}
	if cfg.ChunkPoolCap < 0 {
		panic(eh.Errorf("processor.NewProcessor: invalid ChunkPoolCap %d (must be >= 0)", cfg.ChunkPoolCap))
	}
	p := &Processor[K, V]{
		consumer:  consumer,
		cfg:       cfg,
		chunkPool: NewSlicePool[V](cfg.ChunkPoolCap),
		metrics:   &noopMetrics{},
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

// WithMetrics installs an observability collector. If not set, a no-op
// collector is used so the Run loop can fire hooks without nil checks.
func WithMetrics[K comparable, V EntityItem[K]](m MetricsCollectorI) Option[K, V] {
	return func(p *Processor[K, V]) {
		p.metrics = m
	}
}

// noopMetrics is the default collector — discards every event.
type noopMetrics struct{}

func (*noopMetrics) RecordBatch()                         {}
func (*noopMetrics) RecordRows(int)                       {}
func (*noopMetrics) RecordEntityFinalized(bool)           {}
func (*noopMetrics) RecordEntityDuration(time.Duration)   {}

// Run streams batches from source, partitions rows by entity ID, and invokes
// the consumer on each entity's row stream in a dedicated goroutine.
//
// Consumer contract: Process MUST honor ctx. On cancellation Run closes the
// row channel and waits for the consumer goroutine to exit before returning,
// so a consumer that ignores ctx will block Run indefinitely.
//
// A consumer that returns nil before consuming all of its rows signals
// "done with this entity"; remaining rows for that entity are dropped and
// Run continues with the next entity. A non-nil return aborts the pipeline;
// the error is wrapped as "consumer for entity <id>: <err>" so the failing
// entity is identifiable.
//
// Rows are assumed grouped by entity ID; a non-contiguous reappearance of
// the same ID is treated as a new lifecycle (a fresh consumer goroutine).
//
// Run may be called multiple times on the same Processor — the chunk pool
// is reusable. Concurrent Run calls on the same Processor share the
// supplied consumer; if you call Run from multiple goroutines, the consumer
// must be safe to invoke from multiple goroutines.
//
// Observability hooks (see WithMetrics) fire from Run's own goroutine in
// the order: RecordBatch (per non-empty batch), RecordRows (per chunk sent
// to a consumer), RecordEntityFinalized + RecordEntityDuration (once per
// entity lifecycle, regardless of success or failure).
func (inst *Processor[K, V]) Run(ctx context.Context, source BatchReaderI[K, V]) (err error) {
	var (
		currentID    K
		currentCh    chan []V
		currentDone  chan error
		isActive     bool
		entityStart  time.Time
	)

	// wrapConsumerErr tags a consumer error with the entity ID that produced
	// it, so multi-entity pipelines can identify the failing entity.
	wrapConsumerErr := func(cerr error) error {
		return eh.Errorf("consumer for entity %v: %w", currentID, cerr)
	}

	// recordFinalized fires the two end-of-entity metric hooks. Called from
	// both closeCurrent and the in-flight <-currentDone select branch.
	recordFinalized := func(consumerErr error) {
		inst.metrics.RecordEntityFinalized(consumerErr == nil)
		inst.metrics.RecordEntityDuration(time.Since(entityStart))
	}

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
		recordFinalized(consumerErr)
		isActive = false
		return
	}

	// Ensure the consumer goroutine is joined on every exit path (including
	// ctx cancellation and panic), and surface ctx.Err() if the input loop
	// terminated due to upstream cancellation.
	defer func() {
		if isActive {
			if cerr := closeCurrent(); err == nil && cerr != nil {
				err = wrapConsumerErr(cerr)
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
		inst.metrics.RecordBatch()

		i := 0
		for i < len(batch) {
			row := batch[i]
			rowID := row.GetEntityID()

			// Entity change: drain previous consumer.
			if isActive && rowID != currentID {
				if cerr := closeCurrent(); cerr != nil {
					err = wrapConsumerErr(cerr)
					return
				}
			}

			// Start consumer for a new (or reappearing) entity.
			if !isActive {
				currentID = rowID
				currentCh = make(chan []V, inst.cfg.BufferSize)
				currentDone = make(chan error, 1)
				isActive = true
				entityStart = time.Now()

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
				inst.metrics.RecordRows(j - i)
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
				close(currentCh)
				for chunk := range currentCh {
					inst.chunkPool.Put(chunk)
				}
				recordFinalized(consumerErr)
				isActive = false
				if consumerErr != nil {
					err = wrapConsumerErr(consumerErr)
					return
				}
				// Consumer returned nil: skip remaining rows of currentID in
				// this batch and continue with the next entity.
				i = j
			}
		}
	}

	if isActive {
		if cerr := closeCurrent(); cerr != nil {
			err = wrapConsumerErr(cerr)
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
