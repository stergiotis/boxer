//go:build llm_generated_opus46

package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"
)

// NewRewriter creates a TokenStreamRewriter for the given parse result.
func NewRewriter(pr *ParseResult) *antlr.TokenStreamRewriter {
	return antlr.NewTokenStreamRewriter(pr.TokenStream)
}

// TrackedRewriter wraps a TokenStreamRewriter and detects overlapping modifications.
type TrackedRewriter struct {
	inner   *antlr.TokenStreamRewriter
	logger  zerolog.Logger
	regions []modifiedRegion
}

type modifiedRegion struct {
	start int
	stop  int
	op    string
}

// NewTrackedRewriter creates a TrackedRewriter that logs warnings on overlapping modifications.
func NewTrackedRewriter(pr *ParseResult, logger zerolog.Logger) (inst *TrackedRewriter) {
	inst = &TrackedRewriter{
		inner:   antlr.NewTokenStreamRewriter(pr.TokenStream),
		logger:  logger,
		regions: make([]modifiedRegion, 0, 16),
	}
	return
}

// Inner returns the underlying TokenStreamRewriter for use with GetText.
func (inst *TrackedRewriter) Inner() *antlr.TokenStreamRewriter {
	return inst.inner
}

func (inst *TrackedRewriter) checkOverlap(start int, stop int, op string) {
	for _, r := range inst.regions {
		if start <= r.stop && stop >= r.start {
			inst.logger.Warn().
				Int("newStart", start).
				Int("newStop", stop).
				Str("newOp", op).
				Int("existingStart", r.start).
				Int("existingStop", r.stop).
				Str("existingOp", r.op).
				Msg("overlapping token modification detected — later write wins silently")
		}
	}
	inst.regions = append(inst.regions, modifiedRegion{start: start, stop: stop, op: op})
}

// ReplaceDefault replaces tokens in range [start, stop] with text.
func (inst *TrackedRewriter) ReplaceDefault(start int, stop int, text string) {
	inst.checkOverlap(start, stop, "replace")
	inst.inner.ReplaceDefault(start, stop, text)
}

// DeleteDefault deletes tokens in range [start, stop].
func (inst *TrackedRewriter) DeleteDefault(start int, stop int) {
	inst.checkOverlap(start, stop, "delete")
	inst.inner.DeleteDefault(start, stop)
}

// InsertBeforeDefault inserts text before the token at index.
func (inst *TrackedRewriter) InsertBeforeDefault(index int, text string) {
	// Inserts don't overlap with range modifications in a conflicting way,
	// but multiple inserts at the same index can produce unexpected ordering.
	for _, r := range inst.regions {
		if r.op == "insertBefore" && r.start == index {
			inst.logger.Warn().
				Int("index", index).
				Msg("multiple InsertBefore at same token index — ordering may be unexpected")
		}
	}
	inst.regions = append(inst.regions, modifiedRegion{start: index, stop: index, op: "insertBefore"})
	inst.inner.InsertBeforeDefault(index, text)
}

// InsertAfterDefault inserts text after the token at index.
func (inst *TrackedRewriter) InsertAfterDefault(index int, text string) {
	for _, r := range inst.regions {
		if r.op == "insertAfter" && r.start == index {
			inst.logger.Warn().
				Int("index", index).
				Msg("multiple InsertAfter at same token index — ordering may be unexpected")
		}
	}
	inst.regions = append(inst.regions, modifiedRegion{start: index, stop: index, op: "insertAfter"})
	inst.inner.InsertAfterDefault(index, text)
}

// GetTextDefault emits the modified text.
func (inst *TrackedRewriter) GetTextDefault() string {
	return inst.inner.GetTextDefault()
}

// HasOverlaps returns true if any overlapping modifications were detected.
func (inst *TrackedRewriter) HasOverlaps() bool {
	for i := 0; i < len(inst.regions); i++ {
		for j := i + 1; j < len(inst.regions); j++ {
			ri := inst.regions[i]
			rj := inst.regions[j]
			if ri.op == "insertBefore" || ri.op == "insertAfter" ||
				rj.op == "insertBefore" || rj.op == "insertAfter" {
				continue
			}
			if ri.start <= rj.stop && ri.stop >= rj.start {
				return true
			}
		}
	}
	return false
}

