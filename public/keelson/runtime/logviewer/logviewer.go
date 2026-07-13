// Package logviewer is the imzero2 AppI that visualises the logbridge
// Sink: counters at the top (capacity / dropped / decoded / written /
// parse errors), a filter row below (level / app id substring /
// message substring), a virtualised tail table below that, and a
// collapsible detail pane that opens when the operator clicks a row.
//
// The widget reads the Sink's retain-N tail buffer once per frame; no
// subscription channel is involved, so the widget pays no cost when
// it's not on screen.
//
// Instances are factory-allocated so the dock host can open more than
// one tile of the same logviewer without Go-side widget ID collisions:
// each instance owns its own filter state, selection state, and
// per-instance WidgetIdStack. The host (windowhost / DockHost) supplies
// the stack via MountCtx.Ids() and pre-pushes a window-unique salt
// onto it before every Frame() call via c.IdScope, so two tiles
// produce disjoint ID sets in the same frame even when they emit the
// same label string.
//
// The Sink reference is shared (one bridge per process); RegisterSink
// is the host bootstrap entry point.
package logviewer

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/errorview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
)

// sinkRef holds the host-registered Sink. An atomic.Pointer keeps the
// access lock-free on the render-loop hot path; the host writes it
// exactly once during App.Before, and every Frame call reads it.
var sinkRef atomic.Pointer[logbridge.Sink]

// RegisterSink lets the host hand the logbridge.Sink to every
// logviewer instance. Must be called before the first Frame of any
// logviewer tile (typically in App.Before, alongside
// logbridge.InstallGlobal). Subsequent calls replace the previous
// Sink; nil disables every instance simultaneously.
func RegisterSink(s *logbridge.Sink) {
	sinkRef.Store(s)
}

// levelOptions is the dropdown vocabulary; trace at index 0 means "no
// floor", error at the high end is the practical operator default.
// Kept in display order so the ComboBox shows trace → panic top-to-
// bottom matching zerolog's severity ladder.
var levelOptions = []zerolog.Level{
	zerolog.TraceLevel,
	zerolog.DebugLevel,
	zerolog.InfoLevel,
	zerolog.WarnLevel,
	zerolog.ErrorLevel,
	zerolog.FatalLevel,
	zerolog.PanicLevel,
}

// detailPanelMinHeight is the bottom panel's initial DefaultSize.
// The panel itself reserves this much; the body's ScrollArea fills
// the slot via AutoShrink(false, false) so collapse/expand of inner
// sections doesn't ripple up to the wrapping Window.
const detailPanelMinHeight = 220.0

// Row-tint and selection colors are sourced from the IDS semantic palette
// (ADR-0031 §SD2):
//
//   - rowTintWarn / rowTintError use the <role>.Subtle tokens
//     (WarningSubtle / ErrorSubtle, L≈0.20 dark tinted) as full-opacity row
//     backgrounds — the Subtle emphasis is already low-saturation so it
//     reads as a wash rather than a solid block, replacing the alpha-based
//     tinting from the pre-IDS palette (mostly Tailwind 500-tier anchors:
//     amber #f59e0b, red #ef4444, blue #3b82f6, plus a couple near-Tailwind
//     tones).
//   - selectionFill / selectionStroke use the Accent role (ADR-0031 §SD2
//     reserves accent for "selection, focus rings, branded highlights").
//     Subtle for the fill behind every cell; Default for the per-cell
//     stroke that forms the continuous outlined-row pattern (mirrors
//     leeway's table2 section-header bar).
//   - detailErrorFg / detailMutedFg consume the same tokens the badge
//     widget uses (commit 8e5d40f3) so the detail pane and the table
//     read as one visual system.
var (
	rowTintWarn      = color.Hex(styletokens.WarningSubtle.AsHex())
	rowTintError     = color.Hex(styletokens.ErrorSubtle.AsHex())
	selectionFill    = color.Hex(styletokens.AccentSubtle.AsHex())
	selectionStroke  = color.Hex(styletokens.AccentDefault.AsHex())
	transparentColor = color.Transparent
	detailErrorFg    = color.Hex(styletokens.ErrorDefault.AsHex())
	detailMutedFg    = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	detailBgClear    = color.Transparent
)

