//go:build llm_generated_opus46

package card

import (
	"fmt"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// SvgTopologySink is an SinkI that renders a compact SVG
// visualization of record batch topology.
//
// Layout: each entity is a horizontal row of section blocks.
// Each section block contains a micro-grid: one dot per attribute,
// colored by value/membership presence. Sections are separated by
// thin gaps. Co-groups share a tinted background band.
//
// Visual encoding:
//   Section width:  proportional to max(nCols, nAttrs), minimum 1 column
//   Attribute dot:  ● value+tags  ◐ value only  ○ tags only  · neither
//   Plain sections: solid bar, one cell per column
//   Co-groups:      subtle background highlight
//   Section label:  tiny text below the block

const (
	scale         = 4
	svgDotSize    = 6 * scale // diameter of attribute dot
	svgDotGap     = 2 * scale // gap between dots
	svgDotStride  = (svgDotSize + svgDotGap) * scale
	svgSectionGap = 10 * scale // horizontal gap between sections
	svgRowHeight  = 36 * scale // total height per entity row (dots + label)
	svgLabelH     = 10 * scale // height reserved for section label
	svgPadX       = 8 * scale  // left/right padding
	svgPadY       = 4 * scale  // top/bottom padding
	svgMaxDotsRow = 16 * scale // max dots per row before wrapping

	// Colors (muted, high contrast on white)
	svgColFull    = "#2d5a27" // value + tags: deep green
	svgColValue   = "#4a7c59" // value only: muted green
	svgColTags    = "#c17817" // tags only: amber
	svgColEmpty   = "#d0d0d0" // no content: light gray
	svgColPlain   = "#3a6ea5" // plain section: steel blue
	svgColCoGroup = "#f5f0e8" // co-group background: warm cream
	svgColLabel   = "#555555" // label text
	svgColBorder  = "#cccccc" // section border
)

// --- Data collection types ---

type svgSection struct {
	name      string
	nCols     int
	nAttrs    int
	attrs     []svgAttr
	isPlain   bool
	coGroupID string
}

type svgAttr struct {
	hasValue bool
	hasTags  bool
}

type svgEntity struct {
	sections []svgSection
}

// --- Sink ---

type SvgTopologySink struct {
	w   io.Writer
	err error

	entities []svgEntity

	// Current state
	curEntity  *svgEntity
	curSection *svgSection
	inPlain    bool
	inTagged   bool
	inCoGroup  bool
	coGroupKey string
	attrValue  bool
	attrTags   bool
	plainCols  int
}

func NewSvgTopologySink(w io.Writer) *SvgTopologySink {
	return &SvgTopologySink{w: w}
}

func (s *SvgTopologySink) Err() error { return s.err }

func (s *SvgTopologySink) emit(format string, args ...any) {
	if s.err != nil {
		return
	}
	_, s.err = fmt.Fprintf(s.w, format, args...)
}

// --- Batch ---

func (s *SvgTopologySink) BeginBatch() {
	s.entities = s.entities[:0]
}

func (s *SvgTopologySink) EndBatch() error {
	s.render()
	return s.err
}

// --- Entity ---

func (s *SvgTopologySink) BeginEntity() {
	s.entities = append(s.entities, svgEntity{})
	s.curEntity = &s.entities[len(s.entities)-1]
}

func (s *SvgTopologySink) EndEntity() error {
	s.curEntity = nil
	return nil
}

// --- Plain sections ---

func (s *SvgTopologySink) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	sec := svgSection{
		name:    shortItemType(itemType),
		nCols:   len(valueNames),
		nAttrs:  nAttrs,
		isPlain: true,
	}
	s.curEntity.sections = append(s.curEntity.sections, sec)
	s.curSection = &s.curEntity.sections[len(s.curEntity.sections)-1]
	s.inPlain = true
	s.plainCols = 0
}

func (s *SvgTopologySink) EndPlainSection() error {
	s.curSection.nAttrs = s.plainCols
	s.inPlain = false
	s.curSection = nil
	return nil
}

func (s *SvgTopologySink) BeginPlainValue()     {}
func (s *SvgTopologySink) EndPlainValue() error { return nil }

// --- Tagged sections ---

func (s *SvgTopologySink) BeginTaggedSections()     {}
func (s *SvgTopologySink) EndTaggedSections() error { return nil }

// --- Co-section groups ---

func (s *SvgTopologySink) BeginCoSectionGroup(name naming.Key) {
	s.inCoGroup = true
	s.coGroupKey = name.String()
}

func (s *SvgTopologySink) EndCoSectionGroup() error {
	s.inCoGroup = false
	s.coGroupKey = ""
	return nil
}

// --- Sections ---

