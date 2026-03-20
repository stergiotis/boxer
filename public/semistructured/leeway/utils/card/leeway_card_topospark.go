//go:build llm_generated_opus46

package card

import (
	"fmt"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// TopologySpark is an SinkI that writes a single-line unicode
// sparkline per entity to an io.Writer, summarizing topological structure.
//
// Legend:
//   ◆name          plain section with its columns
//   ◇N×⟨shape⟩     tagged section: N attributes, shape of first attribute
//   ◈key[...]      co-section group
//   s f64 u32 ...  column canonical types (short form)
//   ∥4             homogenous array of 4 elements
//   {3}            set of 3 elements
//   #2             2 membership tags
//   ˡ              low-card membership marker
//   ʰ              high-card membership marker
//   ᵐ              mixed membership marker
//   ·              column separator within an attribute
//
// Example output:
//   ◆id:y ◇3×⟨s #1ˡ⟩ ◇0×⟨⟩ ◇1×⟨f64 #1ˡ⟩ ◇1×⟨b #1ˡ⟩

type TopologySpark struct {
	w   io.Writer
	err error

	// Line buffer — accumulated during entity, flushed on EndEntity.
	line strings.Builder

	// State tracking
	inPlainSection bool
	inTaggedValue  bool
	inCoGroup      bool
	plainColIdx    int
	taggedColIdx   int
	nAttrs         int
	attrIdx        int

	// Per-attribute scratch buffers
	colBuf  strings.Builder
	tagsBuf strings.Builder
	nTags   int
}

func NewTopologySpark(w io.Writer) *TopologySpark {
	return &TopologySpark{w: w}
}

// Err returns the first write error encountered, if any.
func (s *TopologySpark) Err() error {
	return s.err
}

func (s *TopologySpark) flush() {
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, s.line.String())
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, "\n")
}

// --- Batch ---

func (s *TopologySpark) BeginBatch() {}

func (s *TopologySpark) EndBatch() error {
	return s.err
}

// --- Entity ---

func (s *TopologySpark) BeginEntity() {
	s.line.Reset()
}

func (s *TopologySpark) EndEntity() error {
	s.flush()
	return nil
}

// --- Plain sections ---

func (s *TopologySpark) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	if s.line.Len() > 0 {
		s.line.WriteByte(' ')
	}
	s.line.WriteString("◆")
	s.line.WriteString(shortItemType(itemType))
	s.inPlainSection = true
	s.plainColIdx = 0
}

func (s *TopologySpark) EndPlainSection() error {
	s.inPlainSection = false
	return nil
}

func (s *TopologySpark) BeginPlainValue()     {}
func (s *TopologySpark) EndPlainValue() error { return nil }

// --- Tagged sections ---

func (s *TopologySpark) BeginTaggedSections()     {}
func (s *TopologySpark) EndTaggedSections() error { return nil }

// --- Co-section groups ---

func (s *TopologySpark) BeginCoSectionGroup(name naming.Key) {
	if s.line.Len() > 0 {
		s.line.WriteByte(' ')
	}
	s.line.WriteString("◈")
	s.line.WriteString(name.String())
	s.line.WriteByte('[')
	s.inCoGroup = true
}

func (s *TopologySpark) EndCoSectionGroup() error {
	s.line.WriteByte(']')
	s.inCoGroup = false
	return nil
}

// --- Sections ---

func (s *TopologySpark) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	if !s.inCoGroup {
		if s.line.Len() > 0 {
			s.line.WriteByte(' ')
		}
	}
	s.line.WriteString("◇")
	s.nAttrs = nAttrs
	s.attrIdx = 0
	s.taggedColIdx = 0
	fmt.Fprintf(&s.line, "%d×", nAttrs)
}

func (s *TopologySpark) EndSection() error {
	return nil
}

// --- Tagged values (attributes) ---

func (s *TopologySpark) BeginTaggedValue() {
	s.colBuf.Reset()
	s.tagsBuf.Reset()
	s.taggedColIdx = 0
	s.nTags = 0
	s.inTaggedValue = true
}

