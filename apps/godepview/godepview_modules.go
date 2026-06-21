package godepview

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	egcolor "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// This file is the third-party lens: the master pane's modules mode. It rolls
// the external packages up by owning module (godep.ModuleStats) and tables them
// by fan-in, direct-vs-transitive, and blast radius — the dependency-surface
// questions the per-package table cannot answer. Selecting a module shows its
// first-party importers and blast set in the detail pane.

// ensureModules builds the per-module rollup once (it is collection-fixed) and
// rebuilds the filtered/sorted display order when the filter or sort changes.
func (inst *App) ensureModules() {
	if !inst.modulesReady {
		inst.modulesReady = true
		if inst.idx != nil {
			inst.modules = inst.idx.ModuleStats(inst.man.Run.RootModulePath)
		}
		inst.modViewDirty = true
	}
	if inst.modViewDirty {
		inst.rebuildModView()
	}
}

// rebuildModView refilters (by the shared filter box, matched against the module
// path) and resorts the module display order.
func (inst *App) rebuildModView() {
	inst.modView = inst.modView[:0]
	needle := strings.ToLower(strings.TrimSpace(inst.filter))
	for i := range inst.modules {
		if needle != "" && !strings.Contains(strings.ToLower(inst.modules[i].ModulePath), needle) {
			continue
		}
		inst.modView = append(inst.modView, i)
	}
	inst.sortModView()
	inst.modViewDirty = false
}

func (inst *App) sortModView() {
	md := inst.modules
	less := func(a, b int) bool {
		ma, mb := &md[a], &md[b]
		switch inst.modSortCol {
		case mcolPkgs:
			return ma.NumPkgs < mb.NumPkgs
		case mcolDirect:
			if ma.Direct != mb.Direct {
				return !ma.Direct && mb.Direct // transitive < direct
			}
			return ma.ModulePath < mb.ModulePath
		case mcolFanIn:
			return ma.FanIn < mb.FanIn
		case mcolBlast:
			return ma.BlastRadius < mb.BlastRadius
		default:
			return ma.ModulePath < mb.ModulePath
		}
	}
	sort.SliceStable(inst.modView, func(i, j int) bool {
		if inst.modSortDesc {
			return less(inst.modView[j], inst.modView[i])
		}
		return less(inst.modView[i], inst.modView[j])
	})
}

func (inst *App) toggleModSort(col int) {
	if inst.modSortCol == col {
		inst.modSortDesc = !inst.modSortDesc
	} else {
		inst.modSortCol = col
		// Numeric columns default to descending (biggest first); text ascending.
		inst.modSortDesc = col == mcolPkgs || col == mcolFanIn || col == mcolBlast
	}
	inst.modViewDirty = true
}

// renderModules draws the external-module rollup table (the modules master
// view). Columns: module path, package count, direct/transitive use,
// first-party fan-in, blast radius. Selecting a row focuses the module.
func (inst *App) renderModules() {
	inst.ensureModules()
	if len(inst.modules) == 0 {
		c.Label("No external modules in this collection.").Send()
		return
	}

	type colDef struct {
		title string
		w     float32
	}
	var cols [numModCols]colDef
	cols[mcolModule] = colDef{"Module", 340}
	cols[mcolPkgs] = colDef{"Pkgs", 56}
	cols[mcolDirect] = colDef{"Use", 92}
	cols[mcolFanIn] = colDef{"Fan-in", 70}
	cols[mcolBlast] = colDef{"Blast", 70}

	for i := range numModCols {
		c.EtColumn(cols[i].w).Resizable(true).Send()
	}

	et := c.EndETable(inst.ids.PrepareStr("mod-tbl"), uint64(len(inst.modView)), 20.0, 1, 0)

	for ci := range numModCols {
		title := cols[ci].title
		if inst.modSortCol == ci {
			if inst.modSortDesc {
				title += " ▼"
			} else {
				title += " ▲"
			}
		}
		for range et.Headers(0, uint32(ci)) {
			if c.Button(inst.ids.PrepareSeq(modHdrSeqBase+uint64(ci)), c.Atoms().Text(title).Keep()).
				Frame(false).
				SendResp().HasPrimaryClicked() {
				inst.toggleModSort(ci)
			}
		}
	}

	for row, mi := range inst.modView {
		m := &inst.modules[mi]
		for range et.Cells(uint64(row), mcolModule) {
			if c.SelectableLabel(inst.ids.PrepareSeq(modRowSeqBase+uint64(row)), m.ModulePath == inst.focusModule, m.ModulePath).
				SendResp().HasPrimaryClicked() {
				inst.focusModule = m.ModulePath
			}
		}
		for range et.Cells(uint64(row), mcolPkgs) {
			c.Label(fmt.Sprintf("%d", m.NumPkgs)).Send()
		}
		for range et.Cells(uint64(row), mcolDirect) {
			renderDirectCell(m.Direct)
		}
		for range et.Cells(uint64(row), mcolFanIn) {
			c.Label(fmt.Sprintf("%d", m.FanIn)).Send()
		}
		for range et.Cells(uint64(row), mcolBlast) {
			c.Label(fmt.Sprintf("%d", m.BlastRadius)).Send()
		}
	}

	if inst.focusModule != "" {
		for row, mi := range inst.modView {
			if inst.modules[mi].ModulePath == inst.focusModule {
				et = et.SelectedRow(uint64(row))
				break
			}
		}
	}
	et.Striped(true).Send()
}

