package nanopass

import "github.com/antlr4-go/antlr/v4"

// SourceRange is an inclusive byte range into the source SQL — matching
// ANTLR's token-bound convention (Stop is the index of the last byte, not
// one past). Empty ranges have Stop < Start.
type SourceRange struct {
	Start int
	Stop  int
}

// Empty reports whether the range is unset / invalid.
func (r SourceRange) Empty() bool { return r.Stop < r.Start }

// SourceRangeFromCtx returns the byte range covered by an ANTLR
// ParserRuleContext. Returns the zero (empty) SourceRange when either the
// start or stop token is nil — e.g. for synthetic / dangling contexts.
func SourceRangeFromCtx(ctx antlr.ParserRuleContext) SourceRange {
	s, e := ctx.GetStart(), ctx.GetStop()
	if s == nil || e == nil {
		return SourceRange{Stop: -1}
	}
	return SourceRange{Start: s.GetStart(), Stop: e.GetStop()}
}

// Observation is delivered to ObservationFuncI subscribers when a
// pass walks past a call site of interest (currently:
// FunctionEvaluator visiting a registered function). Always-fire
// semantics — the observation arrives whether or not the call was
// foldable.
//
// Args is populated only when Evaluated is true (i.e. all arguments
// were literals after recursive folding). When Evaluated is false the
// observation still fires so consumers can surface a "cannot inspect:
// arg N is non-literal" affordance.
type Observation struct {
	Name      string
	Args      []any
	Evaluated bool
	Src       SourceRange
}

// ObservationFuncI is the callback signature for OnObservation hooks.
// Called synchronously inside the pass walk; should be cheap and
// non-blocking (logging, append-to-slice, channel send with default).
type ObservationFuncI func(obs Observation)