func (s *SvgTopologySink) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	sec := svgSection{
		name:      name.String(),
		nCols:     len(valueNames),
		nAttrs:    nAttrs,
		coGroupID: s.coGroupKey,
	}
	s.curEntity.sections = append(s.curEntity.sections, sec)
	s.curSection = &s.curEntity.sections[len(s.curEntity.sections)-1]
}

func (s *SvgTopologySink) EndSection() error {
	s.curSection = nil
	return nil
}

// --- Tagged values ---

func (s *SvgTopologySink) BeginTaggedValue() {
	s.inTagged = true
	s.attrValue = false
	s.attrTags = false
}

func (s *SvgTopologySink) EndTaggedValue() error {
	if s.curSection != nil {
		s.curSection.attrs = append(s.curSection.attrs, svgAttr{
			hasValue: s.attrValue,
			hasTags:  s.attrTags,
		})
	}
	s.inTagged = false
	return nil
}

// --- Columns ---

func (s *SvgTopologySink) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	if s.inPlain {
		s.plainCols++
		if s.curSection != nil {
			s.curSection.attrs = append(s.curSection.attrs, svgAttr{hasValue: true})
		}
	}
}

func (s *SvgTopologySink) EndColumn() {}

// --- Value shapes ---

func (s *SvgTopologySink) BeginScalarValue() {
	if s.inTagged {
		s.attrValue = true
	}
}
func (s *SvgTopologySink) EndScalarValue() error { return nil }

func (s *SvgTopologySink) BeginHomogenousArrayValue(card int) {
	if s.inTagged && card > 0 {
		s.attrValue = true
	}
}
func (s *SvgTopologySink) EndHomogenousArrayValue() {}

func (s *SvgTopologySink) BeginSetValue(card int) {
	if s.inTagged && card > 0 {
		s.attrValue = true
	}
}
func (s *SvgTopologySink) EndSetValue() {}

func (s *SvgTopologySink) BeginValueItem(index int) {}
func (s *SvgTopologySink) EndValueItem()            {}

// --- Write (ignored) ---

func (s *SvgTopologySink) Write(p []byte) (n int, err error)         { return len(p), nil }
func (s *SvgTopologySink) WriteString(str string) (n int, err error) { return len(str), nil }

// --- Memberships ---

func (s *SvgTopologySink) BeginTags(nTags int) {
	if s.inTagged && nTags > 0 {
		s.attrTags = true
	}
}
func (s *SvgTopologySink) EndTags() {}

func (s *SvgTopologySink) AddMembershipRef(bool, uint64, string)                             {}
func (s *SvgTopologySink) AddMembershipVerbatim(bool, string, string)                        {}
func (s *SvgTopologySink) AddMembershipRefParametrized(bool, uint64, string, string, string) {}
func (s *SvgTopologySink) AddMembershipMixedLowCardRefHighCardParam(uint64, string, string, string) {
}
func (s *SvgTopologySink) AddMembershipMixedLowCardVerbatimHighCardParam(string, string, string, string) {
}

// ============================================================================
// SVG Rendering
// ============================================================================

func (s *SvgTopologySink) render() {
	if len(s.entities) == 0 {
		return
	}

	// Compute total width: scan all entities, find the widest row.
	maxW := 0
	for _, ent := range s.entities {
		w := s.entityWidth(ent)
		if w > maxW {
			maxW = w
		}
	}
	totalW := maxW + 2*svgPadX
	totalH := len(s.entities)*svgRowHeight + 2*svgPadY

	s.emit(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" `+
		`viewBox="0 0 %d %d" style="font-family:'Menlo','Consolas','DejaVu Sans Mono',monospace;font-size:7px">`,
		totalW, totalH, totalW, totalH)
	s.emit("\n")

	// Background
	s.emit(`<rect width="100%%" height="100%%" fill="#fafafa"/>`)
	s.emit("\n")

	// Render entities
	for i, ent := range s.entities {
		y := svgPadY + i*svgRowHeight
		s.renderEntity(ent, svgPadX, y)
	}

	s.emit("</svg>\n")
}

func (s *SvgTopologySink) entityWidth(ent svgEntity) int {
	w := 0
	for i, sec := range ent.sections {
		if i > 0 {
			w += svgSectionGap
		}
		w += s.sectionWidth(sec)
	}
	return w
}

func (s *SvgTopologySink) sectionWidth(sec svgSection) int {
	nDots := sec.nAttrs
	if nDots == 0 {
		nDots = 1 // minimum 1 dot width
	}
	cols := nDots
	if cols > svgMaxDotsRow {
		cols = svgMaxDotsRow
	}
	return cols * svgDotStride
}

func (s *SvgTopologySink) sectionDotRows(sec svgSection) int {
	if sec.nAttrs == 0 {
		return 1
	}
	return int(math.Ceil(float64(sec.nAttrs) / float64(svgMaxDotsRow)))
}