// LogViewerApp is the per-tile AppI instance. Two open tiles produce
// two LogViewerApp values, each with their own filter selections and
// per-instance WidgetIdStack — clicks in one tile don't bleed into
// the other. The ids stack is supplied by the host via MountCtx.Ids()
// (windowhost pre-pushes a window-unique salt onto it every frame
// via c.IdScope) so cross-app collisions cannot happen.
type LogViewerApp struct {
	manifest app.Manifest
	ids      *c.WidgetIdStack

	// density is read once at Mount and used to resolve every spacing
	// token consumed by the render path (IDS L3 — ADR-0032 §SD2).
	density styletokens.DensityE

	filterLevel   zerolog.Level
	filterAppId   string
	filterMessage string
	follow        bool
	maxRows       int

	// selected is a value copy of the row the operator last clicked.
	// Stored by value (not by pointer into the Sink's tail) so it
	// survives Sink-trim — the detail pane keeps showing the chosen
	// event even if the ring rolls past it.
	selected    factsstore.LogRow
	hasSelected bool

	// fv renders the per-row Fields list in the detail pane via the
	// reusable hierarchical-field viewer. Constructed once per
	// instance so its idPrefix scopes the widget ids it allocates;
	// per-frame Render calls don't re-build state.
	fv fieldview.Renderer

	// ev renders the structured boxer-error chain in the detail
	// pane. Same per-instance lifecycle as fv — constructed in
	// newInstance, called per frame.
	ev errorview.Renderer
}

var _ app.AppI = (*LogViewerApp)(nil)

// newInstance constructs a fresh LogViewerApp with a fallback
// WidgetIdStack. Mount() swaps this for the host-supplied per-instance
// stack so widget ids derived during Frame() are scoped under a window-
// unique salt and cannot collide with another open app's ids.
func newInstance(m app.Manifest) (out *LogViewerApp) {
	ids := c.NewWidgetIdStack()
	out = &LogViewerApp{
		manifest:    m,
		ids:         ids,
		density:     styletokens.DensityFromEnv(),
		filterLevel: zerolog.TraceLevel,
		follow:      true,
		maxRows:     256,
		fv:          fieldview.New(ids, "lv-fld"),
		ev:          errorview.New(ids, "lv-err"),
	}
	return
}

func (inst *LogViewerApp) Manifest() (m app.Manifest) { m = inst.manifest; return }
func (inst *LogViewerApp) Mount(ctx app.MountContextI) (err error) {
	// Pick up the host-supplied per-instance ids stack. fv and ev
	// hold a pointer to the stack — rebuild them so they emit ids
	// scoped under the new stack instead of the ctor's fallback.
	inst.ids = ctx.Ids()
	inst.fv = fieldview.New(inst.ids, "lv-fld")
	inst.ev = errorview.New(inst.ids, "lv-err")
	return
}
func (inst *LogViewerApp) Unmount(ctx app.MountContextI) (err error) { return }
func (inst *LogViewerApp) Frame(ctx app.FrameContextI) (err error)   { return inst.render() }

// render draws the entire widget. The host (DockHost tile or
// adaptToRenderer Window scope) supplies the surrounding ui scope and
// has already pre-pushed a window-unique salt onto inst.ids via
// c.IdScope (windowhost.renderWindowBody) so widget ids are scoped
// under that salt — no local IdScope wrap needed.
//
// Layout uses three *Inside panels so the table doesn't fight the
// dock tile's vertical bound. egui resolves panels in declaration
// order: top reserves its compact height first; bottom (only mounted
// when a row is selected) reserves its slot from the bottom; the
// central panel takes whatever is left, and the egui_table inside it
// runs its own ScrollArea inside that bounded rect. Without this,
// the detail pane stacked below the table would push total content
// past the tile height, expanding the dock tile and stranding the
// outer ScrollArea with a confusing nested-scroll behaviour.
func (inst *LogViewerApp) render() (err error) {
	sink := sinkRef.Load()
	if sink == nil {
		c.Label("Logbridge not installed — host did not call logviewer.RegisterSink()").Send()
		return
	}
	// Top panel: counters + filter row. Compact, never scrolls.
	// DefaultSize is the initial height; the panel auto-sizes to
	// content beyond that, so adding a future row of chips
	// here doesn't truncate.
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).
		DefaultSize(78).
		Resizable(false).
		KeepIter() {
		inst.renderCounters(sink)
		c.Separator().Horizontal().Send()
		inst.renderFilterRow()
	}

	// Bottom panel: detail pane, only mounted when a row is
	// selected. Resizable(true) lets the operator drag the
	// divider to give the chain tree more room when an error has
	// many streams. Fixed default height keeps the table majority
	// of the tile until a user explicitly drags down.
	//
	// The body's ScrollArea (inside renderDetailPane) calls
	// AutoShrink(false, false) — that's what stops collapse/expand
	// of inner CollapsingHeaders from shrinking the panel and
	// rippling up to the wrapping Window. Without it the egui
	// default auto-shrink([true, true]) would size the area to
	// content and the panel would resize on every collapse.
	if inst.hasSelected {
		for range c.PanelBottomInside(inst.ids.PrepareStr("btm")).
			DefaultSize(detailPanelMinHeight).
			Resizable(true).
			KeepIter() {
			inst.renderDetailPane()
		}
	}

	// Central panel: the table. Bounded by the panels above; the
	// egui_table's internal ScrollArea handles row scrolling
	// inside this rect. PanelCentralInside guarantees an owned
	// layout scope so EndETable's height claim is well-defined.
	for range c.PanelCentralInside().KeepIter() {
		rows := sink.Tail(0)
		rows = applyFilters(rows, inst.filterLevel, inst.filterAppId, inst.filterMessage)
		inst.renderTailTable(rows)
	}
	return
}