// renderDirectCell tints the use column: success tone for a direct first-party
// dependency, weak text for a transitively-pulled one.
func renderDirectCell(direct bool) {
	if direct {
		for range c.RichTextLabelColored(egcolor.Hex(styletokens.SuccessDefault.AsHex()), egcolor.Transparent, "direct") {
		}
		return
	}
	for rt := range c.RichTextLabel("transitive") {
		rt.Weak()
	}
}

// renderModuleDetail is the focused module's footprint pane: usage, counts, and
// the click-to-focus first-party importer + blast lists.
func (inst *App) renderModuleDetail() {
	var m *godep.ModuleStat
	for i := range inst.modules {
		if inst.modules[i].ModulePath == inst.focusModule {
			m = &inst.modules[i]
			break
		}
	}
	if m == nil {
		for rt := range c.RichTextLabel("Select a module in the table to inspect its footprint.") {
			rt.Weak()
		}
		return
	}

	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(m.ModulePath) {
			rt.Strong().Size(14)
		}
		c.AddSpace(inst.spaceInner())
		if c.Button(inst.ids.PrepareStr("mod-clear"), c.Atoms().Text("clear").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.focusModule = ""
		}
	}
	usage := "transitive — no first-party package imports it directly"
	if m.Direct {
		usage = "direct — imported by first-party packages"
	}
	c.Label("usage: " + usage).Send()
	c.Label(fmt.Sprintf("packages: %d   ·   fan-in: %d   ·   blast: %d first-party packages",
		m.NumPkgs, m.FanIn, m.BlastRadius)).Send()
	c.AddSpace(inst.spaceTight())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())

	importers := inst.idx.ModuleImporters(m.PkgIDs)
	inst.renderModulePkgList("mimp", fmt.Sprintf("Direct first-party importers (%d)", len(importers)), importers)
	c.AddSpace(inst.spaceInner())
	blast := inst.idx.ReverseReachInternal(m.PkgIDs)
	inst.renderModulePkgList("mblast", fmt.Sprintf("Blast radius — transitively affected (%d)", len(blast)), blast)
}

// renderModulePkgList renders one path-sorted, click-to-focus package list for
// the module detail; a click focuses the package and returns to the package view.
func (inst *App) renderModulePkgList(idkey string, title string, ids []uint64) {
	for rt := range c.RichTextLabel(title) {
		rt.Strong()
	}
	if len(ids) == 0 {
		for rt := range c.RichTextLabel("— none") {
			rt.Weak()
		}
		return
	}
	sorted := inst.sortedByPath(ids)
	shown := min(len(sorted), maxDetailListRows)
	for i := range shown {
		id := sorted[i]
		if c.SelectableLabel(inst.ids.PrepareStr(idkey+":"+strconv.FormatUint(id, 10)), id == inst.focus, inst.pathOf(id)).
			SendResp().HasPrimaryClicked() {
			inst.focus = id
			inst.mode = viewPackages // jump to the package's neighborhood
		}
	}
	if len(sorted) > shown {
		for rt := range c.RichTextLabel(fmt.Sprintf("… +%d more (browse in the table)", len(sorted)-shown)) {
			rt.Weak().Small()
		}
	}
}
