package windowhost

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// testWsCfgKind is the launch kind of the fixture's workingset participant.
// Its probe accepts testCfgBytes only, as testCfgKind's does.
const testWsCfgKind = "windowhostTestWsCfg"

func init() {
	kindcheck.Register(testWsCfgKind, func(b []byte) (err error) {
		if !bytes.Equal(b, testCfgBytes) {
			err = eh.Errorf("test probe: bytes are not this kind's payload")
		}
		return
	})
}

// wsFixture is a host over three factory apps — test.plain takes no launch
// config, test.cfg takes testCfgKind, test.ws is a workingset participant —
// with a catalog attached and the audit stores wired.
type wsFixture struct {
	h     *Inst
	reg   *app.Registry
	cat   *workstatelet.Catalog
	facts testStores
}

func newWsFixture(t *testing.T) (fx wsFixture) {
	t.Helper()
	fx.reg = app.NewRegistry()
	plain := mkManifest("test.plain")
	require.NoError(t, fx.reg.RegisterFactory(plain, func() (a app.AppI, err error) {
		a = &launchApp{manifest: plain}
		return
	}))
	cfg := mkLaunchManifest("test.cfg", testCfgKind)
	require.NoError(t, fx.reg.RegisterFactory(cfg, func() (a app.AppI, err error) {
		a = &launchApp{manifest: cfg}
		return
	}))
	ws := mkWorkingsetManifest("test.ws")
	ws.LaunchKind = testWsCfgKind
	require.NoError(t, fx.reg.RegisterFactory(ws, func() (a app.AppI, err error) {
		a = &wsApp{manifest: ws, cfg: testCfgBytes}
		return
	}))
	fx.cat = workstatelet.NewCatalog()
	fx.h = NewInst(fx.reg, zerolog.Nop())
	fx.h.SetWorkstatelets(fx.cat)
	fx.facts = attachStores(fx.h)
	return
}

func (inst wsFixture) mint(t *testing.T, slug string, desktop workstatelet.Desktop, windows ...workstatelet.Window) (id app.AppIdT) {
	t.Helper()
	id, err := inst.cat.Mint(inst.reg, workstatelet.Doc{
		Slug: slug, Title: "Desk " + slug, Summary: "A test desk", Topics: []app.TopicT{app.TopicRuntime},
		Desktop: desktop, Windows: windows,
	}, workstatelet.OriginBook, "test")
	require.NoError(t, err)
	return
}

// syncOrder runs the order sync a Frame runs, without the Rust runtime.
func syncOrder(h *Inst) {
	h.mu.Lock()
	snapshot := append([]*window(nil), h.windows...)
	h.mu.Unlock()
	h.takeTileInputs(snapshot)
}

func windowOf(t *testing.T, h *Inst, key WindowKeyT) (w *window) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	w = h.openWindowLocked(key)
	require.NotNil(t, w)
	return
}

