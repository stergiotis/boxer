package godepview

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	lgview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
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
	colWasm
	numCols
)

// Widget-id seed bases for per-header / per-row interactive widgets. Kept
// far apart and above any plausible row count so PrepareSeq never collides.
const (
	hdrSeqBase uint64 = 0x9000_0000
	rowSeqBase uint64 = 0xA000_0000
)

// maxGraphNodes caps the focus neighborhood the graph draws. The table is the
// scalable full-closure surface; the graph stays legible by never drawing more
// than the focus node's closest maxGraphNodes neighbors (ADR-0064 SD5). Hub
// packages (fmt, errors, …) reach most of the closure, so without this cap a
// single focus + "importers" walk would emit thousands of opcodes per frame.
const maxGraphNodes = 200

// Dock tab ids — reserved high so they never collide with the row/header seqs.
const (
	tabPackages uint64 = 1 << 60
	tabGraph    uint64 = 1<<60 | 1
	tabDetail   uint64 = 1<<60 | 2
)

// dockMinHeight bounds the dock inside a scrolling host (the widget gallery);
// the live windowed app gets its height from the window. A bounded leaf is also
// what lets each pane's ScrollArea scroll (schemaview's idiom). Sized so the
// even-split detail leaf shows the focus metadata plus a few import/importer
// rows before scrolling, while still fitting the app's 760px preferred window.
const dockMinHeight = 620

// instanceCounter stamps each App with a unique seed so the layered graph's
// absolute canvas id (which does not route through the per-window id stack)
// stays disjoint across multiple open explorer windows (capinspector's idiom).
var instanceCounter atomic.Uint64

// neighborhoodSig is the cache key for the focus neighborhood: any change
// invalidates graphReached and the cached layered layout.
type neighborhoodSig struct {
	focus   uint64
	depth   int
	dir     godep.Direction
	hideStd bool
}

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

	graphHideStd bool   // drop stdlib from the neighborhood graph (legibility default)
	useLayered   bool   // graph engine: false = live (egui_graphs), true = layered (dot)
	seed         uint64 // per-instance id salt for the layered canvas

	// Neighborhood cache — recomputed only when graphSig changes, so the BFS
	// (and the layered engine's dot layout, keyed off the same signature) runs
	// once per change rather than once per frame.
	graphSig       neighborhoodSig
	graphReached   map[uint64]int
	graphTruncated int

	// Layered-engine state: the dot layout cached against graphSig, the last
	// layout error, and the persistent pan/zoom view (reset on neighborhood change).
	layeredLayout *layeredgraph.Layout
	layeredErr    error
	layeredView   lgview.ViewState

	// Detail-pane cache — the focused package's import / importer lists sorted
	// by path. Depends only on focus, so it is keyed separately from graphSig.
	detailReady     bool
	detailFocus     uint64
	detailImports   []uint64
	detailImporters []uint64
}

var _ app.AppI = (*App)(nil)

func newApp(src godep.SourceI) (inst *App) {
	inst = &App{
		ids:          c.NewWidgetIdStack(),
		density:      styletokens.DensityFromEnv(),
		src:          src,
		showStd:      true,
		showInt:      true,
		showExt:      true,
		sortCol:      colImportedBy, // most-depended-upon first is a useful default
		sortDesc:     true,
		depth:        2,
		dir:          godep.DirImports,
		graphHideStd: true, // stdlib hubs flood the graph; the table still lists them
		seed:         instanceCounter.Add(1),
		viewDirty:    true,
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

// intDepth rounds the depth slider to the integer hop count the walk uses.
func (inst *App) intDepth() (d int) { d = max(int(inst.depth+0.5), 1); return }

// neighborhoodSignature is the current cache key for the focus neighborhood.
func (inst *App) neighborhoodSignature() (sig neighborhoodSig) {
	sig = neighborhoodSig{focus: inst.focus, depth: inst.intDepth(), dir: inst.dir, hideStd: inst.graphHideStd}
	return
}

// ensureNeighborhood recomputes the bounded focus neighborhood only when its
// signature changes. Both graph engines read inst.graphReached, and the layered
// engine caches its dot layout against the same signature, so a stable
// focus/depth/dir/hideStd costs neither a BFS nor a re-layout per frame.
func (inst *App) ensureNeighborhood() {
	sig := inst.neighborhoodSignature()
	if sig == inst.graphSig {
		return
	}
	inst.graphSig = sig
	// Invalidate the cached dot layout and re-fit the layered view.
	inst.layeredLayout = nil
	inst.layeredErr = nil
	inst.layeredView = lgview.ViewState{}
	if inst.focus == 0 || inst.idx == nil {
		inst.graphReached = nil
		inst.graphTruncated = 0
		return
	}
	include := func(p *godep.PackageNode) (ok bool) {
		ok = !(inst.graphHideStd && p.Class == godep.ClassStdlib)
		return
	}
	inst.graphReached, inst.graphTruncated = inst.idx.BoundedNeighborhood(inst.focus, godep.NeighborhoodOpts{
		MaxDepth: sig.depth,
		Dir:      inst.dir,
		MaxNodes: maxGraphNodes,
		Include:  include,
	})
}

// space helpers — IDS density tokens (mirrors imztop).
func (inst *App) spaceTight() (px float32) { px = styletokens.PaddingTight(inst.density); return }
func (inst *App) spaceInner() (px float32) { px = styletokens.PaddingInner(inst.density); return }
func (inst *App) spaceOuter() (px float32) { px = styletokens.PaddingOuter(inst.density); return }
