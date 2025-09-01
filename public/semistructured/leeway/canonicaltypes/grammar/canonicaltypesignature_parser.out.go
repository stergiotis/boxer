// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import (
	"fmt"
	"strconv"
	"sync"

	"github.com/antlr4-go/antlr/v4"
)

// Suppress unused import errors
var _ = fmt.Printf
var _ = strconv.Itoa
var _ = sync.Once{}

type CanonicalTypeSignatureParser struct {
	*antlr.BaseParser
}

var CanonicalTypeSignatureParserParserStaticData struct {
	once                   sync.Once
	serializedATN          []int32
	LiteralNames           []string
	SymbolicNames          []string
	RuleNames              []string
	PredictionContextCache *antlr.PredictionContextCache
	atn                    *antlr.ATN
	decisionToDFA          []*antlr.DFA
}

func canonicaltypesignatureparserParserInit() {
	staticData := &CanonicalTypeSignatureParserParserStaticData
	staticData.LiteralNames = []string{
		"", "", "'-'", "'s'", "'y'", "'b'", "'u'", "'i'", "'f'", "'z'", "'d'",
		"'t'", "'h'", "'m'", "'l'", "'n'", "'x'",
	}
	staticData.SymbolicNames = []string{
		"", "SEPARATOR", "GROUP_SEPARATOR", "UTF8_STRING", "BYTE_STRING", "BOOL",
		"UNSIGNED", "SIGNED", "FLOAT", "UTC_DATETIME", "ZONED_DATETIME", "ZONED_TIME",
		"HOMOGENOUS_ARRAY", "SET", "LITTLE_ENDIAN", "BIG_ENDIAN", "FIXED_MODIFIER",
		"NUMBER",
	}
	staticData.RuleNames = []string{
		"baseString", "baseMachineNumeric", "baseTemporal", "scalarModifier",
		"byteOrderModifier", "widthModifier", "canonicalType", "canonicalTypeSequence",
		"canonicalTypeGroup", "canonicalTypeOrGroup", "canonicalTypeOrGroupSequence",
		"canonicalTypeSignature", "singleCanonicalType", "singleCanonicalTypeOrGroup",
		"singleCanonicalGroup",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 1, 17, 106, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2, 4, 7,
		4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 2, 9, 7, 9, 2, 10, 7,
		10, 2, 11, 7, 11, 2, 12, 7, 12, 2, 13, 7, 13, 2, 14, 7, 14, 1, 0, 1, 0,
		1, 1, 1, 1, 1, 2, 1, 2, 1, 3, 1, 3, 1, 4, 1, 4, 1, 5, 1, 5, 1, 5, 1, 6,
		1, 6, 3, 6, 46, 8, 6, 1, 6, 3, 6, 49, 8, 6, 1, 6, 1, 6, 1, 6, 3, 6, 54,
		8, 6, 1, 6, 1, 6, 1, 6, 3, 6, 59, 8, 6, 1, 6, 3, 6, 62, 8, 6, 3, 6, 64,
		8, 6, 1, 7, 1, 7, 1, 7, 5, 7, 69, 8, 7, 10, 7, 12, 7, 72, 9, 7, 1, 8, 1,
		8, 1, 8, 5, 8, 77, 8, 8, 10, 8, 12, 8, 80, 9, 8, 1, 9, 1, 9, 3, 9, 84,
		8, 9, 1, 10, 1, 10, 1, 10, 5, 10, 89, 8, 10, 10, 10, 12, 10, 92, 9, 10,
		1, 11, 1, 11, 1, 11, 1, 12, 1, 12, 1, 12, 1, 13, 1, 13, 1, 13, 1, 14, 1,
		14, 1, 14, 1, 14, 0, 0, 15, 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22,
		24, 26, 28, 0, 5, 1, 0, 3, 5, 1, 0, 6, 8, 1, 0, 9, 11, 1, 0, 12, 13, 1,
		0, 14, 15, 101, 0, 30, 1, 0, 0, 0, 2, 32, 1, 0, 0, 0, 4, 34, 1, 0, 0, 0,
		6, 36, 1, 0, 0, 0, 8, 38, 1, 0, 0, 0, 10, 40, 1, 0, 0, 0, 12, 63, 1, 0,
		0, 0, 14, 65, 1, 0, 0, 0, 16, 73, 1, 0, 0, 0, 18, 83, 1, 0, 0, 0, 20, 85,
		1, 0, 0, 0, 22, 93, 1, 0, 0, 0, 24, 96, 1, 0, 0, 0, 26, 99, 1, 0, 0, 0,
		28, 102, 1, 0, 0, 0, 30, 31, 7, 0, 0, 0, 31, 1, 1, 0, 0, 0, 32, 33, 7,
		1, 0, 0, 33, 3, 1, 0, 0, 0, 34, 35, 7, 2, 0, 0, 35, 5, 1, 0, 0, 0, 36,
		37, 7, 3, 0, 0, 37, 7, 1, 0, 0, 0, 38, 39, 7, 4, 0, 0, 39, 9, 1, 0, 0,
		0, 40, 41, 5, 16, 0, 0, 41, 42, 5, 17, 0, 0, 42, 11, 1, 0, 0, 0, 43, 45,
		3, 0, 0, 0, 44, 46, 3, 10, 5, 0, 45, 44, 1, 0, 0, 0, 45, 46, 1, 0, 0, 0,
		46, 48, 1, 0, 0, 0, 47, 49, 3, 6, 3, 0, 48, 47, 1, 0, 0, 0, 48, 49, 1,
		0, 0, 0, 49, 64, 1, 0, 0, 0, 50, 51, 3, 4, 2, 0, 51, 53, 5, 17, 0, 0, 52,
		54, 3, 6, 3, 0, 53, 52, 1, 0, 0, 0, 53, 54, 1, 0, 0, 0, 54, 64, 1, 0, 0,
		0, 55, 56, 3, 2, 1, 0, 56, 58, 5, 17, 0, 0, 57, 59, 3, 8, 4, 0, 58, 57,
		1, 0, 0, 0, 58, 59, 1, 0, 0, 0, 59, 61, 1, 0, 0, 0, 60, 62, 3, 6, 3, 0,
		61, 60, 1, 0, 0, 0, 61, 62, 1, 0, 0, 0, 62, 64, 1, 0, 0, 0, 63, 43, 1,
		0, 0, 0, 63, 50, 1, 0, 0, 0, 63, 55, 1, 0, 0, 0, 64, 13, 1, 0, 0, 0, 65,
		70, 3, 12, 6, 0, 66, 67, 5, 1, 0, 0, 67, 69, 3, 12, 6, 0, 68, 66, 1, 0,
		0, 0, 69, 72, 1, 0, 0, 0, 70, 68, 1, 0, 0, 0, 70, 71, 1, 0, 0, 0, 71, 15,
		1, 0, 0, 0, 72, 70, 1, 0, 0, 0, 73, 78, 3, 12, 6, 0, 74, 75, 5, 2, 0, 0,
		75, 77, 3, 12, 6, 0, 76, 74, 1, 0, 0, 0, 77, 80, 1, 0, 0, 0, 78, 76, 1,
		0, 0, 0, 78, 79, 1, 0, 0, 0, 79, 17, 1, 0, 0, 0, 80, 78, 1, 0, 0, 0, 81,
		84, 3, 12, 6, 0, 82, 84, 3, 16, 8, 0, 83, 81, 1, 0, 0, 0, 83, 82, 1, 0,
		0, 0, 84, 19, 1, 0, 0, 0, 85, 90, 3, 18, 9, 0, 86, 87, 5, 1, 0, 0, 87,
		89, 3, 18, 9, 0, 88, 86, 1, 0, 0, 0, 89, 92, 1, 0, 0, 0, 90, 88, 1, 0,
		0, 0, 90, 91, 1, 0, 0, 0, 91, 21, 1, 0, 0, 0, 92, 90, 1, 0, 0, 0, 93, 94,
		3, 20, 10, 0, 94, 95, 5, 0, 0, 1, 95, 23, 1, 0, 0, 0, 96, 97, 3, 12, 6,
		0, 97, 98, 5, 0, 0, 1, 98, 25, 1, 0, 0, 0, 99, 100, 3, 18, 9, 0, 100, 101,
		5, 0, 0, 1, 101, 27, 1, 0, 0, 0, 102, 103, 3, 16, 8, 0, 103, 104, 5, 0,
		0, 1, 104, 29, 1, 0, 0, 0, 10, 45, 48, 53, 58, 61, 63, 70, 78, 83, 90,
	}
	deserializer := antlr.NewATNDeserializer(nil)
	staticData.atn = deserializer.Deserialize(staticData.serializedATN)
	atn := staticData.atn
	staticData.decisionToDFA = make([]*antlr.DFA, len(atn.DecisionToState))
	decisionToDFA := staticData.decisionToDFA
	for index, state := range atn.DecisionToState {
		decisionToDFA[index] = antlr.NewDFA(state, index)
	}
}

