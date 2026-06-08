//go:build llm_generated_opus47

package leewaywidgets

import (
	"fmt"
	imgcolor "image/color"
	"strconv"
	"strings"

	"github.com/dim13/colormap"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

var _ streamreadaccess.SinkI = (*Table2CardEmitter)(nil)
var _ streamreadaccess.MembershipSinkI = (*Table2CardEmitter)(nil)

// Table2CardEmitter renders an entire leeway batch as ONE table (egui_extras::TableBuilder
// via the c.NewTable row-iterator surface). Every entity, every section,
// and every attribute share a single unified table whose columns are
//
//	[entity? · ] section · primary · values · secondary
//
// The optional `entity` column appears only when the batch contains more
// than one entity; otherwise the four-column form is used. The `section`
// cell encodes both the section type and the section name
// (`tagged · metric`, `plain · entity-id`, `co · <key> · <inner>`). The
// `values` cell flattens whatever value columns the section declared into
// inline `name=value · …` pairs — intra-section column alignment is sacrificed
// for cross-section uniformity. Membership chips are comma-joined per side
// and classified into primary / secondary buckets via
// membershiprole.ClassifierI.
//
// Separator rows are inserted between consecutive entities and at the
// plain↔tagged boundary inside one entity. Separators occupy a small fixed
// row height with empty cells, so egui_extras' striping breaks at the
// boundary and reads as a divider.
//
// Each cell is a real egui::Ui scope (egui_extras renders cells via
// `body.row(h, |row| row.col(|ui| …))`), so empty placeholders render as
// italic-weak em-dashes, value pairs render in small monospace, and long
// chip strings wrap naturally to fit the cell's row height. No Frame /
// Truncate / set_min_size workarounds are needed.
//
// Rationale: egui_extras::Column::remainder() composes with .at_least()
// and .resizable() natively, so the values column absorbs leftover panel
// width and drag-resizing any column shifts width into / out of values
// without an auto_size cascade. Per-row variable height comes from
// egui_extras::heterogeneous_rows; we feed it via tbl.Row(rowHeight(row)).
// An earlier iteration ran on egui_table via c.EndETable and accumulated
// substantial workaround machinery (manual remainder fix-up, Truncate
// wrap dance, VerticalLeftJustified to coerce cell.min_size, AutoSizeMode
// gymnastics) — all of which the c.NewTable surface eliminates by
// exposing egui_extras' native primitives faithfully.
//
// Trade-offs:
//   - Intra-section column alignment is lost: a tagged section's value
//     columns no longer line up vertically across its rows because they
//     share one packed cell. Cross-section alignment was never meaningful
//     anyway (different sections have different schemas).
//   - OnTagClicked is preserved for API parity but never fires — chip
//     cells render as one comma-joined string per row, not per-tag buttons.
//
// Aspect filtering keeps machine-readable-only columns out of the values
// cell. Primary/secondary classification routes membership chips per side
// via membershiprole.ClassifierI. Co-section flatten prefixes the
// composed identity with the co-group key.
type Table2CardEmitter struct {
	ids        *c.WidgetIdStack
	classifier membershiprole.ClassifierI
	renderer   *membership.Renderer
	palette    imgcolor.Palette

	idCounter uint64

	// Cross-batch counters, reset in BeginBatch.
	entityIdx  int32 // current entity ordinal during streaming
	nEntities  int32 // total entities seen, used at flush time
	sectionIdx int32 // monotonic across the batch — drives accent color

	// Active section state — captured at BeginPlainSection / BeginSection.
	sectionName       naming.StylableName
	sectionType       sectionTypeE
	sectionUseAspects useaspects.AspectSet
	sectionAccentIdx  int32
	plainItemType     common.PlainItemTypeE
	nAttrs            int32
	colNames          []string
	colHidden         []bool

	// Co-section: latched on BeginCoSectionGroup, consumed by next BeginSection.
	inCoGroup  bool
	coGroupKey string

	// Active row being built for the current Begin{Plain,Tagged}Value.
	currentRow *table2UnifiedRow

	// Per-column scratch.
	columnIdx    int32
	cellBuf      strings.Builder
	inCollection bool
	itemIdx      int32

	// Buffered output rows for the entire batch — flushed at EndBatch.
	unified []table2UnifiedRow

	// pendingSectionHeader holds the header row we'd append for the
	// current section if any data row arrives. It is set at
	// BeginPlainSection / BeginSection and drained by flushPendingHeader
	// from inside the data-row append paths (EndPlainValue,
	// EndTaggedValue). When a section ends with no data rows — either
	// because nAttrs was 0, or because every value column was filtered
	// out by colHidden — the next section's begin call silently
	// overwrites this without flushing, suppressing the empty header.
	pendingSectionHeader *table2UnifiedRow

	// Per-section collapse state, persisted across frames. Keyed by
	// composedSectionName (the same identity displayed in the section
	// header bar). A section is considered collapsed when its key maps
	// to true; absence/false = expanded.
	//
	// Keying by name (not sectionAccentIdx) survives schema-stable
	// re-streams: sectionAccentIdx is sectionIdx, which resets every
	// BeginBatch. Two consecutive frames over the same fixture would
	// re-issue the same accent idx; that works for the dedicated
	// fixture demo but a real consumer with mutable structure could
	// see ids drift, so we use the stable section identity instead.
	collapsedSections map[string]bool

	// OnTagClicked is preserved as a sink for downstream consumers (e.g.
	// the play detail pane wires a clipboard / filter pivot here), but the
	// unified-table cells render comma-joined chip strings, so the callback
	// never fires from this emitter.
	OnTagClicked func(displayText string, detail string)

	err error
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
	// rowKindSectionHeader is a "virtual" row inserted by the emitter at
	// each new section, rendered as a colored bar that visually groups
	// the data rows below it. Has no source representation in the leeway
	// stream — it's purely a UI affordance.
	rowKindSectionHeader
)

type table2NamedValue struct {
	name  string
	value string
}

// table2SectionStats accumulates per-section value-volume metrics for
// display in the section header bar. Only the values cell contributes:
// chips and the section/entity columns are layout sugar, not user
// payload, so excluding them gives a more honest "how heavy is this
// section's data" signal.
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

// NewTable2CardEmitter constructs a Table2CardEmitter with an optional
// membership classifier (membershiprole.DefaultClassifier{} when nil).
func NewTable2CardEmitter(ids *c.WidgetIdStack, palette ColorPaletteE, classifier membershiprole.ClassifierI) (inst *Table2CardEmitter) {
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
	if classifier == nil {
		classifier = membershiprole.DefaultClassifier{}
	}
	inst = &Table2CardEmitter{
		ids:               ids,
		classifier:        classifier,
		renderer:          membership.DefaultRenderer(),
		palette:           pal,
		unified:           make([]table2UnifiedRow, 0, 16),
		collapsedSections: make(map[string]bool, 8),
	}
	return
}

func (inst *Table2CardEmitter) nextId() *c.WidgetIdStack {
	inst.idCounter++
	return inst.ids.PrepareSeq(inst.idCounter)
}

func (inst *Table2CardEmitter) accentColor(idx int32) (col color.Color) {
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

// --- batch / entity ---

func (inst *Table2CardEmitter) BeginBatch() {
	inst.unified = inst.unified[:0]
	inst.entityIdx = 0
	inst.nEntities = 0
	inst.sectionIdx = 0
	inst.idCounter = 0x30000
	inst.inCoGroup = false
	inst.coGroupKey = ""
	inst.pendingSectionHeader = nil
}

// flushPendingHeader appends the deferred section-header row (if any)
// to the unified row list and clears the pending slot. Called from
// the data-row append paths so empty sections — those that produce
// no rowKindData entries — never get a header bar.
func (inst *Table2CardEmitter) flushPendingHeader() {
	if inst.pendingSectionHeader == nil {
		return
	}
	inst.unified = append(inst.unified, *inst.pendingSectionHeader)
	inst.pendingSectionHeader = nil
}

func (inst *Table2CardEmitter) EndBatch() (err error) {
	if len(inst.unified) > 0 {
		inst.flushUnified()
	}
	return inst.err
}

func (inst *Table2CardEmitter) BeginEntity() {
	if inst.entityIdx > 0 {
		inst.unified = append(inst.unified, table2UnifiedRow{
			kind:      rowKindEntitySep,
			entityIdx: inst.entityIdx,
		})
	}
}

func (inst *Table2CardEmitter) EndEntity() (err error) {
	inst.entityIdx++
	inst.nEntities = inst.entityIdx
	return inst.err
}

// --- plain section ---

func (inst *Table2CardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionType = sectionTypePlain
	inst.plainItemType = itemType
	inst.sectionName = naming.MustBeValidStylableName(itemType.String())
	inst.sectionUseAspects = useaspects.EmptyAspectSet
	inst.colNames = stylableNamesToStrings(valueNames)
	inst.colHidden = make([]bool, len(valueNames))
	inst.nAttrs = int32(nAttrs)
	inst.sectionIdx++
	inst.sectionAccentIdx = inst.sectionIdx
	inst.pendingSectionHeader = &table2UnifiedRow{
		kind:             rowKindSectionHeader,
		entityIdx:        inst.entityIdx,
		sectionType:      sectionTypePlain,
		sectionName:      inst.sectionName.String(),
		sectionAccentIdx: inst.sectionAccentIdx,
	}
}

func (inst *Table2CardEmitter) EndPlainSection() (err error) {
	return inst.err
}

func (inst *Table2CardEmitter) BeginPlainValue() {
	inst.currentRow = &table2UnifiedRow{
		kind:             rowKindData,
		entityIdx:        inst.entityIdx,
		sectionType:      inst.sectionType,
		sectionName:      inst.sectionName.String(),
		sectionAccentIdx: inst.sectionAccentIdx,
		valuePairs:       make([]table2NamedValue, 0, len(inst.colNames)),
	}
	inst.columnIdx = -1
}

// EndPlainValue fans the buffered plain attribute out into one row per
// visible value column. The column name occupies the primary cell — for
// plain sections the column name *is* the row's identity, so it takes the
// role a primary membership chip plays in tagged sections. The values cell
// then carries just the bare value (renderValuesCell drops the `name=`
// prefix when there is exactly one pair).
func (inst *Table2CardEmitter) EndPlainValue() (err error) {
	if inst.currentRow == nil {
		return inst.err
	}
	for _, p := range inst.currentRow.valuePairs {
		inst.flushPendingHeader()
		inst.unified = append(inst.unified, table2UnifiedRow{
			kind:             rowKindData,
			entityIdx:        inst.currentRow.entityIdx,
			sectionType:      inst.currentRow.sectionType,
			sectionName:      inst.currentRow.sectionName,
			sectionAccentIdx: inst.currentRow.sectionAccentIdx,
			primary:          []table2Tag{{display: p.name}},
			valuePairs:       []table2NamedValue{{name: "", value: p.value}},
		})
	}
	inst.currentRow = nil
	return inst.err
}

// --- tagged sections / co-groups ---

func (inst *Table2CardEmitter) BeginTaggedSections() {}

func (inst *Table2CardEmitter) EndTaggedSections() (err error) { return inst.err }

func (inst *Table2CardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.inCoGroup = true
	inst.coGroupKey = string(name)
}

func (inst *Table2CardEmitter) EndCoSectionGroup() (err error) {
	inst.inCoGroup = false
	inst.coGroupKey = ""
	return inst.err
}

func (inst *Table2CardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, useAspects useaspects.AspectSet, nAttrs int) {
	if inst.inCoGroup {
		inst.sectionType = sectionTypeCo
	} else {
		inst.sectionType = sectionTypeTagged
	}
	inst.sectionName = name
	inst.sectionUseAspects = useAspects
	inst.colNames = stylableNamesToStrings(valueNames)
	inst.colHidden = make([]bool, len(valueNames))
	inst.nAttrs = int32(nAttrs)
	inst.sectionIdx++
	inst.sectionAccentIdx = inst.sectionIdx
	inst.pendingSectionHeader = &table2UnifiedRow{
		kind:             rowKindSectionHeader,
		entityIdx:        inst.entityIdx,
		sectionType:      inst.sectionType,
		sectionName:      inst.composedSectionName(),
		sectionAccentIdx: inst.sectionAccentIdx,
	}
}

func (inst *Table2CardEmitter) EndSection() (err error) {
	return inst.err
}

func (inst *Table2CardEmitter) BeginTaggedValue() {
	inst.currentRow = &table2UnifiedRow{
		kind:             rowKindData,
		entityIdx:        inst.entityIdx,
		sectionType:      inst.sectionType,
		sectionName:      inst.composedSectionName(),
		sectionAccentIdx: inst.sectionAccentIdx,
		primary:          make([]table2Tag, 0, 2),
		secondary:        make([]table2Tag, 0, 2),
		valuePairs:       make([]table2NamedValue, 0, len(inst.colNames)),
	}
	inst.columnIdx = -1
}

func (inst *Table2CardEmitter) EndTaggedValue() (err error) {
	if inst.currentRow != nil {
		inst.flushPendingHeader()
		inst.unified = append(inst.unified, *inst.currentRow)
		inst.currentRow = nil
	}
	return inst.err
}

// composedSectionName formats the section name displayed in the section
// column. Co-section children are prefixed with the co-group key so the
// merged BeginSection's flattened identity remains visible.
func (inst *Table2CardEmitter) composedSectionName() string {
	name := inst.sectionName.String()
	if inst.sectionType == sectionTypeCo && inst.coGroupKey != "" {
		return fmt.Sprintf("%s · %s", inst.coGroupKey, name)
	}
	return name
}

// --- column / value ---

func (inst *Table2CardEmitter) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, _ naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, valueSemantics valueaspects.AspectSet) {
	inst.columnIdx++
	if int(inst.columnIdx) < len(inst.colHidden) {
		hidden := valueSemantics.Contains(valueaspects.AspectMachineReadable) && !valueSemantics.Contains(valueaspects.AspectHumanReadable)
		inst.colHidden[inst.columnIdx] = hidden
	}
	inst.cellBuf.Reset()
	inst.inCollection = false
}

func (inst *Table2CardEmitter) EndColumn() {
	if inst.currentRow == nil {
		return
	}
	if inst.columnIdx < 0 || int(inst.columnIdx) >= len(inst.colNames) {
		return
	}
	if inst.colHidden[inst.columnIdx] {
		inst.inCollection = false
		return
	}
	inst.currentRow.valuePairs = append(inst.currentRow.valuePairs, table2NamedValue{
		name:  inst.colNames[inst.columnIdx],
		value: utfsafe.EnsureUTF8(inst.cellBuf.String()),
	})
	inst.inCollection = false
}

func (inst *Table2CardEmitter) BeginScalarValue() {
	inst.inCollection = false
}

func (inst *Table2CardEmitter) EndScalarValue() (err error) { return inst.err }

func (inst *Table2CardEmitter) BeginHomogenousArrayValue(_ int) {
	inst.inCollection = true
}

func (inst *Table2CardEmitter) EndHomogenousArrayValue() {
	inst.inCollection = false
}

func (inst *Table2CardEmitter) BeginSetValue(_ int) {
	inst.inCollection = true
}

func (inst *Table2CardEmitter) EndSetValue() {
	inst.inCollection = false
}

// BeginValueItem joins multi-item collections inline with ", " — table rows
// have a single packed values cell, so vertical bullet stacks would need
// wrapping that doesn't translate; flat lists fit one row each.
func (inst *Table2CardEmitter) BeginValueItem(index int) {
	inst.itemIdx = int32(index)
	if index > 0 {
		inst.cellBuf.WriteString(", ")
	}
}

func (inst *Table2CardEmitter) EndValueItem() {}

func (inst *Table2CardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *Table2CardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	inst.cellBuf.WriteString(s)
	return
}

// --- memberships ---

func (inst *Table2CardEmitter) BeginTags(_ int) {}
func (inst *Table2CardEmitter) EndTags()        {}

func (inst *Table2CardEmitter) AddMembershipRef(lowCard bool, ref uint64) {
	mv := membership.MembershipValue{Kind: membership.MembershipKindRef, LowCard: lowCard, Ref: ref}
	inst.addMembership(mv, inst.renderer.RenderRef(ref), "", fmt.Sprintf("Ref ref=0x%x", ref))
}

func (inst *Table2CardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string) {
	mv := membership.MembershipValue{Kind: membership.MembershipKindVerbatim, LowCard: lowCard, Verbatim: verbatim}
	inst.addMembership(mv, inst.renderer.RenderVerbatim(verbatim), "", fmt.Sprintf("Verbatim value=%q", verbatim))
}

func (inst *Table2CardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	mv := membership.MembershipValue{Kind: membership.MembershipKindRefParametrized, LowCard: lowCard, Ref: ref, Params: params}
	inst.addMembership(mv, inst.renderer.RenderRef(ref), inst.renderer.RenderParams(params), fmt.Sprintf("RefParametrized ref=0x%x params=%q", ref, params))
}

func (inst *Table2CardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	mv := membership.MembershipValue{Kind: membership.MembershipKindMixedLowCardRefHighCardParam, Ref: ref, Params: params}
	inst.addMembership(mv, inst.renderer.RenderRef(ref), inst.renderer.RenderParams(params), fmt.Sprintf("MixedLowCardRefHighCardParam ref=0x%x params=%q", ref, params))
}

func (inst *Table2CardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	mv := membership.MembershipValue{Kind: membership.MembershipKindMixedLowCardVerbatimHighCardParam, Verbatim: verbatim, Params: params}
	inst.addMembership(mv, inst.renderer.RenderVerbatim(verbatim), inst.renderer.RenderParams(params), fmt.Sprintf("MixedLowCardVerbatimHighCardParam value=%q params=%q", verbatim, params))
}

func (inst *Table2CardEmitter) addMembership(mv membership.MembershipValue, label string, params string, detail string) {
	if inst.currentRow == nil {
		return
	}
	if membership.IsPlaceholder(mv) {
		return
	}
	display := label
	if params != "" {
		display = fmt.Sprintf("%s(%s)", label, params)
	}
	tag := table2Tag{display: display, detail: detail}
	role, _ := inst.classifier.Classify(membershiprole.SectionContext{Name: inst.sectionName, UseAspects: inst.sectionUseAspects}, mv)
	switch role {
	case membershiprole.MembershipRolePrimary:
		inst.currentRow.primary = append(inst.currentRow.primary, tag)
	default:
		inst.currentRow.secondary = append(inst.currentRow.secondary, tag)
	}
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
func (inst *Table2CardEmitter) flushUnified() {
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
	for tbl := range c.NewTable(inst.nextId()).
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
				renderHeaderCell("primary")
			}
			for range hdr.Col() {
				renderHeaderCell("secondary")
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
func (inst *Table2CardEmitter) renderSectionHeaderRow(
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
	clicked := false
	for col := uint32(0); col < nCols; col++ {
		for range r.Col() {
			cellId := inst.ids.PrepareSeq(0x60000 + uint64(row.sectionAccentIdx)*16 + uint64(col))
			ts := c.TintedScope(cellId, transparentFill).
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
func (inst *Table2CardEmitter) computeSectionStats() (stats map[string]*table2SectionStats, totalChars int32) {
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
	n := len(row.primary)
	if len(row.secondary) > n {
		n = len(row.secondary)
	}
	// Heuristic: ~2 packed `name=value` pairs fit per visual line in the
	// values column at table2ValuesColW.
	pairLines := (len(row.valuePairs) + 1) / 2
	if pairLines > n {
		n = pairLines
	}
	switch {
	case n >= 3:
		return table2RowHeightTriple
	case n == 2:
		return table2RowHeightDouble
	}
	return table2RowHeightSingle
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
func (inst *Table2CardEmitter) renderSectionCell(row *table2UnifiedRow) {
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
	if len(pairs) == 1 {
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
