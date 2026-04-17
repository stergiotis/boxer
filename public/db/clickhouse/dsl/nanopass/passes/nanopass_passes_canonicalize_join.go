//go:build llm_generated_opus46

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeJoin canonicalizes JOIN syntax.
//
//  1. Join keyword order: strictness always precedes direction.
//     LEFT ALL JOIN  → ALL LEFT JOIN
//
//  2. OUTER keyword removed (redundant):
//     LEFT OUTER JOIN → LEFT JOIN
//
//  3. Comma join → CROSS JOIN:
//     FROM t1, t2 → FROM t1 CROSS JOIN t2
//
//  4. USING without parens → USING with parens:
//     USING col → USING (col)
func CanonicalizeJoin(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeJoin: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.JoinOpInnerContext:
			normalizeJoinOpInner(rw, pr, c)
		case *grammar1.JoinOpLeftRightContext:
			normalizeJoinOpLeftRight(rw, pr, c)
		case *grammar1.JoinOpFullContext:
			normalizeJoinOpFull(rw, pr, c)
		case *grammar1.JoinOpCrossContext:
			normalizeJoinOpCross(rw, pr, c)
		case *grammar1.JoinConstraintClauseContext:
			normalizeJoinConstraint(rw, pr, c)
		}
		return true
	})

	result = nanopass.GetText(rw)
	return
}

var _ nanopass.Pass = CanonicalizeJoin

// deleteTokenWithWhitespace deletes a token and any immediately preceding
// whitespace on the hidden channel to prevent double spaces.
func deleteTokenWithWhitespace(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, tokIdx int) {
	nanopass.DeleteToken(rw, tokIdx)
	if tokIdx > 0 {
		prev := pr.TokenStream.Get(tokIdx - 1)
		if prev.GetChannel() != antlr.TokenDefaultChannel {
			nanopass.DeleteToken(rw, prev.GetTokenIndex())
		}
	}
}

func normalizeJoinOpInner(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.JoinOpInnerContext) {
	var strictnessTok antlr.Token
	var innerTok antlr.Token

	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar1.ClickHouseLexerINNER:
				innerTok = term.GetSymbol()
			case grammar1.ClickHouseLexerALL, grammar1.ClickHouseLexerANY, grammar1.ClickHouseLexerASOF:
				strictnessTok = term.GetSymbol()
			}
		}
	}

	if strictnessTok == nil || innerTok == nil {
		return
	}
	if strictnessTok.GetTokenIndex() > innerTok.GetTokenIndex() {
		nanopass.ReplaceToken(rw, innerTok.GetTokenIndex(), strictnessTok.GetText()+" INNER")
		deleteTokenWithWhitespace(rw, pr, strictnessTok.GetTokenIndex())
	}
}

func normalizeJoinOpLeftRight(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.JoinOpLeftRightContext) {
	var strictnessTok antlr.Token
	var directionTok antlr.Token
	var outerTok antlr.Token

	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar1.ClickHouseLexerLEFT, grammar1.ClickHouseLexerRIGHT:
				directionTok = term.GetSymbol()
			case grammar1.ClickHouseLexerOUTER:
				outerTok = term.GetSymbol()
			case grammar1.ClickHouseLexerSEMI, grammar1.ClickHouseLexerALL, grammar1.ClickHouseLexerANTI,
				grammar1.ClickHouseLexerANY, grammar1.ClickHouseLexerASOF:
				strictnessTok = term.GetSymbol()
			}
		}
	}

	if outerTok != nil {
		deleteTokenWithWhitespace(rw, pr, outerTok.GetTokenIndex())
	}

	if strictnessTok == nil || directionTok == nil {
		return
	}
	if strictnessTok.GetTokenIndex() > directionTok.GetTokenIndex() {
		nanopass.ReplaceToken(rw, directionTok.GetTokenIndex(), strictnessTok.GetText()+" "+directionTok.GetText())
		deleteTokenWithWhitespace(rw, pr, strictnessTok.GetTokenIndex())
	}
}

func normalizeJoinOpFull(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.JoinOpFullContext) {
	var strictnessTok antlr.Token
	var fullTok antlr.Token
	var outerTok antlr.Token

	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar1.ClickHouseLexerFULL:
				fullTok = term.GetSymbol()
			case grammar1.ClickHouseLexerOUTER:
				outerTok = term.GetSymbol()
			case grammar1.ClickHouseLexerALL, grammar1.ClickHouseLexerANY:
				strictnessTok = term.GetSymbol()
			}
		}
	}

	if outerTok != nil {
		deleteTokenWithWhitespace(rw, pr, outerTok.GetTokenIndex())
	}

	if strictnessTok == nil || fullTok == nil {
		return
	}
	if strictnessTok.GetTokenIndex() > fullTok.GetTokenIndex() {
		nanopass.ReplaceToken(rw, fullTok.GetTokenIndex(), strictnessTok.GetText()+" FULL")
		deleteTokenWithWhitespace(rw, pr, strictnessTok.GetTokenIndex())
	}
}

// normalizeJoinOpCross: FROM t1, t2 → FROM t1 CROSS JOIN t2
func normalizeJoinOpCross(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.JoinOpCrossContext) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerCOMMA {
				nanopass.ReplaceToken(rw, term.GetSymbol().GetTokenIndex(), " CROSS JOIN")
				return
			}
		}
	}
}

func normalizeJoinConstraint(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.JoinConstraintClauseContext) {
	hasUsing := false
	hasLParen := false

	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			if tt == grammar1.ClickHouseLexerUSING {
				hasUsing = true
			}
			if tt == grammar1.ClickHouseLexerLPAREN {
				hasLParen = true
			}
		}
	}

	if !hasUsing || hasLParen {
		return
	}

	for i := 0; i < ctx.GetChildCount(); i++ {
		if cel, ok := ctx.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			nanopass.InsertBefore(rw, cel, "(")
			nanopass.InsertAfter(rw, cel, ")")
			return
		}
	}
}
