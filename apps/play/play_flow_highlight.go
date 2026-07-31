package play

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_flow_highlight.go bridges the Flow tab's local selection to the SQL
// editor's styled-overlay channel (ADR-0130): clicking a clause node tints
// that clause's bytes in the buffer. Statement lens only — lens nodes carry
// no source ranges.
//
// Three coordinate systems meet here, and every hop re-verifies:
//   - flowNode.Start/End are relative to the split node's SQL fragment;
//   - splitNode.SrcOff places that fragment in its statement (or -1);
//   - the statement is located in the CURRENT buffer by text equality —
//     the split derives from the last Run, and the buffer may have moved on.
// The final guard compares the actual byte slices; any mismatch anywhere
// declines silently rather than tinting plausible-but-wrong text.

// styleFlowTint backs the clicked clause. Info-toned, alpha-blended: the
// palette has no mid-tone background token (the ToneSubqueryTint finding —
// every *Subtle sits within a few levels of the editor's near-black), and the
// accent alpha is already the subquery region's voice; info keeps the two
// regions distinguishable when both are on screen. The tone value is also not
// one the gutter recognises, so a flow tint earns no spurious gutter mark.
//
// designlint:ignore=L2 (no mid-tone background token exists; see above)
var styleFlowTint = color.RGBA(styletokens.InfoDefault.R,
	styletokens.InfoDefault.G, styletokens.InfoDefault.B, 0x40)

// flowSelectionSection maps the Flow tab's selected clause to a background
// section over inst.sql, or declines (any failed hop → no overlay). Rides
// editorStyledSections' quiescence gate like every other producer.
func (inst *PlayApp) flowSelectionSection() (sec codeview.StyledSection, ok bool) {
	fn, node, selOK := inst.flow.statementSelection()
	if !selOK || node.SrcOff < 0 || fn.End <= fn.Start || fn.Start < 0 || fn.End > len(node.SQL) {
		return
	}
	sink, sinkOK := findSplitNode(inst.currentSplit, inst.currentSplit.Sink)
	if !sinkOK {
		return
	}
	ranges, _ := inst.statementRanges()
	stmtStart, found := locateStatementStart(inst.sql, ranges, sink.SQL)
	if !found {
		return
	}
	start := stmtStart + node.SrcOff + fn.Start
	end := stmtStart + node.SrcOff + fn.End
	if start < 0 || end > len(inst.sql) || start >= end {
		return
	}
	// The ultimate guard: the buffer bytes must BE the clause bytes the graph
	// was derived from. Catches every stale-offset case the hops above can
	// construct (an edited buffer that still contains the statement text
	// elsewhere, a NodeText/source divergence, a moved prelude).
	if inst.sql[start:end] != node.SQL[fn.Start:fn.End] {
		return
	}
	return codeview.StyledSection{
		Start: uint32(start), Stop: uint32(end),
		Flags: codeview.StyleBackground,
		Color: styleFlowTint,
	}, true
}

// locateStatementStart finds where a split statement's (trimmed) text begins
// in the current buffer, by exact text equality against the buffer's own
// statement split. Equality — not containment — is the staleness gate: an
// edited statement simply stops matching. Duplicate identical statements
// resolve to the first, which tints identical text either way.
func locateStatementStart(buffer string, ranges []statementRange, stmtText string) (start int, ok bool) {
	if stmtText == "" {
		return 0, false
	}
	for _, r := range ranges {
		if r.Src.Start < 0 || r.Src.End > len(buffer) || r.Src.Start >= r.Src.End {
			continue
		}
		slice := buffer[r.Src.Start:r.Src.End]
		if strings.TrimSpace(slice) != stmtText {
			continue
		}
		idx := strings.Index(slice, stmtText)
		if idx < 0 {
			continue
		}
		return r.Src.Start + idx, true
	}
	return 0, false
}
