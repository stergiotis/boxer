//go:build llm_generated_opus46

package card

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

var _ streamreadaccess.SinkI = (*UnicodeCardEmitter)(nil)

// UnicodeEmitterConfig controls rendering limits and appearance.
type UnicodeEmitterConfig struct {
	// MaxColumnWidth caps individual column width in runes. 0 = unlimited.
	MaxColumnWidth int
	// MaxCollectionItems caps displayed items per array/set. 0 = unlimited.
	MaxCollectionItems int
	// MaxAttributesPerSection caps displayed attributes per section. 0 = unlimited.
	MaxAttributesPerSection int
	// ShowRowSeparators draws a light dotted line between multi-line attribute rows.
	ShowRowSeparators bool
	// NullString is the text used for null values.
	NullString string
}

// DefaultUnicodeEmitterConfig returns a sensible default config.
func DefaultUnicodeEmitterConfig() UnicodeEmitterConfig {
	return UnicodeEmitterConfig{
		MaxColumnWidth:          60,
		MaxCollectionItems:      20,
		MaxAttributesPerSection: 100,
		ShowRowSeparators:       true,
		NullString:              "∅",
	}
}

// UnicodeCardEmitter renders Leeway entities as compact unicode box-drawing tables.
// Implements StructuredOutput2I.
//
// Names are emitted using StylableName.String() — the IR is assumed to carry
// the desired naming style already.
type UnicodeCardEmitter struct {
	w     io.Writer
	width int
	cfg   UnicodeEmitterConfig

	// Per-section accumulation
	sectionName string
	colNames    []naming.StylableName
	colTypes    []canonicaltypes.PrimitiveAstNodeI
	nAttrs      int
	rows        []textRow

	// Current row being built
	currentRow *textRow
	currentCol int

	// Collection state
	inCollection     bool
	collectionType   int // 0=scalar, 1=array, 2=set
	itemIndex        int
	collectionCard   int // total items in current collection
	itemsEmitted     int // items emitted so far in current collection
	collectionCapped bool

	// Entity counter
	entityIdx     int
	attrCount     int // attributes emitted in current section
	sectionCapped bool

	// Scratch buffers
	lineBuf []byte

	err error
}

type textRow struct {
	cells []textCell
	tags  []string
}

type textCell struct {
	lines []string
}

func NewUnicodeCardEmitter(w io.Writer, width int) *UnicodeCardEmitter {
	return NewUnicodeCardEmitterWithConfig(w, width, DefaultUnicodeEmitterConfig())
}

func NewUnicodeCardEmitterWithConfig(w io.Writer, width int, cfg UnicodeEmitterConfig) *UnicodeCardEmitter {
	return &UnicodeCardEmitter{
		w:       w,
		width:   width,
		cfg:     cfg,
		lineBuf: make([]byte, 0, 512),
		rows:    make([]textRow, 0, 16),
	}
}

// --- Batch ---

func (inst *UnicodeCardEmitter) BeginBatch() {
	inst.entityIdx = 0
}

func (inst *UnicodeCardEmitter) EndBatch() (err error) {
	return inst.err
}

// --- Entity ---

func (inst *UnicodeCardEmitter) BeginEntity() {
	if inst.entityIdx > 0 {
		inst.writeRaw([]byte("\n"))
	}
	inst.writef("━━━ Entity %d ━━━\n", inst.entityIdx)
	inst.entityIdx++
}

func (inst *UnicodeCardEmitter) EndEntity() (err error) {
	return inst.err
}

// --- Plain section ---

func (inst *UnicodeCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = itemType.String()
	inst.colNames = valueNames
	inst.colTypes = valueCanonicalTypes
	inst.nAttrs = nAttrs
	inst.rows = inst.rows[:0]
	inst.currentRow = nil
	inst.attrCount = 0
	inst.sectionCapped = false
}

func (inst *UnicodeCardEmitter) EndPlainSection() (err error) {
	if inst.nAttrs == 0 {
		return inst.err
	}
	inst.flushSection()
	return inst.err
}

// --- Plain value ---

