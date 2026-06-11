package example

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// buildBatch writes nEntities entities (one "special" and one "multi"
// attribute each) into a fresh DML instance and transfers the records.
// It mirrors the call shapes of the correctness tests in this package
// so the measured path is the tested path.
func buildBatch(pool memory.Allocator, nEntities int, ts time.Time) ([]arrow.RecordBatch, error) {
	e := NewInEntityTesttable(pool, nEntities)
	for i := 0; i < nEntities; i++ {
		e.BeginEntity().SetId(uint64(i + 1)).SetTimestamp(ts)
		e.GetSectionSpecial().BeginAttribute("spc").AddToCoContainers(uint32(i), uint32(i+1)).EndAttribute()
		e.GetSectionMulti().BeginAttribute("name").AddToCoContainers(uint32(i), uint64(i%7)).EndAttribute()
		err := e.CommitEntity()
		if err != nil {
			return nil, err
		}
	}
	return e.TransferRecords(nil)
}

// BenchmarkBuildBatch measures the full batch cost — DML construction,
// N entity writes (two attributes each), commits, and record transfer.
// The ns/row metric separates the amortized per-entity cost from the
// fixed per-batch setup visible in the N=1 figure.
func BenchmarkBuildBatch(b *testing.B) {
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()
	for _, n := range []int{1, 100, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				recs, err := buildBatch(pool, n, ts)
				if err != nil {
					b.Fatal(err)
				}
				releaseAll(recs)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/row")
		})
	}
}

// BenchmarkAppendCommit measures the steady-state marginal cost of one
// committed entity (two attributes) on a long-lived DML instance.
// Buffered rows are transferred and released every flushEvery entities
// so memory stays bounded; the flush cost amortizes into the figure,
// matching how a batching producer would run.
func BenchmarkAppendCommit(b *testing.B) {
	const flushEvery = 8192
	pool := memory.NewGoAllocator()
	ts := time.Unix(1700000000, 0).UTC()
	e := NewInEntityTesttable(pool, flushEvery)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.BeginEntity().SetId(uint64(i + 1)).SetTimestamp(ts)
		e.GetSectionSpecial().BeginAttribute("spc").AddToCoContainers(uint32(i), uint32(i+1)).EndAttribute()
		e.GetSectionMulti().BeginAttribute("name").AddToCoContainers(uint32(i), uint64(i%7)).EndAttribute()
		if err := e.CommitEntity(); err != nil {
			b.Fatal(err)
		}
		if (i+1)%flushEvery == 0 {
			recs, err := e.TransferRecords(nil)
			if err != nil {
				b.Fatal(err)
			}
			releaseAll(recs)
		}
	}
	recs, err := e.TransferRecords(nil)
	if err != nil {
		b.Fatal(err)
	}
	releaseAll(recs)
}
