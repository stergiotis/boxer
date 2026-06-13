// Package bench holds round-trip benchmarks for the runtime.facts
// dml ↔ ra pipeline. Three representative chstore use cases × three
// phases × two batch sizes = 18 benchmarks.
//
// Use cases (mirror the section/membership pattern of chstore.Write*):
//
//   - Grant — chstore.WriteGrant: symbol×5 + bool×1.
//   - State — chstore.WriteState: symbol×3 + blob×1.
//   - Log5 — chstore.WriteLog with five typed fields: symbol×4 + string×1
//     + per-field i64/u64/f64/bool/time mixed via writeLogTypedFields.
//
// Phases:
//
//   - Build              — dml.InEntityFacts builder only (BeginEntity →
//                          GetSection*().BeginAttribute(...).EndAttribute()
//                          → CommitEntity). No record transfer, no IPC.
//   - BuildAndIPCEncode  — Build + TransferRecords + ipc.NewFileWriter
//                          (the wire format chclient.InsertArrow ships).
//   - RoundTrip          — BuildAndIPCEncode + ipc.NewFileReader +
//                          ra.ReadAccessFacts.LoadFromRecord on every record.
//
// Batch sizes: {1, 1000}. 1 mirrors today's chstore.Write* (one entity
// per call); 1000 measures the amortized cost if batching ever lands.
//
// Run with:
//
//	go test -tags "$(cat tags | tr -d $'\n')" -bench . -benchmem \
//	        ./public/keelson/runtime/factsschema/bench/
//
// No CH or chclient transport — the goal is to characterise the in-process
// dml/ra cost, not network noise.
package bench

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// useCase pairs a name with the per-row fill closure. The closure is
// responsible for BeginEntity → SetId/SetTimestamp → section work →
// CommitEntity so the benchmark loop only has to call it N times per
// batch.
type useCase struct {
	name string
	fill func(*dml.InEntityFacts, int) error
}

var useCases = []useCase{
	{"Grant", fixtureGrant},
	{"State", fixtureState},
	{"Log5", fixtureLog5},
}

var batchSizes = []int{1, 1000}

// sink is a package-level value the benchmark loops write into to keep
// the compiler from dead-code-eliminating the work.
var sink int64

// ----------------------------------------------------------------------
// Fixtures
// ----------------------------------------------------------------------

// fixtureGrant mirrors chstore.WriteGrant for one row.
// Sections touched: symbol×5, bool×1.
func fixtureGrant(ent *dml.InEntityFacts, i int) (err error) {
	id := uint64(i + 1)
	ts := time.Unix(int64(1_700_000_000+i), 0).UTC()
	appId := fmt.Sprintf("app%d", i&0x3F) // tiny cardinality to mimic real low-card refs
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

// fixtureState mirrors chstore.WriteState for one row.
// Sections touched: symbol×3, blob×1.
func fixtureState(ent *dml.InEntityFacts, i int) (err error) {
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

// fixtureLog5 mirrors chstore.WriteLog with five typed fields, one per
// typed-field-kind. Sections touched: symbol×4, string×1, plus i64, u64,
// f64, bool, time each emitted by writeLogTypedFields.
func fixtureLog5(ent *dml.InEntityFacts, i int) (err error) {
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

	// Five typed fields — one of each non-string kind. Mirrors the
	// six-branch fan-out in chstore.writeLogTypedFields; we skip string
	// because it already rides on the message section above.
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
// Phase 1: Build only
// ----------------------------------------------------------------------

func BenchmarkBuild(b *testing.B) {
	alloc := memory.NewGoAllocator()
	for _, uc := range useCases {
		for _, n := range batchSizes {
			b.Run(fmt.Sprintf("%s/N=%d", uc.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ent := dml.NewInEntityFacts(alloc, n)
					for j := 0; j < n; j++ {
						if err := uc.fill(ent, j); err != nil {
							b.Fatal(err)
						}
					}
					// Transfer + release so the per-iter allocator
					// pressure doesn't accumulate across iterations.
					recs, err := ent.TransferRecords(nil)
					if err != nil {
						b.Fatal(err)
					}
					for _, r := range recs {
						sink += r.NumRows()
						r.Release()
					}
				}
			})
		}
	}
}

// ----------------------------------------------------------------------
// Phase 2: Build + IPC encode
// ----------------------------------------------------------------------

func BenchmarkBuildAndIPCEncode(b *testing.B) {
	alloc := memory.NewGoAllocator()
	for _, uc := range useCases {
		for _, n := range batchSizes {
			b.Run(fmt.Sprintf("%s/N=%d", uc.name, n), func(b *testing.B) {
				var buf bytes.Buffer
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ent := dml.NewInEntityFacts(alloc, n)
					for j := 0; j < n; j++ {
						if err := uc.fill(ent, j); err != nil {
							b.Fatal(err)
						}
					}
					recs, err := ent.TransferRecords(nil)
					if err != nil {
						b.Fatal(err)
					}
					if len(recs) == 0 {
						b.Fatal("no records produced")
					}
					buf.Reset()
					w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(recs[0].Schema()))
					if err != nil {
						b.Fatal(err)
					}
					for _, r := range recs {
						if err := w.Write(r); err != nil {
							b.Fatal(err)
						}
					}
					if err := w.Close(); err != nil {
						b.Fatal(err)
					}
					sink += int64(buf.Len())
					for _, r := range recs {
						r.Release()
					}
				}
			})
		}
	}
}

// ----------------------------------------------------------------------
// Phase 3: Full round-trip — build + IPC encode + IPC decode + ra.Load
// ----------------------------------------------------------------------

func BenchmarkRoundTrip(b *testing.B) {
	alloc := memory.NewGoAllocator()
	for _, uc := range useCases {
		for _, n := range batchSizes {
			b.Run(fmt.Sprintf("%s/N=%d", uc.name, n), func(b *testing.B) {
				var buf bytes.Buffer
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					// --- encode ---
					ent := dml.NewInEntityFacts(alloc, n)
					for j := 0; j < n; j++ {
						if err := uc.fill(ent, j); err != nil {
							b.Fatal(err)
						}
					}
					recs, err := ent.TransferRecords(nil)
					if err != nil {
						b.Fatal(err)
					}
					if len(recs) == 0 {
						b.Fatal("no records produced")
					}
					buf.Reset()
					w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(recs[0].Schema()))
					if err != nil {
						b.Fatal(err)
					}
					for _, r := range recs {
						if err := w.Write(r); err != nil {
							b.Fatal(err)
						}
					}
					if err := w.Close(); err != nil {
						b.Fatal(err)
					}
					for _, r := range recs {
						r.Release()
					}

					// --- decode + RA load ---
					rd, err := ipc.NewFileReader(bytes.NewReader(buf.Bytes()))
					if err != nil {
						b.Fatal(err)
					}
					accessor := ra.NewReadAccessFacts()
					for k := 0; k < rd.NumRecords(); k++ {
						rec, rerr := rd.Read()
						if rerr != nil {
							b.Fatal(rerr)
						}
						if err := accessor.LoadFromRecord(rec); err != nil {
							b.Fatal(err)
						}
						sink += rec.NumRows()
						rec.Release()
					}
					accessor.Release()
					if err := rd.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// keep arrow import live even if the bench compiler decides to inline it
// away — accessors keep arrow types in the live import graph through ra
// indirectly, but referencing arrow.RecordBatch here makes the intent
// explicit for future readers.
var _ arrow.RecordBatch
