package membership

import "fmt"

// RefFormatterI renders a uint64 membership ref id to a human-readable string.
type RefFormatterI interface {
	FormatRef(ref uint64) (humanReadable string)
}

// VerbatimFormatterI renders a verbatim membership name (raw bytes) to a
// human-readable string.
type VerbatimFormatterI interface {
	FormatVerbatim(raw []byte) (humanReadable string)
}

// ParamsFormatterI renders a membership params blob (raw bytes) to a
// human-readable string.
type ParamsFormatterI interface {
	FormatParams(raw []byte) (humanReadable string)
}

// DefaultRefFormatter formats uint64 refs as hex (e.g. "0x2a").
type DefaultRefFormatter struct{}

var _ RefFormatterI = DefaultRefFormatter{}

func (DefaultRefFormatter) FormatRef(ref uint64) (humanReadable string) {
	return fmt.Sprintf("0x%x", ref)
}

// DefaultVerbatimFormatter converts raw bytes to string.
type DefaultVerbatimFormatter struct{}

var _ VerbatimFormatterI = DefaultVerbatimFormatter{}

func (DefaultVerbatimFormatter) FormatVerbatim(raw []byte) (humanReadable string) {
	return string(raw)
}

// DefaultParamsFormatter converts raw bytes to string.
type DefaultParamsFormatter struct{}

var _ ParamsFormatterI = DefaultParamsFormatter{}

func (DefaultParamsFormatter) FormatParams(raw []byte) (humanReadable string) {
	return string(raw)
}
