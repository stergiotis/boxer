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

// TreemapSpark is an SinkI that renders each entity's topology
// as a 3-line proportional box-drawing treemap.
//
// Each section gets a box whose width is proportional to its value column
// count (minimum: section name length + 2). The interior shows attribute
// density as a fill bar and a tag density indicator.
//
// Box styles:
//   ┌─┬─┐     single-line: regular sections
//   ╔═╤═╗     double-line: co-section groups
//
// Interior fill:
//   █  attribute with values + tags
//   ▓  attribute with values only
//   ░  attribute with tags only
//   ·  empty attribute slot (padding to max)
//
// Example (3 lines):
//   ┌─id─┬──nums───┬─labels─╠══geo════╗
//   │ ▪▪ │ ███▓▓·· │ ██░··  ║ ████ ██ ║
//   └────┴─────────┴────────╚═════════╝

// treemapCell collects per-section data during driving.
type treemapCell struct {
	name      string
	nCols     int // value column count (determines box width)
	nAttrs    int
	attrs     []attrInfo // one per attribute
	isCoGroup bool
	coGroupID string // co-group key (for grouping)
}

type attrInfo struct {
	hasValue bool
	hasTags  bool
}

type TreemapSpark struct {
	w   io.Writer
	err error

	// Per-entity state
	cells []*treemapCell

	// Current cell being built
	cur *treemapCell

	// Attribute tracking
	inPlainSection bool
	inTaggedValue  bool
	inCoGroup      bool
	coGroupKey     string
	curAttrValue   bool
	curAttrTags    bool
	plainColCount  int
}

func NewTreemapSpark(w io.Writer) *TreemapSpark {
	return &TreemapSpark{w: w}
}

func (s *TreemapSpark) Err() error {
	return s.err
}

func (s *TreemapSpark) write(str string) {
	if s.err != nil {
		return
	}
	_, s.err = io.WriteString(s.w, str)
}

// --- Rendering ---

func (s *TreemapSpark) render() {
	if len(s.cells) == 0 {
		s.write("(empty)\n")
		return
	}

	// Compute box widths: max(len(name)+2, nCols*2, 4)
	widths := make([]int, len(s.cells))
	for i, c := range s.cells {
		w := len(c.name) + 2
		if cw := c.nCols*2 + 2; cw > w {
			w = cw
		}
		if aw := c.nAttrs + 2; aw > w {
			w = aw
		}
		if w < 4 {
			w = 4
		}
		widths[i] = w
	}

	var top, mid, bot strings.Builder

	for i, c := range s.cells {
		w := widths[i]
		innerW := w - 2 // inside the borders

		isFirst := i == 0
		isLast := i == len(s.cells)-1
		prevCoGroup := ""
		if i > 0 {
			prevCoGroup = s.cells[i-1].coGroupID
		}

		enteringCoGroup := c.isCoGroup && (isFirst || prevCoGroup != c.coGroupID)
		leavingCoGroup := false
		if i > 0 && s.cells[i-1].isCoGroup {
			if !c.isCoGroup || c.coGroupID != s.cells[i-1].coGroupID {
				leavingCoGroup = true
			}
		}
		lastInCoGroup := c.isCoGroup && (isLast || !s.cells[i+1].isCoGroup || s.cells[i+1].coGroupID != c.coGroupID)

		// --- Top line ---
		if enteringCoGroup {
			if isFirst {
				top.WriteString("╔")
			} else if leavingCoGroup {
				top.WriteString("╠")
			} else {
				top.WriteString("╔")
			}
		} else if c.isCoGroup {
			top.WriteString("╤")
		} else {
			if isFirst {
				top.WriteString("┌")
			} else if leavingCoGroup {
				top.WriteString("╠")
			} else {
				top.WriteString("┬")
			}
		}

		// Center the name in the top border
		namePad := innerW - len(c.name)
		leftPad := namePad / 2
		rightPad := namePad - leftPad

		hChar := "─"
		if c.isCoGroup {
			hChar = "═"
		}
		top.WriteString(strings.Repeat(hChar, leftPad))
		top.WriteString(c.name)
		top.WriteString(strings.Repeat(hChar, rightPad))

		if isLast || (lastInCoGroup && (isLast || !s.cells[i+1].isCoGroup)) {
			if c.isCoGroup {
				top.WriteString("╗")
			} else {
				top.WriteString("┐")
			}
		}

		// --- Middle line (fill bar) ---
		if c.isCoGroup {
			if enteringCoGroup {
				mid.WriteString("║")
			} else {
				mid.WriteString("│")
			}
		} else {
			if leavingCoGroup {
				mid.WriteString("║")
			} else {
				mid.WriteString("│")
			}
		}

		// Build fill content
		var fill strings.Builder
		for a := 0; a < c.nAttrs && a < innerW; a++ {
			if a < len(c.attrs) {
				ai := c.attrs[a]
				if ai.hasValue && ai.hasTags {
					fill.WriteString("█")
				} else if ai.hasValue {
					fill.WriteString("▓")
				} else if ai.hasTags {
					fill.WriteString("░")
				} else {
					fill.WriteString("·")
				}
			} else {
				fill.WriteString("·")
			}
		}
		// Pad remaining width
		remaining := innerW - fill.Len()
		if remaining > 0 {
			fill.WriteString(strings.Repeat(" ", remaining))
		}
		mid.WriteString(fill.String())

		if isLast || (lastInCoGroup && (isLast || !s.cells[i+1].isCoGroup)) {
			if c.isCoGroup {
				mid.WriteString("║")
			} else {
				mid.WriteString("│")
			}
		}

		// --- Bottom line ---
		if enteringCoGroup {
			if isFirst {
				bot.WriteString("╚")
			} else if leavingCoGroup {
				bot.WriteString("╠")
			} else {
				bot.WriteString("╚")
			}
		} else if c.isCoGroup {
			bot.WriteString("╧")
		} else {
			if isFirst {
				bot.WriteString("└")
			} else if leavingCoGroup {
				bot.WriteString("╠")
			} else {
				bot.WriteString("┴")
			}
		}
		bot.WriteString(strings.Repeat(hChar, innerW))

		if isLast || (lastInCoGroup && (isLast || !s.cells[i+1].isCoGroup)) {
			if c.isCoGroup {
				bot.WriteString("╝")
			} else {
				bot.WriteString("┘")
			}
		}
	}

	s.write(top.String())
	s.write("\n")
	s.write(mid.String())
	s.write("\n")
	s.write(bot.String())
	s.write("\n")
}

