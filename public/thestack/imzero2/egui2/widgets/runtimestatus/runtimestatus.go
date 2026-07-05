// Package runtimestatus renders a one-line snapshot of the active
// runtime services suitable for embedding in the carousel's bottom
// panel. Values are captured once at carousel startup (the set of
// active backends is process-static); the render is a pure read.
//
// Layout (monospace, fixed-ish column widths):
//
//	run:XXXXXXXX  facts:ch  bus ✓  fs ✓  persist:mem
//
// Green ✓ = active, red ✗ = unavailable. The "facts" segment shows
// "ch" when the chstore live connection succeeded and "mem" when the
// carousel fell back to InMemoryFactsStore — useful at a glance when
// a user expects persistence but sees an in-mem fallback.
package runtimestatus

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// CapId values used to identify segments to a click callback. Match
// the capinspector.Cap* constants exactly — runtimestatus stays a
// downstream-free widget by re-declaring the strings rather than
// importing capinspector.
const (
	CapRun     = "run"
	CapFacts   = "facts"
	CapBus     = "bus"
	CapFs      = "fs"
	CapPersist = "persist"
)

// ClickHandler is called when a status segment is clicked. capId is
// one of the Cap* constants above. Nil disables the click affordance
// — segments render as plain labels.
type ClickHandler func(capId string)

// Snapshot describes the active runtime services. Constructed by the
// carousel after all subsystems boot; passed by pointer into
// RenderInline so adding a field is cheap. All fields are
// process-static so the snapshot is built once.
type Snapshot struct {
	// RunIdShort is the first 8 chars of the inherited run_id, or
	// "standalone" when running without runinfo. Truncating keeps
	// the bar one-liner-friendly; full id is in PEBBLE2_RUN_ID and
	// every log line.
	RunIdShort string
	// FactsBackend is "ch" when chstore.NewWithFallback returned a
	// live ClickHouse-backed store, "mem" when it fell back to
	// InMemoryFactsStore.
	FactsBackend string
	// BusActive reports whether the carousel constructed inprocbus
	// (Phase A); false leaves MountCtx.Bus() as NoopBus.
	BusActive bool
	// FsBrokerActive reports whether fsbroker.NewService succeeded
	// (Phase B); false leaves fs.* unbound.
	FsBrokerActive bool
	// PersistBackend names the persist.NewService backend. "mem" is
	// the current default (NewMemoryBackend); future amendments may
	// land "facts" or "disk". Empty means the service didn't start
	// (Phase C wiring skipped).
	PersistBackend string
}

// RenderInline draws the snapshot as a single horizontal row of mono
// labels. Designed to nest inside a c.Horizontal()/MenuBar — does not
// open its own layout container. When onClick is non-nil each segment
// is rendered as a SelectableLabel (clickable but visually like a
// label) and the callback fires with the segment's CapId on
// HasPrimaryClicked.
func RenderInline(s *Snapshot, onClick ClickHandler) {
	if s == nil {
		return
	}
	idsLocal := c.NewWidgetIdStack()
	renderSegment(idsLocal, "run:"+s.RunIdShort, CapRun, onClick)
	monoSpacer()
	renderSegment(idsLocal, "facts:"+s.FactsBackend, CapFacts, onClick)
	monoSpacer()
	renderStatusSegment(idsLocal, "bus", s.BusActive, CapBus, onClick)
	monoSpacer()
	renderStatusSegment(idsLocal, "fs", s.FsBrokerActive, CapFs, onClick)
	monoSpacer()
	if s.PersistBackend == "" {
		renderStatusSegment(idsLocal, "persist", false, CapPersist, onClick)
	} else {
		renderSegment(idsLocal, "persist:"+s.PersistBackend, CapPersist, onClick)
	}
}

// renderSegment emits one label-shaped segment. When onClick is nil a
// monoLabel is used (no click overhead); otherwise a SelectableLabel
// captures clicks while preserving the inline label look.
func renderSegment(ids *c.WidgetIdStack, text, capId string, onClick ClickHandler) {
	if onClick == nil {
		monoLabel(text)
		return
	}
	if c.SelectableLabel(ids.PrepareStr("seg-"+capId), false, text).
		SendResp().HasPrimaryClicked() {
		onClick(capId)
	}
}

// renderStatusSegment emits the "name ✓"/"name ✗" pair as either a
// plain label or a clickable selectable label. The colour applies in
// both modes via RichTextColored.
func renderStatusSegment(ids *c.WidgetIdStack, name string, active bool, capId string, onClick ClickHandler) {
	if onClick == nil {
		monoStatus(name, active)
		return
	}
	// SelectableLabel only takes plain text — drop the colour for
	// the clickable variant. The "✓"/"✗" glyph still differentiates.
	var glyph string
	if active {
		glyph = "✓"
	} else {
		glyph = "✗"
	}
	if c.SelectableLabel(ids.PrepareStr("seg-"+capId), false, name+" "+glyph).
		SendResp().HasPrimaryClicked() {
		onClick(capId)
	}
}

// monoLabel emits a plain monospace label.
func monoLabel(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Monospace().End().Keep()).Send()
}

// monoSpacer renders a two-space gap with a fixed-width middot
// separator. Keeps the visual rhythm without ambiguity when the
// labels themselves contain spaces.
func monoSpacer() {
	c.LabelAtoms(c.Atoms().BeginRichText(" · ").Monospace().End().Keep()).Send()
}

// monoStatus renders "name ✓" in the IDS Success role when active and
// "name ✗" in the Error role otherwise (ADR-0031 §SD2). The check /
// cross glyphs are plain Unicode (U+2713 / U+2717) — covered natively
// by Noto Sans and any reasonable proportional/mono font, so no IDS
// icon-font slot is consulted. If a future host font drops them the
// colour still differentiates active from inactive.
func monoStatus(name string, active bool) {
	var glyph string
	var col color.Color
	if active {
		glyph = "✓"
		col = color.Hex(styletokens.SuccessDefault.AsHex())
	} else {
		glyph = "✗"
		col = color.Hex(styletokens.ErrorDefault.AsHex())
	}
	c.LabelAtoms(
		c.Atoms().
			BeginRichText(name+" ").Monospace().End().
			BeginRichTextColored(col, color.Transparent, glyph).Monospace().End().
			Keep(),
	).Send()
}