func (inst *UnicodeCardEmitter) BeginPlainValue() {
	inst.attrCount++
	nCols := len(inst.colNames)
	row := textRow{
		cells: make([]textCell, nCols),
	}
	inst.rows = append(inst.rows, row)
	inst.currentRow = &inst.rows[len(inst.rows)-1]
}

func (inst *UnicodeCardEmitter) EndPlainValue() (err error) {
	inst.currentRow = nil
	return inst.err
}

// --- Tagged sections scope ---

func (inst *UnicodeCardEmitter) BeginTaggedSections() {
	// No-op for text output — plain and tagged sections render sequentially
}

func (inst *UnicodeCardEmitter) EndTaggedSections() (err error) {
	return inst.err
}

// --- Co-section group ---

func (inst *UnicodeCardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.writef("┌─ co: %s ─┐\n", sanitize(string(name)))
}

func (inst *UnicodeCardEmitter) EndCoSectionGroup() (err error) {
	return inst.err
}

// --- Section ---

func (inst *UnicodeCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = name.String()
	inst.colNames = valueNames
	inst.colTypes = valueCanonicalTypes
	inst.nAttrs = nAttrs
	inst.rows = inst.rows[:0]
	inst.currentRow = nil
	inst.attrCount = 0
	inst.sectionCapped = false
}

func (inst *UnicodeCardEmitter) EndSection() (err error) {
	if inst.nAttrs == 0 {
		inst.writef("  ◇ %s (empty)\n", inst.sectionName)
		return inst.err
	}
	inst.flushSection()
	return inst.err
}

// --- Tagged value ---

func (inst *UnicodeCardEmitter) BeginTaggedValue() {
	inst.attrCount++
	if inst.cfg.MaxAttributesPerSection > 0 && inst.attrCount > inst.cfg.MaxAttributesPerSection {
		inst.sectionCapped = true
		inst.currentRow = nil
		return
	}
	nCols := len(inst.colNames)
	row := textRow{
		cells: make([]textCell, nCols),
		tags:  make([]string, 0, 2),
	}
	inst.rows = append(inst.rows, row)
	inst.currentRow = &inst.rows[len(inst.rows)-1]
}

func (inst *UnicodeCardEmitter) EndTaggedValue() (err error) {
	inst.currentRow = nil
	return inst.err
}

// --- Column ---

func (inst *UnicodeCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	inst.currentCol = -1
	for i, n := range inst.colNames {
		if n == name {
			inst.currentCol = i
			break
		}
	}
	inst.inCollection = false
}

func (inst *UnicodeCardEmitter) EndColumn() {
	inst.inCollection = false
}

// --- Scalar value ---

func (inst *UnicodeCardEmitter) BeginScalarValue() {
	inst.inCollection = false
	inst.collectionType = 0
}

func (inst *UnicodeCardEmitter) EndScalarValue() (err error) {
	return inst.err
}

// --- Array value ---

func (inst *UnicodeCardEmitter) BeginHomogenousArrayValue(card int) {
	inst.inCollection = true
	inst.collectionType = 1
	inst.collectionCard = card
	inst.itemsEmitted = 0
	inst.collectionCapped = false
}

func (inst *UnicodeCardEmitter) EndHomogenousArrayValue() {
	if inst.collectionCapped && inst.currentRow != nil && inst.currentCol >= 0 && inst.currentCol < len(inst.currentRow.cells) {
		remaining := inst.collectionCard - inst.itemsEmitted
		cell := &inst.currentRow.cells[inst.currentCol]
		cell.lines = append(cell.lines, fmt.Sprintf("… (%d more)", remaining))
	}
	inst.inCollection = false
}

// --- Set value ---

func (inst *UnicodeCardEmitter) BeginSetValue(card int) {
	inst.inCollection = true
	inst.collectionType = 2
	inst.collectionCard = card
	inst.itemsEmitted = 0
	inst.collectionCapped = false
}

func (inst *UnicodeCardEmitter) EndSetValue() {
	if inst.collectionCapped && inst.currentRow != nil && inst.currentCol >= 0 && inst.currentCol < len(inst.currentRow.cells) {
		remaining := inst.collectionCard - inst.itemsEmitted
		cell := &inst.currentRow.cells[inst.currentCol]
		cell.lines = append(cell.lines, fmt.Sprintf("… (%d more)", remaining))
	}
	inst.inCollection = false
}