func (s *SvgTopologySink) renderEntity(ent svgEntity, x, y int) {
	// First pass: draw co-group backgrounds
	cx := x
	coStart := -1
	coKey := ""
	for i, sec := range ent.sections {
		if sec.coGroupID != "" && sec.coGroupID != coKey {
			// Start new co-group
			coStart = cx
			coKey = sec.coGroupID
		}
		secW := s.sectionWidth(sec)
		nextIsCoGroup := i+1 < len(ent.sections) && ent.sections[i+1].coGroupID == coKey
		if sec.coGroupID != "" && !nextIsCoGroup {
			// End co-group — draw background
			coEnd := cx + secW
			coW := coEnd - coStart
			s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="3" fill="%s" stroke="%s" stroke-width="0.5"/>`,
				coStart-2, y, coW+4, svgRowHeight-2, svgColCoGroup, svgColBorder)
			s.emit("\n")
			coKey = ""
			coStart = -1
		}
		cx += secW
		if i < len(ent.sections)-1 {
			cx += svgSectionGap
		}
	}

	// Second pass: draw sections
	cx = x
	for i, sec := range ent.sections {
		s.renderSection(sec, cx, y)
		cx += s.sectionWidth(sec)
		if i < len(ent.sections)-1 {
			cx += svgSectionGap
		}
	}
}

func (s *SvgTopologySink) renderSection(sec svgSection, x, y int) {
	dotAreaH := svgRowHeight - svgLabelH - 2
	dotsPerRow := svgMaxDotsRow

	if sec.isPlain {
		// Plain: draw solid colored bar per column
		for i := 0; i < sec.nAttrs; i++ {
			col := i % dotsPerRow
			row := i / dotsPerRow
			dx := x + col*svgDotStride + svgDotSize/2
			dy := y + 2 + row*(svgDotSize+1) + svgDotSize/2
			if dy+svgDotSize/2 > y+dotAreaH {
				break
			}
			s.emit(`<rect x="%d" y="%d" width="%d" height="%d" rx="1" fill="%s"/>`,
				dx-svgDotSize/2, dy-svgDotSize/2, svgDotSize, svgDotSize, svgColPlain)
			s.emit("\n")
		}
	} else {
		// Tagged: draw attribute dots
		for i := 0; i < sec.nAttrs; i++ {
			col := i % dotsPerRow
			row := i / dotsPerRow
			cx := x + col*svgDotStride + svgDotSize/2
			cy := y + 2 + row*(svgDotSize+1) + svgDotSize/2
			if cy+svgDotSize/2 > y+dotAreaH {
				break
			}

			hasVal := i < len(sec.attrs) && sec.attrs[i].hasValue
			hasTag := i < len(sec.attrs) && sec.attrs[i].hasTags

			r := svgDotSize / 2

			if hasVal && hasTag {
				// Full circle
				s.emit(`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`, cx, cy, r, svgColFull)
			} else if hasVal {
				// Left half (value) + right outline
				s.emit(`<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="0.5"/>`, cx, cy, r, svgColBorder)
				s.emit(`<path d="M%d,%d A%d,%d 0 0,0 %d,%d Z" fill="%s"/>`,
					cx, cy-r, r, r, cx, cy+r, svgColValue)
			} else if hasTag {
				// Right half (tags) + left outline
				s.emit(`<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="0.5"/>`, cx, cy, r, svgColBorder)
				s.emit(`<path d="M%d,%d A%d,%d 0 0,1 %d,%d Z" fill="%s"/>`,
					cx, cy-r, r, r, cx, cy+r, svgColTags)
			} else {
				// Empty dot
				s.emit(`<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="0.5"/>`, cx, cy, r, svgColEmpty)
			}
			s.emit("\n")
		}

		// Draw placeholder if empty section
		if sec.nAttrs == 0 {
			cx := x + svgDotSize/2
			cy := y + 2 + svgDotSize/2
			r := svgDotSize / 2
			s.emit(`<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="0.5" stroke-dasharray="2,2"/>`,
				cx, cy, r, svgColEmpty)
			s.emit("\n")
		}
	}

	// Label
	labelX := x + 1
	labelY := y + svgRowHeight - 2
	s.emit(`<text x="%d" y="%d" fill="%s" text-anchor="start">%s</text>`,
		labelX, labelY, svgColLabel, svgEscape(sec.name))
	s.emit("\n")
}

func svgEscape(str string) string {
	// Minimal XML escaping for text content
	var out []byte
	for _, b := range []byte(str) {
		switch b {
		case '<':
			out = append(out, []byte("&lt;")...)
		case '>':
			out = append(out, []byte("&gt;")...)
		case '&':
			out = append(out, []byte("&amp;")...)
		case '"':
			out = append(out, []byte("&quot;")...)
		default:
			out = append(out, b)
		}
	}
	return string(out)
}
