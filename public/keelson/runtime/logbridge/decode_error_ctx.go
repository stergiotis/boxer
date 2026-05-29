//go:build llm_generated_opus47

package logbridge

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// decodeErrorContext recognises the structured shape that
// eh.MarshalError emits under the zerolog `error` field and projects
// it into a typed *factsstore.LogErrorContext. Returns nil (and the
// caller falls back to asString) for any other shape — a plain
// string, nil, or some unrelated map.
//
// Wire shape (post-CBOR-decode, with fxamacker/cbor's interface{}
// defaults for nested types):
//
//	v: map[string]any | map[any]any
//	  └── "streams": []any
//	         ├── map[any]any{ <stream-name>: []any }   // one entry per stream
//	         │    └── []any of map[any]any{...fact...}
//	         └── ...
//
// Stream names are "no-stack" for stackless errors and "stack-N"
// for the Nth deduplicated stack trace. Each fact map contains
// some subset of: msg, source, line, func, data (raw CBOR bytes),
// dataDiag (cbor.Diagnose output), id, parentId.
//
// The decoder is defensive about missing keys and type drift —
// boxer's eh package owns the wire format and may evolve it; a
// future addition of a new fact key shouldn't break this consumer
// (unknown keys are ignored), and an unexpected value type silently
// degrades to the empty-string / zero-value default for that field
// rather than failing the whole decode.
func decodeErrorContext(v any) (ctx *factsstore.LogErrorContext) {
	m := asAnyMap(v)
	if m == nil {
		return
	}
	streamsRaw, ok := m["streams"]
	if !ok {
		return
	}
	streamsArr, ok := streamsRaw.([]any)
	if !ok {
		return
	}
	out := &factsstore.LogErrorContext{
		Streams: make([]factsstore.LogErrorStream, 0, len(streamsArr)),
	}
	for _, sRaw := range streamsArr {
		st, ok := decodeStream(sRaw)
		if !ok {
			continue
		}
		out.Streams = append(out.Streams, st)
	}
	if len(out.Streams) == 0 {
		return
	}
	ctx = out
	return
}

// decodeStream unpacks a single stream object. Each stream is a
// one-key map whose key is the stream name and whose value is the
// fact array. The single-key shape comes from
// errorFactsLogger.MarshalZerologObject calling e.Array(name, ...)
// at object scope.
func decodeStream(v any) (st factsstore.LogErrorStream, ok bool) {
	m := asAnyMap(v)
	if m == nil {
		return
	}
	for k, factsRaw := range m {
		factsArr, isArr := factsRaw.([]any)
		if !isArr {
			continue
		}
		st.Name = k
		st.Facts = make([]factsstore.LogErrorFact, 0, len(factsArr))
		for _, fRaw := range factsArr {
			fact, fOk := decodeFact(fRaw)
			if !fOk {
				continue
			}
			st.Facts = append(st.Facts, fact)
		}
		ok = true
		return
	}
	return
}

// decodeFact unpacks a single error-fact object. Empty / unset
// fields stay at their zero values; unknown keys are ignored.
func decodeFact(v any) (f factsstore.LogErrorFact, ok bool) {
	m := asAnyMap(v)
	if m == nil {
		return
	}
	if s, has := m["msg"]; has {
		f.Msg = asStringDefault(s)
	}
	if s, has := m["source"]; has {
		f.Source = asStringDefault(s)
	}
	if s, has := m["line"]; has {
		f.Line = asStringDefault(s)
	}
	if s, has := m["func"]; has {
		f.Function = asStringDefault(s)
	}
	if s, has := m["data"]; has {
		if b, isBytes := s.([]byte); isBytes {
			f.Data = b
		}
	}
	if s, has := m["dataDiag"]; has {
		f.DataDiag = asStringDefault(s)
	}
	if s, has := m["id"]; has {
		f.Id = asUint64Default(s)
	}
	if s, has := m["parentId"]; has {
		f.ParentId = asUint64Default(s)
	}
	ok = true
	return
}

// asAnyMap normalises a CBOR-decoded map value to map[string]any
// regardless of whether fxamacker/cbor produced map[string]any (top
// level) or map[any]any (nested default). Returns nil for any
// non-map input so the caller can short-circuit.
func asAnyMap(v any) (out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		out = t
		return
	case map[any]any:
		out = make(map[string]any, len(t))
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				continue
			}
			out[ks] = val
		}
		return
	}
	return
}

// asStringDefault is a permissive string accessor — used in the
// error-context decode where the wire is always strings (per the eh
// marshaler) but defensive type checks let the decoder survive a
// future format drift without panicking.
func asStringDefault(v any) (s string) {
	if t, ok := v.(string); ok {
		s = t
	}
	return
}

// asUint64Default is the matching permissive accessor for the id /
// parentId fields. fxamacker/cbor decodes positive CBOR ints as
// uint64 by default, but accept the signed alternative too in case
// the wire format changes.
func asUint64Default(v any) (n uint64) {
	switch t := v.(type) {
	case uint64:
		n = t
	case int64:
		if t >= 0 {
			n = uint64(t)
		}
	}
	return
}
