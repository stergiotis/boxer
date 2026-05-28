package antlr4utils

import (
	"reflect"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers/co"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/cbor/builder"
)

type AntlrTreeToCbor[E cbor.FullEncoderI] struct {
	enc           E
	contextPrefix string
}

func NewAntlrTreeToCbor[E cbor.FullEncoderI](enc E, contextPrefix string) (inst *AntlrTreeToCbor[E]) {
	inst = &AntlrTreeToCbor[E]{
		enc:           enc,
		contextPrefix: contextPrefix,
	}
	return
}

func (inst *AntlrTreeToCbor[E]) Convert(parser antlr.Recognizer, tree antlr.Tree) (err error) {
	enc := inst.enc
	if tree == nil {
		_, err = enc.EncodeNil()
		if err != nil {
			return
		}
		return
	}
	return inst.convert(parser, tree, "")
}
func (inst *AntlrTreeToCbor[E]) convert(parser antlr.Recognizer, tree antlr.Tree, lastName string) (err error) {
	switch treet := tree.(type) {
	case antlr.ErrorNode:
		b := eb.Build()
		AddErrorNodeToCborBuilder(b, treet)
		err = b.Errorf("tree contains error node")
		break
	case antlr.TerminalNode:
		err = inst.convertTerminal(parser, treet)
		break
	default:
		enc := inst.enc
		n := tree.GetChildCount()
		tmpS := make([]int, 0, n)
		tmpC := make([]int, 0, n)
		l := uint64(0)
		// gather children by name (rule name or alternative label)
		for i := 0; i < n; i++ {
			child := tree.GetChild(i)
			//var tn string
			var t int
			switch childt := child.(type) {
			case antlr.ErrorNode:
				err = inst.convert(parser, child, "")
				if err != nil {
					return
				}
				break
			case antlr.TerminalNode:
				t = -1
				break
			case antlr.RuleNode:
				t = childt.GetRuleContext().GetRuleIndex()
				break
			default:
				log.Warn().Type("child", child).Msg("unhandled antlr tree node type, skipping")
				continue
			}
			var existed bool
			_, existed, tmpS, tmpC = co.InsertSliceSorted(tmpS, tmpC, t, i)
			if !existed {
				l++
			}
		}
		contextTypeName := FormatContextTypeName(tree)
		if contextTypeName != lastName {
			_, err = enc.EncodeMapDefinite(1)
			if err != nil {
				return
			}
			prefix := inst.contextPrefix
			if prefix != "" {
				_, err = enc.EncodeString(contextTypeName)
			} else {
				_, err = enc.EncodeString(prefix + contextTypeName)
			}
			if err != nil {
				return
			}
		}
		_, err = enc.EncodeMapDefinite(l)
		if err != nil {
			return
		}
		ruleNames := parser.GetRuleNames()
		for t, children := range co.IterateSliceGrouped(tmpS, tmpC) {
			var tn string
			if t < 0 {
				tn = "terminal"
			} else {
				tn = ruleNames[t]
			}
			_, err = enc.EncodeString(tn)
			if err != nil {
				return
			}
			l2 := len(children)
			switch l2 {
			case 1:
				err = inst.convert(parser, tree.GetChild(children[0]), tn)
				if err != nil {
					return
				}
				break
			default:
				_, err = enc.EncodeArrayDefinite(uint64(l2))
				if err != nil {
					return
				}
				for _, c := range children {
					err = inst.convert(parser, tree.GetChild(c), tn)
					if err != nil {
						return
					}
				}
			}
		}
	}

	return
}
func FormatContextTypeName(val any) string {
	s := reflect.ValueOf(val).Type().String()
	s = strings.TrimSuffix(s, "Context")
	_, b, f := strings.Cut(s, ".")
	if f {
		s = b
	}
	s = strings.ToLower(string(s[0])) + s[1:] // to match rule name
	return s
}
func (inst *AntlrTreeToCbor[E]) formatSourceInterval(rctxt *antlr.BaseParserRuleContext) (err error) {
	start := rctxt.GetStart()
	stop := rctxt.GetStop()
	enc := inst.enc
	_, err = enc.EncodeString("start")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(start.GetStart()))
	if err != nil {
		return
	}
	_, err = enc.EncodeString("stop")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(stop.GetStop()))
	if err != nil {
		return
	}
	return
}

const EOFTerminalString = "<EOF>"

func (inst *AntlrTreeToCbor[E]) convertTerminal(parser antlr.Recognizer, node antlr.TerminalNode) (err error) {
	symb := node.GetSymbol()
	symbIdx := symb.GetTokenType()
	if symbIdx == antlr.TokenEOF {
		_, err = inst.enc.EncodeString(EOFTerminalString)
		return
	}
	symbName := parser.GetSymbolicNames()[symbIdx]
	enc := inst.enc
	l := uint64(3)
	if true {
		l++
	}

	_, err = enc.EncodeMapDefinite(l)
	if err != nil {
		return
	}
	_, err = enc.EncodeString("terminal")
	if err != nil {
		return
	}
	_, err = enc.EncodeString(symbName)
	if err != nil {
		return
	}
	if true {
		_, err = enc.EncodeString("text")
		if err != nil {
			return
		}
		_, err = enc.EncodeString(symb.GetText())
		if err != nil {
			return
		}
	}
	sourceInterval := node.GetSourceInterval()
	_, err = enc.EncodeString("start")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(sourceInterval.Start))
	if err != nil {
		return
	}
	_, err = enc.EncodeString("stop")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(sourceInterval.Stop))
	if err != nil {
		return
	}
	return
}
func AddErrorNodeToCborBuilder[R builder.CborKVBuilder[R]](build builder.CborKVBuilder[R], errNode antlr.ErrorNode) {
	build.Str("text", errNode.GetText())
}
