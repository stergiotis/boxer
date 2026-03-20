//go:build llm_generated_opus46

package card

import (
	"fmt"
	"image/color"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// SvgCardEmitter renders Leeway entities as SVG "attribute cards",
// porting the visual design of HtmlCardEmitter to pure SVG.
//
// Two-pass approach: sink calls collect data into an intermediate
// structure, EndBatch() computes layout and emits SVG.

// --- Theme constants ---

const (
	svgcCardW       = 260 // card width
	svgcCardPad     = 8   // card inner padding
	svgcRowH        = 16  // key-value row height
	svgcAccentH     = 4   // accent bar height
	svgcTagH        = 16  // tag row height
	svgcTagPad      = 4   // padding above/below tags
	svgcCardGap     = 8   // gap between cards
	svgcSectionGap  = 20  // vertical gap between sections
	svgcEntityGap   = 24  // vertical gap between entities
	svgcLabelH      = 18  // section label height
	svgcMarginX     = 16  // left/right page margin
	svgcMarginY     = 12  // top/bottom page margin
	svgcRadius      = 6   // card corner radius
	svgcDotR        = 4   // section dot radius
	svgcMaxCardsRow = 4   // max cards per row before wrapping
	svgcFontSize    = 11  // main font size
	svgcFontSmall   = 9   // small font (types, tags)
	svgcFontLabel   = 11  // section label font
	svgcTagChipH    = 14  // tag chip height
	svgcTagChipPad  = 4   // tag chip horizontal padding
	svgcTagChipGap  = 3   // gap between tag chips
	svgcTagChipR    = 3   // tag chip corner radius
	svgcCoGroupPad  = 6   // co-group wrapper padding
)

// Dark theme colors (matching HtmlCardEmitter CSS)
const (
	svgcColBg         = "#0d0d11"
	svgcColBgEntity   = "#131318"
	svgcColBgCard     = "#1a1a22"
	svgcColFg         = "#c8c8d4"
	svgcColFgDim      = "#70708a"
	svgcColFgKey      = "#9090aa"
	svgcColFgVal      = "#e8e8f4"
	svgcColFgBright   = "#f4f4ff"
	svgcColFgType     = "#585870"
	svgcColBorder     = "#252535"
	svgcColTagBg      = "rgba(255,255,255,0.06)"
	svgcColTagBorder  = "#303045"
	svgcColCoGroupBg  = "rgba(255,255,255,0.02)"
	svgcColCoGroupBdr = "#252535"
)

// --- Palette ---

type SvgCardPaletteE int

const (
	SvgCardPaletteInferno SvgCardPaletteE = iota
	SvgCardPaletteViridis
	SvgCardPaletteMagma
	SvgCardPalettePlasma
)

// --- Intermediate data model ---

type svgcTag struct {
	prefix string // "ref(L)", "mv", etc.
	text   string // human-readable main text
	params string // optional params text
}

type svgcKV struct {
	key     string
	keyType string
	value   string // pre-escaped
}

type svgcAttrCard struct {
	kvs  []svgcKV
	tags []svgcTag
}

type svgcSection struct {
	name      string
	nAttrs    int
	isPlain   bool
	coGroupID string
	accent    string // hex color
	cards     []svgcAttrCard
}

type svgcEntity struct {
	index    int
	sections []svgcSection
}

// --- Sink ---

type SvgCardEmitter struct {
	w       io.Writer
	palette []color.Color
	err     error

	entities []svgcEntity

	// Build state
	curEntity  *svgcEntity
	curSection *svgcSection
	curCard    *svgcAttrCard
	sectionOrd int

	// Column state
	curColName string
	curColType string
	cellBuf    strings.Builder
	inColl     bool
	collType   int // 1=array, 2=set
	itemIdx    int

	// Tag accumulation
	curTags []svgcTag

	// Section tracking
	inPlain   bool
	inTagged  bool
	inCoGroup bool
	coGroupID string
}

func NewSvgCardEmitter(w io.Writer, palette SvgCardPaletteE) *SvgCardEmitter {
	pal := defaultPalette(palette)
	return &SvgCardEmitter{
		w:       w,
		palette: pal,
	}
}

func (s *SvgCardEmitter) Err() error { return s.err }

func (s *SvgCardEmitter) emit(format string, args ...any) {
	if s.err != nil {
		return
	}
	_, s.err = fmt.Fprintf(s.w, format, args...)
}

func (s *SvgCardEmitter) sectionColor(idx int) string {
	n := len(s.palette)
	if n == 0 {
		return "#888888"
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
	r, g, b, _ := s.palette[pos].RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// --- Batch ---

func (s *SvgCardEmitter) BeginBatch() {
	s.entities = s.entities[:0]
	s.sectionOrd = 0
}

func (s *SvgCardEmitter) EndBatch() error {
	s.render()
	return s.err
}

// --- Entity ---

func (s *SvgCardEmitter) BeginEntity() {
	s.entities = append(s.entities, svgcEntity{index: len(s.entities)})
	s.curEntity = &s.entities[len(s.entities)-1]
	s.sectionOrd = 0
}

func (s *SvgCardEmitter) EndEntity() error {
	s.curEntity = nil
	return nil
}

// --- Plain section ---

func (s *SvgCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	sec := svgcSection{
		name:    itemType.String(),
		nAttrs:  nAttrs,
		isPlain: true,
		accent:  s.sectionColor(s.sectionOrd),
	}
	s.sectionOrd++
	s.curEntity.sections = append(s.curEntity.sections, sec)
	s.curSection = &s.curEntity.sections[len(s.curEntity.sections)-1]
	s.inPlain = true
}

func (s *SvgCardEmitter) EndPlainSection() error {
	s.inPlain = false
	s.curSection = nil
	return nil
}

func (s *SvgCardEmitter) BeginPlainValue() {
	s.curCard = &svgcAttrCard{}
	s.curTags = s.curTags[:0]
}

func (s *SvgCardEmitter) EndPlainValue() error {
	if s.curSection != nil && s.curCard != nil {
		s.curSection.cards = append(s.curSection.cards, *s.curCard)
	}
	s.curCard = nil
	return nil
}

// --- Tagged sections ---

func (s *SvgCardEmitter) BeginTaggedSections()     {}
func (s *SvgCardEmitter) EndTaggedSections() error { return nil }

// --- Co-section group ---

func (s *SvgCardEmitter) BeginCoSectionGroup(name naming.Key) {
	s.inCoGroup = true
	s.coGroupID = name.String()
}

func (s *SvgCardEmitter) EndCoSectionGroup() error {
	s.inCoGroup = false
	s.coGroupID = ""
	return nil
}

// --- Section ---

func (s *SvgCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	sec := svgcSection{
		name:      name.String(),
		nAttrs:    nAttrs,
		accent:    s.sectionColor(s.sectionOrd),
		coGroupID: s.coGroupID,
	}
	s.sectionOrd++
	s.curEntity.sections = append(s.curEntity.sections, sec)
	s.curSection = &s.curEntity.sections[len(s.curEntity.sections)-1]
}

func (s *SvgCardEmitter) EndSection() error {
	s.curSection = nil
	return nil
}

// --- Tagged value ---

func (s *SvgCardEmitter) BeginTaggedValue() {
	s.curCard = &svgcAttrCard{}
	s.curTags = s.curTags[:0]
	s.inTagged = true
}

func (s *SvgCardEmitter) EndTaggedValue() error {
	if s.curCard != nil {
		s.curCard.tags = append(s.curCard.tags[:0], s.curTags...)
		if s.curSection != nil {
			s.curSection.cards = append(s.curSection.cards, *s.curCard)
		}
	}
	s.curCard = nil
	s.inTagged = false
	return nil
}

// --- Column ---

func (s *SvgCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	s.curColName = name.String()
	if canonicalType != nil {
		s.curColType = canonicalType.String()
	} else {
		s.curColType = ""
	}
	s.cellBuf.Reset()
	s.inColl = false
}

func (s *SvgCardEmitter) EndColumn() {
	if s.curCard != nil {
		s.curCard.kvs = append(s.curCard.kvs, svgcKV{
			key:     s.curColName,
			keyType: s.curColType,
			value:   s.cellBuf.String(),
		})
	}
}

// --- Scalar ---

func (s *SvgCardEmitter) BeginScalarValue()     {}
func (s *SvgCardEmitter) EndScalarValue() error { return nil }

// --- Array ---

func (s *SvgCardEmitter) BeginHomogenousArrayValue(card int) {
	s.inColl = true
	s.collType = 1
}

func (s *SvgCardEmitter) EndHomogenousArrayValue() {
	s.inColl = false
}

// --- Set ---

func (s *SvgCardEmitter) BeginSetValue(card int) {
	s.inColl = true
	s.collType = 2
}

func (s *SvgCardEmitter) EndSetValue() {
	s.inColl = false
}

// --- Value item ---

func (s *SvgCardEmitter) BeginValueItem(index int) {
	s.itemIdx = index
	if s.cellBuf.Len() > 0 {
		if s.collType == 1 {
			s.cellBuf.WriteString(", ")
		} else {
			s.cellBuf.WriteString(" · ")
		}
	}
	if s.collType == 1 {
		fmt.Fprintf(&s.cellBuf, "[%d]", index)
	}
}

func (s *SvgCardEmitter) EndValueItem() {}

// --- Write ---

func (s *SvgCardEmitter) Write(p []byte) (n int, err error) {
	return s.WriteString(string(p))
}

func (s *SvgCardEmitter) WriteString(str string) (n int, err error) {
	n = len(str)
	s.cellBuf.WriteString(svgEscapeCard(str))
	return
}

// --- Tags ---

func (s *SvgCardEmitter) BeginTags(nTags int) {}
func (s *SvgCardEmitter) EndTags()            {}

func (s *SvgCardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	s.curTags = append(s.curTags, svgcTag{prefix: "ref(" + c + ")", text: humanReadableRef})
}

func (s *SvgCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	s.curTags = append(s.curTags, svgcTag{prefix: "v(" + c + ")", text: humanReadableVerbatim})
}

func (s *SvgCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	c := "H"
	if lowCard {
		c = "L"
	}
	s.curTags = append(s.curTags, svgcTag{prefix: "rp(" + c + ")", text: humanReadableRef, params: humanReadableParams})
}

func (s *SvgCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	s.curTags = append(s.curTags, svgcTag{prefix: "mr", text: humanReadableRef, params: humanReadableParams})
}

func (s *SvgCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	s.curTags = append(s.curTags, svgcTag{prefix: "mv", text: humanReadableVerbatim, params: humanReadableParams})
}

// ============================================================================
// SVG Rendering (second pass)
// ============================================================================

func (s *SvgCardEmitter) render() {
	if len(s.entities) == 0 {
		return
	}

	// Compute total dimensions by laying out all entities.
	totalW, totalH, entityLayouts := s.computeLayout()

	s.emit(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		totalW, totalH, totalW, totalH)
	s.emit("\n<defs>\n")
	s.emit(`<style>`)
	s.emit(`.mono{font-family:'JetBrains Mono','Fira Code','Cascadia Code','SF Mono','Consolas',monospace}`)
	s.emit(`.sans{font-family:'Inter','Segoe UI',system-ui,sans-serif}`)
	s.emit("</style>\n</defs>\n")

	// Background
	s.emit(`<rect width="100%%" height="100%%" fill="%s"/>`, svgcColBg)
	s.emit("\n")

	for _, el := range entityLayouts {
		s.renderEntityLayout(el)
	}

	s.emit("</svg>\n")
}

// --- Layout types ---

type svgcCardLayout struct {
	x, y, w, h int
	card       *svgcAttrCard
	accent     string
}

type svgcSectionLayout struct {
	x, y, w, h int
	section    *svgcSection
	cards      []svgcCardLayout
}

type svgcCoGroupBox struct {
	x, y, w, h int
	label      string
}

type svgcEntityLayout struct {
	x, y, w, h int
	entity     *svgcEntity
	sections   []svgcSectionLayout
	coGroups   []svgcCoGroupBox
}

func (s *SvgCardEmitter) computeLayout() (totalW, totalH int, layouts []svgcEntityLayout) {
	y := svgcMarginY
	maxW := 0

	for i := range s.entities {
		ent := &s.entities[i]
		el := s.layoutEntity(ent, svgcMarginX, y)
		layouts = append(layouts, el)
		if el.w > maxW {
			maxW = el.w
		}
		y += el.h + svgcEntityGap
	}

	totalW = maxW + 2*svgcMarginX
	totalH = y - svgcEntityGap + svgcMarginY
	return
}

func (s *SvgCardEmitter) layoutEntity(ent *svgcEntity, x, y int) svgcEntityLayout {
	el := svgcEntityLayout{x: x, y: y, entity: ent}

	// Entity header
	curY := y + svgcLabelH + 4

	// Track co-group regions
	coGroupStart := make(map[string]int) // coGroupID → start Y
	coGroupX := make(map[string]int)

	for i := range ent.sections {
		sec := &ent.sections[i]
		sl := s.layoutSection(sec, x, curY)
		el.sections = append(el.sections, sl)

		// Track co-group extents
		if sec.coGroupID != "" {
			if _, ok := coGroupStart[sec.coGroupID]; !ok {
				coGroupStart[sec.coGroupID] = curY - svgcCoGroupPad
				coGroupX[sec.coGroupID] = x - svgcCoGroupPad
			}
		}

		// If this is the last section in a co-group, emit the co-group box
		nextCoGroup := ""
		if i+1 < len(ent.sections) {
			nextCoGroup = ent.sections[i+1].coGroupID
		}
		if sec.coGroupID != "" && nextCoGroup != sec.coGroupID {
			startY := coGroupStart[sec.coGroupID]
			endY := curY + sl.h + svgcCoGroupPad
			el.coGroups = append(el.coGroups, svgcCoGroupBox{
				x:     coGroupX[sec.coGroupID],
				y:     startY,
				w:     sl.w + 2*svgcCoGroupPad,
				h:     endY - startY,
				label: sec.coGroupID,
			})
		}

		curY += sl.h + svgcSectionGap
	}

	el.h = curY - y - svgcSectionGap
	el.w = 0
	for _, sl := range el.sections {
		if sl.w > el.w {
			el.w = sl.w
		}
	}
	return el
}

func (s *SvgCardEmitter) layoutSection(sec *svgcSection, x, y int) svgcSectionLayout {
	sl := svgcSectionLayout{x: x, y: y, section: sec}

	cardY := y + svgcLabelH
	cardX := x
	rowMaxH := 0
	colIdx := 0

	for i := range sec.cards {
		card := &sec.cards[i]
		ch := s.cardHeight(card)
		cw := svgcCardW

		if colIdx >= svgcMaxCardsRow {
			// Wrap to next row
			cardY += rowMaxH + svgcCardGap
			cardX = x
			colIdx = 0
			rowMaxH = 0
		}

		cl := svgcCardLayout{
			x:      cardX,
			y:      cardY,
			w:      cw,
			h:      ch,
			card:   card,
			accent: sec.accent,
		}
		sl.cards = append(sl.cards, cl)

		if ch > rowMaxH {
			rowMaxH = ch
		}
		cardX += cw + svgcCardGap
		colIdx++
	}

	// Compute section dimensions
	sl.h = (cardY + rowMaxH) - y
	if len(sec.cards) == 0 {
		sl.h = svgcLabelH
	}
	sl.w = 0
	for _, cl := range sl.cards {
		right := cl.x + cl.w - x
		if right > sl.w {
			sl.w = right
		}
	}
	if sl.w < svgcCardW {
		sl.w = svgcCardW
	}
	return sl
}

func (s *SvgCardEmitter) cardHeight(card *svgcAttrCard) int {
	h := svgcAccentH              // accent bar
	h += len(card.kvs) * svgcRowH // key-value rows
	h += svgcCardPad              // bottom padding for kv area
	if len(card.tags) > 0 {
		// Tags: estimate rows (simple: assume they all fit in one row for now)
		nTagRows := 1
		h += svgcTagPad + nTagRows*svgcTagH + svgcTagPad
	}
	return h
}

// --- Rendering ---

func (s *SvgCardEmitter) renderEntityLayout(el svgcEntityLayout) {
	// Entity label
	s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" font-weight="600" class="sans">Entity %d</text>`,
		el.x, el.y+svgcFontLabel, svgcColFgBright, svgcFontLabel, el.entity.index)
	s.emit("\n")

	// Co-group backgrounds
	for _, cg := range el.coGroups {
		s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="4" fill="%s" stroke="%s" stroke-width="1" stroke-dasharray="4,3"/>`,
			cg.x, cg.y, cg.w, cg.h, svgcColCoGroupBg, svgcColCoGroupBdr)
		s.emit("\n")
		s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" class="mono" opacity="0.5">co: %s</text>`,
			cg.x+4, cg.y+10, svgcColFgDim, svgcFontSmall, svgEscapeCard(cg.label))
		s.emit("\n")
	}

	// Sections
	for _, sl := range el.sections {
		s.renderSectionLayout(sl)
	}
}

func (s *SvgCardEmitter) renderSectionLayout(sl svgcSectionLayout) {
	sec := sl.section

	// Section label: dot + name + count
	dotCx := sl.x + svgcDotR
	dotCy := sl.y + svgcDotR + 3
	s.emit(`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`, dotCx, dotCy, svgcDotR, sec.accent)
	labelX := sl.x + svgcDotR*2 + 6
	s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" font-weight="600" class="mono">%s`,
		labelX, sl.y+svgcFontLabel, svgcColFgDim, svgcFontLabel, svgEscapeCard(sec.name))
	if !sec.isPlain && sec.nAttrs > 0 {
		s.emit(` <tspan fill="%s" font-size="%d" font-weight="400">(%d)</tspan>`, svgcColFgDim, svgcFontSmall, sec.nAttrs)
	}
	if sec.isPlain {
		s.emit(` <tspan fill="%s" font-size="%d" font-weight="400" opacity="0.4">PLAIN</tspan>`, svgcColFgDim, svgcFontSmall-1)
	}
	s.emit("</text>\n")

	// Cards
	for _, cl := range sl.cards {
		s.renderCard(cl)
	}
}

