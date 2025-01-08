package dsl

import (
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	mutable_string "github.com/philip-peterson/go-mutablestring"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/yassinebenaid/godump"
	"strings"
	"unicode"
)

type hlFunc func(expr chparser.Expr) (before string, after string)

type SyntaxHighlighter struct {
	hl hlFunc
}

func AnsiHighlightFunc(expr chparser.Expr) (before string, after string) {
	const placeholder = string(unicode.ReplacementChar)
	d := godump.DefaultTheme
	var c godump.Style
	switch expr.(type) {
	case *chparser.FunctionExpr:
		c = d.Func
		break
	case *chparser.Ident:
		c = d.Types
		break
	case *chparser.NumberLiteral:
		c = d.Number
		break
	case *chparser.StringLiteral:
		c = d.String
		break
	case *chparser.ColumnIdentifier, *chparser.TableIdentifier:
		c = d.Address
		break
	case *chparser.PlaceHolder:
		c = d.Fields
		break
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
func (inst *SyntaxHighlighter) Highlite(sql string, ast []chparser.Expr) (sqlHighlighted string, err error) {
	hl := inst.hl
	m := mutable_string.NewMutableString(sql)
	vis := &chparser.DefaultASTVisitor{
		Visit: func(expr chparser.Expr) error {
			b := expr.Pos()
			e := expr.End()
			before, after := hl(expr)
			if before != "" {
				err = m.Insert(int(b), before)
				if err != nil {
					return err
				}
			}
			if after != "" {
				err = m.Insert(int(e), after)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
	for _, expr := range ast {
		err = expr.Accept(vis)
		if err != nil {
			err = eh.Errorf("error while walking and highlighting ast: %w", err)
			return
		}
	}
	sqlHighlighted, err = m.Commit()
	if err != nil {
		err = eh.Errorf("unable to apply text operations: %w", err)
		return
	}
	return
}
