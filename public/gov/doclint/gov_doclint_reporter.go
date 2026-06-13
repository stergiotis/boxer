package doclint

import (
	"encoding/json/v2"
	"fmt"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type FormatE uint8

const (
	FormatHuman FormatE = 1
	FormatJson  FormatE = 2
)

func (inst FormatE) String() (s string) {
	switch inst {
	case FormatHuman:
		s = "human"
	case FormatJson:
		s = "json"
	default:
		s = "unknown"
	}
	return
}

var AllFormats = []FormatE{FormatHuman, FormatJson}

// ReporterI receives findings as they are produced and writes them out
// when FinishE is called.
type ReporterI interface {
	Add(f Finding)
	FinishE() (err error)
}

type humanReporter struct {
	w io.Writer
}

func (inst *humanReporter) Add(f Finding) {
	fmt.Fprintf(inst.w, "%s:%d:%d  %s  %s  %s\n",
		f.Path, f.Line, f.Col, f.RuleId, f.Severity, f.Message)
}

func (inst *humanReporter) FinishE() (err error) {
	return
}

type jsonReporter struct {
	w        io.Writer
	findings []Finding
}

func (inst *jsonReporter) Add(f Finding) {
	inst.findings = append(inst.findings, f)
}

func (inst *jsonReporter) FinishE() (err error) {
	if inst.findings == nil {
		inst.findings = []Finding{}
	}
	err = json.MarshalWrite(inst.w, inst.findings)
	if err != nil {
		err = eh.Errorf("doclint json reporter write: %w", err)
		return
	}
	return
}

func NewReporterE(format FormatE, w io.Writer) (r ReporterI, err error) {
	switch format {
	case FormatHuman:
		r = &humanReporter{w: w}
	case FormatJson:
		r = &jsonReporter{w: w}
	default:
		err = eh.Errorf("doclint: unknown reporter format")
	}
	return
}
