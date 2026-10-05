package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
)

// filepickerDemoState is the per-app-instance state for the
// filepicker demo. Each open gallery window owns its own four
// filepicker.Dialog values so per-dialog UI state (visibility, cursor,
// scroll) does not bleed across windows, and the four widget ids
// derive from the host-supplied WidgetIdStack so they don't collide
// with ids from another open app's filepicker demo.
type filepickerDemoState struct {
	open       *filepicker.Dialog
	multi      *filepicker.Dialog
	save       *filepicker.Dialog
	folder     *filepicker.Dialog
	lastAction filepicker.ActionE
	lastPaths  []string
}

func init() {
	registry.Register(registry.Demo{
		Name:     "filepicker",
		Category: "Inputs & pickers",
		Title:    icons.IconFolderOpen + " filepicker",
		Stage:    [2]float32{960, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "In-app open / save / pick-folder dialog rendered as an " +
			"egui::Window around the fsbrowser widget. The tree is any io/fs.FS " +
			"(default os.DirFS; swap the backend for sandboxed/remote sources). " +
			"Click one of the four triggers to drive the picker — double-click " +
			"enters a directory or opens a file; in multi-select, ctrl-click " +
			"adds and shift-click extends.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = &filepickerDemoState{
				open: filepicker.New(ids, "demo-open", filepicker.Options{
					Mode: filepicker.ModeOpen, Extensions: []string{".go", ".md", ".txt"}, StartAtOsHome: true}),
				multi: filepicker.New(ids, "demo-multi", filepicker.Options{
					Mode: filepicker.ModeOpen, Extensions: []string{".go", ".md", ".txt"}, MultiSelect: true, StartAtOsHome: true}),
				save: filepicker.New(ids, "demo-save", filepicker.Options{
					Mode: filepicker.ModeSave, DefaultFilename: "untitled.txt", StartAtOsHome: true}),
				folder: filepicker.New(ids, "demo-folder", filepicker.Options{
					Mode: filepicker.ModePickFolder, StartAtOsHome: true}),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFilepicker(ids, state.(*filepickerDemoState))
		},
		SourceFunc: demoFilepicker,
	})
}

// demoFilepicker is the demo carousel body. Four trigger buttons feed
// the open / multi-open / save / pick-folder pickers; below them, the
// most-recent action+path-set is echoed so both the screenshot tour
// and the interactive driver show what came back from the dialog.
func demoFilepicker(ids *c.WidgetIdStack, st *filepickerDemoState) {
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("fp-open-trigger"),
			c.Atoms().Text("Open file…").Keep()).
			SendResp().HasPrimaryClicked() {
			st.open.Show()
		}
		if c.Button(ids.PrepareStr("fp-multi-trigger"),
			c.Atoms().Text("Open many…").Keep()).
			SendResp().HasPrimaryClicked() {
			st.multi.Show()
		}
		if c.Button(ids.PrepareStr("fp-save-trigger"),
			c.Atoms().Text("Save file as…").Keep()).
			SendResp().HasPrimaryClicked() {
			st.save.Show()
		}
		if c.Button(ids.PrepareStr("fp-folder-trigger"),
			c.Atoms().Text("Pick folder…").Keep()).
			SendResp().HasPrimaryClicked() {
			st.folder.Show()
		}
	}

	for _, pp := range []*filepicker.Dialog{st.open, st.multi, st.save, st.folder} {
		if ev := pp.Render(); ev.Action != filepicker.ActionNone {
			st.lastAction, st.lastPaths = ev.Action, ev.Paths
		}
	}

	c.Label(fmt.Sprintf("last: action=%s n=%d", st.lastAction, len(st.lastPaths))).Send()
	for _, p := range st.lastPaths {
		c.Label("  " + p).Send()
	}
}