func TestWorkstatelet_OpensInOrderAndSetsTheLayout(t *testing.T) {
	fx := newWsFixture(t)
	existing, err := fx.h.Open("test.plain")
	require.NoError(t, err)
	syncOrder(fx.h)

	id := fx.mint(t, "desk", workstatelet.Desktop{Layout: "main-stack", MainRatio: 0.7},
		workstatelet.Window{App: "test.plain"},
		workstatelet.Window{App: "test.cfg", Kind: testCfgKind, Config: testCfgBytes},
		workstatelet.Window{App: "test.plain", Floating: true})

	opened, skipped, err := fx.h.OpenWorkstatelet(id)
	require.NoError(t, err)
	assert.Empty(t, skipped)
	require.Len(t, opened, 3)
	assert.Equal(t, 4, fx.h.Len(), "opening adds: the window that was open stays")

	syncOrder(fx.h)
	assert.Equal(t, []WindowKeyT{opened[0], opened[1], opened[2], existing}, fx.h.tileOrder,
		"the document's windows lead the order in document order; what was open follows")
	assert.Greater(t, opened[0], opened[2], "the first window opened last, so it stacks in front")
	assert.Equal(t, opened[0], fx.h.pendingRaise)
	assert.Equal(t, LayoutMainStack, fx.h.Layout())
	assert.Equal(t, LayoutParams{MainRatio: 0.7, MainCount: DefaultLayoutParams.MainCount}, fx.h.LayoutParams(),
		"a parameter the document does not give keeps the host's value")
	assert.False(t, fx.h.WindowFloating(opened[0]))
	assert.True(t, fx.h.WindowFloating(opened[2]))

	mountOpenWindows(t, fx.h)
	assert.Equal(t, testCfgBytes, windowOf(t, fx.h, opened[1]).appInst.(*launchApp).gotCfg,
		"a window's config is delivered at Mount like any launch config")
	assert.Nil(t, windowOf(t, fx.h, opened[0]).appInst.(*launchApp).gotCfg)

	launches := fx.facts.Launches()
	require.Len(t, launches, 3)
	for _, l := range launches {
		assert.Equal(t, id, l.CallerAppId, "the launch rows name the workstatelet")
	}
	byKey := map[uint64]int{}
	for i, l := range launches {
		byKey[l.TileKey] = i
	}
	cfgRow := launches[byKey[uint64(opened[1])]]
	assert.Equal(t, app.AppIdT("test.cfg"), cfgRow.TargetAppId)
	assert.Equal(t, testCfgKind, cfgRow.ConfigKind)
	assert.Equal(t, testCfgBytes, cfgRow.Config)
	assert.Empty(t, launches[byKey[uint64(opened[0])]].ConfigKind)
}

func TestWorkstatelet_OpenDispatchesAndALaunchRequestIsRefused(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{},
		workstatelet.Window{App: "test.plain"}, workstatelet.Window{App: "test.cfg"})

	fx.h.SetLayout(LayoutColumns)
	key, err := fx.h.Open(id)
	require.NoError(t, err)
	assert.Equal(t, 2, fx.h.Len())
	assert.Equal(t, app.AppIdT("test.plain"), windowOf(t, fx.h, key).manifest.Id, "Open answers with the document's first window")
	assert.Equal(t, LayoutColumns, fx.h.Layout(), "a document that names no layout leaves the desktop's")

	_, opened, err := fx.h.OpenOrRaise(id)
	require.NoError(t, err)
	assert.True(t, opened, "a workstatelet has no window of its own to raise; it opens again")
	assert.Equal(t, 4, fx.h.Len())

	_, err = fx.h.OpenWithConfig(id, "", nil)
	require.Error(t, err, "the door a launch request and a task's launch come through")
	assert.Contains(t, err.Error(), "not through a launch request")
	_, err = fx.h.OpsOpen(id, "", nil)
	assert.Error(t, err)
	assert.Equal(t, 4, fx.h.Len())
}

func TestWorkstatelet_SkipsWhatIsRefusedAndOpensTheRest(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{},
		workstatelet.Window{App: "test.absent"},
		workstatelet.Window{App: "test.plain"},
		workstatelet.Window{App: "test.cfg", Kind: testCfgKind, Config: []byte("not the kind's bytes")},
		workstatelet.Window{App: "test.plain", Kind: testCfgKind, Config: testCfgBytes})

	opened, skipped, err := fx.h.OpenWorkstatelet(id)
	require.NoError(t, err)
	require.Len(t, opened, 1)
	assert.Equal(t, app.AppIdT("test.plain"), windowOf(t, fx.h, opened[0]).manifest.Id)
	require.Len(t, skipped, 3)
	assert.Equal(t, []int{0, 2, 3}, []int{skipped[0].Index, skipped[1].Index, skipped[2].Index})
	for _, s := range skipped {
		assert.Error(t, s.Err)
	}
	assert.Len(t, fx.facts.Launches(), 1, "a skipped window writes no launch row")
}

func TestWorkstatelet_NothingOpenedIsAnError(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{Layout: "columns"}, workstatelet.Window{App: "test.absent"})

	opened, skipped, err := fx.h.OpenWorkstatelet(id)
	require.Error(t, err)
	assert.Empty(t, opened)
	assert.Len(t, skipped, 1)
	assert.Equal(t, LayoutFloating, fx.h.Layout(), "a document that opened nothing does not move the desktop")
}

