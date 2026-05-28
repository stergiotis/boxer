package antlr4utils

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"slices"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type ParseTreeSerializer struct {
	ruleNames             []string
	symbolicNames         []string
	isValueTerminalSorted []int
	emitSourceInfo        bool
	tokenStream           antlr.TokenStream
	sql                   string
}

func NewParseTreeSerializer(r antlr.Recognizer, tokenStream antlr.TokenStream, sql string, emitSourceInfo bool) (inst *ParseTreeSerializer, err error) {
	ruleNames := r.GetRuleNames()
	symbolicNames := r.GetSymbolicNames()
	inst = &ParseTreeSerializer{
		ruleNames:             ruleNames,
		symbolicNames:         symbolicNames,
		isValueTerminalSorted: nil,
		tokenStream:           tokenStream,
		emitSourceInfo:        emitSourceInfo,
		sql:                   sql,
	}
	return
}
func (inst *ParseTreeSerializer) AddValueTerminal(symbolicName string) (err error) {
	idx := slices.Index(inst.symbolicNames, symbolicName)
	if idx < 0 {
		err = eb.Build().Str("symbolicName", symbolicName).Errorf("terminal symbolic name not found in symbolicNames")
		return
	}
	inst.isValueTerminalSorted = append(inst.isValueTerminalSorted, idx)
	// TODO incremental sort
	slices.Sort(inst.isValueTerminalSorted)
	return
}

func (inst *ParseTreeSerializer) formatSourceInterval2(out io.StringWriter, rctxt *antlr.BaseParserRuleContext) (err error) {
	if inst.emitSourceInfo {
		start := rctxt.GetStart()
		stop := rctxt.GetStop()
		s := fmt.Sprintf(" ((%d . %d) . ", start.GetStart(), stop.GetStop())
		_, err = out.WriteString(s)
		if err != nil {
			return
		}
		t := inst.sql[start.GetStart() : stop.GetStop()+1]

		if false {
			_, err = out.WriteString("#|")
			if err != nil {
				return
			}
			_, err = out.WriteString(t)
			if err != nil {
				return
			}
			_, err = out.WriteString("|#")
			if err != nil {
				return
			}
		} else {
			err = escapeJsonString(out, t)
			if err != nil {
				return
			}
		}
		_, err = out.WriteString(")")
		if err != nil {
			return
		}
	}
	return
}
func (inst *ParseTreeSerializer) formatSourceInterval(out io.StringWriter, interval antlr.Interval) (err error) {
	if inst.emitSourceInfo {
		s := fmt.Sprintf(" (%d . %d) #|", interval.Start, interval.Stop)
		_, err = out.WriteString(s)
		if err != nil {
			return
		}
		ts := inst.tokenStream
		for i := interval.Start; i < interval.Stop; i++ {
			t := ts.Get(i).GetText()
			_, err = out.WriteString(t)
			if err != nil {
				return
			}
			_, err = out.WriteString(" ")
			if err != nil {
				return
			}
		}
		_, err = out.WriteString("|#")
		if err != nil {
			return
		}
	}
	return
}
func (inst *ParseTreeSerializer) Serialize(out io.StringWriter, tree antlr.Tree) (err error) {
	err = inst.serialize(out, tree)
	if err != nil {
		err = eh.Errorf("error while serializing tree: %w", err)
		return
	}
	return
}
func (inst *ParseTreeSerializer) serialize(out io.StringWriter, tree antlr.Tree) (err error) {
	c := tree.GetChildCount()
	if c > 0 {
		_, err = out.WriteString("\n(")
		if err != nil {
			return
		}
	}
	err = inst.serializeNode(out, tree)
	if err != nil {
		return
	}

	if c > 0 {
		for i := 0; i < c; i++ {
			err = inst.serialize(out, tree.GetChild(i))
			if err != nil {
				return
			}
			if i != c-1 {
				_, err = out.WriteString(" ")
				if err != nil {
					return
				}
			}
		}
		_, err = out.WriteString(") ")
		if err != nil {
			return
		}
	}
	return
}
func escapeJsonString(out io.StringWriter, str string) (err error) {
	var b []byte
	b, err = json.Marshal(str)
	if err != nil {
		return
	}
	_, err = out.WriteString(string(b))
	return
}
func (inst *ParseTreeSerializer) serializeNode(out io.StringWriter, t antlr.Tree) (err error) {
	switch t2 := t.(type) {
	case antlr.RuleNode:
		t3 := t2.GetRuleContext()
		altNumber := t3.GetAltNumber()

		rn := inst.ruleNames[t3.GetRuleIndex()]
		if altNumber != antlr.ATNInvalidAltNumber {
			rn += ":" + strconv.FormatUint(uint64(altNumber), 10)
		}
		_, err = out.WriteString(rn)
		if err != nil {
			return
		}
		rctxt, ok := t.GetPayload().(*antlr.BaseParserRuleContext)
		if ok {
			err = inst.formatSourceInterval2(out, rctxt)
			if err != nil {
				return
			}
		} else {
			err = inst.formatSourceInterval(out, t3.GetSourceInterval())
			if err != nil {
				return
			}
		}

		break
	case antlr.ErrorNode:
		_, err = out.WriteString(fmt.Sprintf("%+v", t2))
		if err != nil {
			return
		}
		break
	case antlr.TerminalNode:
		symb := t2.GetSymbol()
		if symb != nil {
			idx := symb.GetTokenType()
			_, v := slices.BinarySearch(inst.isValueTerminalSorted, idx)
			if idx == antlr.TokenEOF {
				// <EOF>
			} else if v {
				err = escapeJsonString(out, t2.GetText())
				if err != nil {
					err = eb.Build().Str("symbolicName", inst.symbolicNames[idx]).Errorf("unable to serialize value of token")
					return
				}
			} else {
				n := inst.symbolicNames[idx]
				_, err = out.WriteString("TERMINAL-")
				if err != nil {
					return
				}
				_, err = out.WriteString(n)
				if err != nil {
					return
				}
			}
		}
	default:
		err = eb.Build().Type("nodeType", t).Errorf("unhandled node type")
		return
	}
	return
}
