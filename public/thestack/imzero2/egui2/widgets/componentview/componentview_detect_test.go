package componentview_test

// ADR-0075's detect+decode, wired to ADR-0146's contract and Detect. These
// tests write a row with a WRITER DTO carrying several components, then read it
// back through per-component DTOs — the archetype comes off the wire, which is
// the claim ADR-0075 made and never implemented.

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
)

// The writer: one fat DTO carrying identity, battery and tasked, plus a
// `window` no component claims.
type cvtWriter struct {
	_        struct{} `kind:"cvtWriter"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Status   string   `lw:"droneStatus,symbol"`
	Tags     []string `lw:"droneTags,symbolArray"`
	Battery  uint64   `lw:"battery,u64Array,unit"`
}

// The readers: one DTO per component, each declaring only its own slots.
type cvtIdentity struct {
	_        struct{} `kind:"cvtIdentity"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Status   string   `lw:"droneStatus,symbol"`
}

type cvtBattery struct {
	_        struct{} `kind:"cvtBattery"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Charge   uint64   `lw:"battery,u64Array,unit"`
}

type cvtTasked struct {
	_        struct{} `kind:"cvtTasked"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Tags     []string `lw:"droneTags,symbolArray"`
}

func cvtLookup() marshallreflect.MapLookup {
	return marshallreflect.MapLookup{"droneStatus": 1, "droneTags": 2, "battery": 3}
}

func cvtBinder(t *testing.T) *componentview.Binder {
	t.Helper()
	b := componentview.NewBinder()
	b.Lookup = cvtLookup()

	identity, err := componentview.Bind(componentview.KindIdentity, func(r cvtIdentity) any {
		return componentview.IdentityVal{Status: r.Status}
	})
	require.NoError(t, err)
	battery, err := componentview.Bind(componentview.KindBattery, func(r cvtBattery) any {
		return componentview.BatteryVal{Charge: r.Charge}
	})
	require.NoError(t, err)
	tasked, err := componentview.Bind(componentview.KindTasked, func(r cvtTasked) any {
		return componentview.TaskedVal{Tags: r.Tags}
	})
	require.NoError(t, err)

	require.NoError(t, b.Add(identity))
	require.NoError(t, b.Add(battery))
	require.NoError(t, b.Add(tasked))
	return b
}

func cvtWrite(t *testing.T, rows []cvtWriter) (arrow.RecordBatch, func()) {
	t.Helper()
	tbl := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Marshal(tbl, rows, cvtLookup()))
	recs, err := tbl.TransferRecords(nil)
	require.NoError(t, err)
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

func cvtReaders(t *testing.T, rec arrow.RecordBatch) (*marshallreflect.SectionReaders, func()) {
	t.Helper()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	symR := anchor.NewReadAccessTestTableTaggedSymbol()
	symR.SetColumnIndices(symR.GetColumnIndices())
	require.NoError(t, symR.LoadFromRecord(rec))
	saR := anchor.NewReadAccessTestTableTaggedSymbolArray()
	saR.SetColumnIndices(saR.GetColumnIndices())
	require.NoError(t, saR.LoadFromRecord(rec))
	uR := anchor.NewReadAccessTestTableTaggedU64Array()
	uR.SetColumnIndices(uR.GetColumnIndices())
	require.NoError(t, uR.LoadFromRecord(rec))
	return marshallreflect.NewSectionReaders(idR.Len()).
			PlainColumn("id", idR.ValueId).
			PlainColumn("naturalKey", idR.ValueNaturalKey).
			Section("symbol", symR.GetAttributes(), symR.GetMemberships()).
			Section("symbolArray", saR.GetAttributes(), saR.GetMemberships()).
			Section("u64Array", uR.GetAttributes(), uR.GetMemberships()),
		func() { idR.Release(); symR.Release(); saR.Release(); uR.Release() }
}

// The archetype differs per row and is read, not assumed: drone 3 carries no
// tags, so its container slot holds zero attributes and `tasked` is absent.
func TestBinder_DetectsPerRowArchetype(t *testing.T) {
	rec, release := cvtWrite(t, []cvtWriter{
		{ID: 7, Tracking: []byte("TRK-7"), Status: "IN_TRANSIT", Tags: []string{"survey"}, Battery: 8500},
		{ID: 3, Tracking: []byte("TRK-3"), Status: "CHARGING", Tags: nil, Battery: 900},
	})
	defer release()
	readers, rel := cvtReaders(t, rec)
	defer rel()
	b := cvtBinder(t)

	row0, err := b.Detect(readers, 0)
	require.NoError(t, err)
	require.Equal(t, []componentview.KindPresence{
		{Kind: componentview.KindIdentity, Presence: mappingplan.PresenceExact},
		{Kind: componentview.KindBattery, Presence: mappingplan.PresenceExact},
		{Kind: componentview.KindTasked, Presence: mappingplan.PresenceExact},
	}, row0)

	row1, err := b.Detect(readers, 1)
	require.NoError(t, err)
	require.Equal(t, mappingplan.PresenceAbsent, row1[2].Presence,
		"drone 3 has no tags, so its container slot is empty and `tasked` is absent")
	require.Equal(t, mappingplan.PresenceExact, row1[0].Presence)
}

