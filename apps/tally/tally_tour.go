package tally

// tally_tour.go enrols one synthetic scene into the imzero2 demo registry
// (ADR-0057, keelson-app A8) so the central TestDriver captures the browser
// pane in the widgets tour. The live App reads a lading store over
// ClickHouse; the tour draws the same pane over an in-memory io/fs tree, the
// seam the Results pane's tests already use (tally_resultfs_test.go), so
// the scene is deterministic and needs no server. Screenshot scaffolding
// only: the lower tabs are store queries and are not drawn here.

import (
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

const (
	// tourPaneH bounds the browser in the gallery, whose interactive host
	// is an unbounded scroll area; the live pane takes its height from a
	// probe instead.
	tourPaneH = float32(420)
	// tourDir is the directory the scene opens on: deep enough for the
	// breadcrumb to carry segments, with files whose sizes sort differently
	// from their names.
	tourDir = "doc/adr"
	// tourSel is the file the scene selects, so the capture shows a selected
	// row.
	tourSel = "doc/adr/0198-fs-snapshot-store.md"
)

// tourTree is the snapshot the scene browses: the shape of a repository
// checkout, chosen so the listing has directories and files, dot-names, a
// large file and modification times a day apart.
func tourTree() (fsys fstest.MapFS) {
	t0 := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	at := func(d int) time.Time { return t0.Add(time.Duration(d) * 24 * time.Hour) }
	blob := func(n int) []byte { return make([]byte, n) }
	return fstest.MapFS{
		"README.md":                              {Data: blob(4_120), ModTime: at(0)},
		"go.mod":                                 {Data: blob(612), ModTime: at(2)},
		".gitignore":                             {Data: blob(41), ModTime: at(0)},
		"doc/ARCHITECTURE.md":                    {Data: blob(18_900), ModTime: at(5)},
		"doc/adr/0047-keelson-help-system.md":    {Data: blob(9_320), ModTime: at(1)},
		"doc/adr/0198-fs-snapshot-store.md":      {Data: blob(27_480), ModTime: at(3)},
		"doc/adr/0200-tally-lading-browser.md":   {Data: blob(14_050), ModTime: at(4)},
		"doc/adr/0222-tally-path-set-queries.md": {Data: blob(11_700), ModTime: at(6)},
		"doc/adr/README.md":                      {Data: blob(1_024), ModTime: at(0)},
		"doc/adr/img/ledger.png":                 {Data: blob(118_000), ModTime: at(2)},
		"doc/howto/lading-snapshot-store.md":     {Data: blob(7_210), ModTime: at(5)},
		"apps/tally/tally.go":                    {Data: blob(52_300), ModTime: at(6)},
		"apps/tally/help/overview.md":            {Data: blob(5_900), ModTime: at(6)},
		"scripts/dev/scene.sh":                   {Data: blob(1_480), ModTime: at(1)},
		"vendor/archive.tar.gz":                  {Data: blob(33_554_432), ModTime: at(0)},
	}
}

func init() {
	registry.Register(registry.Demo{
		Name:     "tally-browse",
		Category: "Data",
		Title:    icons.PhFolderOpen + " tally — a snapshot as a file tree",
		Stage:    [2]float32{980, 560},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "The browser pane of the lading snapshot browser over a seeded tree: one directory as a " +
			"sortable list with a breadcrumb, a quick filter and a selected file. The live app reads the " +
			"tree from a lading store in ClickHouse and adds the mount list, a second pane, and the " +
			"preview, info, history, diff, find, du and problems tabs, each a store query.",
		Init:           tallyTourInit,
		RenderStateful: tallyTourRender,
		SourceFunc:     (*App).renderGalleryPane,
	})
}

// tallyTourState is the scene: the app for its pane state and ids, and the
// tree in place of a store view.
type tallyTourState struct {
	inst *App
	fsys fstest.MapFS
}

func tallyTourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	p := &inst.panes[paneIDA]
	p.st.SetDir(tourDir)
	p.pendingSel = tourSel
	return &tallyTourState{inst: inst, fsys: tourTree()}
}

func tallyTourRender(ids *c.WidgetIdStack, state any) {
	if s, ok := state.(*tallyTourState); ok && s != nil {
		s.inst.ids = ids
		s.inst.renderGalleryPane(s.fsys)
	}
}

// renderGalleryPane is pane A drawn over a given tree rather than a store
// view: the same widget call renderPane makes, minus the store, the probe
// and the width resolver, none of which the gallery host supplies.
func (inst *App) renderGalleryPane(fsys fstest.MapFS) {
	p := &inst.panes[paneIDA]
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:       inst.ids,
		ScopeKey:  "pane-" + paneIDA.String(),
		FS:        fsys,
		RootLabel: "tour @ latest",
		CacheKey:  "tour",
		State:     &p.st,
		Mode:      p.mode,
		Striped:   true,
		MaxHeight: tourPaneH,
	})
	inst.applyPendingSelection(p, res)
}
