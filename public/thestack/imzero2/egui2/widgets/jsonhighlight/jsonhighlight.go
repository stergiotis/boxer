//go:build llm_generated_opus47

// Package jsonhighlight tokenizes JSON for syntax highlighting.
//
// It drives encoding/json/jsontext.Decoder once over the input, emitting one
// span per JSON token (string, number, literal, or structural delimiter)
// plus filler spans for inter-token bytes (commas, colons, whitespace).
// Object-key strings are distinguished from value strings via the decoder's
// stack-position state.
//
// On parse error, spans for the prefix that decoded cleanly are returned;
// the unparsed remainder becomes a single CategoryPlain span. This mirrors
// gohighlight's degrade-gracefully approach so the editor never goes blank.
//
// The output is a flat slice of byte-offset spans suitable for direct
// consumption by the codeview widget's retained CodeViewJob.Section
// calls. Output guarantees:
//   - Every byte of src is covered by exactly one span.
//   - For each span, src[Start:Stop] == Text and Stop-Start == len(Text).
package jsonhighlight

import (
	"encoding/json/jsontext"
	"strings"
)

// CategoryE classifies a span for highlighting.
type CategoryE int

const (
	CategoryPlain       CategoryE = iota // unclassified bytes (parse-error remainder)
	CategoryPunctuation                  // { } [ ] , :
	CategoryKey                          // object-member name (the "name" of "name":value)
	CategoryStringLit                    // string value
	CategoryNumberLit                    // number value
	CategoryBoolLit                      // true / false
	CategoryNullLit                      // null
	CategoryWhitespace                   // gaps between tokens (spaces, tabs, newlines)
)

// Span represents a highlighted region of input source.
type Span struct {
	Start    int32
	Stop     int32
	Text     string
	Category CategoryE
}

// Highlight tokenizes src and returns spans covering every byte exactly once.
// On parse error, the parsed prefix retains semantic categories and the
// unparseable tail is returned as a single CategoryPlain span.
func Highlight(src string) (spans []Span) {
	dec := jsontext.NewDecoder(strings.NewReader(src))
	spans = make([]Span, 0, 64)
	prev := int32(0)
	for {
		kind := dec.PeekKind()
		if kind == jsontext.KindInvalid {
			break
		}

		// Capture key-vs-value before the read consumes the token.
		// Inside an object, tokens at even positions (0, 2, 4, …) are keys.
		// StackIndex(d) reports the *current* frame; StackIndex(0) is the
		// implicit top level (KindInvalid) and is intentionally skipped.
		isKey := false
		if d := dec.StackDepth(); d > 0 {
			parentKind, count := dec.StackIndex(d)
			if parentKind == jsontext.KindBeginObject && count%2 == 0 {
				isKey = true
			}
		}

		_, err := dec.ReadToken()
		if err != nil {
			break
		}
		post := int32(dec.InputOffset())

		// Locate the token's start: InputOffset points one past its end, so
		// scan forward from prev over the inter-token gap (whitespace +
		// commas + colons) to find the first non-skippable byte.
		tokStart := scanForward(src, prev)
		if tokStart > prev {
			spans = append(spans, makeFiller(src, prev, tokStart))
		}

		spans = append(spans, Span{
			Start:    tokStart,
			Stop:     post,
			Text:     src[tokStart:post],
			Category: classifyKind(kind, isKey),
		})
		prev = post
	}
	if prev < int32(len(src)) {
		spans = append(spans, makeFiller(src, prev, int32(len(src))))
	}
	return
}

func classifyKind(k jsontext.Kind, isKey bool) (cat CategoryE) {
	switch k {
	case jsontext.KindString:
		if isKey {
			cat = CategoryKey
		} else {
			cat = CategoryStringLit
		}
	case jsontext.KindNumber:
		cat = CategoryNumberLit
	case jsontext.KindTrue, jsontext.KindFalse:
		cat = CategoryBoolLit
	case jsontext.KindNull:
		cat = CategoryNullLit
	case jsontext.KindBeginObject, jsontext.KindEndObject,
		jsontext.KindBeginArray, jsontext.KindEndArray:
		cat = CategoryPunctuation
	default:
		cat = CategoryPlain
	}
	return
}

// scanForward returns the offset of the first byte at or after pos that
// could begin a JSON token — i.e. the first byte that isn't whitespace and
// isn't a structural separator (',' or ':'). Used to recover the start
// offset of a token whose end was reported by Decoder.InputOffset.
func scanForward(src string, pos int32) (out int32) {
	out = pos
	n := int32(len(src))
	for out < n {
		switch src[out] {
		case ' ', '\t', '\n', '\r', ',', ':':
			out++
		default:
			return
		}
	}
	return
}

// makeFiller categorises a gap between tokens (or a trailing tail). The gap
// is a single span — not split per-byte — because in every existing palette
// punctuation and whitespace share the default color, and a single span
// keeps the byte-coverage invariant simple.
//
// A gap containing only whitespace bytes is CategoryWhitespace. A gap with
// JSON structural separators (',' ':') alongside whitespace is
// CategoryPunctuation. Anything else (e.g. an unparseable tail after a
// parse error) falls through to CategoryPlain.
func makeFiller(src string, start int32, stop int32) (s Span) {
	cat := CategoryWhitespace
	hasPunct := false
	for i := start; i < stop; i++ {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
		case ',', ':':
			hasPunct = true
		default:
			s = Span{
				Start:    start,
				Stop:     stop,
				Text:     src[start:stop],
				Category: CategoryPlain,
			}
			return
		}
	}
	if hasPunct {
		cat = CategoryPunctuation
	}
	s = Span{
		Start:    start,
		Stop:     stop,
		Text:     src[start:stop],
		Category: cat,
	}
	return
}