// renderCounters draws the top-of-widget metrics strip. Each value is
// rendered as a soft-tone badge; zero-valued attention counters
// (Dropped / ParseErrs) render Neutral so a clean stream stays calm,
// non-zero values flip to Warning / Error tones to draw the eye. The
// hover tooltips spell out what each metric means since the chip
// itself is intentionally terse.
func (inst *LogViewerApp) renderCounters(sink *logbridge.Sink) {
	dropped := sink.Dropped()
	decoded := sink.Decoded()
	written := sink.Written()
	parseErrs := sink.ParseErrors()
	tailLen := sink.TailLen()
	tailCap := sink.TailCapacity()

	// Vertical breathing room above and below the strip. The host scope
	// is Vertical (DockHost tile / Window body), so an AddSpace outside
	// the inner Horizontal renders as a vertical gap.
	c.AddSpace(styletokens.PaddingInner(inst.density))
	for range c.Horizontal().KeepIter() {
		// SizeSm chips (the smallest the badge package exposes) with
		// 8px between them — close enough to read as a single
		// dashboard strip, far enough that adjacent badges don't
		// visually merge.
		badge.New(inst.ids.PrepareStr("c-tail"),
			fmt.Sprintf("Tail %d / %d", tailLen, tailCap)).
			Tone(badge.ToneNeutral).Variant(badge.VariantSoft).
			Size(badge.SizeSm).Monospace().
			Tooltip("rows currently retained in the in-memory tail ring (used by this widget) vs ring capacity").
			Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		badge.New(inst.ids.PrepareStr("c-decoded"),
			fmt.Sprintf("Decoded %d", decoded)).
			Tone(badge.ToneInfo).Variant(badge.VariantSoft).
			Size(badge.SizeSm).Monospace().
			Tooltip("zerolog events successfully decoded into LogRows").
			Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		badge.New(inst.ids.PrepareStr("c-written"),
			fmt.Sprintf("Written %d", written)).
			Tone(badge.ToneSuccess).Variant(badge.VariantSoft).
			Size(badge.SizeSm).Monospace().
			Tooltip("LogRows successfully flushed to the FactsStore").
			Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		droppedTone := badge.ToneNeutral
		droppedVariant := badge.VariantSoft
		if dropped > 0 {
			droppedTone = badge.ToneWarning
			droppedVariant = badge.VariantSolid
		}
		badge.New(inst.ids.PrepareStr("c-dropped"),
			fmt.Sprintf("Dropped %d", dropped)).
			Tone(droppedTone).Variant(droppedVariant).
			Size(badge.SizeSm).Monospace().
			Tooltip("events dropped because the flush ring filled up — bridge is falling behind the writer").
			Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		parseTone := badge.ToneNeutral
		parseVariant := badge.VariantSoft
		if parseErrs > 0 {
			parseTone = badge.ToneError
			parseVariant = badge.VariantSolid
		}
		badge.New(inst.ids.PrepareStr("c-parse"),
			fmt.Sprintf("ParseErrs %d", parseErrs)).
			Tone(parseTone).Variant(parseVariant).
			Size(badge.SizeSm).Monospace().
			Tooltip("zerolog events that failed CBOR decoding — usually a writer schema mismatch").
			Send()
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
}

// renderFilterRow draws the three filter inputs plus the follow
// checkbox. Filters apply at render time — adjusting one does not
// touch the Sink's tail buffer.
//
// Spacing budget: 6px between each label and its input (so "Level:"
// doesn't kiss the dropdown), 24px between filter groups (so the
// eye reads them as separate units rather than one long row).
func (inst *LogViewerApp) renderFilterRow() {
	c.AddSpace(styletokens.PaddingHair(inst.density))
	for range c.Horizontal().KeepIter() {
		c.Label("Level:").Send()
		c.AddSpace(styletokens.GapInline(inst.density))
		current := inst.filterLevel.String()
		for range c.ComboBox(inst.ids.PrepareStr("level-cb"),
			c.WidgetText().Text("level").Keep(),
			c.WidgetText().Text(current).Keep()).KeepIter() {
			for _, lvl := range levelOptions {
				selected := lvl == inst.filterLevel
				if c.Button(inst.ids.PrepareStr("lvl-"+lvl.String()),
					c.Atoms().Text(lvl.String()).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					inst.filterLevel = lvl
				}
			}
		}

		c.AddSpace(styletokens.GapPanels(inst.density))
		c.Label("App:").Send()
		c.AddSpace(styletokens.GapInline(inst.density))
		c.TextEdit(inst.ids.PrepareStr("app-filter"), inst.filterAppId, false).
			SendRespVal(&inst.filterAppId)

		c.AddSpace(styletokens.GapPanels(inst.density))
		c.Label("Message:").Send()
		c.AddSpace(styletokens.GapInline(inst.density))
		c.TextEdit(inst.ids.PrepareStr("msg-filter"), inst.filterMessage, false).
			SendRespVal(&inst.filterMessage)

		c.AddSpace(styletokens.GapPanels(inst.density))
		c.Checkbox(inst.ids.PrepareStr("follow"), inst.follow, "Follow").
			SendRespVal(&inst.follow)
	}
	c.AddSpace(styletokens.PaddingHair(inst.density))
}

// Per-widget id namespaces for the tail table. renderTailTable and
// cellTint mint ids with PrepareSeq (allocation-free) rather than
// fmt.Sprintf; the disjoint bases keep cell, badge and header ids from
// colliding within the app's id scope. Bases sit far above
// maxRows*lvNumCols so raising maxRows cannot overlap them.
const (
	lvNumCols     = 5           // time, level, app, caller, message
	lvIdCellBase  = 0x1000_0000 // per-cell TintedScope ids: base + row*lvNumCols + col
	lvIdBadgeBase = 0x2000_0000 // per-row level-badge ids:  base + row
	lvIdHdrBase   = 0x3000_0000 // per-column header ids:    base + col
)

// renderTailTable virtualises the filtered rows via egui_table's
// deferred-block API. When follow is active and there is at least one
// row, we scroll to the last index every frame — egui_table picks up
// the request and lands the bottom of the table in view. Manual
// scroll up flips Follow off via the egui interaction (the checkbox
// remains the authoritative toggle; the user resumes by re-checking).
//
// Per-cell visuals:
//   - Time:    SelectableLabel — clicking toggles row selection, which
//     drives the detail pane below the table.
//   - Level:   small Pill badge whose tone is derived from the level
//     string (debug/trace = Neutral, info = Info,
//     warn = Warning, error/fatal/panic = Error).
//   - App:     plain Label, shortened by shortAppId.
//   - Caller:  monospace plain Label so file:line columns line up.
//   - Message: plain Label with subtle Strong styling on warn+ rows.
//
// Warn / error / fatal / panic rows additionally paint a soft row-wide
// background tint (rowTintWarn / rowTintError) behind every cell, so
// severe events stand out at a glance without needing to scan the
// Level column.
func (inst *LogViewerApp) renderTailTable(rows []factsstore.LogRow) {
	const (
		// rowHeight = 28 leaves room for the SizeSm Pill badge plus the
		// PaddingInner top/bottom inner margin cellTint adds on every
		// cell. Bumped from 26→28 to give cell content visible breathing
		// room from the row gridlines (header padding uses the same
		// budget via the Headers deferred block below).
		rowHeight   = 28.0
		stickyHeads = 1
		stickyCols  = 0
	)
	if len(rows) == 0 {
		c.Label("(no rows match the current filters)").Send()
		return
	}
	if len(rows) > inst.maxRows {
		rows = rows[len(rows)-inst.maxRows:]
	}

	// Column widths account for the cellTint inner margin (PaddingInner
	// each side). Time fits "15:04:05.000" + padding; Level fits the
	// longest pill ("ERROR"); Caller absorbs typical file:line lengths
	// in monospace; Message takes whatever's left.
	c.EtColumn(115.0).Resizable(true).Send() // time
	c.EtColumn(90.0).Resizable(true).Send()  // level
	c.EtColumn(160.0).Resizable(true).Send() // app
	c.EtColumn(180.0).Resizable(true).Send() // caller
	c.EtColumn(560.0).Resizable(true).Send() // message

	et := c.EndETable(inst.ids.PrepareStr("logviewer-table"),
		uint64(len(rows)), rowHeight, stickyHeads, stickyCols)
	if inst.follow {
		et = et.ScrollToRow(uint64(len(rows)-1), 1)
	}

	// Headers as deferred blocks (instead of EtHeaderText) so each
	// label gets the same Frame InnerMargin padding the cells get
	// below — without the block the dispatcher falls back to
	// `ui.heading(text)` which has no inset, leaving the column
	// titles flush against the column gridlines.
	headerTexts := [...]string{"Time", "Level", "App", "Caller", "Message"}
	for col, text := range headerTexts {
		for range et.Headers(0, uint32(col)) {
			for range c.Frame(inst.ids.PrepareSeq(lvIdHdrBase + uint64(col))).
				InnerMargin(styletokens.PaddingInner(inst.density)).
				KeepIter() {
				hdrAtoms := c.Atoms().BeginRichText(text).Strong().End().Keep()
				c.LabelAtoms(hdrAtoms).Send()
			}
		}
	}

	selectedKey := ""
	if inst.hasSelected {
		selectedKey = rowKey(inst.selected)
	}

	// Emit only the rows egui_table will draw this frame. Without this gate
	// every one of the up-to-maxRows rows builds a deferred block (and cell
	// ids) each frame, ~90% of which egui_table immediately culls — the
	// dominant allocation source in this app. VisibleRange reports the
	// previous frame's window (one-frame lag, self-correcting); ok is false
	// on the first frame a table is shown, where we emit the full range.
	rowBegin, rowEnd := 0, len(rows)
	if rb, re, _, _, _, ok := et.VisibleRange(); ok {
		rowBegin = int(rb)
		if int(re) < rowEnd {
			rowEnd = int(re)
		}
	}
	for i := rowBegin; i < rowEnd; i++ {
		r := rows[i]
		isSelected := selectedKey != "" && rowKey(r) == selectedKey
		tint, hasTint := rowTint(r.Level)
		clicked := false

		// Each cell is wrapped in a TintedScope (leeway table2 pattern):
		// a per-cell fill+stroke that, when summed across the row,
		// reads as a contiguous outlined row across egui_table's
		// natural inter-column gutters. SenseClick on every cell means
		// any click anywhere in the row toggles selection — much more
		// forgiving than a single SelectableLabel target.
		et.BeginCells(uint64(i), 0)
		if inst.cellTint(i, 0, hasTint, tint, isSelected, func() {
			c.Label(r.Ts.Format("15:04:05.000")).Send()
		}) {
			clicked = true
		}
		et.EndCells()

		et.BeginCells(uint64(i), 1)
		if inst.cellTint(i, 1, hasTint, tint, isSelected, func() {
			badge.New(inst.ids.PrepareSeq(lvIdBadgeBase+uint64(i)), levelLabel(r.Level)).
				Tone(levelTone(r.Level)).
				Variant(badge.VariantSolid).
				Size(badge.SizeSm).
				Pill().
				Monospace().
				Send()
		}) {
			clicked = true
		}
		et.EndCells()

		et.BeginCells(uint64(i), 2)
		if inst.cellTint(i, 2, hasTint, tint, isSelected, func() {
			c.Label(shortAppId(string(r.AppId))).Send()
		}) {
			clicked = true
		}
		et.EndCells()

		et.BeginCells(uint64(i), 3)
		if inst.cellTint(i, 3, hasTint, tint, isSelected, func() {
			callerAtoms := c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear, r.Caller).
				Monospace().Small().End().Keep()
			c.LabelAtoms(callerAtoms).Send()
		}) {
			clicked = true
		}
		et.EndCells()

		et.BeginCells(uint64(i), 4)
		if inst.cellTint(i, 4, hasTint, tint, isSelected, func() {
			if hasTint {
				// On severe rows, give the message a touch of weight so it
				// reads first when the eye scans the row.
				msgAtoms := c.Atoms().BeginRichText(r.Message).Strong().End().Keep()
				c.LabelAtoms(msgAtoms).Send()
			} else {
				c.Label(r.Message).Send()
			}
		}) {
			clicked = true
		}
		et.EndCells()

		if clicked {
			if isSelected {
				inst.hasSelected = false
				inst.selected = factsstore.LogRow{}
			} else {
				inst.hasSelected = true
				inst.selected = r
			}
		}
	}
	et.Striped(true).Send()
}

