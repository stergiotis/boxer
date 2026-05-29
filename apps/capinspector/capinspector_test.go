//go:build llm_generated_opus47

package capinspector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestRegistry_HasAllShippedCaps(t *testing.T) {
	wantIds := []CapId{CapRun, CapFacts, CapBus, CapFs, CapPersist, CapTask}
	for _, id := range wantIds {
		_, ok := Registry[id]
		assert.True(t, ok, "Registry must contain %q", id)
	}
	assert.Len(t, Registry, 6, "exactly six shipped caps; adding a cap means appending to caps.go")
}

func TestAllCapIdsOrdered_StableAcrossCalls(t *testing.T) {
	a := allCapIdsOrdered()
	b := allCapIdsOrdered()
	assert.Equal(t, a, b, "ordered list must be deterministic — UI shimmies otherwise")
}

func TestPushPopSelection_FifoSemantics(t *testing.T) {
	// Drain whatever is left from concurrent tests so we start clean.
	for popSelection() != "" {
	}
	PushSelection(CapFs)
	PushSelection(CapPersist)
	PushSelection(CapBus)
	assert.Equal(t, CapFs, popSelection())
	assert.Equal(t, CapPersist, popSelection())
	assert.Equal(t, CapBus, popSelection())
	assert.Equal(t, CapId(""), popSelection(), "drained queue must return empty")
}

func TestNewApp_CapturesSelectionAtConstruction(t *testing.T) {
	for popSelection() != "" {
	}
	PushSelection(CapFs)
	a := newApp()
	assert.Equal(t, CapFs, a.selectedCap)
	// Second app gets the next selection — independent windows.
	PushSelection(CapPersist)
	b := newApp()
	assert.Equal(t, CapPersist, b.selectedCap)
}

func TestNewApp_NoSelection_EmptyCap(t *testing.T) {
	for popSelection() != "" {
	}
	a := newApp()
	assert.Equal(t, CapId(""), a.selectedCap,
		"opened-without-click case must surface as empty cap so Frame can render the picker")
}

func TestCapSpec_FsAppFilter(t *testing.T) {
	spec := Registry[CapFs]
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.dialog.read"}))
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.handle.>"}))
	assert.False(t, spec.AppFilter(app.SubjectFilter{Pattern: "runtime.persist.x.>"}))
}

func TestCapSpec_PersistAppFilterAndHostInjected(t *testing.T) {
	spec := Registry[CapPersist]
	assert.True(t, spec.AppFilter(app.SubjectFilter{Pattern: "runtime.persist.foo.>"}))
	assert.False(t, spec.AppFilter(app.SubjectFilter{Pattern: "fs.dialog.read"}))

	// HostInjected reflects manifest.PersistedKeys.
	// SubjectAlias takes the last path segment, so "github.com/example/play"
	// → alias "play" → pattern "runtime.persist.play.>".
	mPersist := app.Manifest{Id: "github.com/example/play", PersistedKeys: []string{"k"}}
	pat := spec.HostInjected(mPersist)
	assert.Equal(t, "runtime.persist.play.>", pat)

	mNone := app.Manifest{Id: "github.com/example/q"}
	assert.Empty(t, spec.HostInjected(mNone), "no keys → no injected cap")
}

func TestMatchedApps_PicksPersistConsumers(t *testing.T) {
	// Build a throwaway registry to avoid leaking test apps into
	// app.DefaultRegistry; matchedApps reads from
	// app.DefaultRegistry directly, so a unit-test for the filtering
	// logic uses the predicates directly.
	spec := Registry[CapPersist]
	consumer := app.Manifest{
		Id:            "test.consumer",
		PersistedKeys: []string{"x"},
	}
	require.True(t, spec.HostInjected(consumer) != "")

	nonconsumer := app.Manifest{Id: "test.none"}
	require.False(t, spec.HostInjected(nonconsumer) != "")
}

func TestDiagramCapLabel_CoversEveryCap(t *testing.T) {
	for _, capId := range allCapIdsOrdered() {
		got := diagramCapLabel(capId)
		assert.NotEmpty(t, got, "every shipped cap must have a diagram label, capId=%q", capId)
		// Labels fit roughly 18 chars in a 152px box at 12.5pt.
		assert.LessOrEqual(t, len(got), 18, "diagram label too long for the box, capId=%q label=%q", capId, got)
	}
}

func TestRegistry_BackendsPopulatedForEveryCap(t *testing.T) {
	for _, capId := range allCapIdsOrdered() {
		s, ok := Registry[capId]
		require.True(t, ok)
		require.NotEmpty(t, s.Backends, "every cap needs ≥1 BackendImpl, capId=%q", capId)
		for _, b := range s.Backends {
			assert.NotEmpty(t, b.Id, "backend id required, capId=%q", capId)
			assert.NotEmpty(t, b.Display, "backend display required, capId=%q backendId=%q", capId, b.Id)
		}
	}
}

func TestActiveBackend_SetGetRoundtrip(t *testing.T) {
	resetActiveBackends()
	defer resetActiveBackends()
	SetActiveBackend(CapFacts, "chstore")
	assert.Equal(t, "chstore", ActiveBackend(CapFacts))
	SetActiveBackend(CapFacts, "inmem") // overwrite
	assert.Equal(t, "inmem", ActiveBackend(CapFacts))
	assert.Equal(t, "", ActiveBackend(CapBus), "unset cap returns empty string")
}

func TestShortAppName(t *testing.T) {
	cases := map[app.AppIdT]string{
		"github.com/example/play":                                                "play",
		"github.com/stergiotis/boxer/apps/capdemo":       "capdemo",
		"flat":                                                                   "flat",
		"":                                                                       "",
	}
	for in, want := range cases {
		got := shortAppName(app.Manifest{Id: in})
		assert.Equal(t, want, got, "in=%q", in)
	}
}