// --- Value item ---

func (inst *UnicodeCardEmitter) BeginValueItem(index int) {
	inst.itemIndex = index
}

func (inst *UnicodeCardEmitter) EndValueItem() {}

// --- Write ---

func (inst *UnicodeCardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *UnicodeCardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	if inst.currentRow == nil || inst.currentCol < 0 || inst.currentCol >= len(inst.currentRow.cells) {
		return
	}

	if inst.inCollection {
		if inst.cfg.MaxCollectionItems > 0 && inst.itemsEmitted >= inst.cfg.MaxCollectionItems {
			inst.collectionCapped = true
			return
		}
		inst.itemsEmitted++
	}

	cell := &inst.currentRow.cells[inst.currentCol]
	text := sanitize(s)

	if inst.inCollection {
		switch inst.collectionType {
		case 1: // array
			text = fmt.Sprintf("[%d] %s", inst.itemIndex, text)
		case 2: // set
			text = "• " + text
		}
	}

	cell.lines = append(cell.lines, text)
	return
}

// --- Tags ---

func (inst *UnicodeCardEmitter) BeginTags(nTags int) {}
func (inst *UnicodeCardEmitter) EndTags()            {}

func (inst *UnicodeCardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	if inst.currentRow == nil {
		return
	}
	card := "H"
	if lowCard {
		card = "L"
	}
	inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("ref(%s):%s", card, sanitize(humanReadableRef)))
}

func (inst *UnicodeCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	if inst.currentRow == nil {
		return
	}
	card := "H"
	if lowCard {
		card = "L"
	}
	inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("v(%s):%s", card, sanitize(humanReadableVerbatim)))
}

func (inst *UnicodeCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentRow == nil {
		return
	}
	card := "H"
	if lowCard {
		card = "L"
	}
	if humanReadableParams != "" {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("rp(%s):%s(%s)", card, sanitize(humanReadableRef), sanitize(humanReadableParams)))
	} else {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("rp(%s):%s", card, sanitize(humanReadableRef)))
	}
}

func (inst *UnicodeCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentRow == nil {
		return
	}
	if humanReadableParams != "" {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("mr:%s(%s)", sanitize(humanReadableRef), sanitize(humanReadableParams)))
	} else {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("mr:%s", sanitize(humanReadableRef)))
	}
}

func (inst *UnicodeCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	if inst.currentRow == nil {
		return
	}
	if humanReadableParams != "" {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("mv:%s(%s)", sanitize(humanReadableVerbatim), sanitize(humanReadableParams)))
	} else {
		inst.currentRow.tags = append(inst.currentRow.tags, fmt.Sprintf("mv:%s", sanitize(humanReadableVerbatim)))
	}
}

// --- Flush / Render ---