// cellTint wraps body inside a TintedScope and returns whether the
// cell was clicked this frame (one-frame-lag, standard r7-routed
// response). Combining fill (row tint) and stroke (selection accent)
// in one primitive matches leeway's table2 section-header bar
// pattern: the per-cell stroke forms a continuous outlined row
// across egui_table's column gutters, and the per-cell fill paints
// the row backdrop.
//
// Layering rules:
//   - selection beats tint: a selected warn row reads as "selected"
//     first, so the fill flips to selectionFill even if hasTint.
//   - non-selected, no-tint cells use a transparent fill so the
//     egui_table Striped(true) zebra still shows through.
//   - the stroke is invisible (width 0, transparent) when the row
//     isn't selected, so non-selected rows don't get an accidental
//     1px accent border.
//
// KeepIter (not Send) provides the deferred PopIdFromStackChecked —
// TintedScope.Send is a no-pop terminal, same hazard as Frame.Send.
func (inst *LogViewerApp) cellTint(row, col int, hasTint bool, tint color.Color, selected bool, body func()) (clicked bool) {
	fill := transparentColor
	switch {
	case selected:
		fill = selectionFill
	case hasTint:
		fill = tint
	}
	strokeWidth := float32(0)
	stroke := transparentColor
	if selected {
		strokeWidth = 1.5
		stroke = selectionStroke
	}
	ts := c.TintedScope(inst.ids.PrepareSeq(lvIdCellBase+uint64(row)*lvNumCols+uint64(col)), fill).
		Stroke(strokeWidth, stroke).
		OuterMargin(0).
		InnerMargin(styletokens.PaddingInner(inst.density)).
		SenseClick()
	for range ts.KeepIter() {
		body()
	}
	clicked = ts.HasPrimaryClicked()
	return
}

