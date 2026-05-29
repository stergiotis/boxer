//go:build llm_generated_opus47

package m1fixture

import (
	"io"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/stergiotis/boxer/public/functional/option"
)

// makeDense builds one M1Sample row with every field populated. Mirrors
// sampleM1Sample but inlined here so the benchmark file doesn't depend
// on test-only helpers.
func makeDense() M1Sample {
	return M1Sample{
		Id:           0x0011223344556677,
		Ts:           time.Unix(1700000000, 0).UTC(),
		Source:       "m1-fixture",
		Severity:     7,
		MajorVer:     42,
		Sequence:     0xCAFEBABE,
		LatencyNanos: 1_234_567_890_123,
		CpuPct:       3.14,
		LoadAvg1:     2.71828,
		Healthy:      true,
		PeerV4:       [4]byte{10, 0, 0, 42},
		PeerV6:       [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01},
		LastSuccess:  option.Some(time.Unix(1699999000, 0).UTC()),
		OperatorName: option.Some("alice"),
		Tags:         []string{"t1", "t2", "t3"},
		CapBits:      roaring.BitmapOf(1000001, 2000002, 3000003),
	}
}

// makeAbsent builds an M1Sample with all Optional / slice / roaring
// fields cleared — measures the cheap absent path.
func makeAbsent() M1Sample {
	s := makeDense()
	s.LastSuccess = option.None[time.Time]()
	s.OperatorName = option.None[string]()
	s.Tags = nil
	s.CapBits = nil
	return s
}

// makeBitmapLarge builds an M1Sample whose CapBits carries 10,000
// elements — surfaces the roaring marshal cost.
func makeBitmapLarge() M1Sample {
	s := makeDense()
	bm := roaring.New()
	for i := uint32(0); i < 10_000; i++ {
		bm.Add(i * 13)
	}
	s.CapBits = bm
	return s
}

// BenchmarkM1Sample_MarshalSingleRow measures one dense row end-to-end:
// Append + Marshal into an io.Discard sink. Reports ns/op + allocs/op.
// Comparable target: rowmarshall.BenchmarkCapabilityGrant (51 ns/op).
func BenchmarkM1Sample_MarshalSingleRow(b *testing.B) {
	row := makeDense()
	cols := &M1SampleColumns{}
	cols.Append(row)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkM1Sample_MarshalBatch_1000 measures throughput on a 1000-row
// batch — dominant case for bulk ingestion.
func BenchmarkM1Sample_MarshalBatch_1000(b *testing.B) {
	row := makeDense()
	cols := &M1SampleColumns{}
	for i := 0; i < 1000; i++ {
		cols.Append(row)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkM1Sample_MarshalAbsent measures the absent-path cost: all
// Options None, Tags nil, CapBits nil. Should be substantially cheaper
// than the dense path (no slice growth, no bitmap iteration).
func BenchmarkM1Sample_MarshalAbsent(b *testing.B) {
	row := makeAbsent()
	cols := &M1SampleColumns{}
	cols.Append(row)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkM1Sample_BitmapLarge measures the per-row cost when CapBits
// carries 10,000 elements — surfaces roaring.ToArray() allocation and
// the u32 mixed-section accumulator overhead.
func BenchmarkM1Sample_BitmapLarge(b *testing.B) {
	row := makeBitmapLarge()
	cols := &M1SampleColumns{}
	cols.Append(row)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cols.Marshal(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkM1Sample_AppendOnly measures the SoA-build path independent
// of Marshal — useful for high-ingest callers that batch upstream of
// the wire write.
func BenchmarkM1Sample_AppendOnly(b *testing.B) {
	row := makeDense()
	cols := &M1SampleColumns{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cols.Append(row)
	}
}

// ADR-0042 M8 canary benchmarks against MarshalDriver / Pooled /
// Hinted retired in M11: the canonical Marshal IS the driver path
// now (see fixture.out.go), and fixture_driver.go is gone. Single-row
// / Batch_1000 / Absent / BitmapLarge benchmarks above measure the
// same path the experiment fork used to.
