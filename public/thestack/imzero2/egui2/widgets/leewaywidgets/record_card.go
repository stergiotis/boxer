package leewaywidgets

import (
	"fmt"
	imgcolor "image/color"
	"strconv"
	"strings"

	"github.com/dim13/colormap"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwread"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// RecordCard draws leeway records as lwread reads them (ADR-0289 §SD3) as
// ONE table (egui_extras::TableBuilder via the c.NewTable row-iterator
// surface). Every record, every section and every attribute
// share a single table whose columns are
//
//	[entity? · ] section · attribute · labels · values
//
// The optional `entity` column appears only when more than one record is
// drawn. The `section` cell encodes both the section type and its name
// (`tagged · metric`, `plain · entity-id`, `co · <key> · <inner>`). The
// `attribute` cell is what the read model names the attribute — its first
// membership, through the caller's renderer, or a plain column's name — and
// `labels` its further memberships. The `values` cell flattens the
// attribute's value columns into inline `name=value · …` pairs; columns the
// readability aspect hides are left out, as lwread leaves them.
//
// A section header row precedes each section's rows and a separator row
// each record after the first. Each cell is a real egui::Ui scope, so empty
// placeholders render as italic-weak em-dashes and long strings wrap.
//
// Values are drawn in the read model's spelling unless the host's gloss
// rewrites them (SetCellGloss) or claims a block face for them
// (SetCellBlock); both seams see the driver's text and the value column's
// Arrow index, as they did on the card's earlier emitter.
type RecordCard struct {
	ids      *c.WidgetIdStack
	scopeKey string
	palette  imgcolor.Palette

	nEntities int32

	// cellGloss, when set, rewrites each value's text before it is laid into
	// the card (ADR-0186 §SD4): the host's per-column gloss inline face.
	// Nil = the read model's spelling.
	cellGloss CellGlossFunc
	// cellBlock, when set, is asked for a block face per value before the
	// text is glossed (ADR-0186, block faces on the card).
	cellBlock CellBlockFunc

	// unified holds the rows of the last Prepare.
	unified []table2UnifiedRow

	// collapsedSections is the per-section collapse state, persisted across
	// frames and keyed by the composed section name the header bar shows. A
	// section is collapsed when its key maps to true.
	collapsedSections map[string]bool
}

type sectionTypeE uint8

const (
	sectionTypeNone sectionTypeE = iota
	sectionTypePlain
	sectionTypeTagged
	sectionTypeCo
)

type rowKindE uint8

const (
	rowKindData rowKindE = iota
	rowKindEntitySep
	// rowKindSectionHeader is a "virtual" row inserted at each new section,
	// rendered as a colored bar that visually groups the data rows below it.
	rowKindSectionHeader
)

type table2NamedValue struct {
	name  string
	value string
	// blocks are the value's block faces (ADR-0186), one per scalar or item
	// the host's CellBlockFunc claimed; nil for a text-only value. When set,
	// the values cell draws them under the pair's name instead of value —
	// which stays the inline face, for SectionDigests.
	blocks []CellBlock
}

// table2SectionStats accumulates per-section value-volume metrics for
// display in the section header bar. Only the values cell contributes.
type table2SectionStats struct {
	nRows   int32 // count of rowKindData rows in the section
	nValues int32 // count of non-empty value strings across those rows
	nChars  int32 // sum of len(value) (UTF-8 bytes) across those values
}

type table2UnifiedRow struct {
	kind             rowKindE
	entityIdx        int32
	sectionType      sectionTypeE
	sectionName      string
	sectionAccentIdx int32
	primary          []table2Tag
	secondary        []table2Tag
	valuePairs       []table2NamedValue
}

type table2Tag struct {
	display string
	detail  string
}

// NewRecordCard constructs a card drawing under scopeKey with the given
// section-accent palette.
func NewRecordCard(ids *c.WidgetIdStack, scopeKey string, palette ColorPaletteE) (inst *RecordCard) {
	if scopeKey == "" {
		scopeKey = "leeway-card"
	}
	var pal imgcolor.Palette
	switch palette {
	case ColorPaletteViridis:
		pal = colormap.Viridis
	case ColorPaletteMagma:
		pal = colormap.Magma
	case ColorPalettePlasma:
		pal = colormap.Plasma
	default:
		pal = colormap.Inferno
	}
	return &RecordCard{ids: ids, scopeKey: scopeKey, palette: pal, collapsedSections: make(map[string]bool, 8)}
}

func (inst *RecordCard) accentColor(idx int32) (col color.Color) {
	n := len(inst.palette)
	if n == 0 {
		// Empty-palette fallback: the IDS Neutral.Default token (ADR-0031 §SD2)
		// reads as a calm grey on the dark spine.
		return color.Hex(styletokens.NeutralDefault.AsHex()).Keep()
	}
	lo := n / 5
	hi := n * 4 / 5
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	pos := lo + (int(idx)*37)%span
	if pos >= n {
		pos = pos % n
	}
	// Bridge the runtime-supplied image/color palette into egui2.color via
	// the FromImage helper — keeps designlint L2 quiet because the
	// conversion lives in the L2-allowlisted color package.
	return color.FromImage(inst.palette[pos]).Keep()
}

// CellGlossFunc rewrites one value cell's text before the card lays it out
// (ADR-0186 §SD4): arrowIdx is the value column's Arrow index in the driven
// batch, text the marshalled scalar or one item of a collection. It returns
// text unchanged for a column it does not gloss. It runs per value per drive,
// so it must be cheap — the host resolves the column once and applies an
// inline face here.
type CellGlossFunc func(arrowIdx int, text string) string

// SetCellGloss installs (or, with nil, removes) the per-value gloss. It is
// the text seam a host has into the card's values, beside the hide rule
// BeginColumn applies from the value semantics; SetCellBlock is the other.
// SetCellGloss installs (or, with nil, removes) the per-value gloss. It is
// the text seam a host has into the card's values; SetCellBlock is the other.
func (inst *RecordCard) SetCellGloss(fn CellGlossFunc) {
	inst.cellGloss = fn
}

// CellBlock is a block face for one value (ADR-0186 §SD1, on the card since
// its 2026-08-15 Update): Render draws it inside the values cell, under the
// pair's name and in place of the value's text; Height is the points it
// asks for, and the row grows to fit — clamped to [one text line,
// table2BlockMaxHeight], so a face that may run longer draws into its own
// vertical ScrollArea. Render runs at draw time, once per frame the card is
// drawn, and must scope its own widget ids.
type CellBlock struct {
	Render func()
	Height float32
}

// CellBlockFunc returns the block face for one value, or ok=false to keep
// the text. Like CellGlossFunc it is keyed by the value column's Arrow index
// and runs per value per drive; it sees the value's marshalled text before
// the gloss rewrites it, since a block face wants the document, not its
// first line. The order of calls within a column is the value order, so a
// host that caches per value can count.
type CellBlockFunc func(arrowIdx int, text string) (block CellBlock, ok bool)

// SetCellBlock installs (or, with nil, removes) the per-value block face.
func (inst *RecordCard) SetCellBlock(fn CellBlockFunc) {
	inst.cellBlock = fn
}

// Prepare lays out m's records as the card's rows: a section header before
// each run of attributes in one section, a separator before each record
// after the first, a plain column on a row of its own. The gloss and block
// seams run here, once per value, so a Prepare per drawn record is what a
// caller pays — not one per frame.
func (inst *RecordCard) Prepare(m *lwread.Model) {
	inst.unified = inst.unified[:0]
	inst.nEntities = 0
	if m == nil {
		return
	}
	inst.nEntities = int32(len(m.Records))
	accent := int32(0)
	for ri := range m.Records {
		r := &m.Records[ri]
		if ri > 0 {
			inst.unified = append(inst.unified, table2UnifiedRow{kind: rowKindEntitySep, entityIdx: int32(ri)})
		}
		prevKey := ""
		for ai := range r.Attributes {
			a := &r.Attributes[ai]
			st := sectionTypeTagged
			switch {
			case a.Plain:
				st = sectionTypePlain
			case a.CoGroup != "":
				st = sectionTypeCo
			}
			name := a.Section
			if st == sectionTypeCo {
				name = fmt.Sprintf("%s · %s", a.CoGroup, a.Section)
			}
			pairs := inst.valuePairs(a)
			if a.Plain && len(pairs) == 0 {
				continue
			}
			if key := strconv.Itoa(int(st)) + "\x00" + name; key != prevKey {
				prevKey = key
				accent++
				inst.unified = append(inst.unified, table2UnifiedRow{kind: rowKindSectionHeader, entityIdx: int32(ri),
					sectionType: st, sectionName: name, sectionAccentIdx: accent})
			}
			row := table2UnifiedRow{kind: rowKindData, entityIdx: int32(ri), sectionType: st, sectionName: name, sectionAccentIdx: accent}
			if a.Plain {
				// A plain column's name is the row's identity; the values
				// cell carries the bare value.
				row.primary = []table2Tag{{display: a.Name}}
				pairs[0].name = ""
				row.valuePairs = pairs[:1]
				inst.unified = append(inst.unified, row)
				continue
			}
			if a.Named != nil {
				row.primary = []table2Tag{{display: a.Name, detail: membershipDetail(a.Named)}}
			}
			for li := range a.Labels {
				row.secondary = append(row.secondary, table2Tag{display: a.Labels[li].Text, detail: membershipDetail(&a.Labels[li])})
			}
			row.valuePairs = pairs
			inst.unified = append(inst.unified, row)
		}
	}
}

// PrepareFrom reads what drive streams into a fresh read model — memberships
// named through renderer, nil spelling a ref as its hex id — and prepares
// the card from it. It is Prepare for a caller that holds a sink-driving
// function rather than a model: a fixture, a decoder, a Driver over a slice.
func (inst *RecordCard) PrepareFrom(drive func(sink streamreadaccess.SinkI) error, renderer *membership.Renderer) (err error) {
	sink := lwread.NewSink(lwread.Options{Renderer: renderer})
	err = drive(sink)
	if err != nil {
		return
	}
	m := sink.Model()
	m.Qualify()
	inst.Prepare(m)
	return
}

// valuePairs is a's value columns as the card's pairs, each item through the
// host's block and gloss seams.
func (inst *RecordCard) valuePairs(a *lwread.Attribute) (pairs []table2NamedValue) {
	for _, v := range a.Values {
		var sb strings.Builder
		var blocks []CellBlock
		for i, it := range v.Items {
			if i > 0 {
				sb.WriteString(", ")
			}
			if inst.cellBlock != nil {
				if b, ok := inst.cellBlock(v.ArrowIdx, it.Raw); ok && b.Render != nil {
					blocks = append(blocks, b)
				}
			}
			text := it.Text
			if inst.cellGloss != nil {
				if g := inst.cellGloss(v.ArrowIdx, it.Raw); g != it.Raw {
					text = g
				}
			}
			sb.WriteString(text)
		}
		if v.More > 0 {
			fmt.Fprintf(&sb, ", … %d more", v.More)
		}
		pairs = append(pairs, table2NamedValue{name: v.Column, value: utfsafe.EnsureUTF8(sb.String()), blocks: blocks})
	}
	return
}

// membershipDetail is the hover text of a membership chip.
func membershipDetail(m *lwread.Membership) string {
	if m.IsRef {
		return fmt.Sprintf("ref 0x%x", m.Ref)
	}
	return "verbatim"
}

// Render draws the rows of the last Prepare into the current ui scope; a
// no-op before one.
func (inst *RecordCard) Render() {
	if len(inst.unified) > 0 {
		inst.flushUnified()
	}
}

// SectionValue is one value pair (column name + rendered value) of a section.
type SectionValue struct {
	Name  string
	Value string
}

// SectionDigest is a read-only summary of one tagged section's data row: the
// attribute's name (Primary) and labels (Secondary) exactly as the card's
// columns, and every value pair (the section's co-attributes). It lets a caller
// reuse the card's per-section content instead of re-reading the record — e.g.
// the Detail timeline labels a temporal flag with its owning section's name and
// co-attributes, matching the card below it.
type SectionDigest struct {
	SectionName string
	Primary     []string // the attribute's name, empty when no membership names it
	Secondary   []string // its labels
	Values      []SectionValue
}

// SectionDigests returns one digest per tagged / co-section data row of the
// last Prepare, in record order. Plain / backbone sections are excluded: they
// carry no memberships and fan out one row per column. The returned slices
// are fresh copies, so they survive the next Prepare.
func (inst *RecordCard) SectionDigests() (digests []SectionDigest) {
	for i := range inst.unified {
		row := &inst.unified[i]
		if row.kind != rowKindData ||
			(row.sectionType != sectionTypeTagged && row.sectionType != sectionTypeCo) {
			continue
		}
		d := SectionDigest{SectionName: row.sectionName}
		for _, t := range row.primary {
			d.Primary = append(d.Primary, t.display)
		}
		for _, t := range row.secondary {
			d.Secondary = append(d.Secondary, t.display)
		}
		for _, p := range row.valuePairs {
			d.Values = append(d.Values, SectionValue{Name: p.name, Value: p.value})
		}
		digests = append(digests, d)
	}
	return digests
}

// --- rendering (egui_extras::TableBuilder via c.NewTable) ---

const (
	table2RowHeightSingle = 24.0
	table2RowHeightDouble = 38.0
	table2RowHeightTriple = 54.0
	table2RowHeightSep    = 8.0
	// Section-header row height accounts for: outer margin top+bottom,
	// stroke width (drawn inside the outer rect), inner margin
	// top+bottom, and the Size(14) section-name galley (≈ 17 px of
	// ascender+descender). With (2 + 1.5 + 4) × 2 = 15 px of padding,
	// 33 px leaves ~18 px content area — enough for the Size(14) text
	// without descender clipping into the bottom border.
	table2RowHeightSectionHeader = 33.0
	table2HeaderHeight           = 28.0

	// Section-header bar padding. The outer margin keeps the accent
	// outline from sitting flush against the cell edge / column
	// dividers; the inner margin keeps the chevron, name, and stats
	// text from kissing the outline.
	table2SectionHeaderOuterMargin = 2.0
	table2SectionHeaderInnerMargin = 4.0

	// Block faces (ADR-0186) stack under a row's inline values: a caption
	// line per block pair when the row has more than one pair, each block
	// at its requested height within [table2RowHeightSingle,
	// table2BlockMaxHeight], and a gap after it. The maximum keeps one
	// document from turning the card into that document; a face that may
	// run longer scrolls inside its own area.
	table2BlockCaptionHeight = 16.0
	table2BlockGap           = 4.0
	table2BlockMaxHeight     = 320.0

	table2EntityColW  = 60.0
	table2SectionColW = 200.0
	table2ChipColW    = 150.0

	table2EntityColMin  = 40.0
	table2SectionColMin = 140.0
	table2ChipColMin    = 100.0
	table2ValuesColMin  = 240.0
)

// flushUnified renders all buffered rows as one egui_extras::TableBuilder
// table.  Columns: [entity?] section · primary · values · secondary.
//
// The values column is `Remainder().AtLeast(table2ValuesColMin)` so it
// absorbs whatever width is left over after the others — drag any other
// column and only values shrinks/grows. egui_extras handles drag
// persistence natively (its own TableState), so no manual fix-up is
// needed here. Per-row variable height comes from rowHeight(...) → tbl.Row(h);
// egui_extras::heterogeneous_rows is the upstream mechanism.
// flushUnified draws the buffered rows under the emitter's scope
// (ADR-0267 W4): every widget id the card derives is relative to it.
func (inst *RecordCard) flushUnified() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.flushUnifiedScoped()
	}
}

