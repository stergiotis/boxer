//go:build llm_generated_opus47

package errorview

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Default palette sources from the IDS semantic palette (ADR-0031 §SD2);
// shares the same tokens as the logviewer detail pane (b648b57f) and the
// badge widget (8e5d40f3) so the error-chain renderer drops into existing
// surfaces with structurally-identical colors.
var (
	defaultErrorFg  = color.Hex(styletokens.ErrorDefault.AsHex())
	defaultMutedFg  = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	transparentBgEv = color.Transparent
)

// Renderer is the configured error-chain viewer. Holds a pointer
// to the caller's WidgetIdStack so widget IDs derive deterministically
// from the caller's id scope plus the per-Renderer idPrefix — two
// renderers on the same stack don't collide as long as their
// prefixes differ.
//
// Renderer is intentionally a value (not a pointer): config changes
// don't mutate the caller's instance, which makes a "build a base
// config once, override per-call" pattern safe.
type Renderer struct {
	ids         *c.WidgetIdStack
	idPrefix    string
	defaultOpen bool
	indent      float32
	errorFg     color.Color
	mutedFg     color.Color
	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at construction.
	density styletokens.DensityE
}

// New builds a Renderer with sensible defaults: DefaultOpen on so
// freshly-rendered chains reveal their facts immediately, Indent
// 12 px matching the fieldview default. The idPrefix scopes every
// widget ID this Renderer emits so multiple renderers can share an
// ids stack without collisions; pass a stable short string
// ("card-err" / "log-err" / "trace").
func New(ids *c.WidgetIdStack, idPrefix string) (inst Renderer) {
	inst = Renderer{
		ids:         ids,
		idPrefix:    idPrefix,
		defaultOpen: true,
		indent:      12,
		errorFg:     defaultErrorFg,
		mutedFg:     defaultMutedFg,
		density:     styletokens.DensityFromEnv(),
	}
	return
}

// DefaultOpen sets the initial collapsed/expanded state of the
// per-stream CollapsingHeaders. The outer "error chain — N streams"
// header tracks the same default. Set false for deep chains where
// the initial summary should be terse.
func (inst Renderer) DefaultOpen(v bool) (out Renderer) {
	inst.defaultOpen = v
	out = inst
	return
}

// Indent sets the per-fact left padding before frame triples and
// structured-data blocks, in pixels. Default 12. Zero is allowed
// for a flat layout.
func (inst Renderer) Indent(v float32) (out Renderer) {
	inst.indent = v
	out = inst
	return
}

// ErrorFg overrides the foreground colour used for fact messages.
// Default is a soft red (Tailwind red-300) calibrated to read on
// the standard dark egui theme. Override when adopting a different
// theme palette.
func (inst Renderer) ErrorFg(col color.Color) (out Renderer) {
	inst.errorFg = col
	out = inst
	return
}

// MutedFg overrides the foreground colour used for stack-frame
// triples. Default is Tailwind gray-400.
func (inst Renderer) MutedFg(col color.Color) (out Renderer) {
	inst.mutedFg = col
	out = inst
	return
}

// Render draws the error chain at the current ui scope. Outer
// CollapsingHeader titled "error chain — N stream(s)"; per stream
// a sub-header titled "<name> · M fact(s)"; per fact a message
// line, an optional indented frame-triple line, and an optional
// dark-canvas Frame with the CBOR diagnostic of structured data.
//
// No outer wrapper is added beyond the top-level CollapsingHeader;
// the caller owns whatever surrounding scope (panel, Frame, dialog)
// frames the viewer.
//
// Empty contexts (no streams or no facts) short-circuit so the UI
// doesn't grow an "error chain — 0 streams" header.
func (inst Renderer) Render(ctx Context) {
	if ctx.IsEmpty() {
		return
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
	title := fmt.Sprintf("error chain — %d %s", len(ctx.Streams), pluralize("stream", len(ctx.Streams)))
	hdrId := inst.ids.PrepareStr(inst.idPrefix + "-root")
	for range c.CollapsingHeader(hdrId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(inst.defaultOpen).KeepIter() {
		for si, st := range ctx.Streams {
			inst.renderStream(si, st)
		}
	}
}

// renderStream emits one stream's collapsing block.
func (inst Renderer) renderStream(si int, st Stream) {
	header := fmt.Sprintf("%s · %d %s", st.Name, len(st.Facts), pluralize("fact", len(st.Facts)))
	hdrId := inst.ids.PrepareStr(fmt.Sprintf("%s-s-%d", inst.idPrefix, si))
	for range c.CollapsingHeader(hdrId, c.WidgetText().Text(header).Keep()).
		DefaultOpen(inst.defaultOpen).KeepIter() {
		for fi, f := range st.Facts {
			inst.renderFact(si, fi, f)
		}
	}
}

// renderFact emits one fact: optional message (red monospace
// wrapped), optional indented frame triple (muted small monospace
// wrapped), optional indented structured-data block (dark canvas
// Frame containing the CBOR diagnostic, monospace small wrapped).
//
// Each leg is gated on its corresponding field being non-empty so
// message-only facts and frame-only facts both render compactly.
func (inst Renderer) renderFact(si, fi int, f Fact) {
	if f.Msg != "" {
		msgAtoms := c.Atoms().BeginRichTextColored(inst.errorFg, transparentBgEv, "✗ "+f.Msg).
			Monospace().End().Keep()
		c.LabelAtoms(msgAtoms).Wrap().Send()
	}
	if f.Source != "" {
		for range c.Horizontal().KeepIter() {
			if inst.indent > 0 {
				c.AddSpace(inst.indent)
			}
			frameAtoms := c.Atoms().BeginRichTextColored(inst.mutedFg, transparentBgEv, FormatFrame(f)).
				Monospace().Small().End().Keep()
			c.LabelAtoms(frameAtoms).Wrap().Send()
		}
	}
	if f.DataDiag != "" {
		for range c.Horizontal().KeepIter() {
			if inst.indent > 0 {
				c.AddSpace(inst.indent)
			}
			frameId := inst.ids.PrepareStr(fmt.Sprintf("%s-d-%d-%d", inst.idPrefix, si, fi))
			for range c.Frame(frameId).
				PresetDarkCanvas().
				InnerMargin(styletokens.PaddingInner(inst.density)).
				KeepIter() {
				diagAtoms := c.Atoms().BeginRichText(f.DataDiag).Monospace().Small().End().Keep()
				c.LabelAtoms(diagAtoms).Wrap().Send()
			}
		}
	}
}

// pluralize is a tiny helper that picks the right form of a noun
// based on a count. Used by header labels ("1 stream" / "3 streams")
// so collapsing headers don't read as "1 streams".
func pluralize(noun string, n int) (s string) {
	if n == 1 {
		s = noun
		return
	}
	s = noun + "s"
	return
}
