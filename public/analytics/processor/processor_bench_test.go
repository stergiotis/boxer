package processor

import (
	"context"
	"fmt"
	"iter"
	"testing"
)

// benchConsumer counts rows without locking. Safe because Run's join
// (closeCurrent <- currentDone) establishes happens-before between the
// consumer goroutine's writes and the bench loop's next iteration.
type benchConsumer struct {
	rows int
}

func (c *benchConsumer) Process(ctx context.Context, id TestID, rows iter.Seq[TestRow]) error {
	for range rows {
		c.rows++
	}
	return nil
}

// naivePool returns a fresh nil slice on Get and discards on Put. Used to
// quantify the SlicePool's contribution to throughput / allocs.
type naivePool[T any] struct{}

func (naivePool[T]) Get() []T { return nil }
func (naivePool[T]) Put([]T)  {}

// makeBenchBatches builds totalRows rows partitioned across numEntities
// entities (grouped contiguously), chunked into batches of batchSize.
func makeBenchBatches(totalRows, batchSize, numEntities int) [][]TestRow {
	rowsPerEntity := totalRows / numEntities
	if rowsPerEntity < 1 {
		rowsPerEntity = 1
	}

	batches := make([][]TestRow, 0, totalRows/batchSize+1)
	cur := make([]TestRow, 0, batchSize)
	entityIdx := 0
	rowsInEntity := 0
	for r := 0; r < totalRows; r++ {
		cur = append(cur, TestRow{ID: TestID(entityIdx), Val: "x"})
		rowsInEntity++
		if rowsInEntity >= rowsPerEntity {
			entityIdx++
			rowsInEntity = 0
		}
		if len(cur) == batchSize {
			batches = append(batches, cur)
			cur = make([]TestRow, 0, batchSize)
		}
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// BenchmarkProcessor_Baseline_RawIteration is the absolute floor: just
// iterating over the same data without any of the processor machinery.
// Subtract this from the other Processor benchmarks to estimate the
// processor's overhead per row.
func BenchmarkProcessor_Baseline_RawIteration(b *testing.B) {
	batches := makeBenchBatches(10000, 100, 1)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		count := 0
		for _, batch := range batches {
			for range batch {
				count++
			}
		}
		_ = count
	}
}

// BenchmarkProcessor_Throughput measures steady-state throughput on a
// single-entity stream (best case: no entity-change overhead).
func BenchmarkProcessor_Throughput(b *testing.B) {
	batches := makeBenchBatches(10000, 100, 1)
	consumer := &benchConsumer{}
	proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := proc.Run(context.Background(), &MockReader{Batches: batches}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessor_PoolEffect compares the default SlicePool against
// a naive no-op pool to show what the chunk pool actually buys.
func BenchmarkProcessor_PoolEffect(b *testing.B) {
	cases := []struct {
		name string
		opt  Option[TestID, TestRow]
	}{
		{"with_pool", nil},
		{"no_pool", WithPool[TestID, TestRow](naivePool[TestRow]{})},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			batches := makeBenchBatches(10000, 100, 1)
			consumer := &benchConsumer{}

			var opts []Option[TestID, TestRow]
			if tc.opt != nil {
				opts = append(opts, tc.opt)
			}
			proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig(), opts...)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := proc.Run(context.Background(), &MockReader{Batches: batches}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkProcessor_EntitySwitching measures the cost of switching
// consumers as entity IDs change. Each switch spawns a fresh goroutine
// and a fresh row channel, so cost should grow with entity count.
func BenchmarkProcessor_EntitySwitching(b *testing.B) {
	cases := []int{1, 10, 100, 1000}

	for _, n := range cases {
		b.Run(fmt.Sprintf("entities=%d", n), func(b *testing.B) {
			batches := makeBenchBatches(10000, 100, n)
			consumer := &benchConsumer{}
			proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := proc.Run(context.Background(), &MockReader{Batches: batches}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSlicePool_GetPut is a micro-benchmark of the pool primitive
// itself: pure Get / append / Put with no surrounding processor.
func BenchmarkSlicePool_GetPut(b *testing.B) {
	pool := NewSlicePool[int](256)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s := pool.Get()
		s = append(s, 1, 2, 3)
		pool.Put(s)
	}
}

// BenchmarkPrefetcher_Overhead measures the cost the prefetcher adds on
// top of a fast in-memory source. (With a slow source, the prefetcher
// pays for itself by overlapping fetch with processing — that requires
// a different setup to measure.)
func BenchmarkPrefetcher_Overhead(b *testing.B) {
	cases := []struct {
		name        string
		useprefetch bool
	}{
		{"no_prefetch", false},
		{"prefetch_depth=4", true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			batches := makeBenchBatches(10000, 100, 1)
			consumer := &benchConsumer{}
			proc := NewProcessor[TestID, TestRow](consumer, DefaultConfig())

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				var src BatchReaderI[TestID, TestRow] = &MockReader{Batches: batches}
				if tc.useprefetch {
					src = Prefetcher[TestID, TestRow](context.Background(), src, 4)
				}
				if err := proc.Run(context.Background(), src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