// renderDetailPane shows the operator-selected row. Called only
// when inst.hasSelected (the parent panel mount is gated on the
// same flag), so this function never has to short-circuit.
//
// Body wrapped in a vertical ScrollArea so the bottom-panel slot
// stays at its operator-chosen height even when the row carries a
// long error chain or many structured fields — the chain scrolls
// inside the panel rather than expanding the panel and shifting the
// table above. No top-level CollapsingHeader: the panel itself is
// the visual frame, and the operator can drag the panel divider to
// resize.
//
// The pane surfaces every field the bridge decoded but the table
// throws away: full-resolution Ts, Service / Error / Stack / Caller,
// the structured ErrorContext tree (when set), and the per-field
// tagged-union list. Empty optional fields are omitted so the pane
// stays compact for the common case.
func (inst *LogViewerApp) renderDetailPane() {
	r := inst.selected

	// Header row stays outside the ScrollArea so the level badge,
	// timestamp and Clear-selection button are always reachable
	// regardless of how far down the operator scrolled into the
	// chain. Visual "title bar" of the panel — wrapped in a Frame
	// with InnerMargin so the cluster has breathing room from the
	// panel's top-left corner. Badges are SizeSm (matches the table
	// cell badges) so they fit within egui's ui.horizontal() seed
	// cross-size (spacing().interact_size.y, ~22 px); with SizeMd
	// the taller Pill overflows the seed and gets a negative
	// cross-offset, breaking Align::Center for the smaller
	// timestamp / button siblings. Prominence still comes from
	// .Strong() + .Pill() + VariantSolid plus the surrounding
	// padding — the detail pane reads as the "loud" surface
	// without needing a larger badge.
	for range c.Frame(inst.ids.PrepareStr("d-hdr")).
		InnerMargin(styletokens.PaddingTight(inst.density)).
		KeepIter() {
		for range c.Horizontal().KeepIter() {
			badge.New(inst.ids.PrepareStr("d-lvl"), levelLabel(r.Level)).
				Tone(levelTone(r.Level)).Variant(badge.VariantSolid).
				Size(badge.SizeSm).Pill().Monospace().Strong().Send()
			c.AddSpace(styletokens.GapItems(inst.density))
			if r.AppId != "" {
				badge.New(inst.ids.PrepareStr("d-app"), shortAppId(string(r.AppId))).
					Tone(badge.TonePrimary).Variant(badge.VariantOutline).
					Size(badge.SizeSm).Tooltip(string(r.AppId)).Send()
			}
			if r.Service != "" {
				c.AddSpace(styletokens.GapInline(inst.density))
				badge.New(inst.ids.PrepareStr("d-svc"), r.Service).
					Tone(badge.ToneNeutral).Variant(badge.VariantOutline).
					Size(badge.SizeSm).Send()
			}
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			tsAtoms := c.Atoms().BeginRichTextColored(detailMutedFg, detailBgClear,
				r.Ts.UTC().Format(time.RFC3339Nano)+"  ("+humanAge(time.Since(r.Ts))+")").
				Small().Monospace().End().Keep()
			c.LabelAtoms(tsAtoms).Send()
			c.AddSpace(styletokens.PaddingOuter(inst.density))
			if c.Button(inst.ids.PrepareStr("d-clear"),
				c.Atoms().Text("Clear selection").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.hasSelected = false
				inst.selected = factsstore.LogRow{}
			}
		}
	}
	c.Separator().Horizontal().Send()

	// Body: scrollable so a tall error chain doesn't push the
	// header off-screen or fight the panel's bounded height.
	// AutoShrink(false, false): the body must claim the panel's full
	// rect regardless of inner content size, so collapsing one of the
	// inner CollapsingHeaders doesn't shrink the ScrollArea → shrink
	// the panel → ripple up to the wrapping Window. The previous
	// workaround (UiSetMinHeight on the panel) is now obsolete.
	for range c.ScrollArea().
		Vscroll(true).Hscroll(false).
		AutoShrink(false, false).
		KeepIter() {
		// Caller — monospace, separate row so long file:line paths don't
		// crowd the header.
		if r.Caller != "" {
			for range c.Horizontal().KeepIter() {
				c.Label("Caller:").Send()
				callerAtoms := c.Atoms().BeginRichText(r.Caller).Monospace().End().Keep()
				c.LabelAtoms(callerAtoms).Send()
			}
		}

		// Message — full text, selectable so the operator can copy it.
		// Wrap so multi-paragraph messages stay inside the panel width
		// instead of pushing the panel (and the wrapping window) wide.
		c.AddSpace(styletokens.PaddingHair(inst.density))
		c.Label("Message:").Send()
		c.Label(r.Message).Wrap().Selectable(true).Send()

		// Error — coloured red and selectable. Common in errored events.
		// When the bridge decoded a structured boxer chain into
		// ErrorContext, the flat Error string is the chain's outermost
		// message (Summary()) and the Error chain block below renders
		// the per-stack/per-fact tree. Wrap matches the message value
		// for the same reason.
		if r.Error != "" {
			c.AddSpace(styletokens.PaddingHair(inst.density))
			c.Label("Error:").Send()
			errAtoms := c.Atoms().BeginRichTextColored(detailErrorFg, detailBgClear, r.Error).
				Monospace().End().Keep()
			c.LabelAtoms(errAtoms).Wrap().Send()
		}

		// ErrorContext — structured boxer-chain tree. Only set when
		// the writer used eh.Errorf / eb.Build...Errorf and the host
		// wired zerolog.ErrorMarshalFunc = eh.MarshalError (which
		// logbridge.InstallGlobal does). Each stream is a stack-
		// deduplicated bucket; per fact we render the message (red),
		// the stack frame triple if present (monospace muted), and
		// the CBOR-diagnostic dump of any structured-data attached
		// via eb.Build (in a dark canvas Frame).
		if r.ErrorContext != nil {
			inst.ev.Render(toErrorviewContext(r.ErrorContext))
		}

		// Stack — the legacy single-string stack field, set when
		// zerolog's .Stack() was called with the default formatter.
		// Independent of ErrorContext (which carries per-frame data
		// inline in each fact); both can coexist if the writer used
		// .Stack() AND a boxer error.
		if r.Stack != "" {
			c.AddSpace(styletokens.PaddingHair(inst.density))
			c.Label("Stack:").Send()
			for range c.Frame(inst.ids.PrepareStr("d-stk")).
				PresetDarkCanvas().
				InnerMargin(styletokens.PaddingTight(inst.density)).
				KeepIter() {
				stkAtoms := c.Atoms().BeginRichText(r.Stack).Monospace().Small().End().Keep()
				c.LabelAtoms(stkAtoms).Send()
			}
		}

		// Fields — tagged-union list delegated to the reusable
		// fieldview widget. Skips entirely when no extra fields were
		// attached so the pane doesn't grow an empty header.
		if len(r.Fields) > 0 {
			c.AddSpace(styletokens.PaddingInner(inst.density))
			for range c.CollapsingHeader(inst.ids.PrepareStr("d-fields"),
				c.WidgetText().Text(fmt.Sprintf("fields (%d)", len(r.Fields))).Keep()).
				DefaultOpen(true).KeepIter() {
				inst.fv.Render(toFieldviewFields(r.Fields))
			}
		}
	}
}

