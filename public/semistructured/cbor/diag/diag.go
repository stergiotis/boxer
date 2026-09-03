package diag

import (
	"errors"
	"strings"
)

// CategoryE classifies one span of rendered notation for a highlighter.
type CategoryE uint8

const (
	// CategoryFiller is whitespace: the separators, indentation and line
	// breaks between tokens.
	CategoryFiller CategoryE = iota
	// CategoryStructural is a bracket, brace, parenthesis, comma, colon,
	// the indefinite-length marker `_` and the tag parentheses.
	CategoryStructural
	// CategoryKey is a scalar map key, whatever its type. A container used
	// as a key keeps its inner categories.
	CategoryKey
	// CategoryNumber is an integer, a bignum or a float.
	CategoryNumber
	// CategoryText is a text string, quotes included.
	CategoryText
	// CategoryBytes is a byte string, the `h'…'` wrapper included.
	CategoryBytes
	// CategoryTag is a tag number.
	CategoryTag
	// CategorySimple is false, true, null, undefined or simple(n).
	CategorySimple
	// CategoryComment is a `/ … /` comment: a tag name, an annotation.
	CategoryComment
	// CategoryError is the one span that names why rendering stopped.
	CategoryError
)

// Span is one run of rendered text. Consecutive spans are contiguous —
// Start of one is Stop of the previous — and together they cover the whole
// rendering, so a consumer can rebuild the text from them or index it by
// byte offset. Text is the rendered bytes of [Start, Stop).
type Span struct {
	Category CategoryE
	Start    int
	Stop     int
	Text     string
}

// PathElemKindE says how a PathElem addresses a child of its parent.
type PathElemKindE uint8

const (
	// PathElemIndex is an array element, or an indefinite-string chunk.
	PathElemIndex PathElemKindE = iota
	// PathElemKey is a map value, addressed by its key's encoded bytes.
	PathElemKey
	// PathElemTag is the content of a tag.
	PathElemTag
)

// PathElem is one step of the path from the root item to the item being
// rendered. Index holds the element ordinal for PathElemIndex, Key the
// key's encoded CBOR for PathElemKey (a view into the input; do not
// retain), Tag the tag number for PathElemTag.
type PathElem struct {
	Kind  PathElemKindE
	Index int
	Key   []byte
	Tag   uint64
}

// Options controls the rendering. The zero value is the pretty rendering
// with the defaults below and no comments.
type Options struct {
	// Indent is one level of indentation in pretty mode; empty means two
	// spaces.
	Indent string
	// Width is the line width a container's compact rendering must fit in,
	// measured from its opening bracket's column, to stay on one line;
	// zero means DefaultWidth. Ignored in Compact mode.
	Width int
	// Compact renders everything on one line, exactly as the fxamacker
	// library's Diagnose does: elements separated by ", ", map entries as
	// "key: value", sequence items separated by ", ".
	Compact bool
	// FloatPrecision appends the RFC 8949 §8.1 encoding indicator to every
	// float: _1 for binary16, _2 for binary32, _3 for binary64.
	FloatPrecision bool
	// TagComments writes a `/ name /` comment after the opening parenthesis
	// of every tag whose number TagName knows.
	TagComments bool
	// BytesFold breaks a byte string longer than this many bytes into rows
	// of that many bytes inside its h'…' in pretty mode; whitespace is
	// permitted there by RFC 8949 §8. Zero never folds.
	BytesFold int
	// Annotate, when set, is asked for a comment for every item, keyed by
	// its path from the root (empty for the root itself, or for each item
	// of a sequence). A non-empty answer is written as `/ text /` after
	// the item — after the opening bracket of a container that spans
	// lines. Map keys are not annotated; their values are, under a
	// PathElemKey step. The hook must be pure: pretty mode measures a
	// container's compact width before laying it out, and asks again.
	Annotate func(path []PathElem) string
	// Sequence treats the input as an RFC 8742 CBOR sequence: items are
	// rendered one after another, separated by ", " in Compact mode and by
	// a line break otherwise. Without it, bytes after the first item are
	// an error.
	Sequence bool
}