func TestWorkstatelet_UnknownLayoutOpensNothing(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{Layout: "spiral"}, workstatelet.Window{App: "test.plain"})

	_, _, err := fx.h.OpenWorkstatelet(id)
	require.Error(t, err)
	assert.Equal(t, 0, fx.h.Len())
}

func TestWorkstatelet_AWindowNamingOnlyAKindIsResolved(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{},
		workstatelet.Window{Kind: testCfgKind, Config: testCfgBytes})

	opened, _, err := fx.h.OpenWorkstatelet(id)
	require.NoError(t, err)
	require.Len(t, opened, 1)
	assert.Equal(t, app.AppIdT("test.cfg"), windowOf(t, fx.h, opened[0]).manifest.Id)
	launches := fx.facts.Launches()
	require.Len(t, launches, 1)
	assert.True(t, launches[0].ResolvedFromKind)
}

func TestWorkstatelet_AnAliasNamesAnApp(t *testing.T) {
	fx := newWsFixture(t)
	aliased := mkManifest("example.org/apps/aliased")
	require.NoError(t, fx.reg.RegisterFactory(aliased, func() (a app.AppI, err error) {
		a = &launchApp{manifest: aliased}
		return
	}))
	id := fx.mint(t, "desk", workstatelet.Desktop{}, workstatelet.Window{App: "aliased"})

	opened, _, err := fx.h.OpenWorkstatelet(id)
	require.NoError(t, err)
	require.Len(t, opened, 1)
	assert.Equal(t, aliased.Id, windowOf(t, fx.h, opened[0]).manifest.Id)
}

func TestWorkstatelet_TheLaunchLimitHolds(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{},
		workstatelet.Window{App: "test.plain"}, workstatelet.Window{App: "test.cfg"})

	fx.reg.LimitLaunches([]app.AppIdT{id, "test.plain"})
	opened, skipped, err := fx.h.OpenWorkstatelet(id)
	require.NoError(t, err)
	assert.Len(t, opened, 1, "a window whose app the limit refuses is skipped")
	assert.Len(t, skipped, 1)

	fx.reg.LimitLaunches([]app.AppIdT{"test.plain", "test.cfg"})
	_, _, err = fx.h.OpenWorkstatelet(id)
	assert.Error(t, err, "the limit names a workstatelet like any entry")
}

func TestWorkstatelet_RefusedWithoutACatalogOrForAnApp(t *testing.T) {
	fx := newWsFixture(t)
	id := fx.mint(t, "desk", workstatelet.Desktop{}, workstatelet.Window{App: "test.plain"})

	_, _, err := fx.h.OpenWorkstatelet("test.plain")
	assert.Error(t, err, "an app is not a workstatelet")
	_, _, err = fx.h.OpenWorkstatelet("test.absent")
	assert.Error(t, err)

	fx.h.SetWorkstatelets(nil)
	_, err = fx.h.Open(id)
	assert.Error(t, err)
	assert.Equal(t, 0, fx.h.Len())
}

