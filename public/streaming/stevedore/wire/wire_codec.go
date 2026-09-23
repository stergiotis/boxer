package wire

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CodecE names how one frame is delimited on a pipe.
type CodecE uint8

const (
	// CodecLengthPrefixedUint32BE prefixes a frame with its length as a
	// big-endian 32-bit integer. It is the zero value, so an unset codec
	// carries any bytes.
	CodecLengthPrefixedUint32BE CodecE = iota
	// CodecNetstring frames as `<decimal length>:<bytes>,`.
	CodecNetstring
	// CodecLines delimits a frame with a trailing newline, the way a
	// framework strips it: a trailing carriage return goes with it. A payload
	// holding a newline, or ending in a carriage return, cannot travel under
	// it, and a binary reply never can.
	CodecLines
)

// AllCodecs lists every codec, in declaration order.
var AllCodecs = []CodecE{CodecLengthPrefixedUint32BE, CodecNetstring, CodecLines}

// String spells the codec the way a framework's configuration names it.
func (inst CodecE) String() string {
	switch inst {
	case CodecLines:
		return "lines"
	case CodecLengthPrefixedUint32BE:
		return "length_prefixed_uint32_be"
	case CodecNetstring:
		return "netstring"
	default:
		return "unknown"
	}
}

// ParseCodec reads a codec name as a framework's configuration spells it.
func ParseCodec(name string) (codec CodecE, err error) {
	for _, c := range AllCodecs {
		if c.String() == name {
			return c, nil
		}
	}
	err = eb.Build().Str("name", name).Errorf("unknown frame codec — one of lines, length_prefixed_uint32_be, netstring")
	return
}