// CanonicalTypeSignatureParserInit initializes any static state used to implement CanonicalTypeSignatureParser. By default the
// static state used to implement the parser is lazily initialized during the first call to
// NewCanonicalTypeSignatureParser(). You can call this function if you wish to initialize the static state ahead
// of time.
func CanonicalTypeSignatureParserInit() {
	staticData := &CanonicalTypeSignatureParserParserStaticData
	staticData.once.Do(canonicaltypesignatureparserParserInit)
}

// NewCanonicalTypeSignatureParser produces a new parser instance for the optional input antlr.TokenStream.
func NewCanonicalTypeSignatureParser(input antlr.TokenStream) *CanonicalTypeSignatureParser {
	CanonicalTypeSignatureParserInit()
	this := new(CanonicalTypeSignatureParser)
	this.BaseParser = antlr.NewBaseParser(input)
	staticData := &CanonicalTypeSignatureParserParserStaticData
	this.Interpreter = antlr.NewParserATNSimulator(this, staticData.atn, staticData.decisionToDFA, staticData.PredictionContextCache)
	this.RuleNames = staticData.RuleNames
	this.LiteralNames = staticData.LiteralNames
	this.SymbolicNames = staticData.SymbolicNames
	this.GrammarFileName = "CanonicalTypeSignatureParser.g4"

	return this
}

