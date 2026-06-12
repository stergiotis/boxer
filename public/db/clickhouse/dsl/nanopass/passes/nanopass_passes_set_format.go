//go:build llm_generated_opus47

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SetFormat returns a Pass that sets or replaces the FORMAT clause on the
// body's CST and mirrors the result into env.Format. Pass an empty string
// to remove an existing FORMAT clause.
func SetFormat(format string) nanopass.Pass {
	return nanopass.Pass{
		Name: "SetFormat",
		Apply: func(e *env.Environment, body string) (string, error) {
			out, err := setFormatBody(format, body)
			if err != nil {
				return "", err
			}
			if e != nil {
				e.Format = format
			}
			return out, nil
		},
		Properties: nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody | nanopass.RegionFormat,
			Writes:     nanopass.RegionBody | nanopass.RegionFormat,
		},
	}
}

func setFormatBody(format string, sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("SetFormat: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)
	root := pr.Tree

	formatTokenIdx := -1
	var formatNameCtx antlr.ParserRuleContext

	for i := 0; i < root.GetChildCount(); i++ {
		child := root.GetChild(i)
		if tn, ok := child.(antlr.TerminalNode); ok {
			if tn.GetSymbol().GetTokenType() == grammar1.ClickHouseParserGrammar1FORMAT {
				formatTokenIdx = tn.GetSymbol().GetTokenIndex()
			}
		}
		if ioc, ok := child.(*grammar1.IdentifierOrNullContext); ok {
			formatNameCtx = ioc
		}
	}

	hasExistingFormat := formatTokenIdx >= 0 && formatNameCtx != nil

	if format == "" {
		if hasExistingFormat {
			deleteStart := formatTokenIdx
			if deleteStart > 0 {
				prevTok := pr.TokenStream.Get(deleteStart - 1)
				if prevTok.GetTokenType() == grammar1.ClickHouseLexerWHITESPACE {
					deleteStart = prevTok.GetTokenIndex()
				}
			}
			deleteStop := formatNameCtx.GetStop().GetTokenIndex()
			rw.DeleteDefault(deleteStart, deleteStop)
		}
	} else if hasExistingFormat {
		nanopass.ReplaceNode(rw, formatNameCtx, format)
	} else {
		// Anchor on the last default-channel token: appending after a hidden
		// trailing single-line comment would swallow the clause, and the
		// queryStmt grammar puts FORMAT before the optional semicolon.
		lastTokenIdx := -1
		semicolonIdx := -1
		for i := pr.TokenStream.Size() - 1; i >= 0; i-- {
			tok := pr.TokenStream.Get(i)
			if tok.GetTokenType() == antlr.TokenEOF || tok.GetChannel() != antlr.TokenDefaultChannel {
				continue
			}
			if tok.GetTokenType() == grammar1.ClickHouseLexerSEMICOLON && lastTokenIdx < 0 {
				semicolonIdx = tok.GetTokenIndex()
				continue
			}
			lastTokenIdx = tok.GetTokenIndex()
			break
		}
		switch {
		case semicolonIdx >= 0:
			rw.InsertBeforeDefault(semicolonIdx, " FORMAT "+format)
		case lastTokenIdx >= 0:
			rw.InsertAfterDefault(lastTokenIdx, " FORMAT "+format)
		}
	}

	result = nanopass.GetText(rw)
	return
}

// GetFormat returns the current FORMAT value of a query, or empty string if
// none. Body-only helper for callers that operate on raw SQL without going
// through env.Extract.
func GetFormat(sql string) (format string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("GetFormat: %w", err)
		return
	}
	root := pr.Tree
	hasFormatToken := false
	for i := 0; i < root.GetChildCount(); i++ {
		child := root.GetChild(i)
		if tn, ok := child.(antlr.TerminalNode); ok {
			if tn.GetSymbol().GetTokenType() == grammar1.ClickHouseParserGrammar1FORMAT {
				hasFormatToken = true
			}
		}
		if ioc, ok := child.(*grammar1.IdentifierOrNullContext); ok && hasFormatToken {
			format = nanopass.NodeText(pr, ioc)
			return
		}
	}
	return
}

// RemoveFormat is a convenience Pass that removes the FORMAT clause if
// present. Equivalent to SetFormat("").
var RemoveFormat = SetFormat("")
