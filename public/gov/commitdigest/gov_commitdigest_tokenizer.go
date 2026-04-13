//go:build llm_generated_opus46

package commitdigest

import (
	"math"

	"github.com/pkoukk/tiktoken-go"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type TokenCounterI interface {
	CountTokens(text string) (count int64)
}

var _ TokenCounterI = (*TiktokenCounter)(nil)

type TiktokenCounter struct {
	Encoding             string
	CorrectionMultiplier float64
	enc                  *tiktoken.Tiktoken
}

func (inst *TiktokenCounter) correctionMultiplier() (m float64) {
	m = inst.CorrectionMultiplier
	if m <= 0 {
		m = 1.18
	}
	return
}

func (inst *TiktokenCounter) Init() (err error) {
	encoding := inst.Encoding
	if encoding == "" {
		encoding = "o200k_base"
	}
	inst.enc, err = tiktoken.GetEncoding(encoding)
	if err != nil {
		err = eh.Errorf("unable to get tiktoken encoding %q: %w", encoding, err)
		return
	}
	return
}

func (inst *TiktokenCounter) CountTokens(text string) (count int64) {
	tokens := inst.enc.Encode(text, nil, nil)
	count = int64(math.Ceil(float64(len(tokens)) * inst.correctionMultiplier()))
	return
}