// CanonicalTypeSignatureParser tokens.
const (
	CanonicalTypeSignatureParserEOF              = antlr.TokenEOF
	CanonicalTypeSignatureParserSEPARATOR        = 1
	CanonicalTypeSignatureParserGROUP_SEPARATOR  = 2
	CanonicalTypeSignatureParserUTF8_STRING      = 3
	CanonicalTypeSignatureParserBYTE_STRING      = 4
	CanonicalTypeSignatureParserBOOL             = 5
	CanonicalTypeSignatureParserUNSIGNED         = 6
	CanonicalTypeSignatureParserSIGNED           = 7
	CanonicalTypeSignatureParserFLOAT            = 8
	CanonicalTypeSignatureParserUTC_DATETIME     = 9
	CanonicalTypeSignatureParserZONED_DATETIME   = 10
	CanonicalTypeSignatureParserZONED_TIME       = 11
	CanonicalTypeSignatureParserHOMOGENOUS_ARRAY = 12
	CanonicalTypeSignatureParserSET              = 13
	CanonicalTypeSignatureParserLITTLE_ENDIAN    = 14
	CanonicalTypeSignatureParserBIG_ENDIAN       = 15
	CanonicalTypeSignatureParserFIXED_MODIFIER   = 16
	CanonicalTypeSignatureParserNUMBER           = 17
)

// CanonicalTypeSignatureParser rules.
const (
	CanonicalTypeSignatureParserRULE_baseString                   = 0
	CanonicalTypeSignatureParserRULE_baseMachineNumeric           = 1
	CanonicalTypeSignatureParserRULE_baseTemporal                 = 2
	CanonicalTypeSignatureParserRULE_scalarModifier               = 3
	CanonicalTypeSignatureParserRULE_byteOrderModifier            = 4
	CanonicalTypeSignatureParserRULE_widthModifier                = 5
	CanonicalTypeSignatureParserRULE_canonicalType                = 6
	CanonicalTypeSignatureParserRULE_canonicalTypeSequence        = 7
	CanonicalTypeSignatureParserRULE_canonicalTypeGroup           = 8
	CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup         = 9
	CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence = 10
	CanonicalTypeSignatureParserRULE_canonicalTypeSignature       = 11
	CanonicalTypeSignatureParserRULE_singleCanonicalType          = 12
	CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup   = 13
	CanonicalTypeSignatureParserRULE_singleCanonicalGroup         = 14
)

// IBaseStringContext is an interface to support dynamic dispatch.
type IBaseStringContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UTF8_STRING() antlr.TerminalNode
	BYTE_STRING() antlr.TerminalNode
	BOOL() antlr.TerminalNode

	// IsBaseStringContext differentiates from other interfaces.
	IsBaseStringContext()
}

type BaseStringContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBaseStringContext() *BaseStringContext {
	var p = new(BaseStringContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseString
	return p
}

func InitEmptyBaseStringContext(p *BaseStringContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseString
}

func (*BaseStringContext) IsBaseStringContext() {}

func NewBaseStringContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseStringContext {
	var p = new(BaseStringContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseString

	return p
}

func (s *BaseStringContext) GetParser() antlr.Parser { return s.parser }

func (s *BaseStringContext) UTF8_STRING() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserUTF8_STRING, 0)
}

func (s *BaseStringContext) BYTE_STRING() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserBYTE_STRING, 0)
}

func (s *BaseStringContext) BOOL() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserBOOL, 0)
}

func (s *BaseStringContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *BaseStringContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *BaseStringContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitBaseString(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) BaseString() (localctx IBaseStringContext) {
	localctx = NewBaseStringContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 0, CanonicalTypeSignatureParserRULE_baseString)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(30)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&56) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IBaseMachineNumericContext is an interface to support dynamic dispatch.
type IBaseMachineNumericContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UNSIGNED() antlr.TerminalNode
	SIGNED() antlr.TerminalNode
	FLOAT() antlr.TerminalNode

	// IsBaseMachineNumericContext differentiates from other interfaces.
	IsBaseMachineNumericContext()
}

type BaseMachineNumericContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBaseMachineNumericContext() *BaseMachineNumericContext {
	var p = new(BaseMachineNumericContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseMachineNumeric
	return p
}

func InitEmptyBaseMachineNumericContext(p *BaseMachineNumericContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseMachineNumeric
}

func (*BaseMachineNumericContext) IsBaseMachineNumericContext() {}

func NewBaseMachineNumericContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseMachineNumericContext {
	var p = new(BaseMachineNumericContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseMachineNumeric

	return p
}

func (s *BaseMachineNumericContext) GetParser() antlr.Parser { return s.parser }

func (s *BaseMachineNumericContext) UNSIGNED() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserUNSIGNED, 0)
}

func (s *BaseMachineNumericContext) SIGNED() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserSIGNED, 0)
}

func (s *BaseMachineNumericContext) FLOAT() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserFLOAT, 0)
}

func (s *BaseMachineNumericContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *BaseMachineNumericContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *BaseMachineNumericContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitBaseMachineNumeric(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) BaseMachineNumeric() (localctx IBaseMachineNumericContext) {
	localctx = NewBaseMachineNumericContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 2, CanonicalTypeSignatureParserRULE_baseMachineNumeric)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(32)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&448) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IBaseTemporalContext is an interface to support dynamic dispatch.
type IBaseTemporalContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UTC_DATETIME() antlr.TerminalNode
	ZONED_DATETIME() antlr.TerminalNode
	ZONED_TIME() antlr.TerminalNode

	// IsBaseTemporalContext differentiates from other interfaces.
	IsBaseTemporalContext()
}

type BaseTemporalContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBaseTemporalContext() *BaseTemporalContext {
	var p = new(BaseTemporalContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseTemporal
	return p
}

func InitEmptyBaseTemporalContext(p *BaseTemporalContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseTemporal
}

func (*BaseTemporalContext) IsBaseTemporalContext() {}

func NewBaseTemporalContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseTemporalContext {
	var p = new(BaseTemporalContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_baseTemporal

	return p
}

func (s *BaseTemporalContext) GetParser() antlr.Parser { return s.parser }

func (s *BaseTemporalContext) UTC_DATETIME() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserUTC_DATETIME, 0)
}

