package processor

import (
	"context"
	"iter"
	"sync"
	"time"
)

type EntityItem[K comparable] interface {
	GetEntityID() K
}

type BatchReaderI[K comparable, V EntityItem[K]] interface {
	StreamBatches(ctx context.Context) iter.Seq2[[]V, error]
}

type ConsumerI[K comparable, V EntityItem[K]] interface {
	Process(ctx context.Context, id K, rows iter.Seq[V]) (err error)
}

// ChunkPoolI defines the contract for memory pooling.
type ChunkPoolI[T any] interface {
	Get() []T
	Put([]T)
}

// MetricsCollectorI defines the observability hooks fired by Processor.Run.
//
// All hooks are called from Run's own goroutine (the caller's goroutine), so
// the collector need not be goroutine-safe for sequential use. Callers that
// share one collector across concurrent Run invocations are responsible for
// the collector's thread safety.
type MetricsCollectorI interface {
	RecordBatch()                         // a non-empty batch was consumed from the source
	RecordRows(n int)                     // n rows were forwarded to the active consumer
	RecordEntityFinalized(ok bool)        // ok=true if Process returned nil; false on error or panic
	RecordEntityDuration(d time.Duration) // wall time from goroutine spawn to consumer return
}

// SlicePool implements a type-safe sync.Pool for slices.
type SlicePool[T any] struct {
	internal *sync.Pool
	capacity int
	zero     bool
}

// SlicePoolOption mutates a SlicePool at construction time.
type SlicePoolOption[T any] func(*SlicePool[T])
type Config struct {
	BufferSize   int // Channel buffer size
	ChunkPoolCap int // Capacity of pooled slices
}

func DefaultConfig() Config {
	return Config{
		BufferSize:   2,
		ChunkPoolCap: 256,
	}
}

type Processor[K comparable, V EntityItem[K]] struct {
	consumer  ConsumerI[K, V]
	cfg       Config
	chunkPool ChunkPoolI[V]
	metrics   MetricsCollectorI
}

// Option mutates a Processor at construction time. See [WithPool].
type Option[K comparable, V EntityItem[K]] func(*Processor[K, V])
