package widgets

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
)

// =============================================================================
// fieldview widget demo — hierarchical typed-field inspector
//
// Three sample fixtures (every primitive kind, nested object/array, long-value
// wrap) rendered through a fieldview.Renderer whose ShowKind / Indent /
// BytesMax / DefaultOpen knobs are wired to live UI controls, so the operator
// sees how each option affects the rendered samples without restarting.
//
// Lifted from the standalone apps/fieldviewdemo App into the widgets demo
// app — per-window state keeps the live-config knobs isolated between
// gallery windows.
// =============================================================================

// fieldviewDemoState carries the live-toggleable Renderer config per
// gallery window. The three sample contexts are shared package-level
// because they're immutable test fixtures.
type fieldviewDemoState struct {
	showKind    bool
	defaultOpen bool
	indent      uint64
	bytesMax    uint64
}

// Sample fixtures built once at package init so the per-frame cost of
// the demo is bounded by rendering, not allocation. Mutating callers
// would need to copy first; the demo is read-only.
var (
	fvSamplePrimitives = buildFvPrimitivesFixture()
	fvSampleNested     = buildFvNestedFixture()
	fvSampleLong       = buildFvLongValueFixture()
)

func init() {
	registry.Register(registry.Demo{
		Name:        "fieldview",
		Category:    "Inspectors & feedback",
		Title:       icons.IconSearch + " fieldview",
		Stage:       [2]float32{1024, 700},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Reusable widgets/fieldview package: tagged-union typed fields (str/int/uint/float/bool/bytes/time) with optional Object/Array nesting. Live config knobs (ShowKind / DefaultOpen / Indent / BytesMax) over three sample fixtures — every primitive kind, a deep object/array tree, and a long-value wrap demo.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &fieldviewDemoState{
				showKind:    true,
				defaultOpen: true,
				indent:      uint64(12),
				bytesMax:    uint64(64),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoFieldView(ids, state.(*fieldviewDemoState))
		},
		SourceFunc: demoFieldView,
	})
}

// demoFieldView renders the config row at the top, then three
// CollapsingHeader-wrapped sample sections, each driven by a
// per-frame Renderer assembled from the live config — Renderer is a
// value type so this is allocation-free.
func demoFieldView(ids *c.WidgetIdStack, st *fieldviewDemoState) {
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("fv-show-kind"), st.showKind, "ShowKind").
			SendRespVal(&st.showKind)
		c.AddSpace(gapSections())
		c.Checkbox(ids.PrepareStr("fv-default-open"), st.defaultOpen, "DefaultOpen").
			SendRespVal(&st.defaultOpen)
		c.AddSpace(gapSections())
		c.Label("Indent (px):").Send()
		c.AddSpace(padInner())
		c.DragValueU64(ids.PrepareStr("fv-indent"), st.indent).
			Speed(1.0).
			SendRespVal(&st.indent)
		c.AddSpace(gapSections())
		c.Label("BytesMax:").Send()
		c.AddSpace(padInner())
		c.DragValueU64(ids.PrepareStr("fv-bytesmax"), st.bytesMax).
			Speed(1.0).
			SendRespVal(&st.bytesMax)
	}
	c.Separator().Horizontal().Send()

	r := fieldview.New(ids, "fv-demo").
		ShowKind(st.showKind).
		Indent(float32(st.indent)).
		BytesMax(int(st.bytesMax)).
		DefaultOpen(st.defaultOpen)

	for range c.CollapsingHeader(ids.PrepareStr("fv-sec-prim"),
		c.WidgetText().Text("Primitives — one of every kind").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(fvSamplePrimitives)
	}
	c.AddSpace(gapInline())
	for range c.CollapsingHeader(ids.PrepareStr("fv-sec-nested"),
		c.WidgetText().Text("Hierarchical — nested object + array").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(fvSampleNested)
	}
	c.AddSpace(gapInline())
	for range c.CollapsingHeader(ids.PrepareStr("fv-sec-long"),
		c.WidgetText().Text("Long values — wrap demo").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(fvSampleLong)
	}
}