func TestBinder_ComponentsDecodeOffTheWire(t *testing.T) {
	rec, release := cvtWrite(t, []cvtWriter{
		{ID: 7, Tracking: []byte("TRK-7"), Status: "IN_TRANSIT", Tags: []string{"survey", "urgent"}, Battery: 8500},
		{ID: 3, Tracking: []byte("TRK-3"), Status: "CHARGING", Tags: nil, Battery: 900},
	})
	defer release()
	readers, rel := cvtReaders(t, rec)
	defer rel()
	b := cvtBinder(t)

	got, err := b.Components(readers, 0)
	require.NoError(t, err)
	require.Equal(t, []componentview.Component{
		{Kind: componentview.KindIdentity, Value: componentview.IdentityVal{Status: "IN_TRANSIT"}},
		{Kind: componentview.KindBattery, Value: componentview.BatteryVal{Charge: 8500}},
		{Kind: componentview.KindTasked, Value: componentview.TaskedVal{Tags: []string{"survey", "urgent"}}},
	}, got)

	// Absent components are omitted, not rendered as empty values — the
	// dispatcher's ShowAbsent draws them from the registry instead.
	got, err = b.Components(readers, 1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, componentview.KindIdentity, got[0].Kind)
	require.Equal(t, componentview.KindBattery, got[1].Kind)
}

// A component the binder has never heard of contributes nothing: its section is
// simply an attribute no binding claims. This is what lets a stage read rows
// that other stages fused and enriched without knowing their whole vocabulary.
func TestBinder_IgnoresUnknownComponents(t *testing.T) {
	rec, release := cvtWrite(t, []cvtWriter{
		{ID: 7, Tracking: []byte("TRK-7"), Status: "IN_TRANSIT", Battery: 8500},
	})
	defer release()
	readers, rel := cvtReaders(t, rec)
	defer rel()

	// A binder that knows ONLY identity still reads it cleanly off a row
	// carrying battery too.
	b := componentview.NewBinder()
	b.Lookup = cvtLookup()
	identity, err := componentview.Bind(componentview.KindIdentity, func(r cvtIdentity) any {
		return componentview.IdentityVal{Status: r.Status}
	})
	require.NoError(t, err)
	require.NoError(t, b.Add(identity))

	got, err := b.Components(readers, 0)
	require.NoError(t, err)
	require.Equal(t, []componentview.Component{
		{Kind: componentview.KindIdentity, Value: componentview.IdentityVal{Status: "IN_TRANSIT"}},
	}, got)
}

// The binder's registry catalogues what is bound, and reports rather than
// refuses overlap (ADR-0146 D5).
func TestBinder_RegistryCataloguesContracts(t *testing.T) {
	b := cvtBinder(t)
	reg := b.Registry()
	require.ElementsMatch(t, []string{"cvtIdentity", "cvtBattery", "cvtTasked"}, reg.Kinds())
	require.Equal(t, []string{"cvtIdentity"}, reg.SlotClaims()["symbol@droneStatus"])

	// A second component kind reading the SAME slots is allowed: two renderers
	// over one shape is a presentation choice, and overlap is not something the
	// catalogue judges (ADR-0146 D5).
	shadow, err := componentview.Bind(componentview.ComponentKindE("shadow"), func(r cvtIdentity) any {
		return componentview.IdentityVal{Status: r.Status}
	})
	require.NoError(t, err)
	require.NoError(t, b.Add(shadow))
	require.Len(t, b.Bindings(), 4)
	require.Equal(t, []string{"cvtIdentity"}, reg.SlotClaims()["symbol@droneStatus"],
		"the shared DTO contributes one contract, not two")
}

func TestBinder_RejectsDuplicateKind(t *testing.T) {
	b := cvtBinder(t)
	dup, err := componentview.Bind(componentview.KindIdentity, func(r cvtBattery) any {
		return componentview.IdentityVal{}
	})
	require.NoError(t, err)
	require.ErrorContains(t, b.Add(dup), "already bound")
}

func TestBinder_MissingSectionReaderIsReported(t *testing.T) {
	rec, release := cvtWrite(t, []cvtWriter{{ID: 7, Tracking: []byte("T"), Status: "S", Battery: 1}})
	defer release()
	idR := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idR.SetColumnIndices(idR.GetColumnIndices())
	require.NoError(t, idR.LoadFromRecord(rec))
	defer idR.Release()

	readers := marshallreflect.NewSectionReaders(idR.Len()).
		PlainColumn("id", idR.ValueId).
		PlainColumn("naturalKey", idR.ValueNaturalKey)

	b := cvtBinder(t)
	_, err := b.Detect(readers, 0)
	require.ErrorContains(t, err, "symbol")
}