func (s *BaseTemporalContext) ZONED_DATETIME() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserZONED_DATETIME, 0)
}

func (s *BaseTemporalContext) ZONED_TIME() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserZONED_TIME, 0)
}

func (s *BaseTemporalContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *BaseTemporalContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *BaseTemporalContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitBaseTemporal(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) BaseTemporal() (localctx IBaseTemporalContext) {
	localctx = NewBaseTemporalContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 4, CanonicalTypeSignatureParserRULE_baseTemporal)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(34)
		_la = p.GetTokenStream().LA(1)

		if !((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&3584) != 0) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IScalarModifierContext is an interface to support dynamic dispatch.
type IScalarModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	HOMOGENOUS_ARRAY() antlr.TerminalNode
	SET() antlr.TerminalNode

	// IsScalarModifierContext differentiates from other interfaces.
	IsScalarModifierContext()
}

type ScalarModifierContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyScalarModifierContext() *ScalarModifierContext {
	var p = new(ScalarModifierContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_scalarModifier
	return p
}

func InitEmptyScalarModifierContext(p *ScalarModifierContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_scalarModifier
}

func (*ScalarModifierContext) IsScalarModifierContext() {}

func NewScalarModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ScalarModifierContext {
	var p = new(ScalarModifierContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_scalarModifier

	return p
}

func (s *ScalarModifierContext) GetParser() antlr.Parser { return s.parser }

func (s *ScalarModifierContext) HOMOGENOUS_ARRAY() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserHOMOGENOUS_ARRAY, 0)
}

func (s *ScalarModifierContext) SET() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserSET, 0)
}

func (s *ScalarModifierContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ScalarModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ScalarModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitScalarModifier(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) ScalarModifier() (localctx IScalarModifierContext) {
	localctx = NewScalarModifierContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 6, CanonicalTypeSignatureParserRULE_scalarModifier)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(36)
		_la = p.GetTokenStream().LA(1)

		if !(_la == CanonicalTypeSignatureParserHOMOGENOUS_ARRAY || _la == CanonicalTypeSignatureParserSET) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IByteOrderModifierContext is an interface to support dynamic dispatch.
type IByteOrderModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	BIG_ENDIAN() antlr.TerminalNode
	LITTLE_ENDIAN() antlr.TerminalNode

	// IsByteOrderModifierContext differentiates from other interfaces.
	IsByteOrderModifierContext()
}

type ByteOrderModifierContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyByteOrderModifierContext() *ByteOrderModifierContext {
	var p = new(ByteOrderModifierContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_byteOrderModifier
	return p
}

func InitEmptyByteOrderModifierContext(p *ByteOrderModifierContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_byteOrderModifier
}

func (*ByteOrderModifierContext) IsByteOrderModifierContext() {}

func NewByteOrderModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ByteOrderModifierContext {
	var p = new(ByteOrderModifierContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_byteOrderModifier

	return p
}

func (s *ByteOrderModifierContext) GetParser() antlr.Parser { return s.parser }

func (s *ByteOrderModifierContext) BIG_ENDIAN() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserBIG_ENDIAN, 0)
}

func (s *ByteOrderModifierContext) LITTLE_ENDIAN() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserLITTLE_ENDIAN, 0)
}

func (s *ByteOrderModifierContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ByteOrderModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ByteOrderModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitByteOrderModifier(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) ByteOrderModifier() (localctx IByteOrderModifierContext) {
	localctx = NewByteOrderModifierContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 8, CanonicalTypeSignatureParserRULE_byteOrderModifier)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(38)
		_la = p.GetTokenStream().LA(1)

		if !(_la == CanonicalTypeSignatureParserLITTLE_ENDIAN || _la == CanonicalTypeSignatureParserBIG_ENDIAN) {
			p.GetErrorHandler().RecoverInline(p)
		} else {
			p.GetErrorHandler().ReportMatch(p)
			p.Consume()
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IWidthModifierContext is an interface to support dynamic dispatch.
type IWidthModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	FIXED_MODIFIER() antlr.TerminalNode
	NUMBER() antlr.TerminalNode

	// IsWidthModifierContext differentiates from other interfaces.
	IsWidthModifierContext()
}

type WidthModifierContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyWidthModifierContext() *WidthModifierContext {
	var p = new(WidthModifierContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_widthModifier
	return p
}

func InitEmptyWidthModifierContext(p *WidthModifierContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_widthModifier
}

func (*WidthModifierContext) IsWidthModifierContext() {}

func NewWidthModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *WidthModifierContext {
	var p = new(WidthModifierContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_widthModifier

	return p
}

func (s *WidthModifierContext) GetParser() antlr.Parser { return s.parser }

func (s *WidthModifierContext) FIXED_MODIFIER() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserFIXED_MODIFIER, 0)
}

func (s *WidthModifierContext) NUMBER() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserNUMBER, 0)
}