func (inst *UnicodeCardEmitter) flushSection() {
	const minimalWidth = 24
	nCols := len(inst.colNames)
	if nCols == 0 {
		inst.flushVoidSection()
		return
	}

	maxColW := inst.cfg.MaxColumnWidth
	if maxColW <= 0 {
		maxColW = 1<<31 - 1
	}

	// Render names and types to strings for width computation and display
	colNameStrs := make([]string, nCols)
	colTypeStrs := make([]string, nCols)
	for c := range nCols {
		colNameStrs[c] = inst.colNames[c].String()
		if c < len(inst.colTypes) && inst.colTypes[c] != nil {
			colTypeStrs[c] = inst.colTypes[c].String()
		}
	}

	// Compute column widths (capped)
	colWidths := make([]int, nCols)
	for c := range nCols {
		colWidths[c] = runeWidth(colNameStrs[c])
		if w := runeWidth(colTypeStrs[c]); w > colWidths[c] {
			colWidths[c] = w
		}
	}
	for r := range inst.rows {
		row := &inst.rows[r]
		for c := range min(nCols, len(row.cells)) {
			for _, line := range row.cells[c].lines {
				if w := runeWidth(line); w > colWidths[c] {
					colWidths[c] = w
				}
			}
		}
	}
	for c := range colWidths {
		if colWidths[c] > maxColW {
			colWidths[c] = maxColW
		}
		if colWidths[c] < minimalWidth {
			colWidths[c] = minimalWidth
		}
	}

	inst.applyWidthBudget(colWidths)

	// Section header
	inst.writeTopBorder(inst.sectionName, colWidths)
	inst.writeRowCells(colNameStrs, colWidths)
	inst.writeRowCells(colTypeStrs, colWidths)
	inst.writeMidBorder(colWidths)

	// Data rows
	for r := range inst.rows {
		row := &inst.rows[r]

		if r > 0 && inst.cfg.ShowRowSeparators {
			inst.writeDottedSeparator(colWidths)
		}

		maxLines := 1
		for c := range min(nCols, len(row.cells)) {
			if n := len(row.cells[c].lines); n > maxLines {
				maxLines = n
			}
		}
		for lineIdx := range maxLines {
			cells := make([]string, nCols)
			for c := range nCols {
				if c < len(row.cells) && lineIdx < len(row.cells[c].lines) {
					cells[c] = row.cells[c].lines[lineIdx]
				}
			}
			inst.writeRowCells(cells, colWidths)
		}

		if len(row.tags) > 0 {
			tagStr := "  ╰ " + strings.Join(row.tags, "  ")
			totalInner := inst.totalInnerWidth(colWidths)
			if runeWidth(tagStr) > totalInner {
				tagStr = truncateToWidth(tagStr, totalInner-1) + "…"
			}
			inst.writeSpanningLine(tagStr, colWidths)
		}
	}

	if inst.sectionCapped {
		omitted := inst.nAttrs - inst.cfg.MaxAttributesPerSection
		text := fmt.Sprintf("  … %d more attributes (of %d total)", omitted, inst.nAttrs)
		totalInner := inst.totalInnerWidth(colWidths)
		if runeWidth(text) > totalInner {
			text = truncateToWidth(text, totalInner-1) + "…"
		}
		inst.writeSpanningLine(text, colWidths)
	}

	inst.writeBottomBorder(colWidths)
}

func (inst *UnicodeCardEmitter) flushVoidSection() {
	minWidth := runeWidth(inst.sectionName) + 6
	tagWidth := 0
	for r := range inst.rows {
		for _, tag := range inst.rows[r].tags {
			if w := runeWidth(tag); w > tagWidth {
				tagWidth = w
			}
		}
	}
	boxWidth := max(minWidth, tagWidth+4)
	colWidths := []int{boxWidth}

	inst.writeTopBorder(inst.sectionName, colWidths)
	inst.writeMidBorder(colWidths)
	for r := range inst.rows {
		for _, tag := range inst.rows[r].tags {
			inst.writeRowCells([]string{tag}, colWidths)
		}
	}
	inst.writeBottomBorder(colWidths)
}

// applyWidthBudget shrinks columns proportionally if the total table width exceeds inst.width.
func (inst *UnicodeCardEmitter) applyWidthBudget(colWidths []int) {
	if inst.width <= 0 {
		return
	}
	totalW := inst.totalInnerWidth(colWidths) + 2
	if totalW <= inst.width {
		return
	}
	excess := totalW - inst.width
	totalContent := 0
	for _, w := range colWidths {
		totalContent += w
	}
	if totalContent <= 0 {
		return
	}
	for c := range colWidths {
		shrink := (colWidths[c] * excess) / totalContent
		colWidths[c] -= shrink
		if colWidths[c] < 4 {
			colWidths[c] = 4
		}
	}
}

// --- Box drawing primitives ---

