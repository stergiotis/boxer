package antlr4utils

import (
	"errors"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type StoringErrListener struct {
	ConflictsStart        []int    `json:"conflicts_start"`
	ConflictsStop         []int    `json:"conflicts_stop"`
	AmbStart              []int    `json:"amb_start"`
	AmbStop               []int    `json:"amb_stop"`
	SyntaxErrorsLine      []int    `json:"syntax_errors_line"`
	SyntaxErrorsColumn    []int    `json:"syntax_errors_column"`
	SyntaxErrorsStart     []int    `json:"syntax_errors_start"`
	SyntaxErrorsStop      []int    `json:"syntax_errors_stop"`
	SyntaxErrorsMessage   []string `json:"syntax_errors_message"`
	SyntaxErrorsRecovered []bool   `json:"syntax_errors_recovered"`
	MaxConflictsToRecord  int
	MaxAmbToRecord        int
	MaxErrorsToRecord     int
}

func NewStoringErrListener(estConflicts int, maxConflictsToRecord int, maxAmbToRecord int, maxErrorsToRecord int) *StoringErrListener {
	return &StoringErrListener{
		ConflictsStart:        make([]int, 0, estConflicts),
		ConflictsStop:         make([]int, 0, estConflicts),
		AmbStart:              make([]int, 0, estConflicts),
		AmbStop:               make([]int, 0, estConflicts),
		SyntaxErrorsLine:      make([]int, 0, 4),
		SyntaxErrorsColumn:    make([]int, 0, 4),
		SyntaxErrorsStart:     make([]int, 0, 4),
		SyntaxErrorsStop:      make([]int, 0, 4),
		SyntaxErrorsMessage:   make([]string, 0, 4),
		SyntaxErrorsRecovered: make([]bool, 0, 4),
		MaxConflictsToRecord:  maxConflictsToRecord,
		MaxAmbToRecord:        maxAmbToRecord,
		MaxErrorsToRecord:     maxErrorsToRecord,
	}
}
func (inst *StoringErrListener) composeError(i int, skipRecovered bool) (err error) {
	recovered := inst.SyntaxErrorsRecovered[i]
	if recovered && skipRecovered {
		return
	}
	err = eb.Build().Int("start", inst.SyntaxErrorsStart[i]).
		Int("stop", inst.SyntaxErrorsStop[i]).
		Int("line", inst.SyntaxErrorsLine[i]).
		Int("column", inst.SyntaxErrorsColumn[i]).
		Bool("recovered", recovered).
		Str("message", inst.SyntaxErrorsMessage[i]).Errorf("syntax error")
	return
}
func (inst *StoringErrListener) GetSyntheticSyntaxError(maxErrorsToJoin int, skipRecovered bool) (err error) {
	l := len(inst.SyntaxErrorsColumn)
	if l == 0 {
		return
	}
	l = min(l, maxErrorsToJoin)
	errs := make([]error, 0, l)
	for i := 0; i < l; i++ {
		e := inst.composeError(i, skipRecovered)
		if e != nil {
			errs = append(errs, e)
		}
	}
	err = errors.Join(errs...)
	return
}

func (inst *StoringErrListener) Reset() {
	inst.ConflictsStart = inst.ConflictsStart[:0]
	inst.ConflictsStop = inst.ConflictsStop[:0]
	inst.AmbStart = inst.AmbStart[:0]
	inst.AmbStop = inst.AmbStop[:0]
	inst.SyntaxErrorsLine = inst.SyntaxErrorsLine[:0]
	inst.SyntaxErrorsColumn = inst.SyntaxErrorsColumn[:0]
	inst.SyntaxErrorsStart = inst.SyntaxErrorsStart[:0]
	inst.SyntaxErrorsStop = inst.SyntaxErrorsStop[:0]
	inst.SyntaxErrorsMessage = inst.SyntaxErrorsMessage[:0]
	inst.SyntaxErrorsRecovered = inst.SyntaxErrorsRecovered[:0]
}

func (inst *StoringErrListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{}, line, column int, msg string, e antlr.RecognitionException) {
	if len(inst.SyntaxErrorsMessage) < inst.MaxErrorsToRecord {
		inst.SyntaxErrorsLine = append(inst.SyntaxErrorsLine, line)
		inst.SyntaxErrorsColumn = append(inst.SyntaxErrorsColumn, column)
		if e != nil {
			if msg == "" {
				msg = e.GetMessage()
			} else if msg != e.GetMessage() {
				msg = msg + e.GetMessage()
			}
			inst.SyntaxErrorsMessage = append(inst.SyntaxErrorsMessage, msg)
			token := e.GetOffendingToken()
			if token != nil {
				inst.SyntaxErrorsStart = append(inst.SyntaxErrorsStart, token.GetStart())
				inst.SyntaxErrorsStop = append(inst.SyntaxErrorsStop, token.GetStop())
			} else {
				inst.SyntaxErrorsStart = append(inst.SyntaxErrorsStart, -1)
				inst.SyntaxErrorsStop = append(inst.SyntaxErrorsStop, -1)
			}
			inst.SyntaxErrorsRecovered = append(inst.SyntaxErrorsRecovered, false)
			recognizer.SetError(e)
		} else {
			inst.SyntaxErrorsMessage = append(inst.SyntaxErrorsMessage, msg)
			inst.SyntaxErrorsStart = append(inst.SyntaxErrorsStart, -1)
			inst.SyntaxErrorsStop = append(inst.SyntaxErrorsStop, -1)
			inst.SyntaxErrorsRecovered = append(inst.SyntaxErrorsRecovered, true)
		}
	}
}

func (inst *StoringErrListener) ReportAmbiguity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, exact bool, ambigAlts *antlr.BitSet, configs *antlr.ATNConfigSet) {
	tokens := recognizer.GetTokenStream()
	start := tokens.Get(startIndex).GetStart()
	stop := tokens.Get(stopIndex).GetStop()
	if len(inst.AmbStart) < inst.MaxAmbToRecord {
		inst.AmbStart = append(inst.AmbStart, start)
		inst.AmbStop = append(inst.AmbStop, stop)
	}
}

func (inst *StoringErrListener) ReportAttemptingFullContext(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex int, conflictingAlts *antlr.BitSet, configs *antlr.ATNConfigSet) {
	tokens := recognizer.GetTokenStream()
	start := tokens.Get(startIndex).GetStart()
	stop := tokens.Get(stopIndex).GetStop()
	if len(inst.ConflictsStart) < inst.MaxConflictsToRecord {
		inst.ConflictsStart = append(inst.ConflictsStart, start)
		inst.ConflictsStop = append(inst.ConflictsStop, stop)
	}
}

func (inst *StoringErrListener) ReportContextSensitivity(recognizer antlr.Parser, dfa *antlr.DFA, startIndex, stopIndex, prediction int, configs *antlr.ATNConfigSet) {
}

var _ antlr.ErrorListener = (*StoringErrListener)(nil)
var _ SyntaxErrorSynthesizerI = (*StoringErrListener)(nil)

type SyntaxErrorSynthesizerI interface {
	GetSyntheticSyntaxError(maxErrorsToJoin int, skipRecovered bool) (err error)
}
