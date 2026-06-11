package example

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// benchRecord writes nEntities entities (one text attribute with two
// container values and one low-card-ref membership, plus one geo
// attribute) and returns the single transferred record. Mirrors the
// write shapes of TestRoundtrip.
func benchRecord(b *testing.B, nEntities int) arrow.RecordBatch {
	b.Helper()
	dml := NewInEntityTestTable(memory.DefaultAllocator, nEntities)
	ts0 := time.Unix(1700000000, 0).UTC()
	tsProc := []time.Time{ts0.Add(time.Second), ts0.Add(2 * time.Second)}
	secText := dml.GetSectionText()
	secGeo := dml.GetSectionGeo()
	for i := 0; i < nEntities; i++ {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i))
		ent.SetTimestamp(ts0, tsProc)
		secText.BeginAttribute("hello world!").
			AddToCoContainers(5, "hello").
			AddToCoContainers(5, "world").
			AddMembershipLowCardRef(uint64(i % 16)).
			EndAttribute()
		secGeo.BeginAttribute(12.0, -3.5, 0x45494, 0x45454543).
			AddMembershipLowCardRef(uint64(i % 16)).
			EndAttribute()
		if err := ent.CommitEntity(); err != nil {
			b.Fatal(err)
		}
	}
	recs, err := dml.TransferRecords(nil)
	if err != nil {
		b.Fatal(err)
	}
	if len(recs) != 1 {
		b.Fatalf("expected 1 record, got %d", len(recs))
	}
	return recs[0]
}

const benchEntities = 10000

// BenchmarkRALoadFromRecord measures the per-batch fixed cost of the
// read side: binding all sections of a 10k-entity record and building
// the lookup accelerators.
func BenchmarkRALoadFromRecord(b *testing.B) {
	rec := benchRecord(b, benchEntities)
	b.Cleanup(func() { rec.Release() })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ra := NewReadAccessTestTable()
		if err := ra.LoadFromRecord(rec); err != nil {
			b.Fatal(err)
		}
		ra.Release()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(benchEntities), "ns/row")
}

// BenchmarkRAScalarAccess measures the per-call cost of scalar
// accessors through the two-level accelerators (one text scalar and
// one geo float per op).
func BenchmarkRAScalarAccess(b *testing.B) {
	rec := benchRecord(b, benchEntities)
	b.Cleanup(func() { rec.Release() })
	ra := NewReadAccessTestTable()
	if err := ra.LoadFromRecord(rec); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(ra.Release)
	var sinkLen int
	var sinkLat float32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := runtime.EntityIdx(i % benchEntities)
		sinkLen += len(ra.Text.Attributes.GetAttrValueText(e, 0))
		sinkLat += ra.Geo.Attributes.GetAttrValueLat(e, 0)
	}
	if sinkLen < 0 || sinkLat == -1 {
		b.Fatal("sink")
	}
}

// BenchmarkRAContainerIter measures iterating one attribute's
// container values (two words) plus its membership refs (one) through
// the generated Seq accessors.
func BenchmarkRAContainerIter(b *testing.B) {
	rec := benchRecord(b, benchEntities)
	b.Cleanup(func() { rec.Release() })
	ra := NewReadAccessTestTable()
	if err := ra.LoadFromRecord(rec); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(ra.Release)
	var sinkN int
	var sinkRef uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := runtime.EntityIdx(i % benchEntities)
		for w := range ra.Text.Attributes.GetAttrValueWords(e, 0) {
			sinkN += len(w)
		}
		for r := range ra.Text.Memberships.GetMembValueLowCardRef(e, 0) {
			sinkRef += r
		}
	}
	if sinkN < 0 || sinkRef == 1 {
		b.Fatal("sink")
	}
}
