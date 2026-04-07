package processor

import (
	"context"
	"iter"
	"sync"
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

// SlicePool implements a type-safe sync.Pool for slices.
type SlicePool[T any] struct {
	internal *sync.Pool
	capacity int
}
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
}
