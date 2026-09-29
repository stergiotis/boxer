package errorview

import (
	"testing"

	"github.com/stretchr/testify/assert"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
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
func TestInputDefaults(t *testing.T) {
	s := (Input{}).resolve()
	if !s.defaultOpen || s.indent != 12 || s.errorFg != defaultErrorFg || s.mutedFg != defaultMutedFg {
		t.Errorf("defaults: %+v", s)
	}
	if (Input{}).scopeKey() != "errorview" {
		t.Error("scope key default")
	}
	s = (Input{StartCollapsed: true, Indent: -1, ErrorFg: color.Hex(0x11223344)}).resolve()
	if s.defaultOpen || s.indent != 0 || s.errorFg != color.Hex(0x11223344) || s.mutedFg != defaultMutedFg {
		t.Errorf("resolve lost fields: %+v", s)
	}
}

func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	chain := Context{Streams: []Stream{{Name: "s", Facts: []Fact{{Msg: "boom", Source: "a.go", Line: "1", Function: "f", DataDiag: "{1: 2}"}}}}}
	if !Render(Input{Ids: ids, ScopeKey: "t", Chain: chain}).Drawn {
		t.Error("a chain with a fact must draw")
	}
	if Render(Input{Ids: ids, ScopeKey: "t"}).Drawn {
		t.Error("an empty chain must draw nothing")
	}
	if RenderCaptured(Input{Ids: ids, ScopeKey: "t"}).Drawn {
		t.Error("a zero Captured must draw nothing")
	}
}

func TestPluralize(t *testing.T) {
	assert.Equal(t, "stream", pluralize("stream", 1))
	assert.Equal(t, "streams", pluralize("stream", 0))
	assert.Equal(t, "streams", pluralize("stream", 2))
	assert.Equal(t, "fact", pluralize("fact", 1))
	assert.Equal(t, "facts", pluralize("fact", 7))
}
