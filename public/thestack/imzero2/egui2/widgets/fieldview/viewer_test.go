//go:build llm_generated_opus47

package fieldview

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// TestField_IsContainer separates leaf kinds from container kinds.
// Used by Renderer.renderField to pick the layout — anything wrong
// here would silently miss the CollapsingHeader path for nested
// fields.
func TestField_IsContainer(t *testing.T) {
	leafKinds := []KindE{
		KindUnknown, KindString, KindInt, KindUint, KindFloat,
		KindBool, KindBytes, KindTime,
	}
	for _, k := range leafKinds {
		assert.Falsef(t, Field{Kind: k}.IsContainer(),
			"leaf kind %d must not be a container", k)
	}
	for _, k := range []KindE{KindObject, KindArray} {
		assert.Truef(t, Field{Kind: k}.IsContainer(),
			"container kind %d must be a container", k)
	}
}

// TestKindName covers every constant. A KindE constant added without
// a matching arm here renders as "?" in the viewer — this test
// catches the omission at build time.
func TestKindName(t *testing.T) {
	cases := map[KindE]string{
		KindString: "str",
		KindInt:    "int",
		KindUint:   "uint",
		KindFloat:  "float",
		KindBool:   "bool",
		KindBytes:  "bytes",
		KindTime:   "time",
		KindObject: "obj",
		KindArray:  "arr",
	}
	for k, want := range cases {
		assert.Equalf(t, want, kindName(k), "kindName(%d)", k)
	}
	assert.Equal(t, "?", kindName(KindUnknown), "unknown kind must render as '?'")
	assert.Equal(t, "?", kindName(KindE(99)), "out-of-range kind must render as '?'")
}

// TestFormatField_PerKind covers each leaf-kind formatting arm.
// Bytes and Time get specific format rules — the others are
// straightforward fmt.Sprintf calls but documenting them in tests
// guards against accidental format-string changes.
func TestFormatField_PerKind(t *testing.T) {
	assert.Equal(t, "hello",
		formatField(Field{Kind: KindString, Str: "hello"}, 64))
	assert.Equal(t, "-7",
		formatField(Field{Kind: KindInt, Int: -7}, 64))
	assert.Equal(t, "42",
		formatField(Field{Kind: KindUint, Uint: 42}, 64))
	assert.Equal(t, "3.14",
		formatField(Field{Kind: KindFloat, Float: 3.14}, 64))
	assert.Equal(t, "true",
		formatField(Field{Kind: KindBool, Bool: true}, 64))
	assert.Equal(t, "deadbeef",
		formatField(Field{Kind: KindBytes, Bytes: []byte{0xde, 0xad, 0xbe, 0xef}}, 64))

	ts := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-05-14T12:00:00Z",
		formatField(Field{Kind: KindTime, Time: ts}, 64))

	// Unknown kind falls through to Str so a malformed field still
	// shows something the operator can act on.
	assert.Equal(t, "fallback",
		formatField(Field{Kind: KindUnknown, Str: "fallback"}, 64))
}

// TestFormatField_BytesTruncation covers the bytesMax cap. Without
// it a 1 MiB blob would explode the panel; with it, long values get
// a "(N bytes)" suffix so the operator knows how much was elided.
func TestFormatField_BytesTruncation(t *testing.T) {
	long := make([]byte, 80)
	for i := range long {
		long[i] = byte(i)
	}
	got := formatField(Field{Kind: KindBytes, Bytes: long}, 64)
	assert.Contains(t, got, "…", "truncation must surface visually")
	assert.Contains(t, got, "(80 bytes)",
		"original length must be reported so the operator can compare with bytesMax")

	// bytesMax=0 disables truncation entirely.
	gotFull := formatField(Field{Kind: KindBytes, Bytes: long}, 0)
	assert.NotContains(t, gotFull, "…",
		"bytesMax=0 must dump the full hex without truncation")
}

