package widgets

import (
	"fmt"
	"strings"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "fsbrowser",
		Category: "Layout & widgets",
		Title:    icons.PhFolderOpen + " file browser",
		Stage:    [2]float32{1024, 720},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/fsbrowser over an in-memory io/fs tree (ADR-0200 §SD2): one directory as a sortable list, " +
			"or the tree below it as an outline, with a breadcrumb, a quick filter, a per-directory selection and a " +
			"keyboard cursor. Double-click or Enter enters a directory and reports a file; Backspace goes up; " +
			"ctrl-click adds, shift-click extends. The host adds a column of its own beside name, size and modified.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newFsbrowserDemoState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFsbrowser(ids, state.(*fsbrowserDemoState))
		},
		SourceFunc: demoFsbrowser,
	})
}

type fsbrowserDemoState struct {
	st         fsbrowser.State
	fsys       fstest.MapFS
	mode       fsbrowser.ModeE
	showHidden bool
	lastAction string
}

func newFsbrowserDemoState() *fsbrowserDemoState {
	return &fsbrowserDemoState{fsys: fsbrowserFixture(), lastAction: "(nothing yet)"}
}

// fsbrowserFixture is a small project tree with every shape the widget
// distinguishes: nested directories, an empty one, dot-files, sizes that sort
// differently from names, and modification times a week apart.
func fsbrowserFixture() fstest.MapFS {
	t0 := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	blob := func(n int) []byte { return make([]byte, n) }
	return fstest.MapFS{
		"README.md":                          {Data: []byte("# demo\n\nA tree for the browser.\n"), ModTime: at(1)},
		"LICENSE":                            {Data: blob(1_071), ModTime: at(0)},
		"go.mod":                             {Data: blob(212), ModTime: at(3)},
		".gitignore":                         {Data: []byte("bin/\n"), ModTime: at(0)},
		"cmd/app/main.go":                    {Data: blob(1_840), ModTime: at(160)},
		"cmd/app/main_test.go":               {Data: blob(920), ModTime: at(161)},
		"internal/store/store.go":            {Data: blob(14_302), ModTime: at(120)},
		"internal/store/store_test.go":       {Data: blob(6_110), ModTime: at(121)},
		"internal/store/testdata/small.json": {Data: []byte("{}"), ModTime: at(50)},
		"internal/store/testdata/large.csv":  {Data: blob(2_621_440), ModTime: at(51)},
		"internal/util/paths.go":             {Data: blob(3_003), ModTime: at(90)},
		"doc/design.md":                      {Data: blob(22_000), ModTime: at(100)},
		"doc/img/arch.png":                   {Data: blob(118_000), ModTime: at(101)},
		"doc/img/flow.svg":                   {Data: blob(9_400), ModTime: at(102)},
		"scripts/build.sh":                   {Data: blob(640), ModTime: at(10)},
		"scripts/release.sh":                 {Data: blob(1_290), ModTime: at(11)},
		"assets/fonts/.keep":                 {Data: nil, ModTime: at(0)},
		"assets/audio/ping.wav":              {Data: blob(48_044), ModTime: at(70)},
		"assets/video/intro.mp4":             {Data: blob(7_340_032), ModTime: at(71)},
		"vendor/archive.tar.gz":              {Data: blob(33_554_432), ModTime: at(5)},
		"vendor/notes.txt":                   {Data: blob(88), ModTime: at(6)},
	}
}

func demoFsbrowser(ids *c.WidgetIdStack, st *fsbrowserDemoState) {
	stdSection("a file browser over io/fs",
		"list or outline, breadcrumb, quick filter, selection, keyboard — the host owns the State and supplies the tree")
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("fb-list"), c.Atoms().Text(icons.PhListBullets+" list").Keep()).
			Selected(st.mode == fsbrowser.ModeList).SendResp().HasPrimaryClicked() {
			st.mode = fsbrowser.ModeList
			st.lastAction = "mode list"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fb-outline"), c.Atoms().Text(icons.PhTreeStructure+" outline").Keep()).
			Selected(st.mode == fsbrowser.ModeOutline).SendResp().HasPrimaryClicked() {
			st.mode = fsbrowser.ModeOutline
			st.lastAction = "mode outline"
		}
		c.AddSpace(gapInline())
		c.Checkbox(ids.PrepareStr("fb-hidden"), st.showHidden, "show hidden").SendRespVal(&st.showHidden)
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fb-root"), c.Atoms().Text("go to root").Keep()).SendResp().HasPrimaryClicked() {
			st.st.SetDir(".")
			st.lastAction = "go to root"
		}
		c.AddSpace(gapInline())
		if c.Button(ids.PrepareStr("fb-clear"), c.Atoms().Text("clear selection").Keep()).SendResp().HasPrimaryClicked() {
			st.st.ClearSelection()
			st.lastAction = "clear selection"
		}
	}
	c.AddSpace(padInner())

	res := fsbrowser.Render(fsbrowser.Input{
		Ids:        ids,
		ScopeKey:   "demo-fs",
		FS:         st.fsys,
		RootLabel:  "demo",
		CacheKey:   "demo-fixture",
		State:      &st.st,
		Mode:       st.mode,
		ShowHidden: st.showHidden,
		MaxHeight:  360,
		Striped:    true,
		Columns: []fsbrowser.Column{{
			Header: "kind",
			Width:  110,
			Cell:   fsbrowserKindCell,
		}},
	})
	if res.Err != nil {
		st.lastAction = "error: " + res.Err.Error()
	}
	if res.Navigated {
		st.lastAction = "entered " + st.st.Dir()
	}
	if res.Activated >= 0 && res.Activated < len(res.Rows) {
		st.lastAction = "activated " + res.Rows[res.Activated].Path
	}
	if res.Clicked >= 0 && res.Clicked < len(res.Rows) && !res.Navigated {
		st.lastAction = "clicked " + res.Rows[res.Clicked].Path
	}
	c.AddSpace(padInner())
	stdSection("readout", "what the host learns from Result and State — the lines a headless scene asserts on")
	c.Label("dir: " + st.st.Dir()).Send()
	sel := st.st.Selection()
	text := "(nothing)"
	if len(sel) > 0 {
		text = strings.Join(sel, ", ")
	}
	c.Label("selected: " + text).Send()
	c.Label("last action: " + st.lastAction).Send()
	c.Label(fmt.Sprintf("%d rows shown", len(res.Rows))).Send()
}

// fsbrowserKindCell is the host column: a word for the entry's kind, the way a
// lading host would put a content policy or a text guarantee here.
func fsbrowserKindCell(e fsbrowser.Entry) {
	kind := "file"
	switch {
	case e.IsSymlink:
		kind = "link"
	case e.IsDir:
		kind = "directory"
	case e.Size == 0:
		kind = "empty"
	case e.Size >= 1<<20:
		kind = "large"
	}
	c.Label(kind).Selectable(false).Truncate().Send()
}
