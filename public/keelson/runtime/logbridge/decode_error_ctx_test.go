package logbridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecodeErrorContext_NilOnNonMap returns nil for plain strings,
// nils, ints, and arrays — all the shapes the `error` field could
// take when the marshaler isn't wired or the writer used a plain
// fmt.Errorf. The caller falls back to asString() for these.
func TestDecodeErrorContext_NilOnNonMap(t *testing.T) {
	cases := []any{
		nil,
		"plain string error",
		42,
		[]byte("byte string"),
		[]any{"some", "array"},
	}
	for _, v := range cases {
		assert.Nilf(t, decodeErrorContext(v), "decodeErrorContext(%T) must be nil", v)
	}
}

// TestDecodeErrorContext_NilWithoutStreamsKey covers the defensive
// arm: a map without a "streams" key isn't a boxer error context,
// just an arbitrary structured error from some other writer.
func TestDecodeErrorContext_NilWithoutStreamsKey(t *testing.T) {
	v := map[string]any{
		"code":    503,
		"message": "service unavailable",
	}
	assert.Nil(t, decodeErrorContext(v))
}

// TestDecodeErrorContext_HappyPath builds the wire shape eh.MarshalError
// emits and walks it end-to-end. Two streams (no-stack + stack-0),
// each with a couple of facts of mixed shapes (msg-only, frame-only,
// msg+frame, msg+frame+structured-data). Asserts the projection
// preserves stream names, fact ordering, and per-fact key population.
func TestDecodeErrorContext_HappyPath(t *testing.T) {
	// Use map[any]any for nested maps because that's what
	// fxamacker/cbor produces by default for nested CBOR maps when
	// the target is interface{}. The decoder must accept both shapes.
	wire := map[string]any{
		"streams": []any{
			map[any]any{
				"no-stack": []any{
					map[any]any{
						"msg":      "stackless error",
						"id":       uint64(0),
						"parentId": uint64(0),
					},
				},
			},
			map[any]any{
				"stack-0": []any{
					map[any]any{
						"msg":      "outer wrap",
						"id":       uint64(1),
						"parentId": uint64(0),
					},
					map[any]any{
						"source": "github.com/example/pkg/file.go",
						"line":   "42",
						"func":   "DoThing",
						"id":     uint64(2),
					},
					map[any]any{
						"msg":      "inner cause",
						"data":     []byte{0xa1, 0x63, 0x6f, 0x70, 0x65},
						"dataDiag": `{"op": "Sink.appendTail"}`,
						"id":       uint64(3),
						"parentId": uint64(1),
					},
				},
			},
		},
	}

	ctx := decodeErrorContext(wire)
	require.NotNil(t, ctx)
	require.Len(t, ctx.Streams, 2)

	// Stream 0 — no-stack
	assert.Equal(t, "no-stack", ctx.Streams[0].Name)
	require.Len(t, ctx.Streams[0].Facts, 1)
	assert.Equal(t, "stackless error", ctx.Streams[0].Facts[0].Msg)
	assert.Equal(t, uint64(0), ctx.Streams[0].Facts[0].Id)

	// Stream 1 — stack-0
	st := ctx.Streams[1]
	assert.Equal(t, "stack-0", st.Name)
	require.Len(t, st.Facts, 3)

	// outer wrap fact
	assert.Equal(t, "outer wrap", st.Facts[0].Msg)
	assert.Equal(t, uint64(1), st.Facts[0].Id)
	assert.Empty(t, st.Facts[0].Source, "msg-only facts must leave frame fields empty")

	// frame-only fact
	assert.Empty(t, st.Facts[1].Msg)
	assert.Equal(t, "github.com/example/pkg/file.go", st.Facts[1].Source)
	assert.Equal(t, "42", st.Facts[1].Line)
	assert.Equal(t, "DoThing", st.Facts[1].Function)

	// inner cause with structured data
	assert.Equal(t, "inner cause", st.Facts[2].Msg)
	assert.Equal(t, []byte{0xa1, 0x63, 0x6f, 0x70, 0x65}, st.Facts[2].Data)
	assert.Equal(t, `{"op": "Sink.appendTail"}`, st.Facts[2].DataDiag)
	assert.Equal(t, uint64(3), st.Facts[2].Id)
	assert.Equal(t, uint64(1), st.Facts[2].ParentId)
}