// --- Batch ---

func (s *TreemapSpark) BeginBatch()     {}
func (s *TreemapSpark) EndBatch() error { return s.err }

// --- Entity ---

func (s *TreemapSpark) BeginEntity() {
	s.cells = s.cells[:0]
	s.cur = nil
}

func (s *TreemapSpark) EndEntity() error {
	s.render()
	return nil
}

// --- Plain sections ---

func (s *TreemapSpark) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	s.cur = &treemapCell{
		name:   shortItemTypeTreemap(itemType),
		nCols:  len(valueNames),
		nAttrs: nAttrs,
	}
	s.cells = append(s.cells, s.cur)
	s.inPlainSection = true
	s.plainColCount = 0
}

func (s *TreemapSpark) EndPlainSection() error {
	// For plain sections, each column is like an "attribute" with value
	s.cur.nAttrs = s.plainColCount
	s.inPlainSection = false
	s.cur = nil
	return nil
}

func (s *TreemapSpark) BeginPlainValue()     {}
func (s *TreemapSpark) EndPlainValue() error { return nil }

// --- Tagged sections ---

func (s *TreemapSpark) BeginTaggedSections()     {}
func (s *TreemapSpark) EndTaggedSections() error { return nil }

// --- Co-section groups ---

func (s *TreemapSpark) BeginCoSectionGroup(name naming.Key) {
	s.inCoGroup = true
	s.coGroupKey = name.String()
}

func (s *TreemapSpark) EndCoSectionGroup() error {
	s.inCoGroup = false
	s.coGroupKey = ""
	return nil
}

// --- Sections ---

func (s *TreemapSpark) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	s.cur = &treemapCell{
		name:      name.String(),
		nCols:     len(valueNames),
		nAttrs:    nAttrs,
		isCoGroup: s.inCoGroup,
		coGroupID: s.coGroupKey,
	}
	s.cells = append(s.cells, s.cur)
}

func (s *TreemapSpark) EndSection() error {
	s.cur = nil
	return nil
}

// --- Tagged values ---

func (s *TreemapSpark) BeginTaggedValue() {
	s.inTaggedValue = true
	s.curAttrValue = false
	s.curAttrTags = false
}

func (s *TreemapSpark) EndTaggedValue() error {
	if s.cur != nil {
		s.cur.attrs = append(s.cur.attrs, attrInfo{
			hasValue: s.curAttrValue,
			hasTags:  s.curAttrTags,
		})
	}
	s.inTaggedValue = false
	return nil
}

// --- Columns ---

func (s *TreemapSpark) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	if s.inPlainSection {
		s.plainColCount++
		if s.cur != nil {
			s.cur.attrs = append(s.cur.attrs, attrInfo{hasValue: true})
		}
	}
}

func (s *TreemapSpark) EndColumn() {}

// --- Value shapes ---

func (s *TreemapSpark) BeginScalarValue() {
	if s.inTaggedValue {
		s.curAttrValue = true
	}
}
func (s *TreemapSpark) EndScalarValue() error { return nil }

func (s *TreemapSpark) BeginHomogenousArrayValue(card int) {
	if s.inTaggedValue && card > 0 {
		s.curAttrValue = true
	}
}
func (s *TreemapSpark) EndHomogenousArrayValue() {}

func (s *TreemapSpark) BeginSetValue(card int) {
	if s.inTaggedValue && card > 0 {
		s.curAttrValue = true
	}
}
func (s *TreemapSpark) EndSetValue() {}

func (s *TreemapSpark) BeginValueItem(index int) {}
func (s *TreemapSpark) EndValueItem()            {}

// --- Write (ignored) ---

func (s *TreemapSpark) Write(p []byte) (n int, err error)         { return len(p), nil }
func (s *TreemapSpark) WriteString(str string) (n int, err error) { return len(str), nil }

// --- Memberships ---

func (s *TreemapSpark) BeginTags(nTags int) {
	if s.inTaggedValue && nTags > 0 {
		s.curAttrTags = true
	}
}

func (s *TreemapSpark) EndTags() {}

func (s *TreemapSpark) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {}
func (s *TreemapSpark) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
}
func (s *TreemapSpark) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
}
func (s *TreemapSpark) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
}
func (s *TreemapSpark) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
}

// shortItemType is shared with TopologySpark — duplicated here to keep
// the file self-contained. In production, extract to a shared helper.
func shortItemTypeTreemap(t common.PlainItemTypeE) string {
	switch t {
	case common.PlainItemTypeEntityId:
		return "id"
	case common.PlainItemTypeEntityTimestamp:
		return "ts"
	case common.PlainItemTypeEntityRouting:
		return "ro"
	case common.PlainItemTypeEntityLifecycle:
		return "lc"
	case common.PlainItemTypeTransaction:
		return "tx"
	case common.PlainItemTypeOpaque:
		return "oq"
	default:
		return "?"
	}
}