// buildFvPrimitivesFixture covers every primitive Kind exactly once
// — useful as a quick visual regression: any change to fieldview's
// per-kind formatting shows up immediately in this section.
func buildFvPrimitivesFixture() (out []fieldview.Field) {
	ts := time.Date(2026, 5, 14, 12, 0, 0, 123456789, time.UTC)
	out = []fieldview.Field{
		{Name: "title", Kind: fieldview.KindString, Str: "operator dashboard"},
		{Name: "delta_ms", Kind: fieldview.KindInt, Int: -42},
		{Name: "request_id", Kind: fieldview.KindUint, Uint: 1_700_000_042},
		{Name: "ratio", Kind: fieldview.KindFloat, Float: 0.953125},
		{Name: "verified", Kind: fieldview.KindBool, Bool: true},
		{Name: "session_token", Kind: fieldview.KindBytes,
			Bytes: []byte{0xa3, 0x4f, 0x12, 0xde, 0xad, 0xbe, 0xef, 0x01}},
		{Name: "issued_at", Kind: fieldview.KindTime, Time: ts},
		{Name: "missing_kind", Kind: fieldview.KindUnknown, Str: "(falls back to Str)"},
	}
	return
}

// buildFvNestedFixture exercises arbitrary depth. The Children chain
// proves recursion works; array entries use "[N]" naming so the
// array reads as positional rather than as a degenerate Object.
func buildFvNestedFixture() (out []fieldview.Field) {
	out = []fieldview.Field{
		{Name: "request", Kind: fieldview.KindObject, Children: []fieldview.Field{
			{Name: "method", Kind: fieldview.KindString, Str: "POST"},
			{Name: "path", Kind: fieldview.KindString, Str: "/api/v1/cards"},
			{Name: "headers", Kind: fieldview.KindObject, Children: []fieldview.Field{
				{Name: "accept", Kind: fieldview.KindString, Str: "application/json"},
				{Name: "auth", Kind: fieldview.KindBytes,
					Bytes: []byte{0xff, 0x00, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56}},
				{Name: "user_agent", Kind: fieldview.KindString,
					Str: "pebble2impl/0.1 (linux; x86_64)"},
			}},
			{Name: "body", Kind: fieldview.KindObject, Children: []fieldview.Field{
				{Name: "title", Kind: fieldview.KindString, Str: "card #42"},
				{Name: "tags", Kind: fieldview.KindArray, Children: []fieldview.Field{
					{Name: "[0]", Kind: fieldview.KindString, Str: "production"},
					{Name: "[1]", Kind: fieldview.KindString, Str: "us-east"},
					{Name: "[2]", Kind: fieldview.KindString, Str: "high-priority"},
				}},
				{Name: "scores", Kind: fieldview.KindArray, Children: []fieldview.Field{
					{Name: "[0]", Kind: fieldview.KindFloat, Float: 0.91},
					{Name: "[1]", Kind: fieldview.KindFloat, Float: 0.87},
					{Name: "[2]", Kind: fieldview.KindFloat, Float: 0.74},
				}},
			}},
		}},
		{Name: "response", Kind: fieldview.KindObject, Children: []fieldview.Field{
			{Name: "status", Kind: fieldview.KindUint, Uint: 201},
			{Name: "latency_ms", Kind: fieldview.KindFloat, Float: 23.4},
			{Name: "ts", Kind: fieldview.KindTime, Time: time.Date(2026, 5, 14, 12, 0, 1, 0, time.UTC)},
		}},
	}
	return
}

// buildFvLongValueFixture has every value exceed typical panel width
// — drag the host window narrow/wide to see values reflow via the
// LabelAtoms.Wrap discipline fieldview enforces internally.
func buildFvLongValueFixture() (out []fieldview.Field) {
	long := make([]byte, 200)
	for i := range long {
		long[i] = byte((i * 7) % 256)
	}
	out = []fieldview.Field{
		{Name: "sql", Kind: fieldview.KindString,
			Str: "SELECT id, title, body, created_at, updated_at, deleted_at FROM cards WHERE owner = $1 AND created_at > now() - interval '30 days' ORDER BY created_at DESC LIMIT 100"},
		{Name: "url", Kind: fieldview.KindString,
			Str: "https://example.com/api/v1/cards?owner=alice&since=2026-01-01&until=2026-12-31&include=tags,scores,history&format=json&limit=100"},
		{Name: "stack_excerpt", Kind: fieldview.KindString,
			Str: "panic: runtime error: index out of range [42] with length 7\n\tgoroutine 1 [running]:\n\tmain.processCards(0xc000180000?, 0x2a)\n\t/app/cmd/example/main.go:128 +0xa4\n\tmain.main()\n\t/app/cmd/example/main.go:42 +0x18"},
		{Name: "blob_long", Kind: fieldview.KindBytes, Bytes: long},
	}
	return
}