func (s *SvgCardEmitter) renderCard(cl svgcCardLayout) {
	// Card background
	s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s" stroke-width="0.5"/>`,
		cl.x, cl.y, cl.w, cl.h, svgcRadius, svgcColBgCard, svgcColBorder)
	s.emit("\n")

	// Accent bar
	s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s"/>`,
		cl.x, cl.y, cl.w, svgcAccentH, svgcRadius, cl.accent)
	// Cover bottom corners of accent (they stick out of the rounded card top)
	s.emit(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
		cl.x, cl.y+svgcAccentH-2, cl.w, 2, cl.accent)
	s.emit("\n")

	// Key-value rows
	kvY := cl.y + svgcAccentH + 2
	for i, kv := range cl.card.kvs {
		rowY := kvY + i*svgcRowH

		// Alternating row background
		if i%2 == 1 {
			s.emit(`<rect x="%d" y="%d" width="%d" height="%d" fill="rgba(255,255,255,0.02)"/>`,
				cl.x+1, rowY, cl.w-2, svgcRowH)
		}

		// Key + type
		s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" class="mono">%s`,
			cl.x+svgcCardPad, rowY+svgcFontSize, svgcColFgKey, svgcFontSize, svgEscapeCard(kv.key))
		if kv.keyType != "" {
			s.emit(`<tspan fill="%s" font-size="%d"> %s</tspan>`, svgcColFgType, svgcFontSmall, svgEscapeCard(kv.keyType))
		}
		s.emit("</text>\n")

		// Value (right-aligned area)
		valX := cl.x + cl.w/2 + 10
		valMaxW := cl.w - cl.w/2 - 10 - svgcCardPad
		valStr := kv.value
		if len(valStr) > valMaxW/6 { // rough char width estimate
			valStr = valStr[:valMaxW/6] + "…"
		}
		s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" class="mono">%s</text>`,
			valX, rowY+svgcFontSize, svgcColFgVal, svgcFontSize, valStr)
		s.emit("\n")
	}

	// Tags
	if len(cl.card.tags) > 0 {
		tagBaseY := kvY + len(cl.card.kvs)*svgcRowH + svgcTagPad

		// Separator line
		s.emit(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="0.5"/>`,
			cl.x+4, tagBaseY-2, cl.x+cl.w-4, tagBaseY-2, svgcColBorder)
		s.emit("\n")

		tagX := cl.x + svgcCardPad
		tagY := tagBaseY
		for _, tag := range cl.card.tags {
			label := tag.prefix + " " + tag.text
			if tag.params != "" {
				label += "(" + tag.params + ")"
			}
			chipW := len(label)*6 + 2*svgcTagChipPad
			if tagX+chipW > cl.x+cl.w-svgcCardPad {
				// Wrap
				tagX = cl.x + svgcCardPad
				tagY += svgcTagH
			}

			s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s" stroke-width="0.5"/>`,
				tagX, tagY, chipW, svgcTagChipH, svgcTagChipR, svgcColTagBg, svgcColTagBorder)
			s.emit(`<text x="%d" y="%d" fill="%s" font-size="%d" class="mono">%s</text>`,
				tagX+svgcTagChipPad, tagY+svgcFontSmall+1, svgcColFgDim, svgcFontSmall, svgEscapeCard(label))
			s.emit("\n")

			tagX += chipW + svgcTagChipGap
		}
	}
}

