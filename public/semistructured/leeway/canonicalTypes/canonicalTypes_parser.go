package canonicalTypes

import (
	"strconv"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
	grammar2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/grammar"
)

type resetableLexerI interface {
	antlr.Lexer
	SetInputStream(input antlr.CharStream)
}

func NewParser() *Parser {
	errL := antlr4utils.NewStoringErrListener(4, 32, 32, 64)
	lex := grammar2.NewCanonicalTypeSignatureLexer(nil)
	lex.RemoveErrorListeners()
	lex.AddErrorListener(errL)
	tokenStream := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	return &Parser{
		errListener: errL,
		lex:         lex,
		tokenStream: tokenStream,
	}
}
func (inst *Parser) getSyntaxError() (err error) {
	err = inst.errListener.GetSyntheticSyntaxError(64, false)
	return
}
func (inst *Parser) loadString(input string) {
	inst.errListener.Reset()
	inst.lex.SetInputStream(antlr.NewInputStream(input))
	inst.tokenStream.Reset()
}
func (inst *Parser) loadStringAndSetupParser(input string) (parser *grammar2.CanonicalTypeSignatureParser) {
	inst.loadString(input)
	parser = grammar2.NewCanonicalTypeSignatureParser(inst.tokenStream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(inst.errListener)
	return
}

func (inst *Parser) antlrTreeCanonicalTypeToAst(context grammar2.ICanonicalTypeContext) (node PrimitiveAstNodeI, err error) {
	switch ct := context.(type) {
	case *grammar2.CanonicalTypeStringContext:
		p := StringAstNode{
			BaseType:       0,
			WidthModifier:  0,
			Width:          0,
			ScalarModifier: 0,
		}
		baseString := ct.BaseString()
		if baseString != nil {
			if baseString.UTF8_STRING() != nil {
				p.BaseType = BaseTypeStringUtf8
			} else if baseString.BYTE_STRING() != nil {
				p.BaseType = BaseTypeStringBytes
			} else if baseString.BOOL() != nil {
				p.BaseType = BaseTypeStringBool
			}
		}
		widthMod := ct.WidthModifier()
		if widthMod != nil {
			if widthMod.FIXED_MODIFIER() != nil {
				p.WidthModifier = WidthModifierFixed
			}
			number := widthMod.NUMBER()
			if number != nil {
				var w uint64
				w, err = strconv.ParseUint(number.GetText(), 10, 32)
				if err != nil {
					err = eb.Build().Str("text", number.GetText()).Errorf("unable to parse NUMBER literal: %w", err)
					return
				}
				p.Width = Width(w)
			}
		}
		scalarMod := ct.ScalarModifier()
		if scalarMod != nil {
			if scalarMod.SET() != nil {
				p.ScalarModifier = ScalarModifierSet
			} else if scalarMod.HOMOGENOUS_ARRAY() != nil {
				p.ScalarModifier = ScalarModifierHomogenousArray
			}
		}
		node = p
		break
	case *grammar2.CanonicalTypeMachineNumericContext:
		p := MachineNumericTypeAstNode{
			BaseType:          0,
			Width:             0,
			ByteOrderModifier: 0,
			ScalarModifier:    0,
		}
		baseMachineNumeric := ct.BaseMachineNumeric()
		if baseMachineNumeric != nil {
			if baseMachineNumeric.UNSIGNED() != nil {
				p.BaseType = BaseTypeMachineNumericUnsigned
			} else if baseMachineNumeric.SIGNED() != nil {
				p.BaseType = BaseTypeMachineNumericSigned
			} else if baseMachineNumeric.FLOAT() != nil {
				p.BaseType = BaseTypeMachineNumericFloat
			}
		}
		byteOrderMod := ct.ByteOrderModifier()
		if byteOrderMod != nil {
			if byteOrderMod.BIG_ENDIAN() != nil {
				p.ByteOrderModifier = ByteOrderModifierBigEndian
			} else if byteOrderMod.LITTLE_ENDIAN() != nil {
				p.ByteOrderModifier = ByteOrderModifierLittleEndian
			}
		}
		scalarMod := ct.ScalarModifier()
		if scalarMod != nil {
			if scalarMod.SET() != nil {
				p.ScalarModifier = ScalarModifierSet
			} else if scalarMod.HOMOGENOUS_ARRAY() != nil {
				p.ScalarModifier = ScalarModifierHomogenousArray
			}
		}
		number := ct.NUMBER()
		if number != nil {
			var w uint64
			w, err = strconv.ParseUint(number.GetText(), 10, 32)
			if err != nil {
				err = eb.Build().Str("text", number.GetText()).Errorf("unable to parse NUMBER literal: %w", err)
				return
			}
			p.Width = Width(w)
		}
		node = p
		break
	case *grammar2.CanonicalTypeTemporalContext:
		p := TemporalTypeAstNode{
			BaseType:       0,
			Width:          0,
			ScalarModifier: 0,
		}
		baseTemporal := ct.BaseTemporal()
		if baseTemporal != nil {
			if baseTemporal.UTC_DATETIME() != nil {
				p.BaseType = BaseTypeTemporalUtcDatetime
			} else if baseTemporal.ZONED_DATETIME() != nil {
				p.BaseType = BaseTypeTemporalZonedDatetime
			} else if baseTemporal.ZONED_TIME() != nil {
				p.BaseType = BaseTypeTemporalZonedTime
			}
		}
		number := ct.NUMBER()
		if number != nil {
			var w uint64
			w, err = strconv.ParseUint(number.GetText(), 10, 32)
			if err != nil {
				err = eb.Build().Str("text", number.GetText()).Errorf("unable to parse NUMBER literal: %w", err)
				return
			}
			p.Width = Width(w)
		}
		scalarMod := ct.ScalarModifier()
		if scalarMod != nil {
			if scalarMod.SET() != nil {
				p.ScalarModifier = ScalarModifierSet
			} else if scalarMod.HOMOGENOUS_ARRAY() != nil {
				p.ScalarModifier = ScalarModifierHomogenousArray
			}
		}
		node = p
		break
	default:
		err = eb.Build().Type("context", context).Errorf("unimplemented canonical type context: %w", ErrInternalParserError)
	}
	return
}
func (inst *Parser) antlrTreeCanonicalTypeOrGroupToAst(context *grammar2.CanonicalTypeOrGroupContext) (node AstNodeI, err error) {
	ct := context.CanonicalType()
	if ct != nil {
		return inst.antlrTreeCanonicalTypeToAst(ct)
	}
	cg := context.CanonicalTypeGroup()
	if cg == nil {
		err = eh.Errorf("context is expected to be either a canonical type group or canonical type: %w", ErrInternalParserError)
		return
	}
	l := (cg.GetChildCount() - 1) / 2
	members := make([]PrimitiveAstNodeI, 0, l)

	for c := range antlr4utils.IterateAllByType[grammar2.ICanonicalTypeContext](cg) {
		var n PrimitiveAstNodeI
		n, err = inst.antlrTreeCanonicalTypeToAst(c)
		members = append(members, n)
	}
	node = GroupAstNode{
		members: members,
		str:     "",
	}

	return
}
func (inst *Parser) MustParseTypeOrGroupAst(typeOrGroup string) (ast AstNodeI) {
	var err error
	ast, err = inst.ParsePrimitiveTypeOrGroupAst(typeOrGroup)
	if err != nil {
		err = eb.Build().Str("input", typeOrGroup).Errorf("unable to parse canonical type or group")
		return
	}
	return
}
func (inst *Parser) MustParsePrimitiveTypeAst(typeS string) (ast PrimitiveAstNodeI) {
	var err error
	ast, err = inst.ParsePrimitiveTypeAst(typeS)
	if err != nil {
		err = eb.Build().Str("input", typeS).Errorf("unable to parse canonical type")
		return
	}
	return
}
func (inst *Parser) ParsePrimitiveTypeAst(typeS string) (ast PrimitiveAstNodeI, err error) {
	parser := inst.loadStringAndSetupParser(typeS)
	t := parser.SingleCanonicalType()
	err = inst.getSyntaxError()
	if err != nil {
		err = eb.Build().Str("input", typeS).Errorf("error while parsing type or group to AST: %w", err)
		return
	}
	if t.GetChildCount() != 2 {
		err = eb.Build().Str("input", typeS).Int("childCount", t.GetChildCount()).Errorf("expecting exactly two children")
		return
	}
	switch tt := t.GetChild(0).(type) {
	case grammar2.ICanonicalTypeContext:
		ast, err = inst.antlrTreeCanonicalTypeToAst(tt)
		break
	default:
		err = eb.Build().Type("child0", t.GetChild(0)).Errorf("unhandled ast node: %w", ErrInternalParserError)
	}
	return
}
func (inst *Parser) ParsePrimitiveTypeOrGroupAst(typeOrGroup string) (ast AstNodeI, err error) {
	parser := inst.loadStringAndSetupParser(typeOrGroup)
	t := parser.SingleCanonicalTypeOrGroup()
	err = inst.getSyntaxError()
	if err != nil {
		err = eb.Build().Str("input", typeOrGroup).Errorf("error while parsing type or group to AST: %w", err)
		return
	}
	if t.GetChildCount() != 2 {
		err = eb.Build().Str("input", typeOrGroup).Int("childCount", t.GetChildCount()).Errorf("expecting exactly one child")
		return
	}
	switch tt := t.GetChild(0).(type) {
	case *grammar2.CanonicalTypeOrGroupContext:
		ast, err = inst.antlrTreeCanonicalTypeOrGroupToAst(tt)
		break
	default:
		err = ErrInternalParserError
	}
	return
}
func (inst *Parser) ParseSignature(signature string) (parser antlr.Recognizer, tree grammar2.ICanonicalTypeSignatureContext, err error) {
	p := inst.loadStringAndSetupParser(signature)
	tree = p.CanonicalTypeSignature()
	parser = p
	err = inst.getSyntaxError()
	if err != nil {
		err = eb.Build().Str("input", signature).Errorf("error while parsing signature: %w", err)
		return
	}
	return
}
func (inst *Parser) ParseTypeOrGroup(typeOrGroup string) (parser antlr.Recognizer, tree grammar2.ISingleCanonicalTypeOrGroupContext, err error) {
	p := inst.loadStringAndSetupParser(typeOrGroup)
	parser = p
	tree = p.SingleCanonicalTypeOrGroup()
	err = inst.getSyntaxError()
	if err != nil {
		err = eb.Build().Str("input", typeOrGroup).Errorf("error while parsing type or group: %w", err)
		return
	}
	return
}