func (inst *RecordCard) flushUnifiedScoped() {
	showEntity := inst.nEntities > 1

	// Columns must be pushed BEFORE entering NewTable.Body() — they're
	// drained by the apply at the start of the table render.
	//
	// The Remainder column must be LAST. egui_extras' "fill the
	// remainder" path (table.rs line 819) only fires for the trailing
	// column. A non-trailing Remainder column gets clamped to its
	// at_least minimum and stays there. Hence the column order here is
	// section · primary · secondary · values rather than the original
	// section · primary · values · secondary — values goes at the end so
	// it actually fills the panel.
	//
	// Column AtLeast values are the user-visible minimum widths (column
	// can be dragged this narrow but no narrower). The first-frame
	// sizing-pass shrink-to-content quirk in egui_extras is handled
	// inside render_new_table on the Rust side — see the sizing-pass
	// skip there.
	// ClipContents(true) is essential for responsive drag: when clip is
	// false (the default), egui_extras throttles narrower-direction
	// drags to 8 pixels per frame (table.rs line 877's
	// max_shrinkage_per_frame HACK comment). At 60 FPS, a 100-pixel
	// drag takes 0.2s of visible lag. With clip=true the column tracks
	// the pointer 1:1 and content past the column edge is clipped —
	// which is what the original baseline did via Truncate wrap mode.
	if showEntity {
		c.NewTableColumn().Initial(table2EntityColW).AtLeast(table2EntityColMin).ClipContents(true).Resizable(true).Send()
	}
	c.NewTableColumn().Initial(table2SectionColW).AtLeast(table2SectionColMin).ClipContents(true).Resizable(true).Send()
	c.NewTableColumn().Initial(table2ChipColW).AtLeast(table2ChipColMin).ClipContents(true).Resizable(true).Send()
	c.NewTableColumn().Initial(table2ChipColW).AtLeast(table2ChipColMin).ClipContents(true).Resizable(true).Send()
	c.NewTableColumn().Remainder().AtLeast(table2ValuesColMin).ClipContents(true).Resizable(true).Send()

	// AutoShrink(false, true): egui_extras' inner ScrollArea defaults to
	// shrinking on both axes, which leaves the table at its natural column
	// sum and parks the trailing right-side panel area unused. Disabling
	// horizontal shrink lets the Remainder values column absorb that
	// slack so the table fills the panel width.
	for tbl := range c.NewTable(inst.ids.PrepareStr("table")).
		Striped(true).
		HeaderHeight(table2HeaderHeight).
		AutoShrink(false, true).
		Body() {

		for hdr := range tbl.Header() {
			if showEntity {
				for range hdr.Col() {
					renderHeaderCell("entity")
				}
			}
			for range hdr.Col() {
				renderHeaderCell("section")
			}
			for range hdr.Col() {
				renderHeaderCell("attribute")
			}
			for range hdr.Col() {
				renderHeaderCell("labels")
			}
			for range hdr.Col() {
				renderHeaderCell("values")
			}
		}

		nCols := uint32(4)
		if showEntity {
			nCols = 5
		}
		sectionStats, totalChars := inst.computeSectionStats()
		// activeSection is the name of the most recently rendered section
		// header. Data rows belonging to a collapsed section are skipped.
		// Group/entity separators reset it to "" so they always render
		// regardless of collapse state.
		activeSection := ""
		for i := range inst.unified {
			row := &inst.unified[i]

			switch row.kind {
			case rowKindSectionHeader:
				activeSection = row.sectionName
			case rowKindEntitySep:
				activeSection = ""
			case rowKindData:
				if activeSection != "" && inst.collapsedSections[activeSection] {
					continue
				}
			}

			for r := range tbl.Row(rowHeight(row)) {
				if row.kind == rowKindSectionHeader {
					inst.renderSectionHeaderRow(r, row, nCols, showEntity, sectionStats[row.sectionName], totalChars)
					continue
				}
				if showEntity {
					for range r.Col() {
						renderEntityCell(row)
					}
				}
				for range r.Col() {
					inst.renderSectionCell(row)
				}
				for range r.Col() {
					renderChipCell(row.primary, false, row.kind)
				}
				for range r.Col() {
					renderChipCell(row.secondary, true, row.kind)
				}
				for range r.Col() {
					renderValuesCell(row.valuePairs, row.kind)
				}
			}
		}
	}
}

