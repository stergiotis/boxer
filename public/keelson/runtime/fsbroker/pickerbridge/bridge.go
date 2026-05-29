//go:build llm_generated_opus47

// Package pickerbridge connects the fs Powerbox service to the egui
// filepicker widget. Hosts call Bridge.Render(ids) once per frame from
// the rendering loop. The bridge pulls pending fs.dialog requests off
// the service, drives the picker, and completes each dialog via
// Service.Resolve (user picked) or Service.Cancel (user pressed cancel).
//
// ADR-0026 §SD7 M2.6b — the picker IS the cap-broker UI for fs.* grants.
// Wiring: any host that drives a rendering loop calls Render every frame.
// The launcher (widgets package) is the natural integration point; the
// M3 dock host will pull the call into a per-tile overlay.
//
// Single-request semantics: while a dialog is in flight the bridge does
// not start another. Additional pending requests stay queued in the
// service and are processed in subsequent dialogs once the current one
// resolves.
package pickerbridge

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	boxerenv "github.com/stergiotis/boxer/public/config/env"

	"github.com/rs/zerolog"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

// Config configures Bridge construction.
type Config struct {
	// FsRoot is the absolute filesystem path the picker is rooted at.
	// Defaults to "/" if zero. The picker can show only paths under this
	// root; the bridge translates io/fs-style picker paths back to
	// absolute paths before calling Service.Resolve.
	FsRoot string
	// StartDir is the io/fs-style initial cwd within FsRoot. Defaults
	// to $HOME (mapped relative to FsRoot) or "." if HOME is unset or
	// outside FsRoot.
	StartDir string
	// TitleOverride replaces the per-op default title ("Open file" /
	// "Save as" / "Pick folder"). Empty leaves the default.
	TitleOverride string
}

// Bridge couples the fsbroker.Service queue with a filepicker.Inst.
type Bridge struct {
	svc  *fsbroker.Service
	log  zerolog.Logger
	cfg  Config
	fsys fs.FS

	picker *filepicker.Inst
	curReq *fsbroker.PendingRequest
}

// NewBridge constructs a Bridge with the given config. Idle until Render
// is called every frame.
func NewBridge(svc *fsbroker.Service, log zerolog.Logger, cfg Config) (b *Bridge) {
	if cfg.FsRoot == "" {
		cfg.FsRoot = "/"
	}
	fsys := os.DirFS(cfg.FsRoot)
	if cfg.StartDir == "" {
		cfg.StartDir = defaultStartDir(cfg.FsRoot)
	}
	b = &Bridge{
		svc:  svc,
		log:  log.With().Str("component", "fs.pickerbridge").Logger(),
		cfg:  cfg,
		fsys: fsys,
	}
	return
}

// Render is the per-frame entry point. While a dialog is in flight the
// bridge drives the picker; otherwise it polls the service for new
// pending requests and starts one.
func (inst *Bridge) Render(ids *c.WidgetIdStack) {
	if inst.curReq == nil {
		pending := inst.svc.Pending()
		if len(pending) == 0 {
			return
		}
		next := pending[0]
		inst.startPicker(&next)
	}
	if inst.picker == nil {
		return
	}
	action, picked := inst.picker.Render(ids)
	switch action {
	case filepicker.ActionOpen, filepicker.ActionSave, filepicker.ActionPickFolder:
		if len(picked) == 0 {
			// Defensive: a committed action must carry at least one
			// path. Treat the empty case as a cancel so the queued
			// request doesn't hang.
			_ = inst.svc.Cancel(inst.curReq.Id)
			inst.clearCurrent()
			return
		}
		abs := inst.toAbsolute(picked[0])
		_, err := inst.svc.Resolve(inst.curReq.Id, abs)
		if err != nil {
			inst.log.Err(err).Str("reqId", inst.curReq.Id).Msg("pickerbridge: resolve")
		}
		inst.clearCurrent()
	case filepicker.ActionCancel:
		err := inst.svc.Cancel(inst.curReq.Id)
		if err != nil {
			inst.log.Err(err).Str("reqId", inst.curReq.Id).Msg("pickerbridge: cancel")
		}
		inst.clearCurrent()
	}
}

// CurrentRequestId returns the id of the dialog the bridge is currently
// displaying, or "" if idle. Useful for host telemetry and for tests.
func (inst *Bridge) CurrentRequestId() (id string) {
	if inst.curReq != nil {
		id = inst.curReq.Id
	}
	return
}

func (inst *Bridge) startPicker(req *fsbroker.PendingRequest) {
	mode, title := pickerOptionsFor(req.Op, inst.cfg.TitleOverride)
	idStr := "fs-picker-" + req.Id
	inst.picker = filepicker.New(idStr, mode,
		filepicker.WithFsBackend(inst.fsys),
		filepicker.WithStartDir(inst.cfg.StartDir),
		filepicker.WithTitle(title),
	)
	inst.picker.Show()
	inst.curReq = req
}

// pickerOptionsFor maps a fs.dialog.{op} subject to the filepicker
// mode + title that startPicker constructs the widget with. Extracted
// so the dispatch logic is testable without spinning up a real egui
// render context. titleOverride (when non-empty) wins over the
// op-derived default; the mode is never overridden — write maps to
// ModeSave, the folder-oriented ops (bundle, watch) map to
// ModePickFolder, and everything else (read and unknown subjects)
// stays on ModeOpen.
func pickerOptionsFor(op string, titleOverride string) (mode filepicker.ModeE, title string) {
	mode = filepicker.ModeOpen
	title = "Open file"
	switch op {
	case "write":
		mode = filepicker.ModeSave
		title = "Save as"
	case "bundle":
		mode = filepicker.ModePickFolder
		title = "Pick folder"
	case "watch":
		mode = filepicker.ModePickFolder
		title = "Pick folder to watch"
	}
	if titleOverride != "" {
		title = titleOverride
	}
	return
}

func (inst *Bridge) clearCurrent() {
	inst.curReq = nil
	inst.picker = nil
}

// toAbsolute converts a picker-emitted io/fs path back to the absolute
// path the fsbroker.Service expects to open.
func (inst *Bridge) toAbsolute(ioFsPath string) (abs string) {
	if filepath.IsAbs(ioFsPath) {
		abs = ioFsPath
		return
	}
	abs = filepath.Join(inst.cfg.FsRoot, ioFsPath)
	return
}

func defaultStartDir(fsRoot string) (dir string) {
	home := boxerenv.Home.Get()
	if home == "" {
		dir = "."
		return
	}
	rel, err := filepath.Rel(fsRoot, home)
	if err != nil || strings.HasPrefix(rel, "..") {
		dir = "."
		return
	}
	dir = filepath.ToSlash(rel)
	return
}