// DefaultWidth is the pretty-mode line width when Options.Width is zero.
const DefaultWidth = 72

// DefaultIndent is the pretty-mode indentation when Options.Indent is empty.
const DefaultIndent = "  "

// maxNesting bounds the recursion: a container nested deeper than this is
// reported as an error rather than walked.
const maxNesting = 1024

// The failures the walk reports. Every returned error wraps exactly one of
// them; the wrapping error carries the byte offset it happened at.
var (
	// ErrTruncated is an item that runs past the end of the input.
	ErrTruncated = errors.New("item runs past the end of the input")
	// ErrReservedInfo is a head with reserved additional information (28-30).
	ErrReservedInfo = errors.New("reserved additional information (28-30)")
	// ErrUnexpectedBreak is a break stop code outside an indefinite-length item.
	ErrUnexpectedBreak = errors.New("break stop code outside an indefinite-length item")
	// ErrIndefiniteHead is an indefinite length on a major type that has none.
	ErrIndefiniteHead = errors.New("indefinite length on a major type that has none")
	// ErrChunkType is an indefinite-length string chunk that is not a definite
	// string of the same type.
	ErrChunkType = errors.New("indefinite-length string chunk is not a definite string of the same type")
	// ErrInvalidUTF8 is a text string that is not valid UTF-8.
	ErrInvalidUTF8 = errors.New("text string is not valid UTF-8")
	// ErrTrailingBytes is input that continues after the item while Sequence
	// is off.
	ErrTrailingBytes = errors.New("bytes follow the item and Sequence is off")
	// ErrNesting is a container nested deeper than the walk allows.
	ErrNesting = errors.New("nesting deeper than the walk allows")
	// ErrEmpty is empty input.
	ErrEmpty = errors.New("no input")
)

// Print renders b and returns the spans. The spans are the whole
// rendering, contiguous and in order. err is non-nil when the walk stopped
// early; the spans then end with the failure and the unparsed remainder.
func Print(b []byte, opts Options) (spans []Span, err error) {
	p := newPrinter(b, opts, false)
	err = p.run()
	spans = p.spans
	return
}

// String renders b to plain text: the concatenation of Print's spans, with
// the same degradation and the same err.
func String(b []byte, opts Options) (s string, err error) {
	p := newPrinter(b, opts, true)
	err = p.run()
	s = string(p.out)
	return
}

// Text concatenates spans back into the rendered text.
func Text(spans []Span) string {
	var sb strings.Builder
	n := 0
	for i := range spans {
		n += len(spans[i].Text)
	}
	sb.Grow(n)
	for i := range spans {
		sb.WriteString(spans[i].Text)
	}
	return sb.String()
}

// TagName returns the comment name for a tag number the printer knows, or
// "" — the table behind Options.TagComments.
func TagName(tag uint64) (name string) {
	switch tag {
	case 0:
		name = "date-time"
	case 1:
		name = "epoch"
	case 2:
		name = "bignum"
	case 3:
		name = "negative-bignum"
	case 4:
		name = "decimal"
	case 5:
		name = "bigfloat"
	case 21:
		name = "base64url-later"
	case 22:
		name = "base64-later"
	case 23:
		name = "base16-later"
	case 24:
		name = "embedded-cbor"
	case 32:
		name = "uri"
	case 33:
		name = "base64url"
	case 34:
		name = "base64"
	case 36:
		name = "mime"
	case 37:
		name = "uuid"
	case 52:
		name = "ipv4"
	case 54:
		name = "ipv6"
	case 63:
		name = "cbor-sequence"
	case 258:
		name = "set"
	case 260:
		name = "network-address"
	case 1001:
		name = "time"
	case 55799:
		name = "self-described"
	case 55800:
		name = "cbor-sequence-file"
	}
	return
}