// renderSectionHeaderRow paints every cell of the row with the section's
// accent colour and places a chevron + section name in the section
// column. The bar is click-sensed: each cell of the row gets a
// senseClick'd TintedScope, and a click on any of them toggles the
// section's entry in collapsedSections. State persists across frames
// via the map (one-frame display lag for the toggle is normal IM).
//
// All cells get the tint so the bar reads as visually contiguous —
// without it, egui_extras' item_spacing.x leaves a small dark gutter
// between columns. The chevron + name only go in the first cell after
// the optional entity column.
// table2ColumnKeys names the card table's columns in draw order; the
// section-header cells key their ids by it.
var table2ColumnKeys = [...]string{"entity", "section", "primary", "secondary", "values"}

func (inst *RecordCard) renderSectionHeaderRow(
	r *c.NewTableDataRow,
	row *table2UnifiedRow,
	nCols uint32,
	showEntity bool,
	stats *table2SectionStats,
	totalChars int32,
) {
	accent := inst.accentColor(row.sectionAccentIdx)
	transparentFill := color.Transparent.Keep()
	collapsed := inst.collapsedSections[row.sectionName]
	chevron := "▼ "
	if collapsed {
		chevron = "▶ "
	}
	text := chevron + fmt.Sprintf("%s · %s", sectionTypeAbbrev(row.sectionType), row.sectionName)
	statsText := formatSectionStats(stats, totalChars)

	nameCellIdx := uint32(0)
	statsCellIdx := nCols - 1
	if showEntity {
		nameCellIdx = 1
	}
	// Each cell sense-clicks separately and any click flips the section's
	// collapsed state. Distinct ids per cell prevent egui id-clash warnings;
	// they all share the toggle target (row.sectionName), so functionally
	// any cell click is equivalent.
	//
	// The bar is a per-cell coloured outline (accent stroke, transparent
	// fill) rather than a saturated fill, so the section name and stats
	// retain default text contrast against the dark base. Per-cell rect
	// strokes do place a vertical line at every column boundary, but the
	// natural egui_extras column dividers sit in the same place, so the
	// effect reads as an outlined row rather than as visible seams.
	//
	// A cell is keyed by its column's name under the section's accent
	// ordinal under a "section" scope. The packed accentIdx<<16|col it
	// replaces shared the card scope with the table's own ordinal id and
	// collided with it at section 3, column 1; and the column cannot be a
	// second ordinal under the first, since the XOR stack commutes — (1,2)
	// and (2,1) would derive one id.
	columnKeys := table2ColumnKeys[:]
	if !showEntity {
		columnKeys = columnKeys[1:]
	}
	clicked := false
	for range c.IdScope(inst.ids.PrepareStr("section")) {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(row.sectionAccentIdx))) {
			for col := range nCols {
				for range r.Col() {
					ts := c.TintedScope(inst.ids.PrepareStr(columnKeys[col]), transparentFill).
						Stroke(styletokens.StrokeRegular, accent).
						OuterMargin(table2SectionHeaderOuterMargin).
						InnerMargin(table2SectionHeaderInnerMargin).
						SenseClick()
					for range ts.KeepIter() {
						switch col {
						case nameCellIdx:
							for rt := range c.RichTextLabel(text) {
								rt.Strong().Monospace().Size(14)
							}
						case statsCellIdx:
							if statsText != "" {
								for rt := range c.RichTextLabel(statsText) {
									rt.Monospace().Small()
								}
							}
						}
					}
					if ts.HasPrimaryClicked() {
						clicked = true
					}
				}
			}
		}
	}
	if clicked {
		inst.collapsedSections[row.sectionName] = !collapsed
	}
}

