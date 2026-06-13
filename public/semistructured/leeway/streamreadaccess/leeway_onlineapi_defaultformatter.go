package streamreadaccess

import "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"

// DefaultValueFormatter passes through the Arrow ValueStr output.
type DefaultValueFormatter struct{}

func (inst *DefaultValueFormatter) FormatValue(arrowValueStr string, canonicalType canonicaltypes.PrimitiveAstNodeI) (formatted string) {
	formatted = arrowValueStr
	return
}

// DefaultFormatters returns formatters with default implementations.
func DefaultFormatters() Formatters {
	return Formatters{
		ValueFormatter: &DefaultValueFormatter{},
	}
}