func (s *WidthModifierContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *WidthModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *WidthModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitWidthModifier(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) WidthModifier() (localctx IWidthModifierContext) {
	localctx = NewWidthModifierContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 10, CanonicalTypeSignatureParserRULE_widthModifier)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(40)
		p.Match(CanonicalTypeSignatureParserFIXED_MODIFIER)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(41)
		p.Match(CanonicalTypeSignatureParserNUMBER)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeContext is an interface to support dynamic dispatch.
type ICanonicalTypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsCanonicalTypeContext differentiates from other interfaces.
	IsCanonicalTypeContext()
}

type CanonicalTypeContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeContext() *CanonicalTypeContext {
	var p = new(CanonicalTypeContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalType
	return p
}

func InitEmptyCanonicalTypeContext(p *CanonicalTypeContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalType
}

func (*CanonicalTypeContext) IsCanonicalTypeContext() {}

func NewCanonicalTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeContext {
	var p = new(CanonicalTypeContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalType

	return p
}

func (s *CanonicalTypeContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeContext) CopyAll(ctx *CanonicalTypeContext) {
	s.CopyFrom(&ctx.BaseParserRuleContext)
}

func (s *CanonicalTypeContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

type CanonicalTypeTemporalContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeTemporalContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeTemporalContext {
	var p = new(CanonicalTypeTemporalContext)

	InitEmptyCanonicalTypeContext(&p.CanonicalTypeContext)
	p.parser = parser
	p.CopyAll(ctx.(*CanonicalTypeContext))

	return p
}

func (s *CanonicalTypeTemporalContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeTemporalContext) BaseTemporal() IBaseTemporalContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBaseTemporalContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBaseTemporalContext)
}

func (s *CanonicalTypeTemporalContext) NUMBER() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserNUMBER, 0)
}

func (s *CanonicalTypeTemporalContext) ScalarModifier() IScalarModifierContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IScalarModifierContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IScalarModifierContext)
}

func (s *CanonicalTypeTemporalContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeTemporal(s)

	default:
		return t.VisitChildren(s)
	}
}

type CanonicalTypeMachineNumericContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeMachineNumericContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeMachineNumericContext {
	var p = new(CanonicalTypeMachineNumericContext)

	InitEmptyCanonicalTypeContext(&p.CanonicalTypeContext)
	p.parser = parser
	p.CopyAll(ctx.(*CanonicalTypeContext))

	return p
}

func (s *CanonicalTypeMachineNumericContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeMachineNumericContext) BaseMachineNumeric() IBaseMachineNumericContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBaseMachineNumericContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBaseMachineNumericContext)
}

func (s *CanonicalTypeMachineNumericContext) NUMBER() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserNUMBER, 0)
}

func (s *CanonicalTypeMachineNumericContext) ByteOrderModifier() IByteOrderModifierContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IByteOrderModifierContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IByteOrderModifierContext)
}

func (s *CanonicalTypeMachineNumericContext) ScalarModifier() IScalarModifierContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IScalarModifierContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IScalarModifierContext)
}

func (s *CanonicalTypeMachineNumericContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeMachineNumeric(s)

	default:
		return t.VisitChildren(s)
	}
}

type CanonicalTypeStringContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeStringContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeStringContext {
	var p = new(CanonicalTypeStringContext)

	InitEmptyCanonicalTypeContext(&p.CanonicalTypeContext)
	p.parser = parser
	p.CopyAll(ctx.(*CanonicalTypeContext))

	return p
}

func (s *CanonicalTypeStringContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeStringContext) BaseString() IBaseStringContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBaseStringContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBaseStringContext)
}

func (s *CanonicalTypeStringContext) WidthModifier() IWidthModifierContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IWidthModifierContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IWidthModifierContext)
}

func (s *CanonicalTypeStringContext) ScalarModifier() IScalarModifierContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IScalarModifierContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IScalarModifierContext)
}