// computeSectionStats walks the unified row list and aggregates per-
// section value-volume metrics. One pass; returns the per-section map
// plus the grand total of value bytes (for percentage display in the
// header bar).
//
// Section identity is row.sectionName (composedSectionName for tagged
// /co sections), matching the key used for collapsedSections so the
// header-row lookup is the same string.
func (inst *RecordCard) computeSectionStats() (stats map[string]*table2SectionStats, totalChars int32) {
	stats = make(map[string]*table2SectionStats, 8)
	activeSection := ""
	for i := range inst.unified {
		row := &inst.unified[i]
		switch row.kind {
		case rowKindSectionHeader:
			activeSection = row.sectionName
			if _, ok := stats[activeSection]; !ok {
				stats[activeSection] = &table2SectionStats{}
			}
		case rowKindEntitySep:
			activeSection = ""
		case rowKindData:
			if activeSection == "" {
				continue
			}
			s := stats[activeSection]
			if s == nil {
				continue
			}
			s.nRows++
			for _, p := range row.valuePairs {
				if p.value == "" {
					continue
				}
				s.nValues++
				n := int32(len(p.value))
				s.nChars += n
				totalChars += n
			}
		}
	}
	return
}

// formatSectionStats renders the per-section stats as a compact suffix
// shown in the trailing cell of the section-header bar. Empty string
// when the section has no data rows yet (initial frames while the
// stream is still arriving) — caller skips rendering when this returns
// empty.
func formatSectionStats(stats *table2SectionStats, totalChars int32) string {
	if stats == nil || stats.nRows == 0 {
		return ""
	}
	pct := float64(0)
	if totalChars > 0 {
		pct = 100.0 * float64(stats.nChars) / float64(totalChars)
	}
	return fmt.Sprintf("%d rows · %dB · %.0f%%", stats.nRows, stats.nChars, pct)
}

