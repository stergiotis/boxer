package dsl

import (
	"github.com/antlr4-go/antlr/v4"
	mutablestring "github.com/philip-peterson/go-mutablestring"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/yassinebenaid/godump"
	"strings"
	"unicode"
)

type hlFunc func(node antlr.Tree) (before string, after string)

type SyntaxHighlighter struct {
	hl hlFunc
}

func AnsiHighlightFunc(node antlr.Tree) (before string, after string) {
	const placeholder = string(unicode.ReplacementChar)
	d := godump.DefaultTheme
	var c godump.Style
	switch node.(type) {
	case *grammar.ColumnExprFunctionContext:
		c = d.Func
		break
	case *grammar.IdentifierContext:
		c = d.Types
		break
	case *grammar.NumberLiteralContext:
		c = d.Number
		break
	case *grammar.LiteralContext:
		c = d.String
		break
	case *grammar.ColumnIdentifierContext, *grammar.TableIdentifierContext:
		c = d.Address
		break
	case *grammar.ParamSlotContext:
		c = d.Fields
		break
	case *grammar.KeywordContext, *grammar.KeywordForAliasContext:
		c = d.Chan
	default:
		return
	}
	before, after, _ = strings.Cut(c.Apply(placeholder), placeholder)
	return
}

func NewSyntaxHighlighter(hl hlFunc) *SyntaxHighlighter {
	return &SyntaxHighlighter{
		hl: hl,
	}
}
func (inst *SyntaxHighlighter) Highlight(sql string, parseTree antlr.Tree) (sqlHighlighted string, err error) {
	hl := inst.hl
	m := mutablestring.NewMutableString(sql)
	for node := range IterateAll(parseTree) {
		switch nodet := node.(type) {
		case antlr.TerminalNode:
			before, after := hl(node)
			if before != "" {
				startToken := nodet.GetSymbol()
				if startToken != nil {
					start := startToken.GetStart()
					err = m.Insert(start, before)
					if err != nil {
						return
					}
				}
			}
			if after != "" {
				stopToken := nodet.GetSymbol()
				if stopToken != nil {
					err = m.Insert(stopToken.GetStop()+1, after)
					if err != nil {
						return
					}
				}
			}
			break
		case antlr.ParserRuleContext:
			before, after := hl(node)
			if before != "" {
				startToken := nodet.GetStart()
				if startToken != nil {
					start := startToken.GetStart()
					err = m.Insert(start, before)
					if err != nil {
						return
					}
				}
			}
			if after != "" {
				stopToken := nodet.GetStop()
				if stopToken != nil {
					err = m.Insert(stopToken.GetStop()+1, after)
					if err != nil {
						return
					}
				}
			}
			break
		}
	}
	sqlHighlighted, err = m.Commit()
	if err != nil {
		err = eh.Errorf("unable to apply text operations: %w", err)
		return
	}
	return
}
