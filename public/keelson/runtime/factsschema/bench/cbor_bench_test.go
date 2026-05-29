//go:build llm_generated_opus47

// Phase-0' benchmarks: arrowrowcbor sparse-CBOR shim. Same fixtures as
// bench_test.go (Arrow) and rowbinary_bench_test.go (RowBinary), routed
// through dml_cbor. Direct cross-comparison with both prior phases.
//
// What's tested:
//
//   - BenchmarkCBORBuild: shim builder only.
//   - BenchmarkCBORBuildAndRow: builder + TransferRecordsRow (the
//     parallel to Arrow's BuildAndIPCEncode and RB's BuildAndRow).
//   - TestCBORWireSize: one-shot deterministic wire-byte count per
//     fixture × batch size for cross-table comparison.
//
// Decode path is out of scope for Phase 0' (no `ra` companion reading
// sparse CBOR yet).
package bench

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/arrowrowcbor"
	cbordml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml_cbor"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

type cborUseCase struct {
	name string
	fill func(*cbordml.InEntityFacts, int) error
}

var cborUseCases = []cborUseCase{
	{"Grant", cborFixtureGrant},
	{"State", cborFixtureState},
	{"Log5", cborFixtureLog5},
}

// ----------------------------------------------------------------------
// Fixtures (same data shape; swapped package)
// ----------------------------------------------------------------------

