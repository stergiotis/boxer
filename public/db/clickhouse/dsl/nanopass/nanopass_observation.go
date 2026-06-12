package nanopass

import "github.com/antlr4-go/antlr/v4"

// SourceRange is a half-open byte range [Start, End) into the source SQL
// string, suitable for direct slicing: src[r.Start:r.End]. The zero value is
// the empty range.
//
// Note ANTLR reports token positions as rune offsets; [ParseResult.SourceRangeOf]
// performs the rune→byte conversion against the original input.
type SourceRange struct {
	Start int // byte offset, inclusive
	End   int // byte offset, exclusive
}

// Empty reports whether the range covers no bytes.
func (inst SourceRange) Empty() bool { return inst.End <= inst.Start }

// SourceRangeOf returns the byte range of pr.Source covered by an ANTLR
// ParserRuleContext. Returns the empty (zero) SourceRange when either bound
// token is nil or the context matched no input — e.g. synthetic / dangling
// contexts and empty productions.
func (inst *ParseResult) SourceRangeOf(ctx antlr.ParserRuleContext) SourceRange {
	s, e := ctx.GetStart(), ctx.GetStop()
	if s == nil || e == nil {
		return SourceRange{}
	}
	startRune, stopRune := s.GetStart(), e.GetStop()
	if stopRune < startRune || startRune < 0 {
		return SourceRange{}
	}
	// Convert inclusive rune offsets to a half-open byte range in one pass.
	var r SourceRange
	runeIdx := 0
	found := false
	for byteIdx := range inst.Source {
		if runeIdx == startRune {
			r.Start = byteIdx
			found = true
		}
		if runeIdx == stopRune+1 {
			r.End = byteIdx
			return r
		}
		runeIdx++
	}
	if !found {
		return SourceRange{}
	}
	r.End = len(inst.Source)
	return r
}

// Observation is delivered to ObservationFuncI subscribers when a
// pass walks past a call site of interest (currently:
// FunctionEvaluator visiting a registered function). Always-fire
// semantics — the observation arrives whether or not the call was
// foldable.
//
// Firing granularity: one observation per registered call site at the
// outermost non-folded level of each Apply. When an outer registered call is
// fully evaluated, the registered calls nested inside it are consumed by
// that evaluation and do not fire separately; when the outer call is not
// evaluable, the walk descends and each nested registered call fires its
// own observation.
type Observation struct {
	// Name is the registered function name as matched: quoting stripped and
	// lowercased (registration is case-insensitive).
	Name string

	// Args holds the evaluated arguments exactly as they were passed to the
	// registered evaluator: for useAny registrations these are unwrapped Go
	// values (int64, uint64, float64, string, bool, slices, …); otherwise
	// marshalling.TypedLiteral values. Populated only when Evaluated is true.
	// When Evaluated is false the observation still fires so consumers can
	// surface a "cannot inspect: arg N is non-literal" affordance.
	Args []any

	// Evaluated reports whether all arguments folded to literals AND the
	// result was serialised back into the body — i.e. the call site was
	// actually rewritten.
	Evaluated bool

	// Src is the byte range of the call site in the body being processed by
	// the current Apply (not the original pipeline input — earlier passes or
	// fixpoint iterations may have rewritten it).
	Src SourceRange
}

// ObservationFuncI is the callback signature for OnObservation hooks.
// Called synchronously inside the pass walk; should be cheap and
// non-blocking (logging, append-to-slice, channel send with default).
type ObservationFuncI func(obs Observation)