// renderHeaderCell draws one centered, strong header label.
// VerticalCentered actually centers horizontally — egui's naming refers
// to the layout main axis (top_down for VerticalCentered, with Align::Center
// on the cross axis), so VerticalCentered places contents centered along
// the horizontal axis. (HorizontalCentered conversely centers along the
// vertical axis, which is the wrong dimension here.)
func renderHeaderCell(text string) {
	for range c.VerticalCentered().KeepIter() {
		for rt := range c.RichTextLabel(text) {
			rt.Strong().Size(16)
		}
	}
}

// rowHeight picks the height for one buffered row. Separators are short;
// data rows grow when chip lists or value-pair lists are likely to wrap at
// the configured column widths.
func rowHeight(row *table2UnifiedRow) (h float32) {
	switch row.kind {
	case rowKindEntitySep:
		return table2RowHeightSep
	case rowKindSectionHeader:
		return table2RowHeightSectionHeader
	}
	if row.kind != rowKindData {
		return table2RowHeightSep
	}
	n := max(len(row.secondary), len(row.primary))
	// Heuristic: ~2 packed `name=value` pairs fit per visual line in the
	// values column at table2ValuesColW. Block-faced pairs are not packed:
	// they stack under the inline line at their own heights.
	inlinePairs, blocksH := valuesCellExtent(row.valuePairs)
	pairLines := (inlinePairs + 1) / 2
	if pairLines > n {
		n = pairLines
	}
	switch {
	case n >= 3:
		h = table2RowHeightTriple
	case n == 2:
		h = table2RowHeightDouble
	default:
		h = table2RowHeightSingle
	}
	if blocksH > 0 {
		valuesH := blocksH
		if inlinePairs > 0 {
			valuesH += table2RowHeightSingle
		}
		h = max(h, valuesH)
	}
	return h
}

