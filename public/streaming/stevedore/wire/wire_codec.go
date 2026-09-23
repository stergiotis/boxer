package wire

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CodecE names how one frame is delimited on a pipe.
type CodecE uint8

const (
	// CodecLines delimits a frame with a trailing newline. A payload holding a
	// newline cannot travel under it.
	CodecLines CodecE = iota
	// CodecLengthPrefixedUint32BE prefixes a frame with its length as a
	// big-endian 32-bit integer.
	CodecLengthPrefixedUint32BE
	// CodecNetstring frames as `<decimal length>:<bytes>,`.
	CodecNetstring
)

// AllCodecs lists every codec, in declaration order.
var AllCodecs = []CodecE{CodecLines, CodecLengthPrefixedUint32BE, CodecNetstring}

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