func TestWorkstatelet_CaptureWritesTheDesktop(t *testing.T) {
	fx := newWsFixture(t)
	shell := mkManifest("test.shell")
	shell.Shell = true
	require.NoError(t, fx.reg.RegisterFactory(shell, func() (a app.AppI, err error) {
		a = &launchApp{manifest: shell}
		return
	}))
	kPlain, err := fx.h.Open("test.plain")
	require.NoError(t, err)
	kCfg, err := fx.h.OpenWithConfig("test.cfg", testCfgKind, testCfgBytes)
	require.NoError(t, err)
	kWs, err := fx.h.Open("test.ws")
	require.NoError(t, err)
	_, err = fx.h.Open("test.shell")
	require.NoError(t, err)
	mountOpenWindows(t, fx.h)
	syncOrder(fx.h)
	require.NoError(t, fx.h.SetLayout(LayoutMainStack))
	fx.h.SetLayoutParams(LayoutParams{MainRatio: 0.5, MainCount: 2})
	require.NoError(t, fx.h.FloatWindow(kPlain, true))

	doc, err := fx.h.CaptureWorkstatelet()
	require.NoError(t, err)
	assert.Equal(t, workstatelet.Desktop{Layout: "main-stack", MainRatio: 0.5, MainCount: 2}, doc.Desktop)
	require.Len(t, doc.Windows, 3, "the shell app's window is left out")
	// The order: newest first, as the sync put them.
	assert.Equal(t, workstatelet.Window{App: "test_ws", Kind: testWsCfgKind, Config: testCfgBytes}, doc.Windows[0],
		"a workingset participant is composed, dirty or not")
	assert.Equal(t, workstatelet.Window{App: "test_cfg", Kind: testCfgKind, Config: testCfgBytes}, doc.Windows[1],
		"a window that composes nothing keeps the config it was opened with")
	assert.Equal(t, workstatelet.Window{App: "test_plain", Floating: true}, doc.Windows[2])
	assert.Equal(t, []app.TopicT{app.TopicRuntime}, doc.Topics)
	assert.Equal(t, "Opens test.ws, test.cfg, test.plain", doc.Summary)
	_, _ = kCfg, kWs

	// What a capture writes, an open reads: the document survives the round
	// trip through its text and opens the same windows.
	doc.Slug, doc.Title = "captured", "Captured"
	composed, err := workstatelet.Compose(doc)
	require.NoError(t, err)
	parsed, err := workstatelet.Parse("captured", composed.Source, nil)
	require.NoError(t, err)
	assert.Equal(t, doc.Windows, parsed.Windows)
	assert.Equal(t, doc.Desktop, parsed.Desktop)
}

func TestWorkstatelet_CaptureFallsBackWhenTheComposedBytesAreRefused(t *testing.T) {
	fx := newWsFixture(t)
	// A participant that composes bytes its own kind's probe refuses: the
	// capture keeps the config the window was opened with instead.
	bad := mkWorkingsetManifest("test.wsbad")
	bad.LaunchKind = testWsCfgKind
	require.NoError(t, fx.reg.RegisterFactory(bad, func() (a app.AppI, err error) {
		a = &wsApp{manifest: bad, cfg: []byte("not the kind's bytes")}
		return
	}))
	_, err := fx.h.OpenWithConfig("test.wsbad", testWsCfgKind, testCfgBytes)
	require.NoError(t, err)
	_, err = fx.h.Open("test.wsbad")
	require.NoError(t, err)
	mountOpenWindows(t, fx.h)
	syncOrder(fx.h)

	doc, err := fx.h.CaptureWorkstatelet()
	require.NoError(t, err)
	require.Len(t, doc.Windows, 2)
	assert.Equal(t, workstatelet.Window{App: "test_wsbad"}, doc.Windows[0],
		"opened plainly and composing nothing usable, the window is written without a config")
	assert.Equal(t, workstatelet.Window{App: "test_wsbad", Kind: testWsCfgKind, Config: testCfgBytes}, doc.Windows[1])
	assert.Equal(t, workstatelet.Desktop{}, doc.Desktop, "a floating desktop writes no layout")
}

func TestWorkstatelet_CaptureRequestsAreAnsweredByTheFrame(t *testing.T) {
	fx := newWsFixture(t)
	var got []error
	fx.h.RequestWorkstateletCapture(func(doc workstatelet.Doc, err error) { got = append(got, err) })
	fx.h.stepCaptures()
	require.Len(t, got, 1)
	assert.Error(t, got[0], "an empty desktop has nothing to capture")

	_, err := fx.h.Open("test.plain")
	require.NoError(t, err)
	var docs []workstatelet.Doc
	fx.h.RequestWorkstateletCapture(func(doc workstatelet.Doc, err error) {
		require.NoError(t, err)
		docs = append(docs, doc)
	})
	fx.h.stepCaptures()
	fx.h.stepCaptures()
	require.Len(t, docs, 1, "a request is answered once")
	assert.Len(t, docs[0].Windows, 1)
}

// The document package restates the host's cap on a launch config so it can
// refuse a document when it is read; the two must not drift.
func TestWorkstatelet_ConfigCapMatchesTheOpenBoundary(t *testing.T) {
	assert.Equal(t, maxLaunchConfigBytes, workstatelet.MaxConfigBytes)
}