func (s *TopologySpark) EndTaggedValue() error {
	if s.attrIdx == 0 {
		s.line.WriteString("⟨")
		s.line.WriteString(s.colBuf.String())
		if s.tagsBuf.Len() > 0 {
			if s.colBuf.Len() > 0 {
				s.line.WriteByte(' ')
			}
			s.line.WriteString(s.tagsBuf.String())
		}
		s.line.WriteString("⟩")
	}
	s.attrIdx++
	s.inTaggedValue = false
	return nil
}

// --- Columns ---

func (s *TopologySpark) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	if s.inPlainSection {
		if s.plainColIdx > 0 {
			s.line.WriteString("·")
		}
		s.line.WriteString(shortType(canonicalType))
		s.plainColIdx++
	} else if s.inTaggedValue && s.attrIdx == 0 {
		if s.taggedColIdx > 0 {
			s.colBuf.WriteString("·")
		}
		s.colBuf.WriteString(shortType(canonicalType))
		s.taggedColIdx++
	}
}

func (s *TopologySpark) EndColumn() {}

// --- Value shapes ---

func (s *TopologySpark) BeginScalarValue()     {}
func (s *TopologySpark) EndScalarValue() error { return nil }

func (s *TopologySpark) BeginHomogenousArrayValue(card int) {
	if s.inPlainSection {
		fmt.Fprintf(&s.line, "∥%d", card)
	} else if s.inTaggedValue && s.attrIdx == 0 {
		fmt.Fprintf(&s.colBuf, "∥%d", card)
	}
}

func (s *TopologySpark) EndHomogenousArrayValue() {}

func (s *TopologySpark) BeginSetValue(card int) {
	if s.inPlainSection {
		fmt.Fprintf(&s.line, "{%d}", card)
	} else if s.inTaggedValue && s.attrIdx == 0 {
		fmt.Fprintf(&s.colBuf, "{%d}", card)
	}
}

func (s *TopologySpark) EndSetValue() {}

func (s *TopologySpark) BeginValueItem(index int) {}
func (s *TopologySpark) EndValueItem()            {}

// --- Write (value content — ignored for topology) ---

func (s *TopologySpark) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (s *TopologySpark) WriteString(str string) (n int, err error) {
	return len(str), nil
}

// --- Memberships ---

func (s *TopologySpark) BeginTags(nTags int) {
	s.nTags = nTags
}

func (s *TopologySpark) EndTags() {
	if !s.inTaggedValue || s.attrIdx > 0 {
		return
	}
	if s.nTags > 0 {
		fmt.Fprintf(&s.tagsBuf, "#%d", s.nTags)
	}
}

func (s *TopologySpark) addMemberMarker(marker rune) {
	if !s.inTaggedValue || s.attrIdx > 0 {
		return
	}
	s.tagsBuf.WriteRune(marker)
}

func (s *TopologySpark) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	if lowCard {
		s.addMemberMarker('ˡ')
	} else {
		s.addMemberMarker('ʰ')
	}
}

func (s *TopologySpark) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	if lowCard {
		s.addMemberMarker('ˡ')
	} else {
		s.addMemberMarker('ʰ')
	}
}

func (s *TopologySpark) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	if lowCard {
		s.addMemberMarker('ˡ')
	} else {
		s.addMemberMarker('ʰ')
	}
}

func (s *TopologySpark) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	s.addMemberMarker('ᵐ')
}

func (s *TopologySpark) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	s.addMemberMarker('ᵐ')
}

// --- Type abbreviation helpers ---

func shortType(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return "?"
	}
	return ct.String()
}

func shortItemType(t common.PlainItemTypeE) string {
	switch t {
	case common.PlainItemTypeEntityId:
		return "id:"
	case common.PlainItemTypeEntityTimestamp:
		return "ts:"
	case common.PlainItemTypeEntityRouting:
		return "ro:"
	case common.PlainItemTypeEntityLifecycle:
		return "lc:"
	case common.PlainItemTypeTransaction:
		return "tx:"
	case common.PlainItemTypeOpaque:
		return "oq:"
	default:
		return "?:"
	}
}