// OverlapCount returns the number of overlapping modification pairs detected.
func (inst *TrackedRewriter) OverlapCount() (count int) {
	for i := 0; i < len(inst.regions); i++ {
		for j := i + 1; j < len(inst.regions); j++ {
			ri := inst.regions[i]
			rj := inst.regions[j]
			if ri.op == "insertBefore" || ri.op == "insertAfter" ||
				rj.op == "insertBefore" || rj.op == "insertAfter" {
				continue
			}
			if ri.start <= rj.stop && ri.stop >= rj.start {
				count++
			}
		}
	}
	return
}

// --- Convenience functions that work with both rewriter types ---

// ReplaceNode replaces all tokens spanned by a CST node with new text.
func ReplaceNode(rw *antlr.TokenStreamRewriter, node antlr.ParserRuleContext, text string) {
	start := node.GetStart().GetTokenIndex()
	stop := node.GetStop().GetTokenIndex()
	rw.ReplaceDefault(start, stop, text)
}

// DeleteNode removes all tokens spanned by a CST node.
func DeleteNode(rw *antlr.TokenStreamRewriter, node antlr.ParserRuleContext) {
	start := node.GetStart().GetTokenIndex()
	stop := node.GetStop().GetTokenIndex()
	rw.DeleteDefault(start, stop)
}

// InsertBefore inserts text before a CST node.
func InsertBefore(rw *antlr.TokenStreamRewriter, node antlr.ParserRuleContext, text string) {
	rw.InsertBeforeDefault(node.GetStart().GetTokenIndex(), text)
}

// InsertAfter inserts text after a CST node.
func InsertAfter(rw *antlr.TokenStreamRewriter, node antlr.ParserRuleContext, text string) {
	rw.InsertAfterDefault(node.GetStop().GetTokenIndex(), text)
}

// NodeText returns the original source text of a CST node (including whitespace from hidden channel).
func NodeText(pr *ParseResult, node antlr.ParserRuleContext) string {
	start := node.GetStart().GetTokenIndex()
	stop := node.GetStop().GetTokenIndex()
	return pr.TokenStream.GetTextFromInterval(antlr.NewInterval(start, stop))
}

// ReplaceToken replaces a single token by index.
func ReplaceToken(rw *antlr.TokenStreamRewriter, tokenIndex int, text string) {
	rw.ReplaceDefault(tokenIndex, tokenIndex, text)
}

// DeleteToken removes a single token by index.
func DeleteToken(rw *antlr.TokenStreamRewriter, tokenIndex int) {
	rw.DeleteDefault(tokenIndex, tokenIndex)
}

// GetText emits the modified text from the rewriter.
func GetText(rw *antlr.TokenStreamRewriter) string {
	return rw.GetTextDefault()
}

// --- TrackedRewriter convenience functions ---

// TrackedReplaceNode replaces all tokens spanned by a CST node with new text.
func TrackedReplaceNode(rw *TrackedRewriter, node antlr.ParserRuleContext, text string) {
	start := node.GetStart().GetTokenIndex()
	stop := node.GetStop().GetTokenIndex()
	rw.ReplaceDefault(start, stop, text)
}

// TrackedDeleteNode removes all tokens spanned by a CST node.
func TrackedDeleteNode(rw *TrackedRewriter, node antlr.ParserRuleContext) {
	start := node.GetStart().GetTokenIndex()
	stop := node.GetStop().GetTokenIndex()
	rw.DeleteDefault(start, stop)
}

// TrackedInsertBefore inserts text before a CST node.
func TrackedInsertBefore(rw *TrackedRewriter, node antlr.ParserRuleContext, text string) {
	rw.InsertBeforeDefault(node.GetStart().GetTokenIndex(), text)
}

// TrackedInsertAfter inserts text after a CST node.
func TrackedInsertAfter(rw *TrackedRewriter, node antlr.ParserRuleContext, text string) {
	rw.InsertAfterDefault(node.GetStop().GetTokenIndex(), text)
}

// TrackedReplaceToken replaces a single token by index.
func TrackedReplaceToken(rw *TrackedRewriter, tokenIndex int, text string) {
	rw.ReplaceDefault(tokenIndex, tokenIndex, text)
}

// TrackedDeleteToken removes a single token by index.
func TrackedDeleteToken(rw *TrackedRewriter, tokenIndex int) {
	rw.DeleteDefault(tokenIndex, tokenIndex)
}

// TrackedGetText emits the modified text from the tracked rewriter.
func TrackedGetText(rw *TrackedRewriter) string {
	return rw.GetTextDefault()
}