// TestDecodeErrorContext_StringKeyedMap proves the decoder accepts
// map[string]any at the nested level too — CBOR readers configured
// with cbor.DecOptions{MapType: cbor.MapTypeStringKeyed} would
// produce this shape, and we shouldn't tie the decoder to one set
// of options.
func TestDecodeErrorContext_StringKeyedMap(t *testing.T) {
	wire := map[string]any{
		"streams": []any{
			map[string]any{
				"stack-0": []any{
					map[string]any{"msg": "leaf", "id": uint64(7)},
				},
			},
		},
	}
	ctx := decodeErrorContext(wire)
	require.NotNil(t, ctx)
	require.Len(t, ctx.Streams, 1)
	assert.Equal(t, "stack-0", ctx.Streams[0].Name)
	require.Len(t, ctx.Streams[0].Facts, 1)
	assert.Equal(t, "leaf", ctx.Streams[0].Facts[0].Msg)
	assert.Equal(t, uint64(7), ctx.Streams[0].Facts[0].Id)
}

// TestDecodeErrorContext_DropsMalformedStreams: a stream entry that
// isn't a one-key map (or whose value isn't an array) is silently
// skipped. Strict failure here would tie the decoder to eh's
// current wire format too tightly — drift in eh would crash
// the decoder.
func TestDecodeErrorContext_DropsMalformedStreams(t *testing.T) {
	wire := map[string]any{
		"streams": []any{
			"not a map",
			map[any]any{}, // empty map
			map[any]any{"stack-0": "not an array"},
			map[any]any{"stack-1": []any{
				map[any]any{"msg": "good fact"},
			}},
		},
	}
	ctx := decodeErrorContext(wire)
	require.NotNil(t, ctx, "the one good stream keeps the context non-nil")
	require.Len(t, ctx.Streams, 1)
	assert.Equal(t, "stack-1", ctx.Streams[0].Name)
}

// TestDecodeErrorContext_EmptyStreamsArray returns nil so the
// caller drops back to the asString fallback rather than producing
// an empty context — empty Streams would render as "error chain —
// 0 streams" in the detail pane, which is noise.
func TestDecodeErrorContext_EmptyStreamsArray(t *testing.T) {
	wire := map[string]any{"streams": []any{}}
	assert.Nil(t, decodeErrorContext(wire))
}

// TestAsAnyMap_AcceptsBothMapShapes: the helper unifies map[string]any
// and map[any]any. Non-string keys in a map[any]any are dropped
// rather than causing a panic — defensive against future eh format
// drift that might key by integer.
func TestAsAnyMap_AcceptsBothMapShapes(t *testing.T) {
	stringKeyed := map[string]any{"a": 1, "b": 2}
	got := asAnyMap(stringKeyed)
	assert.Equal(t, stringKeyed, got)

	anyKeyed := map[any]any{"a": 1, 42: "dropped", "b": 2}
	got2 := asAnyMap(anyKeyed)
	assert.Equal(t, map[string]any{"a": 1, "b": 2}, got2,
		"non-string keys must be skipped, not panic")

	// Non-map inputs return nil so the caller can short-circuit.
	assert.Nil(t, asAnyMap("not a map"))
	assert.Nil(t, asAnyMap(nil))
	assert.Nil(t, asAnyMap(42))
}

// TestAsUint64Default_AcceptsBothSignedAndUnsigned: fxamacker/cbor
// decodes positive CBOR ints as uint64 by default, but a future
// configuration change could land them as int64; the accessor
// handles both. Negative int64 is clamped to zero (unrepresentable
// as a uint, so the safest fallback is the zero value).
func TestAsUint64Default_AcceptsBothSignedAndUnsigned(t *testing.T) {
	assert.Equal(t, uint64(7), asUint64Default(uint64(7)))
	assert.Equal(t, uint64(7), asUint64Default(int64(7)))
	assert.Equal(t, uint64(0), asUint64Default(int64(-1)),
		"negative int64 must clamp to zero rather than wrap")
	assert.Equal(t, uint64(0), asUint64Default("not a number"))
}
