// Code generated from CanonicalTypeSignatureLexer.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar

import (

"fmt"
"sync"
"unicode"

"github.com/antlr4-go/antlr/v4"
)

// Suppress unused import error
var _ = fmt.Printf
var _ = sync.Once{}
var _ = unicode.IsLetter

type CanonicalTypeSignatureLexer struct {
	*antlr.BaseLexer
	channelNames []string
	modeNames    []string
	// TODO: EOF string
}

var CanonicalTypeSignatureLexerLexerStaticData struct {
	once                   sync.Once
	serializedATN          []int32
	ChannelNames           []string
	ModeNames              []string
	LiteralNames           []string
	SymbolicNames          []string
	RuleNames              []string
	PredictionContextCache *antlr.PredictionContextCache
	atn                    *antlr.ATN
	decisionToDFA          []*antlr.DFA
}

func canonicaltypesignaturelexerLexerInit() {
	staticData := &CanonicalTypeSignatureLexerLexerStaticData
	staticData.ChannelNames = []string{
		"DEFAULT_TOKEN_CHANNEL", "HIDDEN",
	}
	staticData.ModeNames = []string{
		"DEFAULT_MODE",
	}
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
		"SEPARATOR", "GROUP_SEPARATOR", "UTF8_STRING", "BYTE_STRING", "BOOL",
		"UNSIGNED", "SIGNED", "FLOAT", "UTC_DATETIME", "ZONED_DATETIME", "ZONED_TIME",
		"HOMOGENOUS_ARRAY", "SET", "LITTLE_ENDIAN", "BIG_ENDIAN", "FIXED_MODIFIER",
		"NUMBER",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 0, 17, 77, 6, -1, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2,
		4, 7, 4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 2, 9, 7, 9, 2,
		10, 7, 10, 2, 11, 7, 11, 2, 12, 7, 12, 2, 13, 7, 13, 2, 14, 7, 14, 2, 15,
		7, 15, 2, 16, 7, 16, 1, 0, 4, 0, 37, 8, 0, 11, 0, 12, 0, 38, 1, 1, 1, 1,
		1, 2, 1, 2, 1, 3, 1, 3, 1, 4, 1, 4, 1, 5, 1, 5, 1, 6, 1, 6, 1, 7, 1, 7,
		1, 8, 1, 8, 1, 9, 1, 9, 1, 10, 1, 10, 1, 11, 1, 11, 1, 12, 1, 12, 1, 13,
		1, 13, 1, 14, 1, 14, 1, 15, 1, 15, 1, 16, 1, 16, 5, 16, 73, 8, 16, 10,
		16, 12, 16, 76, 9, 16, 0, 0, 17, 1, 1, 3, 2, 5, 3, 7, 4, 9, 5, 11, 6, 13,
		7, 15, 8, 17, 9, 19, 10, 21, 11, 23, 12, 25, 13, 27, 14, 29, 15, 31, 16,
		33, 17, 1, 0, 3, 4, 0, 32, 32, 44, 44, 59, 59, 95, 95, 1, 0, 49, 57, 1,
		0, 48, 57, 78, 0, 1, 1, 0, 0, 0, 0, 3, 1, 0, 0, 0, 0, 5, 1, 0, 0, 0, 0,
		7, 1, 0, 0, 0, 0, 9, 1, 0, 0, 0, 0, 11, 1, 0, 0, 0, 0, 13, 1, 0, 0, 0,
		0, 15, 1, 0, 0, 0, 0, 17, 1, 0, 0, 0, 0, 19, 1, 0, 0, 0, 0, 21, 1, 0, 0,
		0, 0, 23, 1, 0, 0, 0, 0, 25, 1, 0, 0, 0, 0, 27, 1, 0, 0, 0, 0, 29, 1, 0,
		0, 0, 0, 31, 1, 0, 0, 0, 0, 33, 1, 0, 0, 0, 1, 36, 1, 0, 0, 0, 3, 40, 1,
		0, 0, 0, 5, 42, 1, 0, 0, 0, 7, 44, 1, 0, 0, 0, 9, 46, 1, 0, 0, 0, 11, 48,
		1, 0, 0, 0, 13, 50, 1, 0, 0, 0, 15, 52, 1, 0, 0, 0, 17, 54, 1, 0, 0, 0,
		19, 56, 1, 0, 0, 0, 21, 58, 1, 0, 0, 0, 23, 60, 1, 0, 0, 0, 25, 62, 1,
		0, 0, 0, 27, 64, 1, 0, 0, 0, 29, 66, 1, 0, 0, 0, 31, 68, 1, 0, 0, 0, 33,
		70, 1, 0, 0, 0, 35, 37, 7, 0, 0, 0, 36, 35, 1, 0, 0, 0, 37, 38, 1, 0, 0,
		0, 38, 36, 1, 0, 0, 0, 38, 39, 1, 0, 0, 0, 39, 2, 1, 0, 0, 0, 40, 41, 5,
		45, 0, 0, 41, 4, 1, 0, 0, 0, 42, 43, 5, 115, 0, 0, 43, 6, 1, 0, 0, 0, 44,
		45, 5, 121, 0, 0, 45, 8, 1, 0, 0, 0, 46, 47, 5, 98, 0, 0, 47, 10, 1, 0,
		0, 0, 48, 49, 5, 117, 0, 0, 49, 12, 1, 0, 0, 0, 50, 51, 5, 105, 0, 0, 51,
		14, 1, 0, 0, 0, 52, 53, 5, 102, 0, 0, 53, 16, 1, 0, 0, 0, 54, 55, 5, 122,
		0, 0, 55, 18, 1, 0, 0, 0, 56, 57, 5, 100, 0, 0, 57, 20, 1, 0, 0, 0, 58,
		59, 5, 116, 0, 0, 59, 22, 1, 0, 0, 0, 60, 61, 5, 104, 0, 0, 61, 24, 1,
		0, 0, 0, 62, 63, 5, 109, 0, 0, 63, 26, 1, 0, 0, 0, 64, 65, 5, 108, 0, 0,
		65, 28, 1, 0, 0, 0, 66, 67, 5, 110, 0, 0, 67, 30, 1, 0, 0, 0, 68, 69, 5,
		120, 0, 0, 69, 32, 1, 0, 0, 0, 70, 74, 7, 1, 0, 0, 71, 73, 7, 2, 0, 0,
		72, 71, 1, 0, 0, 0, 73, 76, 1, 0, 0, 0, 74, 72, 1, 0, 0, 0, 74, 75, 1,
		0, 0, 0, 75, 34, 1, 0, 0, 0, 76, 74, 1, 0, 0, 0, 3, 0, 38, 74, 0,
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

// CanonicalTypeSignatureLexerInit initializes any static state used to implement CanonicalTypeSignatureLexer. By default the
// static state used to implement the lexer is lazily initialized during the first call to
// NewCanonicalTypeSignatureLexer(). You can call this function if you wish to initialize the static state ahead
// of time.
func CanonicalTypeSignatureLexerInit() {
	staticData := &CanonicalTypeSignatureLexerLexerStaticData
	staticData.once.Do(canonicaltypesignaturelexerLexerInit)
}

// NewCanonicalTypeSignatureLexer produces a new lexer instance for the optional input antlr.CharStream.
func NewCanonicalTypeSignatureLexer(input antlr.CharStream) *CanonicalTypeSignatureLexer {
	CanonicalTypeSignatureLexerInit()
	l := new(CanonicalTypeSignatureLexer)
	l.BaseLexer = antlr.NewBaseLexer(input)
	staticData := &CanonicalTypeSignatureLexerLexerStaticData
	l.Interpreter = antlr.NewLexerATNSimulator(l, staticData.atn, staticData.decisionToDFA, staticData.PredictionContextCache)
	l.channelNames = staticData.ChannelNames
	l.modeNames = staticData.ModeNames
	l.RuleNames = staticData.RuleNames
	l.LiteralNames = staticData.LiteralNames
	l.SymbolicNames = staticData.SymbolicNames
	l.GrammarFileName = "CanonicalTypeSignatureLexer.g4"
	// TODO: l.EOF = antlr.TokenEOF

	return l
}

// CanonicalTypeSignatureLexer tokens.
const (
	CanonicalTypeSignatureLexerSEPARATOR        = 1
	CanonicalTypeSignatureLexerGROUP_SEPARATOR  = 2
	CanonicalTypeSignatureLexerUTF8_STRING      = 3
	CanonicalTypeSignatureLexerBYTE_STRING      = 4
	CanonicalTypeSignatureLexerBOOL             = 5
	CanonicalTypeSignatureLexerUNSIGNED         = 6
	CanonicalTypeSignatureLexerSIGNED           = 7
	CanonicalTypeSignatureLexerFLOAT            = 8
	CanonicalTypeSignatureLexerUTC_DATETIME     = 9
	CanonicalTypeSignatureLexerZONED_DATETIME   = 10
	CanonicalTypeSignatureLexerZONED_TIME       = 11
	CanonicalTypeSignatureLexerHOMOGENOUS_ARRAY = 12
	CanonicalTypeSignatureLexerSET              = 13
	CanonicalTypeSignatureLexerLITTLE_ENDIAN    = 14
	CanonicalTypeSignatureLexerBIG_ENDIAN       = 15
	CanonicalTypeSignatureLexerFIXED_MODIFIER   = 16
	CanonicalTypeSignatureLexerNUMBER           = 17
)
