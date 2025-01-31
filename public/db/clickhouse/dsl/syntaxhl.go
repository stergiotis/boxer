package dsl

import (
	"github.com/antlr4-go/antlr/v4"
	mutablestring "github.com/philip-peterson/go-mutablestring"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/yassinebenaid/godump"
	"html"
	"reflect"
	"strings"
	"unicode"
)

type SyntaxHighlighter[H HighlighterI] struct {
	hl H
}
type HighlighterI interface {
	HighlightTerminal(node antlr.TerminalNode) (before string, after string, highlight bool)
	HighlightRule(node antlr.ParserRuleContext) (before string, after string, highlight bool)
	Escape(text string) (textOut string, changed bool)
}

type AnsiHighlighter struct {
	before [7]string
	after  [7]string
}

func (inst *AnsiHighlighter) Escape(text string) (textOut string, changed bool) {
	textOut = text
	changed = false
	return
}

var _ HighlighterI = (*AnsiHighlighter)(nil)

func NewAnsiHighlighter(theme *godump.Theme) (inst *AnsiHighlighter, err error) {
	inst = &AnsiHighlighter{
		before: [7]string{"", "", "", "", "", "", ""},
		after:  [7]string{"", "", "", "", "", "", ""},
	}
	for i, s := range []godump.Style{theme.Func, theme.Types, theme.Number, theme.String, theme.Address, theme.Fields, theme.Chan} {
		err = inst.add(i, s)
		if err != nil {
			return
		}
	}
	return
}
func (inst *AnsiHighlighter) add(slot int, style godump.Style) (err error) {
	const placeholder = string(unicode.ReplacementChar)
	before, after, found := strings.Cut(style.Apply(placeholder), placeholder)
	if !found {
		err = eh.Errorf("unable to extract before/after string from godump style")
		return
	}
	inst.before[slot] = before
	inst.after[slot] = after
	return
}

func (inst *AnsiHighlighter) HighlightTerminal(node antlr.TerminalNode) (before string, after string, highlight bool) {
	return
}
func (inst *AnsiHighlighter) HighlightRule(node antlr.ParserRuleContext) (before string, after string, highlight bool) {
	i := -1
	switch node.(type) {
	case *grammar.ColumnExprFunctionContext:
		i = 0
		break
	case *grammar.IdentifierContext:
		i = 1
		break
	case *grammar.NumberLiteralContext:
		i = 2
		break
	case *grammar.LiteralContext:
		i = 3
		break
	case *grammar.ColumnIdentifierContext, *grammar.TableIdentifierContext:
		i = 4
		break
	case *grammar.ParamSlotContext:
		i = 5
		break
	case *grammar.KeywordContext, *grammar.KeywordForAliasContext:
		i = 6
		break
	default:
		return
	}
	before = inst.before[i]
	after = inst.after[i]
	highlight = true
	return
}

type HtmlHighlighter struct {
}

var _ HighlighterI = (*HtmlHighlighter)(nil)

func NewHtmlHighlighter() *HtmlHighlighter {
	return &HtmlHighlighter{}
}
func formatTypeName(val any) string {
	s := reflect.ValueOf(val).Type().String()
	s = strings.TrimSuffix(s, "Context")
	_, b, f := strings.Cut(s, ".")
	if f {
		s = b
	}
	s = strings.ToLower(string(s[0])) + s[1:] // to match rule name
	return s
}
func (inst *HtmlHighlighter) Escape(text string) (textOut string, changed bool) {
	textOut = text
	return
	/*textOut = html.EscapeString(text)
	changed = len(text) == len(textOut)
	return*/
}
func (inst *HtmlHighlighter) HighlightTerminal(node antlr.TerminalNode) (before string, after string, highlight bool) {
	before = "<span data=\"" + html.EscapeString(node.GetText()) + "\" class=\"terminal\">"
	after = "</span>"
	highlight = true
	return
}
func (inst *HtmlHighlighter) HighlightRule(node antlr.ParserRuleContext) (before string, after string, highlight bool) {
	before = "<span class=\"" + formatTypeName(node) + "\">"
	after = "</span>"
	highlight = true
	/*
		switch node.(type) {
		case *grammar.ColumnExprFunctionContext:
		case *grammar.IdentifierContext:
		case *grammar.NumberLiteralContext:
		case *grammar.LiteralContext:
		case *grammar.ColumnIdentifierContext, *grammar.TableIdentifierContext:
		case *grammar.ParamSlotContext:
		case *grammar.KeywordContext, *grammar.KeywordForAliasContext:
		default:
			return
		}
	*/
	return
}

func NewSyntaxHighlighter[H HighlighterI](hl H) *SyntaxHighlighter[H] {
	return &SyntaxHighlighter[H]{
		hl: hl,
	}
}
func (inst *SyntaxHighlighter[H]) Highlight(sql string, parseTree antlr.Tree) (sqlHighlighted string, err error) {
	hl := inst.hl
	m := mutablestring.NewMutableString(sql)
	for node := range IterateAll(parseTree) {
		var startPos int
		var stopPos int
		var before string
		var after string
		var h bool
		switch nodet := node.(type) {
		case antlr.TerminalNode:
			before, after, h = hl.HighlightTerminal(nodet)
			if h {
				startToken := nodet.GetSymbol()
				if startToken != nil {
					startPos = startToken.GetStart()
				} else {
					h = false
				}
				stopToken := nodet.GetSymbol()
				if stopToken != nil {
					stopPos = stopToken.GetStop()
				} else {
					h = false
				}
			}
			break
		case antlr.ParserRuleContext:
			before, after, h = hl.HighlightRule(nodet)
			if h {
				startToken := nodet.GetStart()
				if startToken != nil {
					startPos = startToken.GetStart()
				} else {
					h = false
				}
				stopToken := nodet.GetStop()
				if stopToken != nil {
					stopPos = stopToken.GetStop()
				}
			}
			break
		}
		if h {
			escaped, changed := hl.Escape(sql[startPos : stopPos+1])
			if changed {
				err = m.ReplaceRange(mutablestring.Range{Pos: startPos, End: stopPos + 1}, before+escaped+after)
				if err != nil {
					return
				}
			} else {
				err = m.Insert(startPos, before)
				if err != nil {
					return
				}
				err = m.Insert(stopPos+1, after)
				if err != nil {
					return
				}
			}
		}
	}
	sqlHighlighted, err = m.Commit()
	if err != nil {
		err = eh.Errorf("unable to apply text operations: %w", err)
		return
	}
	return
}
