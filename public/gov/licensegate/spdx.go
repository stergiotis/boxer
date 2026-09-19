package licensegate

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SPDX license expressions, as a Cargo manifest declares them (ADR-0246).
//
// A CycloneDX component carries identifiers one per row; a crate carries an
// expression, and splitting one into rows errs in both directions. Splitting a
// disjunction turns `MIT OR LGPL-2.1-or-later` into a failing LGPL row on a
// crate that offers MIT. Splitting a conjunction lets one passing row clear
// `MIT AND GPL-3.0-only`. The evaluator below keeps the structure, so each
// operator can err the way a compliance gate can afford:
//
//   - OR elects its most permissive branch (disjunctionRank).
//   - AND binds its most restrictive branch (conjunctionRank).
//   - Unknown ranks lowest in a disjunction, so it never displaces a branch the
//     policy knows, and above the violations in a conjunction, so it never
//     masks one. An unclassifiable identifier degrades to advisory, never to a
//     pass.

// disjunctionRank orders categories by how willingly boxer elects them when a
// crate offers a choice; higher wins. Unknown is below every known category,
// violations included: offered `GPL-3.0-only OR <unknown>`, the gate fails on
// the branch it can read rather than passing on one it cannot.
func disjunctionRank(c CategoryE) (rank int) {
	switch c {
	case CategoryForbidden:
		rank = 1
	case CategoryRestricted:
		rank = 2
	case CategoryReciprocal:
		rank = 3
	case CategoryNotice:
		rank = 4
	case CategoryPermissive:
		rank = 5
	case CategoryUnencumbered:
		rank = 6
	default:
		rank = 0
	}
	return
}

// conjunctionRank orders categories by how much they bind when every branch of
// a conjunction applies; lower wins. Unknown sits between the violations and
// the passing categories.
func conjunctionRank(c CategoryE) (rank int) {
	switch c {
	case CategoryForbidden:
		rank = 0
	case CategoryRestricted:
		rank = 1
	case CategoryReciprocal:
		rank = 3
	case CategoryNotice:
		rank = 4
	case CategoryPermissive:
		rank = 5
	case CategoryUnencumbered:
		rank = 6
	default:
		rank = 2
	}
	return
}

// EvaluateExpressionE classifies an SPDX license expression against the
// policy map. elected is the expression reduced to the branches that bind —
// `(MIT OR Apache-2.0) AND Unicode-3.0` elects `MIT AND Unicode-3.0` — and
// category is what those branches amount to. The legacy `/` separator still
// found in older crates reads as OR; operators match case-insensitively.
//
// A malformed expression is an error: the caller decides what an unreadable
// declaration means, which for the gate is the advisory block (ADR-0246 SD7).
func EvaluateExpressionE(expression string) (category CategoryE, elected string, err error) {
	p := expressionParserT{tokens: tokenizeExpression(expression)}
	if len(p.tokens) == 0 {
		err = eb.Build().Str("expression", expression).Errorf("empty license expression")
		return
	}
	category, elected, err = p.parseOr()
	if err != nil {
		err = eb.Build().Str("expression", expression).Errorf("parse license expression: %w", err)
		return
	}
	if p.pos != len(p.tokens) {
		err = eb.Build().Str("expression", expression).Str("token", p.tokens[p.pos]).Errorf("unexpected token after license expression")
		return
	}
	return
}

// tokenizeExpression splits on whitespace and makes the parentheses and the
// legacy `/` separator tokens of their own, so `(MIT/Apache-2.0)` and
// `( MIT / Apache-2.0 )` tokenize alike.
func tokenizeExpression(expression string) (tokens []string) {
	tokens = make([]string, 0, 8)
	start := -1
	flush := func(end int) {
		if start >= 0 {
			tokens = append(tokens, expression[start:end])
			start = -1
		}
	}
	for i := 0; i < len(expression); i++ {
		switch ch := expression[i]; ch {
		case ' ', '\t', '\n', '\r':
			flush(i)
		case '(', ')', '/':
			flush(i)
			tokens = append(tokens, expression[i:i+1])
		default:
			if start < 0 {
				start = i
			}
		}
	}
	flush(len(expression))
	return
}

// expressionParserT is a recursive-descent parser over the SPDX grammar, with
// the precedence SPDX defines: WITH binds tightest, then AND, then OR.
//
//	or       := and ( ("OR" | "/") and )*
//	and      := with ( "AND" with )*
//	with     := "(" or ")" | id [ "WITH" id ]
type expressionParserT struct {
	tokens []string
	pos    int
}

func (inst *expressionParserT) peekOperator(operator string) (ok bool) {
	if inst.pos >= len(inst.tokens) {
		return
	}
	ok = strings.EqualFold(inst.tokens[inst.pos], operator)
	return
}

func (inst *expressionParserT) parseOr() (category CategoryE, elected string, err error) {
	category, elected, err = inst.parseAnd()
	if err != nil {
		return
	}
	for inst.peekOperator("OR") || inst.peekOperator("/") {
		inst.pos++
		var c CategoryE
		var e string
		c, e, err = inst.parseAnd()
		if err != nil {
			return
		}
		// Strictly greater: a tie keeps the earlier branch, so the author's
		// stated order decides between equals and `MIT OR Apache-2.0` elects MIT.
		if disjunctionRank(c) > disjunctionRank(category) {
			category, elected = c, e
		}
	}
	return
}

func (inst *expressionParserT) parseAnd() (category CategoryE, elected string, err error) {
	category, elected, err = inst.parseWith()
	if err != nil {
		return
	}
	for inst.peekOperator("AND") {
		inst.pos++
		var c CategoryE
		var e string
		c, e, err = inst.parseWith()
		if err != nil {
			return
		}
		// Every conjunct binds, so all of them stay in the elected text.
		elected = elected + " AND " + e
		if conjunctionRank(c) < conjunctionRank(category) {
			category = c
		}
	}
	return
}

func (inst *expressionParserT) parseWith() (category CategoryE, elected string, err error) {
	if inst.pos >= len(inst.tokens) {
		err = eb.Build().Errorf("expression ends where a license was expected")
		return
	}
	token := inst.tokens[inst.pos]
	if token == "(" {
		inst.pos++
		category, elected, err = inst.parseOr()
		if err != nil {
			return
		}
		if inst.pos >= len(inst.tokens) || inst.tokens[inst.pos] != ")" {
			err = eb.Build().Errorf("unbalanced parenthesis in license expression")
			return
		}
		inst.pos++
		return
	}
	if isExpressionOperator(token) {
		err = eb.Build().Str("token", token).Errorf("operator where a license was expected")
		return
	}
	inst.pos++
	elected = token
	category = Categorize(token)
	if !inst.peekOperator("WITH") {
		return
	}
	inst.pos++
	if inst.pos >= len(inst.tokens) || isExpressionOperator(inst.tokens[inst.pos]) {
		err = eb.Build().Str("license", token).Errorf("WITH without an exception")
		return
	}
	exception := inst.tokens[inst.pos]
	inst.pos++
	elected = token + " WITH " + exception
	// An exception narrows the license it qualifies, so the base identifier's
	// category is the conservative reading when the map has no entry for the
	// qualified form.
	if c := Categorize(elected); c != CategoryUnknown {
		category = c
	}
	return
}

func isExpressionOperator(token string) (ok bool) {
	switch {
	case token == "(" || token == ")" || token == "/":
		ok = true
	case strings.EqualFold(token, "AND") || strings.EqualFold(token, "OR") || strings.EqualFold(token, "WITH"):
		ok = true
	}
	return
}
