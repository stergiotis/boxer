// Package gloss is the core of ADR-0186: a catalog of glosses — named ways of
// showing a value — that a host such as play resolves per result column and
// renders in a table cell (the inline face) or a detail pane (a block face
// the host binds by media type).
//
// The package is deliberately free of any UI dependency: what lives here is
// the catalog, the value-kind vocabulary, the typed cell accessor, the
// declaration parser (ADR-0123's `<label>@<media type>` gate, kept verbatim)
// and every built-in inline face. That is what lets the faces be tested in
// plain Go and lets a SQL rewrite pass validate a media type against the
// catalog without pulling a renderer in.
package gloss

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// ValueKindE is the coarse shape of a cell's value — the question a gloss
// asks before it agrees to render a column. It is derivable from an Arrow
// type (KindOfArrow) and, in the host, from a leeway canonical type, so the
// Arrow-backed grids and the text-backed leeway card ask the same question.
type ValueKindE uint8

const (
	ValueKindOther ValueKindE = iota
	ValueKindNumeric
	ValueKindText
	ValueKindBytes
	ValueKindTemporal
	ValueKindBool
)

var AllValueKinds = []ValueKindE{
	ValueKindOther,
	ValueKindNumeric,
	ValueKindText,
	ValueKindBytes,
	ValueKindTemporal,
	ValueKindBool,
}

func (inst ValueKindE) String() string {
	switch inst {
	case ValueKindNumeric:
		return "numeric"
	case ValueKindText:
		return "text"
	case ValueKindBytes:
		return "bytes"
	case ValueKindTemporal:
		return "temporal"
	case ValueKindBool:
		return "bool"
	default:
		return "other"
	}
}

// KindOfArrow classifies an Arrow type. A dictionary reads as its value type
// (ClickHouse LowCardinality(String) arrives that way); lists, structs, maps
// and null are ValueKindOther — a gloss that wants a list's items is applied
// by the host to the inner array (the per-attribute grid), not to the list.
func KindOfArrow(dt arrow.DataType) ValueKindE {
	if dt == nil {
		return ValueKindOther
	}
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64,
		arrow.DECIMAL128, arrow.DECIMAL256:
		return ValueKindNumeric
	case arrow.STRING, arrow.LARGE_STRING:
		return ValueKindText
	case arrow.BINARY, arrow.LARGE_BINARY, arrow.FIXED_SIZE_BINARY:
		return ValueKindBytes
	case arrow.TIMESTAMP, arrow.DATE32, arrow.DATE64, arrow.TIME32, arrow.TIME64, arrow.DURATION:
		return ValueKindTemporal
	case arrow.BOOL:
		return ValueKindBool
	case arrow.DICTIONARY:
		if d, ok := dt.(*arrow.DictionaryType); ok {
			return KindOfArrow(d.ValueType)
		}
	}
	return ValueKindOther
}

// ToneE is the semantic colour an inline face may ask for. The host maps it
// onto the design system's semantic palette (ADR-0031); the core never names
// a colour.
type ToneE uint8

const (
	ToneNeutral ToneE = iota
	ToneInfo
	ToneSuccess
	ToneWarning
	ToneError
	ToneAccent
)

var AllTones = []ToneE{ToneNeutral, ToneInfo, ToneSuccess, ToneWarning, ToneError, ToneAccent}

func (inst ToneE) String() string {
	switch inst {
	case ToneInfo:
		return "info"
	case ToneSuccess:
		return "success"
	case ToneWarning:
		return "warning"
	case ToneError:
		return "error"
	case ToneAccent:
		return "accent"
	default:
		return "neutral"
	}
}

// Inline is the one-line face of a glossed cell: what a table cell shows.
// Text must be valid UTF-8 and free of newlines; Tone is advisory.
type Inline struct {
	Text string
	Tone ToneE
}

// ParamSpec declares one media-type parameter a gloss accepts. Values, when
// non-nil, is the closed set of accepted values (compared case-sensitively —
// `unit=K` and `unit=k` are different things); nil means any value.
type ParamSpec struct {
	Name   string
	Doc    string
	Values []string
}

// GlossI is one catalog entry: a media type, its documentation, the
// parameters it accepts, and a factory for the per-column instance.
//
// MediaType is the catalog key, canonical (lower-case, parameter-free): an
// IANA type for the content family (`text/markdown`), or `gloss/<name>` for
// a presentation gloss (`gloss/temperature`).
type GlossI interface {
	MediaType() string
	Doc() string
	Params() []ParamSpec
	// Bind validates the parameters — already checked against Params by the
	// catalog — and returns the instance for one column. It runs once per
	// column, so it is where a parameter is parsed, never Inline.
	Bind(params map[string]string) (InstanceI, error)
	// Affinities are the default rules the gloss brings along (ADR-0186
	// §SD3): RE2 patterns matched against a column's spec line. Nil for a
	// gloss that is only ever declared or ruled explicitly.
	Affinities() []string
}

// InstanceI is a gloss bound to a column's parameters.
type InstanceI interface {
	Gloss() GlossI
	Params() map[string]string
	// Accepts says whether the instance can render a value of this kind; a
	// refusal carries the reason the host shows beside the plain cell.
	Accepts(kind ValueKindE) (ok bool, reason string)
	// Inline renders one cell. It runs per visible cell per frame and must
	// therefore be cheap: no allocation beyond the returned string, no
	// decoding of anything larger than the text it returns.
	Inline(cell CellI) Inline
}