// --- Helpers ---

func svgEscapeCard(str string) string {
	var out strings.Builder
	for _, r := range str {
		switch r {
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '&':
			out.WriteString("&amp;")
		case '"':
			out.WriteString("&quot;")
		case '\'':
			out.WriteString("&#39;")
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func defaultPalette(p SvgCardPaletteE) []color.Color {
	// Inline a small perceptually uniform palette.
	// In production, use github.com/dim13/colormap.
	palettes := map[SvgCardPaletteE][]color.Color{
		SvgCardPaletteInferno: {
			color.RGBA{0, 0, 4, 255}, color.RGBA{40, 11, 84, 255}, color.RGBA{101, 21, 110, 255},
			color.RGBA{159, 42, 99, 255}, color.RGBA{212, 72, 66, 255}, color.RGBA{245, 125, 21, 255},
			color.RGBA{250, 193, 39, 255}, color.RGBA{252, 255, 164, 255},
		},
		SvgCardPaletteViridis: {
			color.RGBA{68, 1, 84, 255}, color.RGBA{72, 40, 120, 255}, color.RGBA{62, 73, 137, 255},
			color.RGBA{49, 104, 142, 255}, color.RGBA{38, 130, 142, 255}, color.RGBA{31, 158, 137, 255},
			color.RGBA{78, 195, 108, 255}, color.RGBA{158, 217, 56, 255}, color.RGBA{253, 231, 37, 255},
		},
		SvgCardPaletteMagma: {
			color.RGBA{0, 0, 4, 255}, color.RGBA{28, 16, 68, 255}, color.RGBA{79, 18, 123, 255},
			color.RGBA{136, 34, 106, 255}, color.RGBA{186, 54, 85, 255}, color.RGBA{227, 89, 51, 255},
			color.RGBA{249, 149, 65, 255}, color.RGBA{252, 230, 131, 255},
		},
		SvgCardPalettePlasma: {
			color.RGBA{13, 8, 135, 255}, color.RGBA{84, 2, 163, 255}, color.RGBA{139, 10, 165, 255},
			color.RGBA{185, 50, 137, 255}, color.RGBA{219, 92, 104, 255}, color.RGBA{244, 136, 73, 255},
			color.RGBA{254, 188, 43, 255}, color.RGBA{240, 249, 33, 255},
		},
	}
	if pal, ok := palettes[p]; ok {
		return pal
	}
	return palettes[SvgCardPaletteInferno]
}
