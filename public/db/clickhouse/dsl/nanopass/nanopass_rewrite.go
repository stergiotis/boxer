//go:build llm_generated_opus46

package nanopass

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"
)

// RewriterI is the write-and-emit surface shared by
// *antlr.TokenStreamRewriter and *TrackedRewriter. The node-level helpers
// (ReplaceNode, DeleteNode, …) accept either.
type RewriterI interface {
	ReplaceDefault(from, to int, text string)
	DeleteDefault(from, to int)
	InsertBeforeDefault(index int, text string)
	InsertAfterDefault(index int, text string)
	GetTextDefault() string
}

var _ RewriterI = (*antlr.TokenStreamRewriter)(nil)
var _ RewriterI = (*TrackedRewriter)(nil)

// NewRewriter creates a TokenStreamRewriter for the given parse result.
func NewRewriter(pr *ParseResult) *antlr.TokenStreamRewriter {
	return antlr.NewTokenStreamRewriter(pr.TokenStream)
}

// TrackedRewriter wraps a TokenStreamRewriter and classifies conflicting
// modifications as they are recorded.
//
// The underlying ANTLR rewriter resolves multi-op interactions only at
// GetText time, with these semantics (antlr4-go v4.13.x):
//
//   - replace ranges that partially overlap (neither contains the other):
//     panic at GetText.
//   - replace fully containing an earlier replace: the inner op is dropped
//     silently.
//   - overlapping deletes: merged into one delete.
//   - insert recorded after a replace, at an index strictly inside the
//     replace range: panic at GetText. At exactly the replace start the
//     texts merge.
//   - insert recorded before a replace whose range covers it: the insert is
//     dropped silently (merged when at the replace start).
//   - multiple inserts at the same index: texts concatenate.
//
// TrackedRewriter logs fatal combinations (the ones that panic at GetText)
// at error level and benign-but-lossy ones (silent drops, merges) at warn
// level, at the moment the second op is recorded — i.e. with the offending
// call still on the stack. HasConflicts/ConflictCount report the fatal
// combinations.
//
// Note panics from the underlying rewriter are converted to errors at the
// Pass boundary (see Pass.Run / applyWithProps), so a conflicting pass
// fails its Run instead of crashing the process.
type TrackedRewriter struct {
	inner   *antlr.TokenStreamRewriter
	logger  zerolog.Logger
	regions []modifiedRegion
}

type opKind uint8

const (
	opReplace opKind = iota
	opDelete
	opInsertBefore
	opInsertAfter
)

func (k opKind) String() string {
	switch k {
	case opReplace:
		return "replace"
	case opDelete:
		return "delete"
	case opInsertBefore:
		return "insertBefore"
	default:
		return "insertAfter"
	}
}

func (k opKind) isInsert() bool { return k == opInsertBefore || k == opInsertAfter }

type modifiedRegion struct {
	start int
	stop  int
	op    opKind
}

// conflictKind classifies the interaction of a later op with an earlier one.
type conflictKind uint8

const (
	conflictNone conflictKind = iota
	// conflictFatal combinations panic inside GetText.
	conflictFatal
	// conflictLossy combinations are resolved silently by the ANTLR rewriter
	// (an op is dropped or merged); the result may not be what the caller
	// intended.
	conflictLossy
)

// classifyConflict reports how the ANTLR rewriter will treat later given
// that earlier was recorded first.
func classifyConflict(earlier, later modifiedRegion) conflictKind {
	switch {
	case !earlier.op.isInsert() && !later.op.isInsert():
		disjoint := earlier.stop < later.start || earlier.start > later.stop
		if disjoint {
			return conflictNone
		}
		if earlier.op == opDelete && later.op == opDelete {
			return conflictLossy // merged into one delete
		}
		contained := earlier.start >= later.start && earlier.stop <= later.stop
		if contained {
			return conflictLossy // earlier op dropped silently
		}
		return conflictFatal // partial overlap → panic at GetText

	case earlier.op.isInsert() && !later.op.isInsert():
		// Insert first, then a replace/delete covering it: dropped or merged.
		if later.start <= earlier.start && earlier.start <= later.stop {
			return conflictLossy
		}
		return conflictNone

	case !earlier.op.isInsert() && later.op.isInsert():
		// Replace/delete first, then an insert into its range.
		if earlier.start < later.start && later.start <= earlier.stop {
			return conflictFatal // panic at GetText
		}
		if later.start == earlier.start {
			return conflictLossy // texts merge
		}
		return conflictNone

	default: // both inserts
		if earlier.start == later.start {
			return conflictLossy // texts concatenate, ordering surprises
		}
		return conflictNone
	}
}

// NewTrackedRewriter creates a TrackedRewriter that logs conflicting
// modifications.
func NewTrackedRewriter(pr *ParseResult, logger zerolog.Logger) (inst *TrackedRewriter) {
	inst = &TrackedRewriter{
		inner:   antlr.NewTokenStreamRewriter(pr.TokenStream),
		logger:  logger,
		regions: make([]modifiedRegion, 0, 16),
	}
	return
}

// Inner returns the underlying TokenStreamRewriter. Modifications made
// directly on it bypass conflict tracking — HasConflicts cannot see them.
func (inst *TrackedRewriter) Inner() *antlr.TokenStreamRewriter {
	return inst.inner
}