func cborFixtureGrant(ent *cbordml.InEntityFacts, i int) (err error) {
	id := uint64(i + 1)
	ts := time.Unix(int64(1_700_000_000+i), 0).UTC()
	appId := fmt.Sprintf("app%d", i&0x3F)
	nk := []byte("grant\x00" + appId + "\x00nats.>\x00publish")

	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("grant").AddMembershipLowCardRef(vocab.MembKindGrant.GetId().Value()).EndAttribute()
	sym.BeginAttribute(appId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute("nats.>").AddMembershipLowCardRef(vocab.MembGrantSubjectPattern.GetId().Value()).EndAttribute()
	sym.BeginAttribute("publish").AddMembershipLowCardRef(vocab.MembGrantDirection.GetId().Value()).EndAttribute()
	sym.BeginAttribute("policy").AddMembershipLowCardRef(vocab.MembGrantedVia.GetId().Value()).EndAttribute()
	sym.EndSection()

	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(i&1 == 0).AddMembershipLowCardRef(vocab.MembGrantSticky.GetId().Value()).EndAttribute()
	bsec.EndSection()

	err = ent.CommitEntity()
	return
}

func cborFixtureState(ent *cbordml.InEntityFacts, i int) (err error) {
	id := uint64(i + 1)
	ts := time.Unix(int64(1_700_000_000+i), 0).UTC()
	appId := fmt.Sprintf("app%d", i&0x3F)
	key := fmt.Sprintf("tab%d", i&0xFF)
	nk := []byte("state\x00" + appId + "\x00" + key)
	value := []byte("payload-bytes-of-realistic-size-around-thirty-two-bytes")

	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("state").AddMembershipLowCardRef(vocab.MembKindState.GetId().Value()).EndAttribute()
	sym.BeginAttribute(appId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute(key).AddMembershipLowCardRef(vocab.MembPersistKey.GetId().Value()).EndAttribute()
	sym.EndSection()

	blob := ent.GetSectionBlobArray()
	blob.BeginAttributeSingle(value).AddMembershipLowCardRef(vocab.MembPersistKey.GetId().Value()).EndAttribute()
	blob.EndSection()

	err = ent.CommitEntity()
	return
}

func cborFixtureLog5(ent *cbordml.InEntityFacts, i int) (err error) {
	id := uint64(i + 1)
	ts := time.Unix(int64(1_700_000_000+i), 0).UTC()
	appId := fmt.Sprintf("app%d", i&0x3F)
	nk := []byte("log\x00" + appId + "\x00")

	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	logFieldMembId := vocab.MembLogField.GetId().Value()

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("log").AddMembershipLowCardRef(vocab.MembKindLog.GetId().Value()).EndAttribute()
	sym.BeginAttribute(appId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute("info").AddMembershipLowCardRef(vocab.MembLogLevel.GetId().Value()).EndAttribute()
	sym.BeginAttribute("svc-ingest").AddMembershipLowCardRef(vocab.MembLogService.GetId().Value()).EndAttribute()
	sym.EndSection()

	str := ent.GetSectionStringArray()
	str.BeginAttributeSingle("processed request").AddMembershipLowCardRef(vocab.MembLogMessage.GetId().Value()).EndAttribute()
	str.EndSection()

	i64sec := ent.GetSectionI64Array()
	i64sec.BeginAttributeSingle(int64(-42 + i)).AddMembershipMixedLowCardRef(logFieldMembId, []byte("delta")).EndAttribute()
	i64sec.EndSection()

	u64sec := ent.GetSectionU64Array()
	u64sec.BeginAttributeSingle(uint64(i) * 7).AddMembershipMixedLowCardRef(logFieldMembId, []byte("count")).EndAttribute()
	u64sec.EndSection()

	f64sec := ent.GetSectionF64Array()
	f64sec.BeginAttributeSingle(float64(i) / 3.0).AddMembershipMixedLowCardRef(logFieldMembId, []byte("ratio")).EndAttribute()
	f64sec.EndSection()

	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(i&1 == 0).AddMembershipMixedLowCardRef(logFieldMembId, []byte("ok")).EndAttribute()
	bsec.EndSection()

	timeSec := ent.GetSectionTimeArray()
	timeSec.BeginAttributeSingle(ts).AddMembershipMixedLowCardRef(logFieldMembId, []byte("observed_at")).EndAttribute()
	timeSec.EndSection()

	err = ent.CommitEntity()
	return
}

// ----------------------------------------------------------------------
// Build-only
// ----------------------------------------------------------------------

func BenchmarkCBORBuild(b *testing.B) {
	alloc := memory.NewGoAllocator()
	for _, uc := range cborUseCases {
		for _, n := range batchSizes {
			b.Run(fmt.Sprintf("%s/N=%d", uc.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ent := cbordml.NewInEntityFacts(alloc, n)
					for j := 0; j < n; j++ {
						if err := uc.fill(ent, j); err != nil {
							b.Fatal(err)
						}
					}
					recs, err := ent.TransferRecords(nil)
					if err != nil {
						b.Fatal(err)
					}
					rb := arrowrowcbor.JoinRecords(recs)
					sink += int64(len(rb))
				}
			})
		}
	}
}

// ----------------------------------------------------------------------
// Build + TransferRecordsRow (parallel to Arrow's BuildAndIPCEncode)
// ----------------------------------------------------------------------

func BenchmarkCBORBuildAndRow(b *testing.B) {
	alloc := memory.NewGoAllocator()
	for _, uc := range cborUseCases {
		for _, n := range batchSizes {
			b.Run(fmt.Sprintf("%s/N=%d", uc.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ent := cbordml.NewInEntityFacts(alloc, n)
					for j := 0; j < n; j++ {
						if err := uc.fill(ent, j); err != nil {
							b.Fatal(err)
						}
					}
					recs, err := ent.TransferRecords(nil)
					if err != nil {
						b.Fatal(err)
					}
					rb := arrowrowcbor.JoinRecords(recs)
					sink += int64(len(rb))
				}
			})
		}
	}
}

// ----------------------------------------------------------------------
// One-shot wire-byte size measurement
// ----------------------------------------------------------------------

func TestCBORWireSize(t *testing.T) {
	alloc := memory.NewGoAllocator()
	for _, uc := range cborUseCases {
		for _, n := range []int{1, 10, 100, 1000} {
			ent := cbordml.NewInEntityFacts(alloc, n)
			for j := 0; j < n; j++ {
				if err := uc.fill(ent, j); err != nil {
					t.Fatalf("%s N=%d fill: %v", uc.name, n, err)
				}
			}
			recs, err := ent.TransferRecords(nil)
			if err != nil {
				t.Fatalf("%s N=%d transfer: %v", uc.name, n, err)
			}
			rb := arrowrowcbor.JoinRecords(recs)
			perRow := float64(len(rb)) / float64(n)
			t.Logf("CBOR wire %-6s N=%-5d  bytes=%-8d  per_row=%.1f", uc.name, n, len(rb), perRow)
		}
	}
}
