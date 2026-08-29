package play

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// play_files_panel.go is the Files dock tab (ADR-0200 Update 2026-08-21): the
// active result read as a tree of paths and browsed with widgets/fsbrowser —
// the same widget apps/tally hosts over a lading snapshot, here over whatever
// the query returned. That is the ADR's O4, one widget and two hosts, and the
// reason the widget took an [io/fs.FS] rather than a listing interface: a result
// can be made one honestly (play_pathrows.go), where a second data-source seam
// would have been a second contract to keep true.
//
// The panel adds no reading of its own beyond the browse. It does not preview:
// a row carries metadata, not bytes, and play already has a pane that shows the
// row behind an entry in full — the Detail tab, which follows the same row
// cursor this panel writes. A click therefore publishes twice, and the split is
// what the tree makes honest: `selection_key` is the path and is always
// truthful, `selection` is the result row and exists only for an entry a row
// named, never for a directory the interning synthesised.

// filesDriver owns the Files tab state: the browser's caller-owned State, the
// interned file system and its build cache.
// filesPaneProbeSalt namespaces the browser's pane probe r21 slot; threading
// it through the instance's id stack makes the slot window-unique.
const filesPaneProbeSalt uint64 = 0xf11e5b0a71e70001

type filesDriver struct {
	ids *c.WidgetIdStack

	st         fsbrowser.State
	mode       fsbrowser.ModeE
	showHidden bool

	// fsys is the current result as a file system; gen names it for the
	// widget's directory cache. A snapshot cannot change under the browser
	// and neither can a result, so the cache is dropped by the generation
	// rather than invalidated.
	fsys *rowFS
	gen  uint64

	// Build cache: the schema the claim was resolved from plus the result's
	// freshness token, matching what the board and the treemap key on.
	forSchema   *arrow.Schema
	forExecuted time.Time

	// pendingExecuted, density and widths are stashed by renderFilesTab
	// before dispatch — the PanelI Render signature carries no frame.
	pendingExecuted time.Time
	density         styletokens.DensityE
	widths          *colwidth.Resolver

	// emitted is the path last published, so a cursor that lands where it
	// already was does not re-emit every frame.
	emitted string

	// paneH is the last height the Files tab reported to the browser's pane
	// probe. Held across frames because the probe answers one frame late and
	// is absent on the frame the tab comes back — see render.
	paneH float32
}

func newFilesDriver(ids *c.WidgetIdStack) (inst *filesDriver) {
	return &filesDriver{ids: ids}
}

// noteFrame hands the driver what the frame knows and the panel interface does
// not carry.
func (inst *filesDriver) noteFrame(executed time.Time, density styletokens.DensityE, widths *colwidth.Resolver) {
	inst.pendingExecuted = executed
	inst.density = density
	inst.widths = widths
}

// filesPanel is the PanelI face. Acceptance is schema-only and cheap — a
// question about column names, which the schema answers alone.
type filesPanel struct {
	driver *filesDriver
}

func (inst filesPanel) ID() PanelID { return "files" }

func (inst filesPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "paths"}}
}

func (inst filesPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = pathContractHint
		return
	}
	k, r := resolvePathRows(schema)
	if r != "" {
		reason = r
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst filesPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main, ok := filled[chMain]
	if !ok {
		return
	}
	k, isClaim := main.Claim.(pathClaim)
	if !isClaim {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// render interns the result (once per result), draws the strip and the browser,
// and publishes what the selection became.
func (inst *filesDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k pathClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	inst.renderStrip()
	// The pane's height, which bounds the browser: MaxHeight is a ceiling, so
	// a long listing fills the tab and a short one stays short. Probed after
	// the strip and before the browser, because the rect a probe reports is
	// the room left for the NEXT widget. The tab is declared NoScroll — its
	// overflow clips rather than scrolls — so a browser left at the etable's
	// 400 px auto-fit cap would strand the bottom of the pane empty with no
	// way to reach the rows past it.
	if _, h, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(filesPaneProbeSalt).Derive()); ok &&
		h > 0 && !math.IsNaN(float64(h)) {
		inst.paneH = h
	}
	res := fsbrowser.Render(fsbrowser.Input{
		Ids:      inst.ids,
		ScopeKey: "files",
		FS:       inst.fsys,
		// The root is the result, not a place on a disk: naming it "result"
		// keeps the breadcrumb from reading as an absolute path into
		// something the query never claimed to be.
		RootLabel:  "result",
		CacheKey:   strconv.FormatUint(inst.gen, 10),
		State:      &inst.st,
		Mode:       inst.mode,
		Columns:    inst.columns(rec, k),
		ShowHidden: inst.showHidden,
		Striped:    true,
		Widths:     inst.widths,
		WidthTag:   "files",
		MaxHeight:  inst.paneH,
	})
	inst.flushWidths()
	if res.Err != nil {
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
	}
	inst.publish(k, emit)
}

// rebuild interns the result into a file system, keyed on (schema, executed).
// The generation is bumped with it: the browser drops its directory cache and
// its selection on a new key, and keeps the DIRECTORY — so re-running a query
// leaves the reader where they were, which is the same property two snapshots
// of one mount give apps/tally.
func (inst *filesDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k pathClaim) {
	if inst.fsys != nil && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.forSchema, inst.forExecuted = schema, inst.pendingExecuted
	inst.fsys = buildPathFS(rec, k)
	inst.gen++
}

// renderStrip is the panel's own chrome: the two modes, hidden names, and what
// the interning made of the result.
func (inst *filesDriver) renderStrip() {
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("files-list"), c.Atoms().Text(icons.PhListBullets+" List").Keep()).
			Selected(inst.mode == fsbrowser.ModeList).SendResp().HasPrimaryClicked() {
			inst.mode = fsbrowser.ModeList
		}
		if c.Button(inst.ids.PrepareStr("files-outline"), c.Atoms().Text(icons.PhTreeStructure+" Outline").Keep()).
			Selected(inst.mode == fsbrowser.ModeOutline).SendResp().HasPrimaryClicked() {
			inst.mode = fsbrowser.ModeOutline
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		c.Checkbox(inst.ids.PrepareStr("files-hidden"), inst.showHidden, "Hidden names").SendRespVal(&inst.showHidden)
		c.AddSpace(styletokens.GapInline(inst.density) * 2)
		for rt := range c.RichTextLabel(inst.statusLine()) {
			rt.Small().Weak()
		}
	}
}

