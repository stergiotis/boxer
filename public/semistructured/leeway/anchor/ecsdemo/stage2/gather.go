package stage2

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// droneLookup maps each membership name to the id the generated codec writes
// (the kind* constants in dto.out.go). Marshalling, the readback resolver, and
// component extraction all use it, so the ids in the Arrow batch, the SQL, and
// the typed reads line up.
var droneLookup = marshallreflect.MapLookup{
	"droneStatus":  kindStatus,
	"droneBattery": kindBattery,
	"droneTags":    kindTags,
	"droneLoc":     kindLat,
	"droneWindow":  kindWindowBegin,
}

// sectionReaderI is the slice of the generated read-access readers FatRow drives
// uniformly to bind and release them.
type sectionReaderI interface {
	GetColumnIndices() []uint32
	SetColumnIndices([]uint32) []uint32
	LoadFromRecord(runtime.RecordI) error
	Release()
}

// FatRow wraps every section's read-access reader, all bound to one Arrow record
// — the fat entity row from which typed components are gathered. It is the
// stage-2 analogue of a World row; Extract reads one component out of it while
// ignoring the other sections.
type FatRow struct {
	id        *ReadAccessDroneTablePlainEntityIdAttributes
	symbol    *ReadAccessDroneTableTaggedSymbol
	u64Array  *ReadAccessDroneTableTaggedU64Array
	symbolArr *ReadAccessDroneTableTaggedSymbolArray
	geoPoint  *ReadAccessDroneTableTaggedGeoPoint
	timeRange *ReadAccessDroneTableTaggedTimeRange
}

// NewFatRow loads every section reader from rec (which arrow.RecordBatch
// satisfies). Call Release when done.
func NewFatRow(rec runtime.RecordI) (inst *FatRow, err error) {
	inst = &FatRow{
		id:        NewReadAccessDroneTablePlainEntityIdAttributes(),
		symbol:    NewReadAccessDroneTableTaggedSymbol(),
		u64Array:  NewReadAccessDroneTableTaggedU64Array(),
		symbolArr: NewReadAccessDroneTableTaggedSymbolArray(),
		geoPoint:  NewReadAccessDroneTableTaggedGeoPoint(),
		timeRange: NewReadAccessDroneTableTaggedTimeRange(),
	}
	for _, r := range inst.readers() {
		r.SetColumnIndices(r.GetColumnIndices())
		if err = r.LoadFromRecord(rec); err != nil {
			return nil, eh.Errorf("load drone reader from record: %w", err)
		}
	}
	return
}

func (inst *FatRow) readers() []sectionReaderI {
	return []sectionReaderI{inst.id, inst.symbol, inst.u64Array, inst.symbolArr, inst.geoPoint, inst.timeRange}
}

// Release frees the underlying Arrow buffers held by every reader.
func (inst *FatRow) Release() {
	for _, r := range inst.readers() {
		r.Release()
	}
}

// NumRows is the number of entities in the row batch.
func (inst *FatRow) NumRows() int { return inst.id.Len() }

// Archetype reports which components the entity at idx carries, in a fixed order
// — the stage-2 analogue of stage-1's Entity.Components(). A component is present
// iff its primary section is populated for the entity (the leeway approximate
// presence: GetNumberOfAttributes > 0). Each component is keyed on one section
// (identity→symbol, battery→u64Array, located→geoPoint, tasked→timeRange); the
// symbolArray that also backs Tasked.Tags is optional and not part of detection.
func (inst *FatRow) Archetype(idx int) []string {
	ei := runtime.EntityIdx(idx)
	var a []string
	if inst.symbol.GetAttributes().GetNumberOfAttributes(ei) > 0 {
		a = append(a, "identity")
	}
	if inst.u64Array.GetAttributes().GetNumberOfAttributes(ei) > 0 {
		a = append(a, "battery")
	}
	if inst.geoPoint.GetAttributes().GetNumberOfAttributes(ei) > 0 {
		a = append(a, "located")
	}
	if inst.timeRange.GetAttributes().GetNumberOfAttributes(ei) > 0 {
		a = append(a, "tasked")
	}
	return a
}

func (inst *FatRow) readerSet() *marshallreflect.SectionReaders {
	return marshallreflect.NewSectionReaders(inst.id.Len()).
		PlainColumn("id", inst.id.ValueId).
		Section("symbol", inst.symbol.GetAttributes(), inst.symbol.GetMemberships()).
		Section("u64Array", inst.u64Array.GetAttributes(), inst.u64Array.GetMemberships()).
		Section("symbolArray", inst.symbolArr.GetAttributes(), inst.symbolArr.GetMemberships()).
		Section("geoPoint", inst.geoPoint.GetAttributes(), inst.geoPoint.GetMemberships()).
		Section("timeRange", inst.timeRange.GetAttributes(), inst.timeRange.GetMemberships())
}

// Extract reads component T out of the fat row for every entity, in row order. T
// must be one of the component DTOs (Identity, Battery, Located, Tasked); its lw:
// tags select which section(s) are read and the rest of the row is ignored. It
// is the stage-2, per-component analogue of stage-1's World.Gather. An absent
// section yields the component's zero value; pair with a presence check
// (GetNumberOfAttributes) to distinguish absent from zero.
func Extract[T any](inst *FatRow) (out []T, err error) {
	if err = marshallreflect.Unmarshal(inst.readerSet(), &out, droneLookup); err != nil {
		err = eh.Errorf("extract component from fat row: %w", err)
	}
	return
}