// valuesCellExtent measures a row's values cell for rowHeight: how many
// pairs pack into the inline line, and the height the block-faced pairs
// stack under it (caption when the row has more than one pair, each block
// clamped, a gap after each). blocksH is 0 for a row without block faces.
func valuesCellExtent(pairs []table2NamedValue) (inlinePairs int, blocksH float32) {
	for i := range pairs {
		if len(pairs[i].blocks) == 0 {
			inlinePairs++
			continue
		}
		if len(pairs) > 1 {
			blocksH += table2BlockCaptionHeight
		}
		for _, b := range pairs[i].blocks {
			blocksH += clampBlockHeight(b.Height) + table2BlockGap
		}
	}
	return inlinePairs, blocksH
}

// clampBlockHeight bounds a block face's requested height to what a row
// gives it: at least one text line, at most table2BlockMaxHeight.
func clampBlockHeight(h float32) float32 {
	return min(max(h, table2RowHeightSingle), table2BlockMaxHeight)
}

func renderEntityCell(row *table2UnifiedRow) {
	if row.kind != rowKindData {
		return
	}
	for rt := range c.RichTextLabel(strconv.Itoa(int(row.entityIdx))) {
		rt.Strong().Monospace()
	}
}

// renderSectionCell repeats the section's identity in column 1 of
// every data row. The accent-colour decoration that used to live here
// migrated to the section-header bar above each section group; the
// per-row text now stays uncoloured so the eye doesn't compete with
// the bar for which row "owns" the colour.
func (inst *RecordCard) renderSectionCell(row *table2UnifiedRow) {
	if row.kind != rowKindData {
		return
	}
	text := fmt.Sprintf("%s · %s", sectionTypeAbbrev(row.sectionType), row.sectionName)
	for rt := range c.RichTextLabel(text) {
		rt.Monospace().Small().Weak()
	}
}