func (s *CanonicalTypeStringContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeString(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalType() (localctx ICanonicalTypeContext) {
	localctx = NewCanonicalTypeContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 12, CanonicalTypeSignatureParserRULE_canonicalType)
	var _la int

	p.SetState(63)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case CanonicalTypeSignatureParserUTF8_STRING, CanonicalTypeSignatureParserBYTE_STRING, CanonicalTypeSignatureParserBOOL:
		localctx = NewCanonicalTypeStringContext(p, localctx)
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(43)
			p.BaseString()
		}
		p.SetState(45)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == CanonicalTypeSignatureParserFIXED_MODIFIER {
			{
				p.SetState(44)
				p.WidthModifier()
			}

		}
		p.SetState(48)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == CanonicalTypeSignatureParserHOMOGENOUS_ARRAY || _la == CanonicalTypeSignatureParserSET {
			{
				p.SetState(47)
				p.ScalarModifier()
			}

		}

	case CanonicalTypeSignatureParserUTC_DATETIME, CanonicalTypeSignatureParserZONED_DATETIME, CanonicalTypeSignatureParserZONED_TIME:
		localctx = NewCanonicalTypeTemporalContext(p, localctx)
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(50)
			p.BaseTemporal()
		}
		{
			p.SetState(51)
			p.Match(CanonicalTypeSignatureParserNUMBER)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(53)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == CanonicalTypeSignatureParserHOMOGENOUS_ARRAY || _la == CanonicalTypeSignatureParserSET {
			{
				p.SetState(52)
				p.ScalarModifier()
			}

		}

	case CanonicalTypeSignatureParserUNSIGNED, CanonicalTypeSignatureParserSIGNED, CanonicalTypeSignatureParserFLOAT:
		localctx = NewCanonicalTypeMachineNumericContext(p, localctx)
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(55)
			p.BaseMachineNumeric()
		}
		{
			p.SetState(56)
			p.Match(CanonicalTypeSignatureParserNUMBER)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		p.SetState(58)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == CanonicalTypeSignatureParserLITTLE_ENDIAN || _la == CanonicalTypeSignatureParserBIG_ENDIAN {
			{
				p.SetState(57)
				p.ByteOrderModifier()
			}

		}
		p.SetState(61)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		if _la == CanonicalTypeSignatureParserHOMOGENOUS_ARRAY || _la == CanonicalTypeSignatureParserSET {
			{
				p.SetState(60)
				p.ScalarModifier()
			}

		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeSequenceContext is an interface to support dynamic dispatch.
type ICanonicalTypeSequenceContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalType() []ICanonicalTypeContext
	CanonicalType(i int) ICanonicalTypeContext
	AllSEPARATOR() []antlr.TerminalNode
	SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeSequenceContext differentiates from other interfaces.
	IsCanonicalTypeSequenceContext()
}

type CanonicalTypeSequenceContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeSequenceContext() *CanonicalTypeSequenceContext {
	var p = new(CanonicalTypeSequenceContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSequence
	return p
}

func InitEmptyCanonicalTypeSequenceContext(p *CanonicalTypeSequenceContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSequence
}

func (*CanonicalTypeSequenceContext) IsCanonicalTypeSequenceContext() {}

func NewCanonicalTypeSequenceContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeSequenceContext {
	var p = new(CanonicalTypeSequenceContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSequence

	return p
}

func (s *CanonicalTypeSequenceContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeSequenceContext) AllCanonicalType() []ICanonicalTypeContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			len++
		}
	}

	tst := make([]ICanonicalTypeContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ICanonicalTypeContext); ok {
			tst[i] = t.(ICanonicalTypeContext)
			i++
		}
	}

	return tst
}

func (s *CanonicalTypeSequenceContext) CanonicalType(i int) ICanonicalTypeContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeContext)
}

func (s *CanonicalTypeSequenceContext) AllSEPARATOR() []antlr.TerminalNode {
	return s.GetTokens(CanonicalTypeSignatureParserSEPARATOR)
}

func (s *CanonicalTypeSequenceContext) SEPARATOR(i int) antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserSEPARATOR, i)
}

func (s *CanonicalTypeSequenceContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeSequenceContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CanonicalTypeSequenceContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeSequence(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeSequence() (localctx ICanonicalTypeSequenceContext) {
	localctx = NewCanonicalTypeSequenceContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 14, CanonicalTypeSignatureParserRULE_canonicalTypeSequence)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(65)
		p.CanonicalType()
	}
	p.SetState(70)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == CanonicalTypeSignatureParserSEPARATOR {
		{
			p.SetState(66)
			p.Match(CanonicalTypeSignatureParserSEPARATOR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(67)
			p.CanonicalType()
		}

		p.SetState(72)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeGroupContext is an interface to support dynamic dispatch.
type ICanonicalTypeGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalType() []ICanonicalTypeContext
	CanonicalType(i int) ICanonicalTypeContext
	AllGROUP_SEPARATOR() []antlr.TerminalNode
	GROUP_SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeGroupContext differentiates from other interfaces.
	IsCanonicalTypeGroupContext()
}

type CanonicalTypeGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeGroupContext() *CanonicalTypeGroupContext {
	var p = new(CanonicalTypeGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeGroup
	return p
}

func InitEmptyCanonicalTypeGroupContext(p *CanonicalTypeGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeGroup
}

func (*CanonicalTypeGroupContext) IsCanonicalTypeGroupContext() {}

func NewCanonicalTypeGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeGroupContext {
	var p = new(CanonicalTypeGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeGroup

	return p
}

func (s *CanonicalTypeGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeGroupContext) AllCanonicalType() []ICanonicalTypeContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			len++
		}
	}

	tst := make([]ICanonicalTypeContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ICanonicalTypeContext); ok {
			tst[i] = t.(ICanonicalTypeContext)
			i++
		}
	}

	return tst
}

func (s *CanonicalTypeGroupContext) CanonicalType(i int) ICanonicalTypeContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeContext)
}

func (s *CanonicalTypeGroupContext) AllGROUP_SEPARATOR() []antlr.TerminalNode {
	return s.GetTokens(CanonicalTypeSignatureParserGROUP_SEPARATOR)
}

func (s *CanonicalTypeGroupContext) GROUP_SEPARATOR(i int) antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserGROUP_SEPARATOR, i)
}