func (inst *UnicodeCardEmitter) writeTopBorder(title string, colWidths []int) {
	totalInner := inst.totalInnerWidth(colWidths)
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "╭─ "...)
	titleSan := sanitize(title)
	inst.lineBuf = append(inst.lineBuf, titleSan...)
	inst.lineBuf = append(inst.lineBuf, ' ')
	remaining := totalInner - runeWidth(titleSan) - 3
	if remaining < 1 {
		remaining = 1
	}
	for range remaining {
		inst.lineBuf = append(inst.lineBuf, "─"...)
	}
	inst.lineBuf = append(inst.lineBuf, "╮\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) writeMidBorder(colWidths []int) {
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "├"...)
	for c, w := range colWidths {
		if c > 0 {
			inst.lineBuf = append(inst.lineBuf, "┼"...)
		}
		for range w + 2 {
			inst.lineBuf = append(inst.lineBuf, "─"...)
		}
	}
	inst.lineBuf = append(inst.lineBuf, "┤\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) writeDottedSeparator(colWidths []int) {
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "│"...)
	for c, w := range colWidths {
		if c > 0 {
			inst.lineBuf = append(inst.lineBuf, "┊"...)
		}
		for range w + 2 {
			inst.lineBuf = append(inst.lineBuf, "┄"...)
		}
	}
	inst.lineBuf = append(inst.lineBuf, "│\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) writeBottomBorder(colWidths []int) {
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "╰"...)
	for c, w := range colWidths {
		if c > 0 {
			inst.lineBuf = append(inst.lineBuf, "┴"...)
		}
		for range w + 2 {
			inst.lineBuf = append(inst.lineBuf, "─"...)
		}
	}
	inst.lineBuf = append(inst.lineBuf, "╯\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) writeRowCells(cells []string, colWidths []int) {
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "│"...)
	for c := range colWidths {
		inst.lineBuf = append(inst.lineBuf, ' ')
		cell := ""
		if c < len(cells) {
			cell = cells[c]
		}
		cellW := runeWidth(cell)
		if cellW > colWidths[c] {
			cell = truncateToWidth(cell, colWidths[c]-1) + "…"
			cellW = colWidths[c]
		}
		inst.lineBuf = append(inst.lineBuf, cell...)
		pad := colWidths[c] - cellW
		if pad > 0 {
			inst.lineBuf = append(inst.lineBuf, strings.Repeat(" ", pad)...)
		}
		inst.lineBuf = append(inst.lineBuf, ' ')
		if c < len(colWidths)-1 {
			inst.lineBuf = append(inst.lineBuf, "│"...)
		}
	}
	inst.lineBuf = append(inst.lineBuf, "│\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) writeSpanningLine(text string, colWidths []int) {
	totalInner := inst.totalInnerWidth(colWidths)
	inst.lineBuf = inst.lineBuf[:0]
	inst.lineBuf = append(inst.lineBuf, "│"...)
	textW := runeWidth(text)
	if textW > totalInner {
		text = truncateToWidth(text, totalInner-1) + "…"
		textW = totalInner
	}
	inst.lineBuf = append(inst.lineBuf, text...)
	pad := totalInner - textW
	if pad > 0 {
		inst.lineBuf = append(inst.lineBuf, strings.Repeat(" ", pad)...)
	}
	inst.lineBuf = append(inst.lineBuf, "│\n"...)
	inst.writeRaw(inst.lineBuf)
}

func (inst *UnicodeCardEmitter) totalInnerWidth(colWidths []int) (total int) {
	for c, w := range colWidths {
		total += w + 2
		if c > 0 {
			total += 1
		}
	}
	return
}

// --- Output helpers ---

func (inst *UnicodeCardEmitter) writef(format string, args ...any) {
	if inst.err != nil {
		return
	}
	_, err := fmt.Fprintf(inst.w, format, args...)
	if err != nil {
		inst.err = err
	}
}

func (inst *UnicodeCardEmitter) writeRaw(buf []byte) {
	if inst.err != nil {
		return
	}
	_, err := inst.w.Write(buf)
	if err != nil {
		inst.err = err
	}
}

// --- Text utilities ---

func runeWidth(s string) int {
	return utf8.RuneCountInString(s)
}

func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runeWidth(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	if maxWidth > len(runes) {
		return s
	}
	return string(runes[:maxWidth])
}

// sanitize replaces control characters, newlines, and tabs with safe representations.
func sanitize(s string) string {
	needsBuild := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsControl(r) && r != ' ') {
			needsBuild = true
			break
		}
	}
	if !needsBuild {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString("\\n")
		case r == '\r':
			b.WriteString("\\r")
		case r == '\t':
			b.WriteString("\\t")
		case unicode.IsControl(r) && r != ' ':
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
