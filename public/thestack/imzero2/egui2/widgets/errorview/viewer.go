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

// Input is one frame's error chain. Every zero value is the documented
// default, so a chain needs only Ids, ScopeKey and Chain (or Captured).
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two chains in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this chain within the host's id space; empty uses
	// "errorview".
	ScopeKey string
	// Chain is what Render draws.
	Chain Context
	// Captured is what RenderCaptured draws: the error's one line and, under
	// it, its chain. Render ignores it.
	Captured Captured

	// StartCollapsed is the initial state of the per-stream headers; the outer
	// "error chain — N streams" header tracks it. Off, a freshly rendered
	// chain reveals its facts. Set it for deep chains whose first view should
	// be terse.
	StartCollapsed bool
	// Indent is the per-fact left padding before frame triples and structured
	// data, in points; 0 takes 12, a negative value none.
	Indent float32
	// ErrorFg is the foreground of fact messages and MutedFg of stack-frame
	// triples; the zero Color takes the IDS error and secondary-text tokens.
	ErrorFg, MutedFg color.Color
}

// Result is what one Render reports.
type Result struct {
	// Drawn is whether anything was: false for an empty chain, which draws
	// nothing so the UI does not grow an "error chain — 0 streams" header.
	Drawn bool
}

const (
	defaultIndent   float32 = 12
	defaultScopeKey         = "errorview"
)

// settings is one frame's Input with every default resolved.
type settings struct {
	defaultOpen bool
	indent      float32
	errorFg     color.Color
	mutedFg     color.Color
	// density resolves IDS spacing tokens at the active preset (ADR-0032
	// §SD2), re-read every frame because the preset is runtime-switchable.
	density styletokens.DensityE
}

func (in Input) resolve() (s settings) {
	s = settings{
		defaultOpen: !in.StartCollapsed,
		indent:      in.Indent,
		errorFg:     in.ErrorFg,
		mutedFg:     in.MutedFg,
		density:     styletokens.ActiveDensity(),
	}
	switch {
	case s.indent == 0:
		s.indent = defaultIndent
	case s.indent < 0:
		s.indent = 0
	}
	if s.errorFg == (color.Color{}) {
		s.errorFg = defaultErrorFg
	}
	if s.mutedFg == (color.Color{}) {
		s.mutedFg = defaultMutedFg
	}
	return
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// Render draws Input.Chain at the current ui scope, inside one IdScope under
// Input.Ids. Outer CollapsingHeader titled "error chain — N stream(s)"; per
// stream a sub-header titled "<name> · M fact(s)"; per fact a message line,
// an optional indented frame-triple line, and an optional dark-canvas Frame
// with the CBOR diagnostic of structured data.
//
// No outer wrapper is added beyond the top-level CollapsingHeader; the caller
// owns whatever surrounding scope (panel, Frame, dialog) frames the viewer.
// An empty chain (no streams or no facts) draws nothing. A nil Ids draws
// nothing.
func Render(in Input) (res Result) {
	if in.Ids == nil || in.Chain.IsEmpty() {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		in.resolve().renderChain(in.Ids, in.Chain)
	}
	res.Drawn = true
	return
}

// RenderCaptured draws Input.Captured the way an app's status area wants
// it: the error's text on one wrapped line in the error colour, and under
// it the chain as Render draws it, following StartCollapsed. The zero
// Captured draws nothing, so a caller may render its error slot
// unconditionally. The chain carries what the one line drops — the frames
// and the fields eb attached — so build the error with eh and eb and wrap
// it with the surface's context rather than concatenating strings.
func RenderCaptured(in Input) (res Result) {
	if in.Ids == nil || in.Captured.IsEmpty() {
		return
	}
	inst := in.resolve()
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		msgAtoms := c.Atoms().BeginRichTextColored(inst.errorFg, transparentBgEv, "✗ "+in.Captured.Err().Error()).End().Keep()
		c.LabelAtoms(msgAtoms).Wrap().Send()
		if chain := in.Captured.Chain(); !chain.IsEmpty() {
			inst.renderChain(in.Ids, chain)
		}
	}
	res.Drawn = true
	return
}

func (inst settings) renderChain(ids *c.WidgetIdStack, ctx Context) {
	c.AddSpace(styletokens.PaddingInner(inst.density))
	title := fmt.Sprintf("error chain — %d %s", len(ctx.Streams), pluralize("stream", len(ctx.Streams)))
	for range c.CollapsingHeader(ids.PrepareStr("root"), c.WidgetText().Text(title).Keep()).
		DefaultOpen(inst.defaultOpen).KeepIter() {
		for si, st := range ctx.Streams {
			for range c.IdScope(ids.PrepareSeq(uint64(si))) {
				inst.renderStream(ids, st)
			}
		}
	}
}

// renderStream emits one stream's collapsing block.
func (inst settings) renderStream(ids *c.WidgetIdStack, st Stream) {
	header := fmt.Sprintf("%s · %d %s", st.Name, len(st.Facts), pluralize("fact", len(st.Facts)))
	for range c.CollapsingHeader(ids.PrepareStr("stream"), c.WidgetText().Text(header).Keep()).
		DefaultOpen(inst.defaultOpen).KeepIter() {
		for fi, f := range st.Facts {
			for range c.IdScope(ids.PrepareSeq(uint64(fi))) {
				inst.renderFact(ids, f)
			}
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
func (inst settings) renderFact(ids *c.WidgetIdStack, f Fact) {
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
			for range c.Frame(ids.PrepareStr("data")).
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