// toErrorviewContext adapts the bridge-decoded
// factsstore.LogErrorContext into the errorview package's own
// Context shape. Per-stream / per-fact rendering now lives in
// errorview; this function is the only thing the logviewer needs
// to know about the cross-type boundary. Nil input maps to the
// zero Context, which the renderer short-circuits as IsEmpty.
func toErrorviewContext(ctx *factsstore.LogErrorContext) (out errorview.Context) {
	if ctx == nil {
		return
	}
	out.Streams = make([]errorview.Stream, len(ctx.Streams))
	for si, s := range ctx.Streams {
		facts := make([]errorview.Fact, len(s.Facts))
		for fi, f := range s.Facts {
			facts[fi] = errorview.Fact{
				Msg:      f.Msg,
				Source:   f.Source,
				Line:     f.Line,
				Function: f.Function,
				Data:     f.Data,
				DataDiag: f.DataDiag,
				Id:       f.Id,
				ParentId: f.ParentId,
			}
		}
		out.Streams[si] = errorview.Stream{
			Name:  s.Name,
			Facts: facts,
		}
	}
	return
}

// toFieldviewFields adapts the bridge-decoded factsstore.LogField
// slice into the fieldview package's own Field shape. Per-kind
// formatting + wrap layout now lives in fieldview; this function
// is the only thing the logviewer needs to know about the cross-
// type boundary.
//
// Mapping is 1:1 for primitives; factsstore has no container kinds
// (zerolog events are flat), so the produced fields are always
// leaves. Future structured-payload decoding (e.g. nested CBOR maps
// inside a single zerolog field) can grow this adapter to emit
// KindObject / KindArray Fields without changing the renderer.
func toFieldviewFields(fs []factsstore.LogField) (out []fieldview.Field) {
	out = make([]fieldview.Field, len(fs))
	for i, f := range fs {
		out[i] = fieldview.Field{
			Name:  f.Name,
			Kind:  toFieldviewKind(f.Kind),
			Str:   f.Str,
			Int:   f.Int,
			Uint:  f.Uint,
			Float: f.Float,
			Bool:  f.Bool,
			Bytes: f.Bytes,
			Time:  f.Time,
		}
	}
	return
}