func (s *CanonicalTypeGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CanonicalTypeGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeGroup() (localctx ICanonicalTypeGroupContext) {
	localctx = NewCanonicalTypeGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 16, CanonicalTypeSignatureParserRULE_canonicalTypeGroup)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(73)
		p.CanonicalType()
	}
	p.SetState(78)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == CanonicalTypeSignatureParserGROUP_SEPARATOR {
		{
			p.SetState(74)
			p.Match(CanonicalTypeSignatureParserGROUP_SEPARATOR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(75)
			p.CanonicalType()
		}

		p.SetState(80)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeOrGroupContext is an interface to support dynamic dispatch.
type ICanonicalTypeOrGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalType() ICanonicalTypeContext
	CanonicalTypeGroup() ICanonicalTypeGroupContext

	// IsCanonicalTypeOrGroupContext differentiates from other interfaces.
	IsCanonicalTypeOrGroupContext()
}

type CanonicalTypeOrGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeOrGroupContext() *CanonicalTypeOrGroupContext {
	var p = new(CanonicalTypeOrGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup
	return p
}

func InitEmptyCanonicalTypeOrGroupContext(p *CanonicalTypeOrGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup
}

func (*CanonicalTypeOrGroupContext) IsCanonicalTypeOrGroupContext() {}

func NewCanonicalTypeOrGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeOrGroupContext {
	var p = new(CanonicalTypeOrGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup

	return p
}

func (s *CanonicalTypeOrGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeOrGroupContext) CanonicalType() ICanonicalTypeContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeContext)
}

func (s *CanonicalTypeOrGroupContext) CanonicalTypeGroup() ICanonicalTypeGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeGroupContext)
}

func (s *CanonicalTypeOrGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeOrGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CanonicalTypeOrGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeOrGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeOrGroup() (localctx ICanonicalTypeOrGroupContext) {
	localctx = NewCanonicalTypeOrGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 18, CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup)
	p.SetState(83)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 8, p.GetParserRuleContext()) {
	case 1:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(81)
			p.CanonicalType()
		}

	case 2:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(82)
			p.CanonicalTypeGroup()
		}

	case antlr.ATNInvalidAltNumber:
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeOrGroupSequenceContext is an interface to support dynamic dispatch.
type ICanonicalTypeOrGroupSequenceContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalTypeOrGroup() []ICanonicalTypeOrGroupContext
	CanonicalTypeOrGroup(i int) ICanonicalTypeOrGroupContext
	AllSEPARATOR() []antlr.TerminalNode
	SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeOrGroupSequenceContext differentiates from other interfaces.
	IsCanonicalTypeOrGroupSequenceContext()
}

type CanonicalTypeOrGroupSequenceContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeOrGroupSequenceContext() *CanonicalTypeOrGroupSequenceContext {
	var p = new(CanonicalTypeOrGroupSequenceContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence
	return p
}

func InitEmptyCanonicalTypeOrGroupSequenceContext(p *CanonicalTypeOrGroupSequenceContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence
}

func (*CanonicalTypeOrGroupSequenceContext) IsCanonicalTypeOrGroupSequenceContext() {}

func NewCanonicalTypeOrGroupSequenceContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeOrGroupSequenceContext {
	var p = new(CanonicalTypeOrGroupSequenceContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence

	return p
}

func (s *CanonicalTypeOrGroupSequenceContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeOrGroupSequenceContext) AllCanonicalTypeOrGroup() []ICanonicalTypeOrGroupContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ICanonicalTypeOrGroupContext); ok {
			len++
		}
	}

	tst := make([]ICanonicalTypeOrGroupContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ICanonicalTypeOrGroupContext); ok {
			tst[i] = t.(ICanonicalTypeOrGroupContext)
			i++
		}
	}

	return tst
}

func (s *CanonicalTypeOrGroupSequenceContext) CanonicalTypeOrGroup(i int) ICanonicalTypeOrGroupContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeOrGroupContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeOrGroupContext)
}

func (s *CanonicalTypeOrGroupSequenceContext) AllSEPARATOR() []antlr.TerminalNode {
	return s.GetTokens(CanonicalTypeSignatureParserSEPARATOR)
}

func (s *CanonicalTypeOrGroupSequenceContext) SEPARATOR(i int) antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserSEPARATOR, i)
}

