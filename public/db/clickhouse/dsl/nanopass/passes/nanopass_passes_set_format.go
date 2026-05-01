//go:build llm_generated_opus47

package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SetFormat returns a Pass that sets or replaces the FORMAT clause of a
// query. If the body already has a FORMAT clause, it is replaced; if not, a
// FORMAT clause is appended. Pass an empty string to remove an existing
// FORMAT clause.
//
// v1: The pass operates on body's CST directly. Future versions may also
// reflect changes into env.Format.
func SetFormat(format string) nanopass.Pass {
	return nanopass.LiftBodyPass(
		"SetFormat",
		setFormatImpl(format),
		nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody | nanopass.RegionFormat,
			Writes:     nanopass.RegionBody | nanopass.RegionFormat,
		},
	)
}

func setFormatImpl(format string) func(string) (string, error) {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("SetFormat: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		root := pr.Tree

		// Find the existing FORMAT keyword and format name in QueryStmtContext.
		// Structure: QueryContext [FORMAT IdentifierOrNullContext] EOF
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
			// Remove existing FORMAT clause
			if hasExistingFormat {
				// Delete from the FORMAT keyword through the format name
				// Also delete whitespace before FORMAT
				deleteStart := formatTokenIdx
				{ // Try to include preceding whitespace
					if deleteStart > 0 {
						prevTok := pr.TokenStream.Get(deleteStart - 1)
						if prevTok.GetTokenType() == grammar1.ClickHouseLexerWHITESPACE {
							deleteStart = prevTok.GetTokenIndex()
						}
					}
				}
				deleteStop := formatNameCtx.GetStop().GetTokenIndex()
				rw.DeleteDefault(deleteStart, deleteStop)
			}
			// else: no FORMAT to remove — no-op
		} else if hasExistingFormat {
			// Replace existing format name
			nanopass.ReplaceNode(rw, formatNameCtx, format)
		} else {
			// No existing FORMAT — insert before EOF
			// Find the last non-EOF token
			lastTokenIdx := -1
			for i := pr.TokenStream.Size() - 1; i >= 0; i-- {
				tok := pr.TokenStream.Get(i)
				if tok.GetTokenType() != antlr.TokenEOF {
					lastTokenIdx = tok.GetTokenIndex()
					break
				}
			}
			if lastTokenIdx >= 0 {
				rw.InsertAfterDefault(lastTokenIdx, " FORMAT "+format)
			}
		}

		result = nanopass.GetText(rw)
		return
	}
}

// GetFormat returns the current FORMAT value of a query, or empty string if none.
//
//nolint:unused // public helper
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
