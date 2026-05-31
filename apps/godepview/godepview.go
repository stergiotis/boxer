package godepview

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Table column indices — also the etable column order.
const (
	colImportPath = iota
	colName
	colClass
	colModule
	colFiles
	colImports
	colImportedBy
	numCols
)

// Widget-id seed bases for per-header / per-row interactive widgets. Kept
// far apart and above any plausible row count so PrepareSeq never collides.
const (
	hdrSeqBase uint64 = 0x9000_0000
	rowSeqBase uint64 = 0xA000_0000
)

// App is the per-window godepview instance (ADR-0064). It depends only on
// the godep manifest and the godep.SourceI port; the concrete collector is
// injected by the registry ctor (app_register.go), keeping the render path
// free of the go toolchain.
type App struct {
	ids     *c.WidgetIdStack
	density styletokens.DensityE
	log     zerolog.Logger

	// src is the injected data source (a LiveCollector today). The render
	// path uses only this interface — never a concrete collector.
	src godep.SourceI

	// Async collection state. load() fills man/idx/loadErr then publishes
	// via done (atomic release); Frame reads them only after observing
	// done (atomic acquire), so the post-load read needs no mutex.
	done    atomic.Bool
	man     godep.Manifest
	idx     *godep.Index
	loadErr error
	cancel  context.CancelFunc

	// Master list (etable) state.
	filter    string // persistent TextEdit draft (see imztop proc-filter rationale)
	showStd   bool
	showInt   bool
	showExt   bool
	sortCol   int
	sortDesc  bool
	view      []int // display order: indices into man.Packages
	viewDirty bool

	// Detail (graph) state.
	focus uint64 // focused package id; 0 = none
	depth float64
	dir   godep.Direction
}

var _ app.AppI = (*App)(nil)

func newApp(src godep.SourceI) (inst *App) {
	inst = &App{
		ids:       c.NewWidgetIdStack(),
		density:   styletokens.DensityFromEnv(),
		src:       src,
		showStd:   true,
		showInt:   true,
		showExt:   true,
		sortCol:   colImportedBy, // most-depended-upon first is a useful default
		sortDesc:  true,
		depth:     2,
		dir:       godep.DirImports,
		viewDirty: true,
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(mctx app.MountContextI) (err error) {
	inst.ids = mctx.Ids()
	inst.log = mctx.Log()

	ctx, cancel := context.WithCancel(context.Background())
	inst.cancel = cancel
	// Cancel the collection if the host releases the app mid-load.
	go func() {
		select {
		case <-mctx.Cancel():
			cancel()
		case <-ctx.Done():
		}
	}()
	go inst.load(ctx)
	return
}

func (inst *App) Unmount(mctx app.MountContextI) (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	return
}

// load runs the (potentially multi-second) collection off the render
// thread. Its writes to man/idx/loadErr are published to Frame via the
// done atomic.
func (inst *App) load(ctx context.Context) {
	m, err := inst.src.Load(ctx)
	if err != nil {
		inst.loadErr = err
		inst.done.Store(true)
		inst.log.Warn().Err(err).Msg("godepview: collection failed")
		return
	}
	inst.man = m
	inst.idx = inst.man.BuildIndex()
	inst.done.Store(true)
	inst.log.Info().
		Int("packages", len(inst.man.Packages)).
		Uint32("edges", inst.man.Run.NumEdges).
		Msg("godepview: collected dependency graph")
}

func (inst *App) Frame(fctx app.FrameContextI) (err error) {
	if !inst.done.Load() {
		inst.renderLoading()
		return
	}
	if inst.loadErr != nil {
		inst.renderError(inst.loadErr)
		return
	}
	inst.renderExplorer()
	return
}

// space helpers — IDS density tokens (mirrors imztop).
func (inst *App) spaceTight() (px float32) { px = styletokens.PaddingTight(inst.density); return }
func (inst *App) spaceInner() (px float32) { px = styletokens.PaddingInner(inst.density); return }
func (inst *App) spaceOuter() (px float32) { px = styletokens.PaddingOuter(inst.density); return }