func (s *CanonicalTypeOrGroupSequenceContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeOrGroupSequenceContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CanonicalTypeOrGroupSequenceContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeOrGroupSequence(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeOrGroupSequence() (localctx ICanonicalTypeOrGroupSequenceContext) {
	localctx = NewCanonicalTypeOrGroupSequenceContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 20, CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(85)
		p.CanonicalTypeOrGroup()
	}
	p.SetState(90)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == CanonicalTypeSignatureParserSEPARATOR {
		{
			p.SetState(86)
			p.Match(CanonicalTypeSignatureParserSEPARATOR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(87)
			p.CanonicalTypeOrGroup()
		}

		p.SetState(92)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ICanonicalTypeSignatureContext is an interface to support dynamic dispatch.
type ICanonicalTypeSignatureContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeOrGroupSequence() ICanonicalTypeOrGroupSequenceContext
	EOF() antlr.TerminalNode

	// IsCanonicalTypeSignatureContext differentiates from other interfaces.
	IsCanonicalTypeSignatureContext()
}

type CanonicalTypeSignatureContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyCanonicalTypeSignatureContext() *CanonicalTypeSignatureContext {
	var p = new(CanonicalTypeSignatureContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSignature
	return p
}

func InitEmptyCanonicalTypeSignatureContext(p *CanonicalTypeSignatureContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSignature
}

func (*CanonicalTypeSignatureContext) IsCanonicalTypeSignatureContext() {}

func NewCanonicalTypeSignatureContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeSignatureContext {
	var p = new(CanonicalTypeSignatureContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_canonicalTypeSignature

	return p
}

func (s *CanonicalTypeSignatureContext) GetParser() antlr.Parser { return s.parser }

func (s *CanonicalTypeSignatureContext) CanonicalTypeOrGroupSequence() ICanonicalTypeOrGroupSequenceContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeOrGroupSequenceContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeOrGroupSequenceContext)
}

func (s *CanonicalTypeSignatureContext) EOF() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserEOF, 0)
}

func (s *CanonicalTypeSignatureContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *CanonicalTypeSignatureContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *CanonicalTypeSignatureContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitCanonicalTypeSignature(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeSignature() (localctx ICanonicalTypeSignatureContext) {
	localctx = NewCanonicalTypeSignatureContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 22, CanonicalTypeSignatureParserRULE_canonicalTypeSignature)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(93)
		p.CanonicalTypeOrGroupSequence()
	}
	{
		p.SetState(94)
		p.Match(CanonicalTypeSignatureParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ISingleCanonicalTypeContext is an interface to support dynamic dispatch.
type ISingleCanonicalTypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalType() ICanonicalTypeContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalTypeContext differentiates from other interfaces.
	IsSingleCanonicalTypeContext()
}

type SingleCanonicalTypeContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySingleCanonicalTypeContext() *SingleCanonicalTypeContext {
	var p = new(SingleCanonicalTypeContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalType
	return p
}

func InitEmptySingleCanonicalTypeContext(p *SingleCanonicalTypeContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalType
}

func (*SingleCanonicalTypeContext) IsSingleCanonicalTypeContext() {}

func NewSingleCanonicalTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalTypeContext {
	var p = new(SingleCanonicalTypeContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalType

	return p
}

func (s *SingleCanonicalTypeContext) GetParser() antlr.Parser { return s.parser }

func (s *SingleCanonicalTypeContext) CanonicalType() ICanonicalTypeContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeContext)
}

func (s *SingleCanonicalTypeContext) EOF() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserEOF, 0)
}

func (s *SingleCanonicalTypeContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *SingleCanonicalTypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *SingleCanonicalTypeContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitSingleCanonicalType(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalType() (localctx ISingleCanonicalTypeContext) {
	localctx = NewSingleCanonicalTypeContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 24, CanonicalTypeSignatureParserRULE_singleCanonicalType)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(96)
		p.CanonicalType()
	}
	{
		p.SetState(97)
		p.Match(CanonicalTypeSignatureParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ISingleCanonicalTypeOrGroupContext is an interface to support dynamic dispatch.
type ISingleCanonicalTypeOrGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeOrGroup() ICanonicalTypeOrGroupContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalTypeOrGroupContext differentiates from other interfaces.
	IsSingleCanonicalTypeOrGroupContext()
}

type SingleCanonicalTypeOrGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySingleCanonicalTypeOrGroupContext() *SingleCanonicalTypeOrGroupContext {
	var p = new(SingleCanonicalTypeOrGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup
	return p
}

func InitEmptySingleCanonicalTypeOrGroupContext(p *SingleCanonicalTypeOrGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup
}

func (*SingleCanonicalTypeOrGroupContext) IsSingleCanonicalTypeOrGroupContext() {}

func NewSingleCanonicalTypeOrGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalTypeOrGroupContext {
	var p = new(SingleCanonicalTypeOrGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup

	return p
}

func (s *SingleCanonicalTypeOrGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *SingleCanonicalTypeOrGroupContext) CanonicalTypeOrGroup() ICanonicalTypeOrGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeOrGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeOrGroupContext)
}

func (s *SingleCanonicalTypeOrGroupContext) EOF() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserEOF, 0)
}

func (s *SingleCanonicalTypeOrGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *SingleCanonicalTypeOrGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *SingleCanonicalTypeOrGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitSingleCanonicalTypeOrGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalTypeOrGroup() (localctx ISingleCanonicalTypeOrGroupContext) {
	localctx = NewSingleCanonicalTypeOrGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 26, CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(99)
		p.CanonicalTypeOrGroup()
	}
	{
		p.SetState(100)
		p.Match(CanonicalTypeSignatureParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ISingleCanonicalGroupContext is an interface to support dynamic dispatch.
type ISingleCanonicalGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeGroup() ICanonicalTypeGroupContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalGroupContext differentiates from other interfaces.
	IsSingleCanonicalGroupContext()
}

type SingleCanonicalGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySingleCanonicalGroupContext() *SingleCanonicalGroupContext {
	var p = new(SingleCanonicalGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalGroup
	return p
}

func InitEmptySingleCanonicalGroupContext(p *SingleCanonicalGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalGroup
}

func (*SingleCanonicalGroupContext) IsSingleCanonicalGroupContext() {}

func NewSingleCanonicalGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalGroupContext {
	var p = new(SingleCanonicalGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = CanonicalTypeSignatureParserRULE_singleCanonicalGroup

	return p
}

func (s *SingleCanonicalGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *SingleCanonicalGroupContext) CanonicalTypeGroup() ICanonicalTypeGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ICanonicalTypeGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ICanonicalTypeGroupContext)
}

func (s *SingleCanonicalGroupContext) EOF() antlr.TerminalNode {
	return s.GetToken(CanonicalTypeSignatureParserEOF, 0)
}

func (s *SingleCanonicalGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *SingleCanonicalGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *SingleCanonicalGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case CanonicalTypeSignatureParserVisitor:
		return t.VisitSingleCanonicalGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalGroup() (localctx ISingleCanonicalGroupContext) {
	localctx = NewSingleCanonicalGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 28, CanonicalTypeSignatureParserRULE_singleCanonicalGroup)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(102)
		p.CanonicalTypeGroup()
	}
	{
		p.SetState(103)
		p.Match(CanonicalTypeSignatureParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}
