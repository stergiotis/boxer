//go:build llm_generated_opus47

package errorview

import (
	"testing"

	"github.com/stretchr/testify/assert"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// TestContext_IsEmpty matches the Renderer's short-circuit
// condition: zero streams, all-empty streams, and a populated
// stream are the three shapes a caller might construct.
func TestContext_IsEmpty(t *testing.T) {
	assert.True(t, Context{}.IsEmpty(),
		"zero-Streams must short-circuit so the renderer doesn't draw an empty header")
	assert.True(t, Context{Streams: []Stream{{Name: "no-stack"}}}.IsEmpty(),
		"streams without facts must also short-circuit")
	assert.False(t, Context{Streams: []Stream{
		{Name: "stack-0", Facts: []Fact{{Msg: "boom"}}},
	}}.IsEmpty())
}

// TestFormatFrame covers the four arms of the frame-triple
// composer. Each fact in eh's wire output may be missing some
// fields (per-position frame stubs vs message-only facts); the
// composer must produce a sensible string for each combination.
func TestFormatFrame(t *testing.T) {
	cases := []struct {
		f    Fact
		want string
	}{
		// Full triple — the common case.
		{Fact{Source: "x.go", Line: "42", Function: "DoThing"}, "DoThing @ x.go:42"},
		// Frame-only without function — produces "source:line".
		{Fact{Source: "x.go", Line: "42"}, "x.go:42"},
		// Frame-only without line — produces "function @ source".
		{Fact{Source: "x.go", Function: "DoThing"}, "DoThing @ x.go"},
		// Source-only — produces just the path.
		{Fact{Source: "x.go"}, "x.go"},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, FormatFrame(tc.f), "FormatFrame(%+v)", tc.f)
	}
}

// TestNew_Defaults documents the constructor's defaults so a
// future retune is a deliberate, reviewable change rather than an
// accidental behavioural shift.
func TestNew_Defaults(t *testing.T) {
	r := New(c.NewWidgetIdStack(), "test")
	assert.True(t, r.defaultOpen,
		"DefaultOpen on so freshly-rendered chains reveal facts immediately")
	assert.Equal(t, float32(12), r.indent, "Indent default 12 px (matches fieldview)")
	assert.Equal(t, "test", r.idPrefix)
	// Palette defaults exercised here by their non-zero literal —
	// a future palette retune that lands at zero would be caught.
	assert.NotZero(t, r.errorFg.Literal())
	assert.NotZero(t, r.mutedFg.Literal())
}

// TestFluentSetters_AreImmutable proves the fluent setters return
// a modified copy rather than mutating the receiver. Load-bearing
// claim that makes "build a base config once, override per-call"
// safe — same contract as fieldview.Renderer.
func TestFluentSetters_AreImmutable(t *testing.T) {
	base := New(c.NewWidgetIdStack(), "test")

	_ = base.DefaultOpen(false)
	assert.True(t, base.defaultOpen, "DefaultOpen must not mutate the receiver")

	_ = base.Indent(99)
	assert.Equal(t, float32(12), base.indent, "Indent must not mutate the receiver")
}

// TestPluralize is a paranoia guard — a wrong arm here surfaces
// as "1 streams" / "1 facts" in collapsing headers.
func TestPluralize(t *testing.T) {
	assert.Equal(t, "stream", pluralize("stream", 1))
	assert.Equal(t, "streams", pluralize("stream", 0))
	assert.Equal(t, "streams", pluralize("stream", 2))
	assert.Equal(t, "fact", pluralize("fact", 1))
	assert.Equal(t, "facts", pluralize("fact", 7))
}