// toFieldviewKind maps the factsstore enum to the fieldview enum.
// LogFieldKindUnknown intentionally maps to KindUnknown — fieldview
// then renders it as "?" with a Str fallback for the value, matching
// the logviewer's previous behaviour.
func toFieldviewKind(k factsstore.LogFieldKindE) (out fieldview.KindE) {
	switch k {
	case factsstore.LogFieldKindString:
		out = fieldview.KindString
	case factsstore.LogFieldKindInt:
		out = fieldview.KindInt
	case factsstore.LogFieldKindUint:
		out = fieldview.KindUint
	case factsstore.LogFieldKindFloat:
		out = fieldview.KindFloat
	case factsstore.LogFieldKindBool:
		out = fieldview.KindBool
	case factsstore.LogFieldKindBytes:
		out = fieldview.KindBytes
	case factsstore.LogFieldKindTime:
		out = fieldview.KindTime
	default:
		out = fieldview.KindUnknown
	}
	return
}

// levelLabel uppercases the level for the badge cell. zerolog stores
// "info"/"warn"/etc. lowercase; uppercase reads as a status code in
// the badge form. Empty levels render as "—" so the column never has
// holes that disturb the table grid.
func levelLabel(level string) (s string) {
	if level == "" {
		s = "—"
		return
	}
	s = strings.ToUpper(level)
	return
}

