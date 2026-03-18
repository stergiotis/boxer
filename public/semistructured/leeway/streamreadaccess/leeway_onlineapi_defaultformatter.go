//go:build llm_generated_opus46

package streamreadaccess

import (
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// DefaultValueFormatter passes through the Arrow ValueStr output.
type DefaultValueFormatter struct{}

func (inst *DefaultValueFormatter) FormatValue(arrowValueStr string, canonicalType canonicaltypes.PrimitiveAstNodeI) (formatted string) {
	formatted = arrowValueStr
	return
}

// DefaultRefFormatter formats uint64 refs as hex.
type DefaultRefFormatter struct{}

func (inst *DefaultRefFormatter) FormatRef(ref uint64) (humanReadable string) {
	humanReadable = fmt.Sprintf("0x%x", ref)
	return
}

// DefaultVerbatimFormatter converts raw bytes to string.
type DefaultVerbatimFormatter struct{}

func (inst *DefaultVerbatimFormatter) FormatVerbatim(raw []byte) (humanReadable string) {
	humanReadable = string(raw)
	return
}

// DefaultParamsFormatter converts raw bytes to string (empty for now per spec).
type DefaultParamsFormatter struct{}

func (inst *DefaultParamsFormatter) FormatParams(raw []byte) (humanReadable string) {
	humanReadable = string(raw)
	return
}

// DefaultFormatters returns formatters with default implementations.
func DefaultFormatters() Formatters {
	return Formatters{
		ValueFormatter:    &DefaultValueFormatter{},
		RefFormatter:      &DefaultRefFormatter{},
		VerbatimFormatter: &DefaultVerbatimFormatter{},
		ParamsFormatter:   &DefaultParamsFormatter{},
	}
}
