//go:build llm_generated_opus46

package card

import (
	"fmt"
	"image/color"
	"io"
	"strings"

	"github.com/dim13/colormap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

var _ streamreadaccess.SinkI = (*TypstCardEmitter)(nil)

// TypstCardEmitter produces Typst markup (.typ) that compiles to a well-laid-out A4 landscape PDF.
//
// Architecture: accumulate-then-render (same as TextCardEmitter).
// Column data and tags are buffered per attribute card, then the complete section
// is rendered as Typst markup at EndSection/EndPlainSection.
//
// Compile the output with: typst compile output.typ output.pdf
type TypstCardEmitter struct {
	w       io.Writer
	palette color.Palette

	// Per-section state
	sectionName string
	sectionIdx  int
	colNames    []naming.StylableName
	nAttrs      int
	cards       []typstCard
	currentCard *typstCard

	// Per-column accumulation
	currentColName string
	currentColType string
	cellBuf        strings.Builder
	inCollection   bool
	collType       int // 1=array, 2=set
	itemIdx        int

	// Entity counter
	entityIdx int

	err error
}

type typstCard struct {
	columns []typstColumn
	tags    []typstTag
}

type typstColumn struct {
	name  string
	ctype string
	value string
}

type typstTag struct {
	display string
	detail  string
}

func NewTypstCardEmitter(w io.Writer, palette ColorPaletteE) (inst *TypstCardEmitter) {
	var pal color.Palette
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
	inst = &TypstCardEmitter{
		w:       w,
		palette: pal,
		cards:   make([]typstCard, 0, 16),
	}
	return
}

func (inst *TypstCardEmitter) sectionColorHex(idx int) string {
	n := len(inst.palette)
	if n == 0 {
		return "888888"
	}
	lo := n / 5
	hi := n * 4 / 5
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	pos := lo + (idx*37)%span
	if pos >= n {
		pos = pos % n
	}
	r, g, b, _ := inst.palette[pos].RGBA()
	return fmt.Sprintf("%02x%02x%02x", r>>8, g>>8, b>>8)
}

func (inst *TypstCardEmitter) write(s string) {
	if inst.err != nil {
		return
	}
	_, err := io.WriteString(inst.w, s)
	if err != nil {
		inst.err = err
	}
}

func (inst *TypstCardEmitter) writef(format string, args ...any) {
	if inst.err != nil {
		return
	}
	_, err := fmt.Fprintf(inst.w, format, args...)
	if err != nil {
		inst.err = err
	}
}

// --- Batch ---

func (inst *TypstCardEmitter) BeginBatch() {
	inst.entityIdx = 0
	inst.sectionIdx = 0
	inst.write(`// Leeway Record View — generated Typst source
// Compile: typst compile <this-file>.typ <output>.pdf

#set page(paper: "a4", flipped: true, margin: (x: 1.2cm, y: 1cm))
#set text(font: "IBM Plex Sans", size: 8pt, fill: luma(20%))
#set par(leading: 0.4em)

// --- Reusable components ---

#let accent-label(color, body) = {
  text(fill: rgb(color), weight: "bold", size: 7pt, font: "IBM Plex Mono")[#body]
}

#let kv-row(key, ktype, value) = {
  (
    text(fill: luma(45%), size: 7pt, font: "IBM Plex Mono")[#key #text(fill: luma(65%), size: 6pt)[#ktype]],
    text(fill: luma(15%), size: 7.5pt, font: "IBM Plex Mono", weight: "medium")[#value],
  )
}

#let tag-pill(color, body) = {
  box(
    fill: rgb(color).lighten(85%),
    stroke: 0.4pt + rgb(color).lighten(50%),
    radius: 2pt,
    inset: (x: 3pt, y: 1.5pt),
  )[#text(size: 6pt, fill: rgb(color).darken(20%), font: "IBM Plex Mono")[#body]]
}

#let attr-card(color, section-name, kv-pairs, tags) = {
  box(
    stroke: 0.5pt + luma(80%),
    radius: 3pt,
    inset: 0pt,
    width: auto,
  )[
    #box(fill: rgb(color).lighten(90%), width: 100%, inset: (x: 4pt, y: 2pt))[
      #text(fill: rgb(color), size: 6pt, font: "IBM Plex Mono", weight: "bold")[#section-name]
    ]
    #pad(x: 4pt, y: 3pt)[
      #table(
        columns: (auto, auto),
        stroke: none,
        inset: (x: 2pt, y: 1.5pt),
        column-gutter: 6pt,
        row-gutter: 0pt,
        ..kv-pairs.flatten()
      )
      #if tags.len() > 0 {
        v(2pt)
        line(length: 100%, stroke: 0.3pt + luma(85%))
        v(2pt)
        tags.join(h(3pt))
      }
    ]
  ]
}

// --- Content ---

`)
}

func (inst *TypstCardEmitter) EndBatch() (err error) {
	return inst.err
}

// --- Entity ---

func (inst *TypstCardEmitter) BeginEntity() {
	if inst.entityIdx > 0 {
		inst.write("\n#v(6pt)\n#line(length: 100%, stroke: 0.5pt + luma(75%))\n#v(4pt)\n")
	}
	inst.writef("#text(weight: \"bold\", size: 10pt)[Entity %d]\n#v(3pt)\n", inst.entityIdx)
	inst.entityIdx++
	inst.sectionIdx = 0
}

func (inst *TypstCardEmitter) EndEntity() (err error) {
	return inst.err
}

// --- Plain section ---

func (inst *TypstCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = itemType.String()
	inst.colNames = valueNames
	inst.nAttrs = nAttrs
	inst.cards = inst.cards[:0]
	inst.currentCard = nil
	inst.sectionIdx++
}

func (inst *TypstCardEmitter) EndPlainSection() (err error) {
	if inst.nAttrs == 0 || len(inst.cards) == 0 {
		return inst.err
	}
	inst.flushSection(false)
	return inst.err
}

// --- Plain value ---

func (inst *TypstCardEmitter) BeginPlainValue() {
	card := typstCard{
		columns: make([]typstColumn, 0, len(inst.colNames)),
	}
	inst.cards = append(inst.cards, card)
	inst.currentCard = &inst.cards[len(inst.cards)-1]
}

func (inst *TypstCardEmitter) EndPlainValue() (err error) {
	inst.currentCard = nil
	return inst.err
}

// --- Tagged sections scope ---

func (inst *TypstCardEmitter) BeginTaggedSections() {}

func (inst *TypstCardEmitter) EndTaggedSections() (err error) {
	return inst.err
}

// --- Co-section group ---

func (inst *TypstCardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.writef("#box(stroke: (dash: \"dashed\", paint: luma(75%)), radius: 3pt, inset: 4pt)[\n")
	inst.writef("#text(size: 6pt, fill: luma(55%), font: \"IBM Plex Mono\")[CO: %s]\n", typstEscape(string(name)))
}

func (inst *TypstCardEmitter) EndCoSectionGroup() (err error) {
	inst.write("]\n")
	return inst.err
}

// --- Section ---

func (inst *TypstCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.sectionName = name.String()
	inst.colNames = valueNames
	inst.nAttrs = nAttrs
	inst.cards = inst.cards[:0]
	inst.currentCard = nil
	inst.sectionIdx++
}

func (inst *TypstCardEmitter) EndSection() (err error) {
	if inst.nAttrs == 0 || len(inst.cards) == 0 {
		return inst.err
	}
	inst.flushSection(true)
	return inst.err
}

// --- Tagged value ---

func (inst *TypstCardEmitter) BeginTaggedValue() {
	card := typstCard{
		columns: make([]typstColumn, 0, len(inst.colNames)),
		tags:    make([]typstTag, 0, 2),
	}
	inst.cards = append(inst.cards, card)
	inst.currentCard = &inst.cards[len(inst.cards)-1]
}

func (inst *TypstCardEmitter) EndTaggedValue() (err error) {
	inst.currentCard = nil
	return inst.err
}

// --- Column ---

func (inst *TypstCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	inst.currentColName = name.String()
	if canonicalType != nil {
		inst.currentColType = canonicalType.String()
	} else {
		inst.currentColType = ""
	}
	inst.cellBuf.Reset()
	inst.inCollection = false
}

func (inst *TypstCardEmitter) EndColumn() {
	if inst.currentCard != nil {
		inst.currentCard.columns = append(inst.currentCard.columns, typstColumn{
			name:  inst.currentColName,
			ctype: inst.currentColType,
			value: inst.cellBuf.String(),
		})
	}
	inst.inCollection = false
}

// --- Scalar ---

func (inst *TypstCardEmitter) BeginScalarValue() {
	inst.inCollection = false
}

func (inst *TypstCardEmitter) EndScalarValue() (err error) {
	return inst.err
}

// --- Array ---

func (inst *TypstCardEmitter) BeginHomogenousArrayValue(card int) {
	inst.inCollection = true
	inst.collType = 1
}

func (inst *TypstCardEmitter) EndHomogenousArrayValue() {
	inst.inCollection = false
}

// --- Set ---

func (inst *TypstCardEmitter) BeginSetValue(card int) {
	inst.inCollection = true
	inst.collType = 2
}

func (inst *TypstCardEmitter) EndSetValue() {
	inst.inCollection = false
}

// --- Value item ---

func (inst *TypstCardEmitter) BeginValueItem(index int) {
	inst.itemIdx = index
	if inst.cellBuf.Len() > 0 {
		inst.cellBuf.WriteString("; ")
	}
	switch inst.collType {
	case 1:
		fmt.Fprintf(&inst.cellBuf, "[%d]", index)
	case 2:
		inst.cellBuf.WriteString("•")
	}
}

func (inst *TypstCardEmitter) EndValueItem() {}

// --- Write ---

func (inst *TypstCardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *TypstCardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	inst.cellBuf.WriteString(s)
	return
}

// --- Tags ---

func (inst *TypstCardEmitter) BeginTags(nTags int) {}
func (inst *TypstCardEmitter) EndTags()            {}

func (inst *TypstCardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	if inst.currentCard == nil {
		return
	}
	c := "H"
	if lowCard {
		c = "L"
	}
	inst.currentCard.tags = append(inst.currentCard.tags, typstTag{
		display: fmt.Sprintf("ref(%s):%s", c, humanReadableRef),
	})
}

func (inst *TypstCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	if inst.currentCard == nil {
		return
	}
	ca := "H"
	if lowCard {
		ca = "L"
	}
	inst.currentCard.tags = append(inst.currentCard.tags, typstTag{
		display: fmt.Sprintf("v(%s):%s", ca, humanReadableVerbatim),
	})
}

func (inst *TypstCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	ca := "H"
	if lowCard {
		ca = "L"
	}
	d := fmt.Sprintf("rp(%s):%s", ca, humanReadableRef)
	if humanReadableParams != "" {
		d = fmt.Sprintf("rp(%s):%s(%s)", ca, humanReadableRef, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, typstTag{display: d})
}

func (inst *TypstCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	d := fmt.Sprintf("mr:%s", humanReadableRef)
	if humanReadableParams != "" {
		d = fmt.Sprintf("mr:%s(%s)", humanReadableRef, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, typstTag{display: d})
}

func (inst *TypstCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	if inst.currentCard == nil {
		return
	}
	d := fmt.Sprintf("mv:%s", humanReadableVerbatim)
	if humanReadableParams != "" {
		d = fmt.Sprintf("mv:%s(%s)", humanReadableVerbatim, humanReadableParams)
	}
	inst.currentCard.tags = append(inst.currentCard.tags, typstTag{display: d})
}

// --- Rendering ---

func (inst *TypstCardEmitter) flushSection(hasTags bool) {
	colorHex := inst.sectionColorHex(inst.sectionIdx)

	// Section label
	sectionLabel := typstEscape(inst.sectionName)
	if hasTags && inst.nAttrs > 0 {
		sectionLabel = fmt.Sprintf("%s (%d)", sectionLabel, inst.nAttrs)
	}
	inst.writef("#accent-label(\"#%s\")[%s]\n", colorHex, sectionLabel)
	inst.write("#v(2pt)\n")

	// Cards in a wrapping grid layout
	inst.write("#{\n  let cards = (")
	for i := range inst.cards {
		if i > 0 {
			inst.write(", ")
		}
		card := &inst.cards[i]
		inst.writeCardExpr(card, hasTags, colorHex)
	}
	inst.write(",)\n") // trailing comma ensures single-element tuple is array

	inst.writef("  let ncols = calc.min(cards.len(), %d)\n", 4)
	inst.write("  grid(\n")
	inst.write("    columns: range(ncols).map(_ => auto),\n")
	inst.write("    gutter: 4pt,\n")
	inst.write("    ..cards,\n")
	inst.write("  )\n")
	inst.write("}\n")
	inst.write("#v(4pt)\n")
}

func (inst *TypstCardEmitter) writeCardExpr(card *typstCard, hasTags bool, colorHex string) {
	inst.write("attr-card(")
	inst.writef("\"#%s\", ", colorHex)
	inst.writef("\"%s\", ", typstEscape(inst.sectionName))

	// kv-pairs array
	inst.write("(")
	for j, col := range card.columns {
		if j > 0 {
			inst.write(", ")
		}
		inst.writef("kv-row(\"%s\", \"%s\", \"%s\")",
			typstEscape(col.name),
			typstEscape(col.ctype),
			typstEscape(col.value))
	}
	inst.write(",), ") // trailing comma → array even for single element

	// tags array
	if hasTags && len(card.tags) > 0 {
		inst.write("(")
		for j, tag := range card.tags {
			if j > 0 {
				inst.write(", ")
			}
			inst.writef("tag-pill(\"#%s\", \"%s\")", colorHex, typstEscape(tag.display))
		}
		inst.write(",)")
	} else {
		inst.write("()")
	}
	inst.write(")")
}

// --- Typst escaping ---

// typstEscape escapes special characters for Typst string literals.
// Inside Typst "..." strings, backslash and double-quote need escaping.
// We also replace newlines with spaces for single-line rendering.
func typstEscape(s string) string {
	var b strings.Builder
	needsBuild := false
	for _, r := range s {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '#' {
			needsBuild = true
			break
		}
	}
	if !needsBuild {
		return s
	}
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString(" ")
		case '\r':
			// skip
		case '#':
			b.WriteString("\\#")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
