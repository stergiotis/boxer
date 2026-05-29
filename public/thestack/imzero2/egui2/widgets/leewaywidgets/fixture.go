//go:build llm_generated_opus47

package leewaywidgets

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

var (
	fixtureOnce    sync.Once
	fixtureDriver  *streamreadaccess.Driver
	fixtureBatches []arrow.RecordBatch
	fixtureInitErr error
)

// RunFixture drives the canonical fixture record batches through `sink`
// using the leeway streamreadaccess.Driver. The schema lives in
// fixture_schema.go (LoadFixtureSchema); the generated DML builder
// (InEntityFixture in fixture_dml.out.go) populates one entity covering:
//
//   - a plain section with one MachineReadable-only column the Table
//     emitter must hide,
//   - a tagged section "metric" with three attributes exercising
//     LowCardRef, LowCardVerbatim, MixedLowCardRefHighCardParameters and
//     MixedLowCardVerbatimHighCardParameters memberships plus a
//     MachineReadable-only "rawBlob" column,
//   - a co-section group "geo" of two sections (geoPoint + geoArea) so
//     the driver merges them into one wide BeginSection.
//
// Driver, IR and batches are built once on first call and reused.
func RunFixture(sink streamreadaccess.SinkI) {
	fixtureOnce.Do(initFixture)
	if fixtureInitErr != nil {
		return
	}
	for _, rec := range fixtureBatches {
		err := fixtureDriver.DriveRecordBatch(sink, rec)
		if err != nil {
			fixtureInitErr = err
			return
		}
	}
}

func initFixture() {
	tblDesc, err := BuildFixtureTableDesc()
	if err != nil {
		fixtureInitErr = eh.Errorf("fixture init: %w", err)
		return
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tblDesc, tech); err != nil {
		fixtureInitErr = eh.Errorf("fixture init: load IR: %w", err)
		return
	}
	driver, err := streamreadaccess.NewDriver(&tblDesc, ir, streamreadaccess.DefaultFormatters())
	if err != nil {
		fixtureInitErr = eh.Errorf("fixture init: new driver: %w", err)
		return
	}
	fixtureDriver = driver

	batches, err := BuildFixtureBatches(memory.NewGoAllocator())
	if err != nil {
		fixtureInitErr = eh.Errorf("fixture init: build batches: %w", err)
		return
	}
	fixtureBatches = batches
}

// BuildFixtureBatches returns the canonical fixture entity as
// []arrow.RecordBatch produced by the generated InEntityFixture builder.
// Used by RunFixture and exposed as a top-level helper for tests or
// alternative drivers.
func BuildFixtureBatches(allocator memory.Allocator) (batches []arrow.RecordBatch, err error) {
	t := NewInEntityFixture(allocator, 1)

	t.BeginEntity().SetId(0xCAFEBABE_DEADBEEF, "$internal/blob/4f8a0c2e", "acme/widgets/blue")

	// --- metric: 3 attributes, four membership shapes spread across them ---
	metric := t.GetSectionMetric()

	// attr 0 — Ref + Verbatim. tags & bins share co-container cardinality.
	a0 := metric.BeginAttribute(42.5, "0x4f8a0c2e")
	a0.AddToCoContainers("red", 1)
	a0.AddToCoContainers("fast", 5)
	a0.AddToCoContainers("slow", 10)
	a0.AddMembershipLowCardRef(0xA17C0)
	a0.AddMembershipLowCardVerbatim([]byte("env=prod"))
	a0.EndAttribute()

	// attr 1 — RefParametrized + MixedLowCardRefHighCardParam.
	// Different co-container cardinality.
	a1 := metric.BeginAttribute(87.0, "0x9b13d4ff")
	a1.AddToCoContainers("primary", 20)
	a1.AddToCoContainers("backup", 30)
	a1.AddMembershipLowCardRefParametrized([]byte("k=10"))
	a1.AddMembershipMixedLowCardRef(0x4EE71F, []byte("region=eu-west-1"))
	a1.EndAttribute()

	// attr 2 — MixedLowCardVerbatimHighCardParam, empty co-containers.
	a2 := metric.BeginAttribute(12.3, "0x00000000")
	a2.AddMembershipMixedLowCardVerbatim([]byte("channel"), []byte("channel=alpha"))
	a2.EndAttribute()

	metric.EndSection()

	// --- geoPoint (1 attribute, primary verbatim) ---
	gp := t.GetSectionGeoPoint().BeginAttribute(37.7749, -122.4194)
	gp.AddMembershipLowCardVerbatim([]byte("/geo/origin"))
	gp.EndAttribute().EndSection()

	// --- geoArea (1 attribute, no memberships) ---
	ga := t.GetSectionGeoArea().BeginAttribute("hilbert42")
	ga.AddToContainer(1.0)
	ga.AddToContainer(2.0)
	ga.AddToContainer(3.0)
	ga.AddToContainer(4.0)
	ga.EndAttribute().EndSection()

	if err = t.CommitEntity(); err != nil {
		err = eh.Errorf("commit fixture entity: %w", err)
		return
	}

	batches, err = t.TransferRecords(nil)
	if err != nil {
		err = eh.Errorf("transfer fixture records: %w", err)
		return
	}
	return
}
