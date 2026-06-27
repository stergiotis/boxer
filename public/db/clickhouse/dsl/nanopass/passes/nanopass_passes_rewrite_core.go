package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// This file is the functional core shared by the local-rewrite canonicalisation
// passes. Each such pass is a term rewrite over the CST: "match this node shape,
// replace it with text built from the original spans of the children I keep".
// The driver below owns the parse / walk / emit / error plumbing that every one
// of those passes used to copy; a pass is then just its rule(s).
//
// Invariant — the bright line that keeps this sugar over the existing model
// rather than a second tree representation: a rule returns the *replacement
// text*, assembled from the original source spans of untouched children (spanOf).
// It never builds or returns a new tree. Mutation stays in the TokenStreamRewriter,
// so hidden-channel trivia (whitespace, comments) is preserved exactly as in a
// hand-written pass.

// nodeRule matches a CST node and, on a match, returns the replacement text to
// splice in place of that node's tokens. ok == false means "not my node — keep
// walking".
type nodeRule func(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (replacement string, ok bool)

// rewriteNodes runs one top-down CST pass over sql: at each node the rules are
// tried in order; the first to match replaces the node, and the walk then skips
// that node's subtree so edits can never overlap. Nested matches (a rule whose
// output contains another match) are handled by declaring the pass
// NeedsFixedPoint — the runner re-applies rewriteNodes until convergence.
//
// This is the entire body of a local-rewrite pass; the per-pass code is just the
// nodeRule(s) it passes in.
func rewriteNodes(sql string, name string, rules ...nodeRule) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("%s: %w", name, err)
		return
	}
	rw := nanopass.NewRewriter(pr)
	changed := false
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		for _, rule := range rules {
			repl, ok := rule(pr, ctx)
			if !ok {
				continue
			}
			nanopass.ReplaceNode(rw, ctx, repl)
			changed = true
			return false // skip subtree; the fixpoint re-parse catches nested forms
		}
		return true
	})
	if !changed {
		result = sql // no edits: return the input verbatim (byte-identical)
		return
	}
	result = nanopass.GetText(rw)
	return
}

// tokenRule maps a single token to its replacement text; ok == false leaves the
// token untouched.
type tokenRule func(tok antlr.Token) (replacement string, ok bool)

// rewriteTokens runs one token-stream pass over sql, applying rule to every
// token. It is the body of the purely lexical canonicalisations (== → =, and so
// on) that match on token type rather than CST shape.
func rewriteTokens(sql string, name string, rule tokenRule) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("%s: %w", name, err)
		return
	}
	rw := nanopass.NewRewriter(pr)
	changed := false
	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		repl, ok := rule(tok)
		if !ok {
			continue
		}
		nanopass.ReplaceToken(rw, tok.GetTokenIndex(), repl)
		changed = true
	}
	if !changed {
		result = sql
		return
	}
	result = nanopass.GetText(rw)
	return
}

// spanOf returns the original source text (including hidden-channel trivia) of a
// child node — the building block a rule uses to carry an untouched subtree into
// its replacement without re-rendering it.
func spanOf(pr *nanopass.ParseResult, n antlr.ParserRuleContext) string {
	return nanopass.NodeText(pr, n)
}

// terminalText returns the text of node's first direct terminal child of the
// given lexer token type, or "" if there is none. For matching keyword/literal
// tokens that a production carries inline (DATE 'str', TRIM(BOTH …)).
func terminalText(node antlr.ParserRuleContext, tokenType int) string {
	for i := 0; i < node.GetChildCount(); i++ {
		if term, ok := node.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == tokenType {
				return term.GetText()
			}
		}
	}
	return ""
}

// columnExprOperands returns the original spans of a node's direct columnExpr
// children, in source order — the operands of an operator-shaped production
// (ternary, the precedence ladders, …).
func columnExprOperands(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (spans []string) {
	spans = make([]string, 0, node.GetChildCount())
	for i := 0; i < node.GetChildCount(); i++ {
		if ce, ok := node.GetChild(i).(grammar1.IColumnExprContext); ok {
			spans = append(spans, nanopass.NodeText(pr, ce.(antlr.ParserRuleContext)))
		}
	}
	return
}

// callForm renders a canonical function call from already-rendered argument
// spans: callForm("if", a, b, c) == "if(a, b, c)".
func callForm(fn string, args ...string) string {
	return fn + "(" + strings.Join(args, ", ") + ")"
}
