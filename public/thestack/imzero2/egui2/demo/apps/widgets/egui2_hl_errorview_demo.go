//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"strconv"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/errkind"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/errkind/leewayrender"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/errorview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/leewaywidgets"
)

// =============================================================================
// errorview widget demo — structured wrapped-error chain renderer
//
// Three sample contexts (small leaf, multi-stream wrap, structured-data on
// the leaf) rendered through an errorview.Renderer whose DefaultOpen / Indent
// knobs are wired to live UI controls so the operator sees how each option
// affects the rendered samples without restarting.
//
// Lifted from logviewer's renderErrorContext / renderErrorStream /
// renderErrorFact triple into the reusable widgets/errorview package; this
// demo proves it works decoupled from the Sink decoder.
// =============================================================================

// errorviewDemoState carries the live-toggleable Renderer config per
// gallery window. The three sample contexts are shared package-level
// because they're immutable test fixtures; the table2Emitter is per-
// window because it binds to the host-supplied WidgetIdStack.
type errorviewDemoState struct {
	defaultOpen bool
	indent      uint64

	// table2Emitter renders the same shredded error fact as the
	// leewaywidgets table widget — its ids must be the per-instance
	// stack so two open windows do not collide.
	table2Emitter *leewaywidgets.Table2CardEmitter
}

// Sample fixtures built once at package init so the per-frame cost
// of the demo is bounded by rendering, not allocation.
var (
	evSampleSimple     = buildEvSimpleFixture()
	evSampleWrapped    = buildEvWrappedFixture()
	evSampleStructured = buildEvStructuredFixture()

	// End-to-end fixture: a real boxer error walked through
	// errkind.FromBoxerError. Used by both the errorview projection
	// (after errorContextFromErrkind conversion) and the leeway
	// fact-table widget (via leewayrender.Render).
	evRealRowmarshallError = buildEvRealRowmarshallError()
	evRealErrorviewContext = errorContextFromErrkind(evRealRowmarshallError)
)

func init() {
	registry.Register(registry.Demo{
		Name:        "errorview",
		Category:    "Inspectors & feedback",
		Title:       icons.IconError + " errorview",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "Reusable widgets/errorview package: renders an eh.MarshalError-shaped chain (per-stream collapsing headers, per-fact message + frame triple + dark-canvas CBOR diagnostic of structured-data payloads). Live config (DefaultOpen / Indent) over four fixtures — single stackless, multi-stream wrap, structured-data leaf, plus an end-to-end real-boxer-error section that runs FromBoxerError → rowmarshall → leewayrender so the SAME error renders side-by-side through errorview AND the leeway fact-table widget.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			state = &errorviewDemoState{
				defaultOpen:   true,
				indent:        uint64(12),
				table2Emitter: leewaywidgets.NewTable2CardEmitter(ids, leewaywidgets.ColorPaletteViridis, nil),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoErrorView(ids, state.(*errorviewDemoState))
		},
		SourceFunc: demoErrorView,
	})
}

// demoErrorView renders the config row + three CollapsingHeader-
// wrapped sample sections, each driven by the live Renderer config
// from the per-window state.
func demoErrorView(ids *c.WidgetIdStack, st *errorviewDemoState) {
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("ev-default-open"), st.defaultOpen, "DefaultOpen").
			SendRespVal(&st.defaultOpen)
		c.AddSpace(gapSections())
		c.Label("Indent (px):").Send()
		c.AddSpace(padInner())
		c.DragValueU64(ids.PrepareStr("ev-indent"), st.indent).
			Speed(1.0).
			SendRespVal(&st.indent)
	}
	c.Separator().Horizontal().Send()

	r := errorview.New(ids, "ev-demo").
		DefaultOpen(st.defaultOpen).
		Indent(float32(st.indent))

	for range c.CollapsingHeader(ids.PrepareStr("ev-sec-simple"),
		c.WidgetText().Text("Simple — single stackless error").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(evSampleSimple)
	}
	c.AddSpace(gapInline())
	for range c.CollapsingHeader(ids.PrepareStr("ev-sec-wrapped"),
		c.WidgetText().Text("Wrapped — multi-stream chain with stack frames").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(evSampleWrapped)
	}
	c.AddSpace(gapInline())
	for range c.CollapsingHeader(ids.PrepareStr("ev-sec-structured"),
		c.WidgetText().Text("Structured — leaf carries CBOR-diagnostic fields").Keep()).
		DefaultOpen(true).KeepIter() {
		r.Render(evSampleStructured)
	}
	c.AddSpace(gapInline())
	for range c.CollapsingHeader(ids.PrepareStr("ev-sec-realboxer"),
		c.WidgetText().Text("End-to-end — real boxer error rendered through both errorview AND the leeway fact-table widget").Keep()).
		DefaultOpen(true).KeepIter() {
		c.Label("Errorview projection (presentation tree):").Send()
		c.AddSpace(padInner())
		r.Render(evRealErrorviewContext)
		c.AddSpace(gapSections())
		c.Separator().Horizontal().Send()
		c.AddSpace(gapSections())
		c.Label("Leeway fact-table widget (same error, shredded into runtime.facts):").Send()
		c.AddSpace(padInner())
		if err := leewayrender.Render(st.table2Emitter, evRealRowmarshallError); err != nil {
			c.Label(fmt.Sprintf("leewayrender error: %v", err)).Send()
		}
	}
}

