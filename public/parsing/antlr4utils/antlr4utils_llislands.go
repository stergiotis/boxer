package antlr4utils

import (
	"github.com/antlr4-go/antlr/v4"
)

// LL islands in an SLL parse (ADR-0305).
//
// SLL prediction has no call stack: when it reaches the end of a rule it
// continues into every place that rule is invoked, and when two alternatives
// stay viable it takes the lowest. In a few positions of a grammar that choice
// is wrong for the position the parser is actually in — the middle operand of
// `x BETWEEN a AND b`, where continuing with a binary AND looks as good as
// stopping for BETWEEN's own AND — and the parse fails a token later, which
// sends the whole statement through the full-context LL fallback.
//
// An island is a rule invocation whose decisions are predicted in LL instead.
// LL mode still predicts from the shared DFA and escalates to full context only
// on an SLL conflict, so an island costs nothing where SLL would have been
// right. The grammar names its islands by the parent of the invocation, which
// is how the generated code identifies them: the operand of BETWEEN is a
// columnExpr whose parent is the BETWEEN node.
//
// A parse listener follows the current context — it is told on every rule
// entry and exit, including the re-parenting of left-recursive rules — and
// switches the simulator's mode as the parser moves in and out of an island.
// The tree is unaffected; only the predictions inside islands change, and they
// change to what an all-LL parse would predict there.

// IslandFunc reports whether a rule invocation whose parent is parent is an
// island. It is called on every rule entry and exit, so it must be cheap: a
// type switch.
type IslandFunc func(parent antlr.Tree) bool

// llIslands is the parse listener that switches prediction mode.
type llIslands struct {
	antlr.BaseParseTreeListener
	sim      *antlr.ParserATNSimulator
	isIsland IslandFunc
	base     int
	mode     int
}

func (inst *llIslands) follow(current antlr.Tree) {
	m := inst.base
	if current != nil && inst.isIsland(current.GetParent()) {
		m = antlr.PredictionModeLL
	}
	if m != inst.mode {
		inst.sim.SetPredictionMode(m)
		inst.mode = m
	}
}

// EnterEveryRule runs with the entered context current.
func (inst *llIslands) EnterEveryRule(ctx antlr.ParserRuleContext) { inst.follow(ctx) }

// ExitEveryRule runs before the parser returns to the context's parent.
func (inst *llIslands) ExitEveryRule(ctx antlr.ParserRuleContext) { inst.follow(ctx.GetParent()) }

// parseListenerAdder is implemented by every generated parser (through the
// embedded BaseParser) but is not part of antlr.Parser.
type parseListenerAdder interface {
	AddParseListener(listener antlr.ParseTreeListener)
}

// SetLLIslands names the grammar's islands. Call it once, before the first
// parse — from the grammar package's init.
func (inst *DFACache) SetLLIslands(f IslandFunc) {
	inst.islands = f
}

// AcquireMode is [DFACache.Acquire] for a parse in one prediction mode: it
// sets the mode on the returned simulator and, for an SLL parse of a grammar
// with islands, predicts in LL inside them. An LL parse is LL throughout. The
// caller assigns the simulator to parser.Interpreter and calls release when
// parsing is done (defer it — release is panic-safe).
func (inst *DFACache) AcquireMode(p antlr.Parser, predictionMode int) (sim *antlr.ParserATNSimulator, release func()) {
	sim, release = inst.Acquire(p)
	sim.SetPredictionMode(predictionMode)
	if inst.islands != nil && predictionMode == antlr.PredictionModeSLL {
		if a, ok := p.(parseListenerAdder); ok {
			a.AddParseListener(&llIslands{sim: sim, isIsland: inst.islands, base: predictionMode, mode: predictionMode})
		}
	}
	return
}