func sectionTypeAbbrev(t sectionTypeE) (s string) {
	switch t {
	case sectionTypePlain:
		return "plain"
	case sectionTypeCo:
		return "co"
	}
	return "tagged"
}

func renderChipCell(tags []table2Tag, muted bool, kind rowKindE) {
	if kind != rowKindData {
		return
	}
	if len(tags) == 0 {
		for rt := range c.RichTextLabel(emDash) {
			rt.Italics().Weak().Small()
		}
		return
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = t.display
	}
	txt := strings.Join(parts, ", ")
	for rt := range c.RichTextLabel(txt) {
		if muted {
			rt.Weak().Small().Monospace()
		} else {
			rt.Small().Monospace()
		}
	}
}

// renderValuesCell renders the packed values cell. With exactly one visible
// value pair the column name is dropped — the section's primary cell already
// carries identity (chip for tagged/co, column name for plain), so a `name=`
// prefix would just repeat it. Multi-column rows keep the explicit
// `name=value · name=value` form so each pair is unambiguous.
//
// A pair with block faces (ADR-0186) is not packed: the inline pairs keep
// their line, and each block pair follows as its caption (the name, when the
// row has more than one pair) and its blocks, drawn by the host at the
// heights rowHeight reserved.
func renderValuesCell(pairs []table2NamedValue, kind rowKindE) {
	if kind != rowKindData {
		return
	}
	if len(pairs) == 0 {
		for rt := range c.RichTextLabel(emDash) {
			rt.Italics().Weak().Small()
		}
		return
	}
	inlinePairs, blocksH := valuesCellExtent(pairs)
	if blocksH == 0 {
		renderPackedValues(pairs, len(pairs) > 1)
		return
	}
	for range c.Vertical().KeepIter() {
		if inlinePairs > 0 {
			inline := make([]table2NamedValue, 0, inlinePairs)
			for i := range pairs {
				if len(pairs[i].blocks) == 0 {
					inline = append(inline, pairs[i])
				}
			}
			// Names stay: the block pairs below are the other pairs of the row.
			renderPackedValues(inline, len(pairs) > 1)
		}
		for i := range pairs {
			p := &pairs[i]
			if len(p.blocks) == 0 {
				continue
			}
			if len(pairs) > 1 {
				for rt := range c.RichTextLabel(p.name) {
					rt.Monospace().Small().Weak()
				}
			}
			for _, b := range p.blocks {
				b.Render()
			}
		}
	}
}

// renderPackedValues draws value pairs as one text line: the value alone
// when the row has one pair, `name=value · name=value` when named.
func renderPackedValues(pairs []table2NamedValue, named bool) {
	if !named && len(pairs) == 1 {
		v := pairs[0].value
		if v == "" {
			for rt := range c.RichTextLabel(emDash) {
				rt.Italics().Weak().Small()
			}
			return
		}
		for rt := range c.RichTextLabel(v) {
			rt.Monospace().Small()
		}
		return
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(p.name)
		b.WriteByte('=')
		if p.value == "" {
			b.WriteString(emDash)
		} else {
			b.WriteString(p.value)
		}
	}
	for rt := range c.RichTextLabel(b.String()) {
		rt.Monospace().Small()
	}
}