// statusLine says what was interned and what was not. A dropped or skipped row
// is reported rather than swallowed: a browser missing entries looks exactly
// like a query that returned none of them.
func (inst *filesDriver) statusLine() string {
	if inst.fsys == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s files · %s directories", humanize.Comma(inst.fsys.files), humanize.Comma(inst.fsys.dirs))
	if inst.fsys.dropped > 0 {
		fmt.Fprintf(&b, " · %s more rows not interned (the browser caps at %s — add a WHERE or a LIMIT)",
			humanize.Comma(inst.fsys.dropped), humanize.Comma(pathMaxRows))
	}
	if inst.fsys.skipped > 0 {
		fmt.Fprintf(&b, " · %s rows carried no usable path", humanize.Comma(inst.fsys.skipped))
	}
	return b.String()
}

// columns turns every column the contract did not claim into a browser column,
// in the query's own order. They are rebuilt per frame rather than cached: the
// closures capture the record batch, which is only valid for this one.
func (inst *filesDriver) columns(rec arrow.RecordBatch, k pathClaim) (cols []fsbrowser.Column) {
	if len(k.hostCols) == 0 {
		return
	}
	fsys := inst.fsys
	fields := rec.Schema().Fields()
	cols = make([]fsbrowser.Column, 0, len(k.hostCols))
	for _, ci := range k.hostCols {
		col := ci
		cols = append(cols, fsbrowser.Column{
			// The gloss declaration is not a header: `hash@gloss/hex` is a
			// rendering rule, and what the column is called is `hash`.
			Header: pathColumnLabel(fields[ci].Name),
			// The Arrow type discriminates the stored width, as it does for
			// play's own grids: a width fitted to a hash column should not
			// reach the column that replaces it in the next query.
			WidthType: fields[ci].Type.String(),
			Cell: func(e fsbrowser.Entry) {
				row := fsys.rowOf(e.Path)
				if row < 0 {
					// A synthesised directory has no row, so it has no value
					// for this column — an empty cell, not a zero.
					return
				}
				c.Label(formatCell(rec, col, row)).Selectable(false).Truncate().Send()
			},
		})
	}
	return
}

// publish writes the selection out. Both signals or neither: a path is always
// true of what was clicked, a row exists only when a row named it.
func (inst *filesDriver) publish(k pathClaim, emit SignalEmitterI) {
	sel := inst.st.Selection()
	if len(sel) != 1 {
		inst.emitted = ""
		return
	}
	p := sel[0]
	if p == inst.emitted {
		return
	}
	inst.emitted = p
	emit.Emit(signalSelectionKey, p)
	if row := inst.fsys.rowOf(p); row >= 0 && row != k.selRow {
		emit.Emit(signalSelection, row)
	}
}

// flushWidths writes what the widget observed. The widget resolves and observes
// on its own (ADR-0151); the store write is the host's, as it is for play's
// grids, and a failure stays dirty and retries next frame.
func (inst *filesDriver) flushWidths() {
	if inst.widths == nil {
		return
	}
	if _, err := inst.widths.Flush(time.Now()); err != nil {
		log.Warn().Err(err).Msg("play: storing column widths failed; will retry")
	}
}

// renderFilesTab is the Files dock tab body: the active result as a browsable
// tree of paths.
func (inst *PlayApp) renderFilesTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel(pathContractHint) {
			rt.Small().Weak()
		}
		return
	}
	inst.filesDriver.noteFrame(executed, inst.density, inst.colWidthRes)
	reject := dispatchPanel(filesPanel{driver: inst.filesDriver}, map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("files"), rec: rec, schema: schema, sig: inst.frameSig},
	}, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
