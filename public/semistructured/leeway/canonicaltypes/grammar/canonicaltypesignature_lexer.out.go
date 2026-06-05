// Code generated from CanonicalTypeSignatureLexer.g4 by ANTLR 4.13.2. DO NOT EDIT.

package grammar

import (
	"fmt"
	"github.com/antlr4-go/antlr/v4"
	"sync"
	"unicode"
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
		"'t'", "'h'", "'m'", "'l'", "'n'", "'x'", "'v'", "'w'", "'c'",
	}
	staticData.SymbolicNames = []string{
		"", "SEPARATOR", "GROUP_SEPARATOR", "UTF8_STRING", "BYTE_STRING", "BOOL",
		"UNSIGNED", "SIGNED", "FLOAT", "UTC_DATETIME", "ZONED_DATETIME", "ZONED_TIME",
		"HOMOGENOUS_ARRAY", "SET", "LITTLE_ENDIAN", "BIG_ENDIAN", "FIXED_MODIFIER",
		"IPV4", "IPV6", "CIDR_MODIFIER", "NUMBER",
	}
	staticData.RuleNames = []string{
		"SEPARATOR", "GROUP_SEPARATOR", "UTF8_STRING", "BYTE_STRING", "BOOL",
		"UNSIGNED", "SIGNED", "FLOAT", "UTC_DATETIME", "ZONED_DATETIME", "ZONED_TIME",
		"HOMOGENOUS_ARRAY", "SET", "LITTLE_ENDIAN", "BIG_ENDIAN", "FIXED_MODIFIER",
		"IPV4", "IPV6", "CIDR_MODIFIER", "NUMBER",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 0, 20, 89, 6, -1, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2,
		4, 7, 4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 2, 9, 7, 9, 2,
		10, 7, 10, 2, 11, 7, 11, 2, 12, 7, 12, 2, 13, 7, 13, 2, 14, 7, 14, 2, 15,
		7, 15, 2, 16, 7, 16, 2, 17, 7, 17, 2, 18, 7, 18, 2, 19, 7, 19, 1, 0, 4,
		0, 43, 8, 0, 11, 0, 12, 0, 44, 1, 1, 1, 1, 1, 2, 1, 2, 1, 3, 1, 3, 1, 4,
		1, 4, 1, 5, 1, 5, 1, 6, 1, 6, 1, 7, 1, 7, 1, 8, 1, 8, 1, 9, 1, 9, 1, 10,
		1, 10, 1, 11, 1, 11, 1, 12, 1, 12, 1, 13, 1, 13, 1, 14, 1, 14, 1, 15, 1,
		15, 1, 16, 1, 16, 1, 17, 1, 17, 1, 18, 1, 18, 1, 19, 1, 19, 5, 19, 85,
		8, 19, 10, 19, 12, 19, 88, 9, 19, 0, 0, 20, 1, 1, 3, 2, 5, 3, 7, 4, 9,
		5, 11, 6, 13, 7, 15, 8, 17, 9, 19, 10, 21, 11, 23, 12, 25, 13, 27, 14,
		29, 15, 31, 16, 33, 17, 35, 18, 37, 19, 39, 20, 1, 0, 3, 4, 0, 32, 32,
		44, 44, 59, 59, 95, 95, 1, 0, 49, 57, 1, 0, 48, 57, 90, 0, 1, 1, 0, 0,
		0, 0, 3, 1, 0, 0, 0, 0, 5, 1, 0, 0, 0, 0, 7, 1, 0, 0, 0, 0, 9, 1, 0, 0,
		0, 0, 11, 1, 0, 0, 0, 0, 13, 1, 0, 0, 0, 0, 15, 1, 0, 0, 0, 0, 17, 1, 0,
		0, 0, 0, 19, 1, 0, 0, 0, 0, 21, 1, 0, 0, 0, 0, 23, 1, 0, 0, 0, 0, 25, 1,
		0, 0, 0, 0, 27, 1, 0, 0, 0, 0, 29, 1, 0, 0, 0, 0, 31, 1, 0, 0, 0, 0, 33,
		1, 0, 0, 0, 0, 35, 1, 0, 0, 0, 0, 37, 1, 0, 0, 0, 0, 39, 1, 0, 0, 0, 1,
		42, 1, 0, 0, 0, 3, 46, 1, 0, 0, 0, 5, 48, 1, 0, 0, 0, 7, 50, 1, 0, 0, 0,
		9, 52, 1, 0, 0, 0, 11, 54, 1, 0, 0, 0, 13, 56, 1, 0, 0, 0, 15, 58, 1, 0,
		0, 0, 17, 60, 1, 0, 0, 0, 19, 62, 1, 0, 0, 0, 21, 64, 1, 0, 0, 0, 23, 66,
		1, 0, 0, 0, 25, 68, 1, 0, 0, 0, 27, 70, 1, 0, 0, 0, 29, 72, 1, 0, 0, 0,
		31, 74, 1, 0, 0, 0, 33, 76, 1, 0, 0, 0, 35, 78, 1, 0, 0, 0, 37, 80, 1,
		0, 0, 0, 39, 82, 1, 0, 0, 0, 41, 43, 7, 0, 0, 0, 42, 41, 1, 0, 0, 0, 43,
		44, 1, 0, 0, 0, 44, 42, 1, 0, 0, 0, 44, 45, 1, 0, 0, 0, 45, 2, 1, 0, 0,
		0, 46, 47, 5, 45, 0, 0, 47, 4, 1, 0, 0, 0, 48, 49, 5, 115, 0, 0, 49, 6,
		1, 0, 0, 0, 50, 51, 5, 121, 0, 0, 51, 8, 1, 0, 0, 0, 52, 53, 5, 98, 0,
		0, 53, 10, 1, 0, 0, 0, 54, 55, 5, 117, 0, 0, 55, 12, 1, 0, 0, 0, 56, 57,
		5, 105, 0, 0, 57, 14, 1, 0, 0, 0, 58, 59, 5, 102, 0, 0, 59, 16, 1, 0, 0,
		0, 60, 61, 5, 122, 0, 0, 61, 18, 1, 0, 0, 0, 62, 63, 5, 100, 0, 0, 63,
		20, 1, 0, 0, 0, 64, 65, 5, 116, 0, 0, 65, 22, 1, 0, 0, 0, 66, 67, 5, 104,
		0, 0, 67, 24, 1, 0, 0, 0, 68, 69, 5, 109, 0, 0, 69, 26, 1, 0, 0, 0, 70,
		71, 5, 108, 0, 0, 71, 28, 1, 0, 0, 0, 72, 73, 5, 110, 0, 0, 73, 30, 1,
		0, 0, 0, 74, 75, 5, 120, 0, 0, 75, 32, 1, 0, 0, 0, 76, 77, 5, 118, 0, 0,
		77, 34, 1, 0, 0, 0, 78, 79, 5, 119, 0, 0, 79, 36, 1, 0, 0, 0, 80, 81, 5,
		99, 0, 0, 81, 38, 1, 0, 0, 0, 82, 86, 7, 1, 0, 0, 83, 85, 7, 2, 0, 0, 84,
		83, 1, 0, 0, 0, 85, 88, 1, 0, 0, 0, 86, 84, 1, 0, 0, 0, 86, 87, 1, 0, 0,
		0, 87, 40, 1, 0, 0, 0, 88, 86, 1, 0, 0, 0, 3, 0, 44, 86, 0,
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
	CanonicalTypeSignatureLexerIPV4             = 17
	CanonicalTypeSignatureLexerIPV6             = 18
	CanonicalTypeSignatureLexerCIDR_MODIFIER    = 19
	CanonicalTypeSignatureLexerNUMBER           = 20
)