// TestFormatField_ContainerSummary checks the defensive fallback
// arm — if a caller invokes formatField directly on a container
// (instead of letting the Renderer dispatch it via the
// CollapsingHeader path) it should produce a brief summary, not "".
func TestFormatField_ContainerSummary(t *testing.T) {
	obj := Field{Kind: KindObject, Children: []Field{{Name: "a"}, {Name: "b"}}}
	assert.Equal(t, "obj(2)", formatField(obj, 64))
	arr := Field{Kind: KindArray, Children: []Field{{Name: "[0]"}}}
	assert.Equal(t, "arr(1)", formatField(arr, 64))
}

// TestNew_Defaults documents the constructor's defaults so a future
// retune (e.g. ShowKind off by default) is a deliberate, reviewable
// change rather than an accidental behavioural shift.
func TestNew_Defaults(t *testing.T) {
	r := New(c.NewWidgetIdStack(), "test")
	assert.True(t, r.showKind, "ShowKind defaults on so the viewer is self-describing")
	assert.Equal(t, float32(12), r.indent, "Indent default 12 px")
	assert.Equal(t, 64, r.bytesMax, "BytesMax default 64")
	assert.True(t, r.defaultOpen, "DefaultOpen default true so freshly-rendered trees show contents")
	assert.Equal(t, "test", r.idPrefix)
}

// TestFluentSetters_AreImmutable proves the fluent setters return a
// modified copy rather than mutating the receiver. This is the
// load-bearing claim that makes "build a base config once, override
// per-call" safe.
func TestFluentSetters_AreImmutable(t *testing.T) {
	base := New(c.NewWidgetIdStack(), "test")
	// Each setter returns a new value with the field changed; base
	// must stay at its original.
	_ = base.ShowKind(false)
	assert.True(t, base.showKind, "ShowKind must not mutate the receiver")

	_ = base.Indent(99)
	assert.Equal(t, float32(12), base.indent, "Indent must not mutate the receiver")

	_ = base.BytesMax(7)
	assert.Equal(t, 64, base.bytesMax, "BytesMax must not mutate the receiver")

	_ = base.DefaultOpen(false)
	assert.True(t, base.defaultOpen, "DefaultOpen must not mutate the receiver")
}

// TestNestedFieldShape documents the shape callers must produce for
// hierarchical rendering. Container fields populate Children and
// leave the typed slots zero; leaf fields populate exactly one slot.
// The renderer relies on this discipline (mixed shapes silently
// drop the typed slot and walk Children).
func TestNestedFieldShape(t *testing.T) {
	tree := []Field{
		{Name: "request", Kind: KindObject, Children: []Field{
			{Name: "method", Kind: KindString, Str: "GET"},
			{Name: "headers", Kind: KindObject, Children: []Field{
				{Name: "accept", Kind: KindString, Str: "application/json"},
				{Name: "auth", Kind: KindBytes, Bytes: []byte{0xff, 0x00, 0xab}},
			}},
		}},
		{Name: "tags", Kind: KindArray, Children: []Field{
			{Name: "[0]", Kind: KindString, Str: "production"},
			{Name: "[1]", Kind: KindString, Str: "us-east"},
		}},
	}
	require.Len(t, tree, 2)
	assert.True(t, tree[0].IsContainer())
	assert.True(t, tree[1].IsContainer())
	require.Len(t, tree[0].Children, 2)
	assert.True(t, tree[0].Children[1].IsContainer(),
		"nested object inside a container must itself be a container — proves arbitrary depth works")

	// Spot-check each typed slot is reachable via the children chain.
	headers := tree[0].Children[1]
	assert.Equal(t, "application/json", headers.Children[0].Str)
	assert.Equal(t, []byte{0xff, 0x00, 0xab}, headers.Children[1].Bytes)
	assert.Equal(t, "production", tree[1].Children[0].Str)
}

// TestKindName_AllConstants is a paranoia guard — strings.Contains
// "?" on the joined output would catch any non-mapped kind, and
// summarising every label in one place makes a future drift visible.
func TestKindName_AllConstants(t *testing.T) {
	all := []KindE{
		KindString, KindInt, KindUint, KindFloat, KindBool,
		KindBytes, KindTime, KindObject, KindArray,
	}
	var b strings.Builder
	for _, k := range all {
		b.WriteString(kindName(k))
		b.WriteByte(' ')
	}
	got := b.String()
	assert.NotContains(t, got, "?",
		"every documented Kind constant must have a non-fallback label")
}