// levelTone maps a zerolog level string to the badge.ToneE that drives
// the per-cell colouring. Unknown / empty / trace / debug all fall
// through to ToneNeutral so faint background events don't shout; info
// is the calm "things are fine" tone; warn → Warning, error/fatal/
// panic → Error.
func levelTone(level string) (tone badge.ToneE) {
	tone = badge.ToneNeutral
	if level == "" {
		return
	}
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		return
	}
	switch lvl {
	case zerolog.InfoLevel:
		tone = badge.ToneInfo
	case zerolog.WarnLevel:
		tone = badge.ToneWarning
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		tone = badge.ToneError
	}
	return
}

// rowTint returns the row-background fill colour for warn-and-above
// rows. ok=false signals "no tint, render the cell plainly" — the
// common case for trace/debug/info/unknown levels.
func rowTint(level string) (tint color.Color, ok bool) {
	if level == "" {
		return
	}
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		return
	}
	switch lvl {
	case zerolog.WarnLevel:
		tint = rowTintWarn
		ok = true
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		tint = rowTintError
		ok = true
	}
	return
}

// rowKey is the stable composite identifier used to compare a row in
// the current snapshot against the operator's selected row. The
// selected row is stored by value (not by pointer or slice index) so
// it survives Sink-trim; we still need a way to highlight the same
// row in the next snapshot, hence the key.
//
// Ts.UnixNano gives nanosecond resolution which is unique in practice
// for events emitted from the same process; Caller and Message are
// added to disambiguate the (rare) coincident-Ts case where two log
// lines land in the same nanosecond.
func rowKey(r factsstore.LogRow) (s string) {
	s = fmt.Sprintf("%d|%s|%s", r.Ts.UnixNano(), r.Caller, r.Message)
	return
}

// humanAge renders a Duration as a compact "Ns" / "Nm" / "Nh" tag for
// the detail-pane subtitle so the operator can tell at a glance how
// stale the selected event is.
func humanAge(d time.Duration) (s string) {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		s = fmt.Sprintf("%.1fs ago", d.Seconds())
	case d < time.Hour:
		s = fmt.Sprintf("%.1fm ago", d.Minutes())
	default:
		s = fmt.Sprintf("%.1fh ago", d.Hours())
	}
	return
}

// shortAppId trims github.com/.../ prefixes so the App column stays
// readable. Falls back to the full id when no prefix matches; the
// column is Resizable so a curious operator can stretch it out.
func shortAppId(id string) (s string) {
	const ghPrefix = "github.com/"
	if !strings.HasPrefix(id, ghPrefix) {
		s = id
		return
	}
	parts := strings.Split(id, "/")
	s = parts[len(parts)-1]
	return
}

// applyFilters narrows rows to those matching every active filter.
// Level rejects rows whose Level string maps to a lower zerolog
// severity than the threshold. AppId and Message use case-insensitive
// substring matching; empty filters are no-ops. Returned slice shares
// no storage with the input — callers may retain it past the next
// Tail() snapshot.
func applyFilters(rows []factsstore.LogRow, threshold zerolog.Level, appNeedle, msgNeedle string) (out []factsstore.LogRow) {
	out = make([]factsstore.LogRow, 0, len(rows))
	appLower := strings.ToLower(appNeedle)
	msgLower := strings.ToLower(msgNeedle)
	for _, r := range rows {
		if threshold > zerolog.TraceLevel {
			// Empty / unparseable Level signals "structured but level-less"
			// — zerolog's Log() shape, rare in practice. Drop these when
			// the operator tightened the threshold; otherwise warn / error
			// filters would still surface them as noise.
			if r.Level == "" {
				continue
			}
			lvl, perr := zerolog.ParseLevel(r.Level)
			if perr != nil || lvl < threshold {
				continue
			}
		}
		if appLower != "" && !strings.Contains(strings.ToLower(string(r.AppId)), appLower) {
			continue
		}
		if msgLower != "" && !strings.Contains(strings.ToLower(r.Message), msgLower) {
			continue
		}
		out = append(out, r)
	}
	return
}
