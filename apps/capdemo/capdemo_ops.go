package capdemo

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0269, keelson-app A1): the window's state,
// read-only. Every verb here is a dialog the broker puts to the person — a
// file pick, a folder pick, a copy to their clipboard — or a write to the
// window's own persisted key the person types; none is an agent's to ask
// for, so the catalog has no commands and the sections are one resource
// an agent reads.

const (
	resSections = "sections"

	opGetView = "get_view"
)

// sectionsValue is the resource's value: what each section holds,
// comparable so the host can tell a change.
type sectionsValue struct {
	FileHandle    string
	PickOpen      bool
	ScratchpadLen int
	PersistStatus string
	WatchHandle   string
	WatchActive   bool
	WatchEvents   int
	ClipboardNote string
}

// capSnap is what the query reads: a copy taken under the mutex, with the
// scratchpad's length from the render goroutine's field.
type capSnap struct {
	file      FileSection
	persist   PersistSection
	watch     WatchSection
	clipboard ClipboardSection
	bus       bool
	storage   bool
}

// FileSection is the fs.dialog.read section as get_view reports it.
type FileSection struct {
	Handle       string `desc:"the granted handle's subject prefix; empty while none is held"`
	PickOpen     bool   `desc:"true while the picker is open and the person has not answered"`
	PreviewBytes int    `desc:"bytes of the picked file the section shows, at most 1024"`
	TotalBytes   int    `desc:"bytes the picked file held"`
	Status       string `desc:"the last error, empty when none"`
}

// PersistSection is the runtime.persist section.
type PersistSection struct {
	ScratchpadLength int    `desc:"characters in the scratchpad text box"`
	Status           string `desc:"the last verb's note: saved, loaded, absent, deleted, or the error"`
}

// WatchSection is the fs.dialog.watch section.
type WatchSection struct {
	Handle    string `desc:"the granted folder handle's subject prefix; empty while none is held"`
	Backend   string `desc:"the backend the broker chose for the watch"`
	Active    bool   `desc:"true while events are streaming"`
	Events    int    `desc:"events received since the watch started, at most 50 kept"`
	Poller    bool   `desc:"the Force poller backend check box"`
	Recursive bool   `desc:"the Recursive check box"`
	Status    string `desc:"the last error, empty when none"`
}

// ClipboardSection is the clipboard.write section.
type ClipboardSection struct {
	Status string `desc:"the broker's answer to the last copy, empty before one"`
}

// View is get_view's result: the four sections, and whether the window
// has a bus and storage to reach the brokers through.
type View struct {
	File      FileSection      `desc:"fs.dialog.read"`
	Persist   PersistSection   `desc:"runtime.persist.scratchpad"`
	Watch     WatchSection     `desc:"fs.dialog.watch"`
	Clipboard ClipboardSection `desc:"clipboard.write"`
	Bus       bool             `desc:"true when the window has a bus; without one every verb shows the no-bus line"`
	Storage   bool             `desc:"true when the window has storage for the persist section"`
}

func (inst *App) opsSnapshot() (sn capSnap) {
	sn.persist.ScratchpadLength = len(inst.scratchpad)
	sn.bus, sn.storage = inst.bus != nil, inst.storage != nil
	inst.mu.Lock()
	defer inst.mu.Unlock()
	sn.file = FileSection{Handle: inst.lastHandlePrefix, PickOpen: inst.pickInFlight, PreviewBytes: len(inst.previewBytes), TotalBytes: inst.previewTotal, Status: inst.fileErr}
	sn.persist.Status = inst.persistStatus
	sn.watch = WatchSection{Handle: inst.watchHandlePrefix, Backend: inst.watchBackend, Active: inst.watchActive, Events: len(inst.watchEvents),
		Poller: inst.watchUsePoller, Recursive: inst.watchRecursive, Status: inst.watchErr}
	sn.clipboard = ClipboardSection{Status: inst.clipboardNote}
	return
}

func (inst *App) sectionsValue() (v sectionsValue) {
	v.ScratchpadLen = len(inst.scratchpad)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	v.FileHandle, v.PickOpen = inst.lastHandlePrefix, inst.pickInFlight
	v.PersistStatus = inst.persistStatus
	v.WatchHandle, v.WatchActive, v.WatchEvents = inst.watchHandlePrefix, inst.watchActive, len(inst.watchEvents)
	v.ClipboardNote = inst.clipboardNote
	return
}

var ops = func() (s *appops.Set[*App, capSnap]) {
	s = appops.NewSet((*App).opsSnapshot)
	s.Resource(resSections, "what each section holds: the handles held, the picker and the watch, the scratchpad's length, the last status of each",
		func(inst *App) any { return inst.sectionsValue() })
	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the four sections: which handle each holds, whether the picker is open and the watch active, the scratchpad's length, and the last status of each",
		Reads:   []string{resSections}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn capSnap, _ appops.None) (out View, err error) {
			out = View{File: sn.file, Persist: sn.persist, Watch: sn.watch, Clipboard: sn.clipboard, Bus: sn.bus, Storage: sn.storage}
			return
		})
	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