// buildEvSimpleFixture is the minimal happy-path: a single
// stackless fact. Demonstrates the renderer doesn't insist on
// stack info being present.
func buildEvSimpleFixture() (out errorview.Context) {
	out = errorview.Context{
		Streams: []errorview.Stream{
			{Name: "no-stack", Facts: []errorview.Fact{
				{Msg: "ring full — drop-oldest engaged", Id: 0},
			}},
		},
	}
	return
}

// buildEvWrappedFixture mirrors a real boxer chain shape: two
// distinct stack streams plus a no-stack bucket, with per-frame
// stub facts interleaved between message facts. Models what
// eh.MarshalError emits when two wrapped errors come from
// different goroutines.
func buildEvWrappedFixture() (out errorview.Context) {
	out = errorview.Context{
		Streams: []errorview.Stream{
			{Name: "no-stack", Facts: []errorview.Fact{
				{Msg: "context cancelled", Id: 1},
			}},
			{Name: "stack-0", Facts: []errorview.Fact{
				{Msg: "apply: logbridge.flush failed", Id: 2, ParentId: 0},
				{Source: "logbridge/flush.go", Line: "84", Function: "(*Sink).flush",
					Id: 3, ParentId: 2},
				{Msg: "logbridge.flush: ring full", Id: 4, ParentId: 2},
				{Source: "logbridge/sink.go", Line: "212", Function: "(*Sink).enqueue",
					Id: 5, ParentId: 4},
			}},
			{Name: "stack-1", Facts: []errorview.Fact{
				{Msg: "audit: write blocked", Id: 6, ParentId: 0},
				{Source: "audit/writer.go", Line: "44", Function: "(*Writer).Write",
					Id: 7, ParentId: 6},
			}},
		},
	}
	return
}

// buildEvRealRowmarshallError captures a real boxer error (with a wrap
// chain + attached structured data via eb.Build) and runs it through
// errkind.FromBoxerError so both the errorview panel and the leeway
// widget panel render the same underlying tree. CapturedTs is pinned
// to a fixed value so the demo is deterministic across runs.
func buildEvRealRowmarshallError() (out errkind.Error) {
	leaf := eb.Build().
		Str("trace_id", "9c8b-0aef-4421").
		Str("user", "alice").
		Uint64("attempt", 3).
		Errorf("ingest failed: schema mismatch")
	wrapped := eh.Errorf("commit %s: %w", "card-v1", leaf)
	fixedTs := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	out = errkind.FromBoxerError(
		0xDEADBEEF_0001,
		[]byte("ev-demo/realboxer"),
		fixedTs,
		wrapped,
	)
	if len(out.Messages) == 0 {
		// Defensive — keeps the init side total even if FromBoxerError
		// ever returns an empty Error.
		out = errkind.Error{Id: 0xDEADBEEF_0001, CapturedTs: time.Now()}
	}
	return
}

// errorContextFromErrkind projects an errkind.Error into the
// errorview.Context shape the existing renderer expects. The errkind
// shape is flat parallel arrays grouped by StreamName; this helper
// walks them once, opening a new errorview.Stream every time
// StreamNames[i] differs from the previous entry.
func errorContextFromErrkind(e errkind.Error) (out errorview.Context) {
	n := len(e.Messages)
	if n == 0 {
		return
	}
	var (
		current *errorview.Stream
		prev    string
	)
	for i := 0; i < n; i++ {
		if current == nil || e.StreamNames[i] != prev {
			out.Streams = append(out.Streams, errorview.Stream{Name: e.StreamNames[i]})
			current = &out.Streams[len(out.Streams)-1]
			prev = e.StreamNames[i]
		}
		ev := errorview.Fact{
			Msg:      e.Messages[i],
			Source:   e.Sources[i],
			Function: e.Funcs[i],
			Id:       e.FactIds[i],
			ParentId: e.ParentIds[i],
			Data:     e.Data[i],
		}
		if e.Lines[i] > 0 {
			ev.Line = strconv.FormatInt(int64(e.Lines[i]), 10)
		}
		if len(e.Data[i]) > 0 {
			if diag, derr := cbor.Diagnose(e.Data[i]); derr == nil {
				ev.DataDiag = diag
			}
		}
		current.Facts = append(current.Facts, ev)
	}
	return
}

// buildEvStructuredFixture exercises the dark-canvas DataDiag block
// — the leaf fact carries CBOR-diagnostic notation produced by
// cbor.Diagnose on an eb.Build payload.
func buildEvStructuredFixture() (out errorview.Context) {
	out = errorview.Context{
		Streams: []errorview.Stream{
			{Name: "stack-0", Facts: []errorview.Fact{
				{Msg: "card.commit: schema mismatch", Id: 1},
				{Source: "card/commit.go", Line: "187", Function: "Commit",
					Id: 2, ParentId: 1},
				{Msg: "schema mismatch", Id: 3, ParentId: 1,
					DataDiag: `{"expected_version": 7, "got_version": 5, "schema_id": "card-v1", "client_build": "pebble2impl@a3f12de"}`},
			}},
		},
	}
	return
}
