//go:build llm_generated_opus46

package card

import (
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// BrailleSpark is an SinkI that renders entity topology as a
// compact braille-art line per entity.
//
// Each tagged section becomes a group of braille characters separated by │.
// Within each braille character (2×4 dot grid), up to 4 attributes are
// encoded vertically:
//
//   Left column  (dots 1,2,3,7): attribute has value content
//   Right column (dots 4,5,6,8): attribute has membership tags
//
// Plain sections are shown as a prefix using block characters:
//   ▪ per plain scalar column
//
// Co-section groups are wrapped in ⟦ ⟧.
//
// The braille dot layout per character:
//
//   dot1 dot4     ← attribute 0
//   dot2 dot5     ← attribute 1
//   dot3 dot6     ← attribute 2
//   dot7 dot8     ← attribute 3
//
// Examples:
//   ▪▪│⣿⣶│⠶│⡇      3 plain cols, then sections with varying density
//   ▪│⟦⣶⠶⟧│⡗       plain col, co-group, standalone section

const (
	brailleDot1 = 0x01 // row 0, left
	brailleDot2 = 0x02 // row 1, left
	brailleDot3 = 0x04 // row 2, left
	brailleDot4 = 0x08 // row 0, right
	brailleDot5 = 0x10 // row 1, right
	brailleDot6 = 0x20 // row 2, right
	brailleDot7 = 0x40 // row 3, left
	brailleDot8 = 0x80 // row 3, right

	brailleBase = 0x2800

	// Left-column dots indexed by row (0–3)
	// Right-column dots indexed by row (0–3)
)

var leftDots = [4]byte{brailleDot1, brailleDot2, brailleDot3, brailleDot7}
var rightDots = [4]byte{brailleDot4, brailleDot5, brailleDot6, brailleDot8}

type BrailleSpark struct {
	w   io.Writer
	err error

	line strings.Builder

	// State
	inPlainSection bool
	inTaggedValue  bool
	inCoGroup      bool
	firstSection   bool // first tagged section in entity (for separator logic)
	wroteValue     bool // current attribute received value content
	wroteTags      bool // current attribute received membership tags
	nAttrs         int
	attrIdx        int

	// Per-section braille accumulator: one byte per braille character.
	// braille[i] encodes attributes 4*i .. 4*i+3.
	braille []byte
}

func NewBrailleSpark(w io.Writer) *BrailleSpark {
	return &BrailleSpark{w: w}
}

func (s *BrailleSpark) Err() error {
	return s.err
}

func (s *BrailleSpark) flush() {
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, s.line.String())
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, "\n")
}

func (s *BrailleSpark) emitBraille() {
	if len(s.braille) == 0 {
		s.line.WriteRune(rune(brailleBase)) // empty braille
		return
	}
	for _, b := range s.braille {
		s.line.WriteRune(rune(brailleBase + int(b)))
	}
}

func (s *BrailleSpark) setAttrLeft() {
	charIdx := s.attrIdx / 4
	rowIdx := s.attrIdx % 4
	for charIdx >= len(s.braille) {
		s.braille = append(s.braille, 0)
	}
	s.braille[charIdx] |= leftDots[rowIdx]
}

func (s *BrailleSpark) setAttrRight() {
	charIdx := s.attrIdx / 4
	rowIdx := s.attrIdx % 4
	for charIdx >= len(s.braille) {
		s.braille = append(s.braille, 0)
	}
	s.braille[charIdx] |= rightDots[rowIdx]
}

// --- Batch ---

func (s *BrailleSpark) BeginBatch() {}
func (s *BrailleSpark) EndBatch() error {
	return s.err
}

// --- Entity ---

func (s *BrailleSpark) BeginEntity() {
	s.line.Reset()
	s.firstSection = true
}

func (s *BrailleSpark) EndEntity() error {
	s.flush()
	return nil
}

// --- Plain sections ---

func (s *BrailleSpark) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	s.inPlainSection = true
}

func (s *BrailleSpark) EndPlainSection() error {
	s.inPlainSection = false
	return nil
}

func (s *BrailleSpark) BeginPlainValue()     {}
func (s *BrailleSpark) EndPlainValue() error { return nil }

// --- Tagged sections ---

func (s *BrailleSpark) BeginTaggedSections()     {}
func (s *BrailleSpark) EndTaggedSections() error { return nil }

// --- Co-section groups ---

func (s *BrailleSpark) BeginCoSectionGroup(name naming.Key) {
	if s.line.Len() > 0 {
		s.line.WriteString("│")
	}
	s.line.WriteString("⟦")
	s.inCoGroup = true
	s.firstSection = true // reset so first section inside group has no leading separator
}

func (s *BrailleSpark) EndCoSectionGroup() error {
	s.line.WriteString("⟧")
	s.inCoGroup = false
	s.firstSection = false
	return nil
}

// --- Sections ---

func (s *BrailleSpark) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	if !s.firstSection {
		if s.inCoGroup {
			s.line.WriteString("·")
		} else {
			s.line.WriteString("│")
		}
	}
	s.firstSection = false
	s.nAttrs = nAttrs
	s.attrIdx = 0
	s.braille = s.braille[:0]
}

func (s *BrailleSpark) EndSection() error {
	s.emitBraille()
	return nil
}

// --- Tagged values (attributes) ---

func (s *BrailleSpark) BeginTaggedValue() {
	s.inTaggedValue = true
	s.wroteValue = false
	s.wroteTags = false
}

func (s *BrailleSpark) EndTaggedValue() error {
	if s.wroteValue {
		s.setAttrLeft()
	}
	if s.wroteTags {
		s.setAttrRight()
	}
	s.attrIdx++
	s.inTaggedValue = false
	return nil
}

// --- Columns ---

func (s *BrailleSpark) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	if s.inPlainSection {
		s.line.WriteString("▪")
	}
}

func (s *BrailleSpark) EndColumn() {}

// --- Value shapes ---

func (s *BrailleSpark) BeginScalarValue() {
	if s.inTaggedValue {
		s.wroteValue = true
	}
}
func (s *BrailleSpark) EndScalarValue() error { return nil }

func (s *BrailleSpark) BeginHomogenousArrayValue(card int) {
	if s.inTaggedValue && card > 0 {
		s.wroteValue = true
	}
}
func (s *BrailleSpark) EndHomogenousArrayValue() {}

func (s *BrailleSpark) BeginSetValue(card int) {
	if s.inTaggedValue && card > 0 {
		s.wroteValue = true
	}
}
func (s *BrailleSpark) EndSetValue() {}

func (s *BrailleSpark) BeginValueItem(index int) {}
func (s *BrailleSpark) EndValueItem()            {}

// --- Write (ignored) ---

func (s *BrailleSpark) Write(p []byte) (n int, err error)         { return len(p), nil }
func (s *BrailleSpark) WriteString(str string) (n int, err error) { return len(str), nil }

// --- Memberships ---

func (s *BrailleSpark) BeginTags(nTags int) {
	if s.inTaggedValue && nTags > 0 {
		s.wroteTags = true
	}
}

func (s *BrailleSpark) EndTags() {}

func (s *BrailleSpark) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {}
func (s *BrailleSpark) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
}
func (s *BrailleSpark) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
}
func (s *BrailleSpark) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
}
func (s *BrailleSpark) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
}