func (inst *TrackedRewriter) record(r modifiedRegion) {
	for _, prev := range inst.regions {
		switch classifyConflict(prev, r) {
		case conflictFatal:
			inst.logger.Error().
				Int("newStart", r.start).Int("newStop", r.stop).Str("newOp", r.op.String()).
				Int("existingStart", prev.start).Int("existingStop", prev.stop).Str("existingOp", prev.op.String()).
				Msg("conflicting token modifications — GetText will panic (recovered to an error at the Pass boundary)")
		case conflictLossy:
			inst.logger.Warn().
				Int("newStart", r.start).Int("newStop", r.stop).Str("newOp", r.op.String()).
				Int("existingStart", prev.start).Int("existingStop", prev.stop).Str("existingOp", prev.op.String()).
				Msg("overlapping token modifications — one op will be silently dropped or merged at GetText")
		}
	}
	inst.regions = append(inst.regions, r)
}

// ReplaceDefault replaces tokens in range [start, stop] with text.
func (inst *TrackedRewriter) ReplaceDefault(start int, stop int, text string) {
	inst.record(modifiedRegion{start: start, stop: stop, op: opReplace})
	inst.inner.ReplaceDefault(start, stop, text)
}

// DeleteDefault deletes tokens in range [start, stop].
func (inst *TrackedRewriter) DeleteDefault(start int, stop int) {
	inst.record(modifiedRegion{start: start, stop: stop, op: opDelete})
	inst.inner.DeleteDefault(start, stop)
}

// InsertBeforeDefault inserts text before the token at index.
func (inst *TrackedRewriter) InsertBeforeDefault(index int, text string) {
	inst.record(modifiedRegion{start: index, stop: index, op: opInsertBefore})
	inst.inner.InsertBeforeDefault(index, text)
}

// InsertAfterDefault inserts text after the token at index.
func (inst *TrackedRewriter) InsertAfterDefault(index int, text string) {
	inst.record(modifiedRegion{start: index, stop: index, op: opInsertAfter})
	inst.inner.InsertAfterDefault(index, text)
}

// GetTextDefault emits the modified text.
func (inst *TrackedRewriter) GetTextDefault() string {
	return inst.inner.GetTextDefault()
}

// HasConflicts reports whether any fatal modification pair was recorded —
// i.e. whether GetTextDefault will panic.
func (inst *TrackedRewriter) HasConflicts() bool {
	return inst.ConflictCount() > 0
}

// ConflictCount returns the number of fatal modification pairs recorded
// (pairs that make GetTextDefault panic).
func (inst *TrackedRewriter) ConflictCount() (count int) {
	for i := 0; i < len(inst.regions); i++ {
		for j := i + 1; j < len(inst.regions); j++ {
			if classifyConflict(inst.regions[i], inst.regions[j]) == conflictFatal {
				count++
			}
		}
	}
	return
}

// --- Node-level convenience functions (work with both rewriter types) ---

// nodeTokenBounds returns the token index range of a CST node, panicking
// with a descriptive message for synthetic/empty contexts whose bound
// tokens are nil. The panic is converted to an error at the Pass boundary.
func nodeTokenBounds(node antlr.ParserRuleContext, op string) (start, stop int) {
	s, e := node.GetStart(), node.GetStop()
	if s == nil || e == nil {
		panic(fmt.Sprintf("nanopass: %s on a context with nil start/stop token (empty or synthetic production): %T", op, node))
	}
	return s.GetTokenIndex(), e.GetTokenIndex()
}

// ReplaceNode replaces all tokens spanned by a CST node with new text.
func ReplaceNode(rw RewriterI, node antlr.ParserRuleContext, text string) {
	start, stop := nodeTokenBounds(node, "ReplaceNode")
	rw.ReplaceDefault(start, stop, text)
}

// DeleteNode removes all tokens spanned by a CST node.
func DeleteNode(rw RewriterI, node antlr.ParserRuleContext) {
	start, stop := nodeTokenBounds(node, "DeleteNode")
	rw.DeleteDefault(start, stop)
}

// InsertBefore inserts text before a CST node.
func InsertBefore(rw RewriterI, node antlr.ParserRuleContext, text string) {
	start, _ := nodeTokenBounds(node, "InsertBefore")
	rw.InsertBeforeDefault(start, text)
}

// InsertAfter inserts text after a CST node.
func InsertAfter(rw RewriterI, node antlr.ParserRuleContext, text string) {
	_, stop := nodeTokenBounds(node, "InsertAfter")
	rw.InsertAfterDefault(stop, text)
}

// NodeText returns the original source text of a CST node (including whitespace from hidden channel).
func NodeText(pr *ParseResult, node antlr.ParserRuleContext) string {
	start, stop := nodeTokenBounds(node, "NodeText")
	return pr.TokenStream.GetTextFromInterval(antlr.NewInterval(start, stop))
}

// ReplaceToken replaces a single token by index.
func ReplaceToken(rw RewriterI, tokenIndex int, text string) {
	rw.ReplaceDefault(tokenIndex, tokenIndex, text)
}

// DeleteToken removes a single token by index.
func DeleteToken(rw RewriterI, tokenIndex int) {
	rw.DeleteDefault(tokenIndex, tokenIndex)
}

// GetText emits the modified text from the rewriter.
func GetText(rw RewriterI) string {
	return rw.GetTextDefault()
}
