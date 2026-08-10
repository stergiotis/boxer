package sharedsection

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestAssetSharedSectionRoundTrip is the ADR-0105 D2 post-condition: one
// entity carrying two components' memberships in a single shared section,
// written and read back, with each component decoding only its own
// attributes — the property the disjoint-sections gate protects by
// partitioning and this store protects by registry-stable ids.
func TestAssetSharedSectionRoundTrip(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewAssetStore(exec, nil, AssetStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx))

	// The emitted artefacts carry the caller-assigned ids, not 1..N: the
	// codec consts and the exported cross-check map agree with the
	// assignment the store generated under.
	require.Equal(t, uint64(7001), kindAssetName)
	require.Equal(t, uint64(7002), kindAssetPhase)
	require.Equal(t, map[string]map[string]uint64{
		"Label": {"assetName": 7001},
		"State": {"assetPhase": 7002},
	}, AssetMembershipIds)

	t0 := time.Unix(1_600_000_000, 0).UTC()

	// One entity, both kinds, ONE shared section frame. The typed Add
	// verbs cannot express this — the generated DML opens each section
	// frame once per entity (ADR-0146 D6; asserted below) — so the row is
	// composed through Raw()'s section surface, the escape hatch ADR-0100
	// SD6 sanctions for attribute-level manipulation of the open frame.
	b := st.Begin(1, t0)
	sec := b.Raw().GetSectionSymbol()
	nameAttr := sec.BeginAttribute("gizmo")
	nameAttr.AddMembershipLowCardRefP(kindAssetName)
	nameAttr.EndAttributeP()
	phaseAttr := sec.BeginAttribute("active")
	phaseAttr.AddMembershipLowCardRefP(kindAssetPhase)
	phaseAttr.EndAttributeP()
	sec.EndSection()
	require.NoError(t, b.Commit())

	// Two more entities carrying one kind each through the typed verbs —
	// sharing the section ACROSS rows needs no escape hatch.
	require.NoError(t, st.Begin(2, t0).AddLabel(Label{ID: 2, Name: "widget"}).Commit())
	require.NoError(t, st.Begin(3, t0).AddState(State{ID: 3, Phase: "retired"}).Commit())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// The shared row decodes each component from its own membership only.
	ent, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{"label", "state"}, ent.Archetype())
	require.Equal(t, option.Some(Label{ID: 1, Name: "gizmo"}), ent.Label)
	require.Equal(t, option.Some(State{ID: 1, Phase: "active"}), ent.State)

	// The baked Scan filters carry the assigned ids: each kind's Scan
	// finds exactly its rows in the shared section. (Filters baked from
	// any other id source would match nothing, silently — the failure
	// class one id source for codec + filters + map retires.)
	labels := map[uint64]string{}
	for ent, err := range st.ScanLabel(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, err)
		labels[ent.ID] = ent.Label.Val.Name
	}
	require.Equal(t, map[uint64]string{1: "gizmo", 2: "widget"}, labels)
	states := map[uint64]string{}
	for ent, err := range st.ScanState(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, err)
		states[ent.ID] = ent.State.Val.Phase
	}
	require.Equal(t, map[uint64]string{1: "active", 3: "retired"}, states)

	// The typed Add verbs still cannot stack two kinds onto one section
	// within one entity frame: each generated AddSections closes the
	// section, and the DML state machine refuses a second visit
	// (ADR-0146 D6 — RowComposer is the reflect-path answer). Commit
	// reports it and rolls the frame back.
	buffered := st.Buffered()
	err = st.Begin(9, t0).AddLabel(Label{ID: 9, Name: "x"}).AddState(State{ID: 9, Phase: "y"}).Commit()
	require.Error(t, err)
	require.Equal(t, buffered, st.Buffered(), "a failed Commit must not leave a partial row buffered")
}
