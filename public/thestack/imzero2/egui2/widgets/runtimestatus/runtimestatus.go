// Package runtimestatus is an immediate-mode widget (ADR-0267) rendering a
// one-line snapshot of the active runtime services for the host chrome's
// bottom panel. Values are captured once at carousel startup (the set of
// active backends is process-static); the render is a pure read.
//
// Layout (monospace, fixed-ish column widths):
//
//	run:XXXXXXXX  facts:ch  bus ✓  fs ✓  persist:mem  [unattended]
//
// Green ✓ = active, red ✗ = unavailable. The "facts" segment shows
// "ch" when the chstore live connection succeeded and "mem" when the
// carousel fell back to InMemoryFactsStore — useful at a glance when
// a user expects persistence but sees an in-mem fallback.
package runtimestatus

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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
	CapAdhoc   = "adhoc"
)

// Input places the row. Clickable segments need Ids; a row without them
// draws plain labels.
type Input struct {
	// Ids is the host's widget id stack; Render opens one IdScope under it.
	// nil draws the segments as plain, unclickable labels.
	Ids *c.WidgetIdStack
	// ScopeKey names the row within the host's id space; empty uses
	// "runtimestatus".
	ScopeKey string
	// Snapshot is what to show; nil draws nothing.
	Snapshot *Snapshot
	// Clickable draws each segment as a SelectableLabel (clickable but
	// visually like a label) whose click Result.Clicked reports. Needs Ids.
	Clickable bool
}

// Result is what one Render reports.
type Result struct {
	// Clicked is the Cap* id of the segment clicked this frame, "" for none.
	Clicked string
}

// Snapshot describes the active runtime services. Constructed by the
// carousel after all subsystems boot; passed by pointer into
// Render so adding a field is cheap. All fields are
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
	// AdhocActive reports whether the ad-hoc dataset capability started
	// (ADR-0240 §SD3); false leaves adhoc.* unbound.
	AdhocActive bool
	// PersistBackend names the persist.NewService backend: "store"
	// when app state is written to boxer.persiststate through the
	// generated record store (ADR-0105 D3a, durable in ClickHouse),
	// "mem" when it lives in process memory only.
	// Empty means the service didn't start (Phase C wiring skipped).
	//
	// It is reported separately from FactsBackend because the two answer
	// different questions — this one says which code path app state took,
	// FactsBackend says whether that path reaches ClickHouse.
	PersistBackend string
	// Unattended is the state of the agent service's unattended mode
	// (ADR-0298). It is the one segment that is not process-static: the
	// host sets it once the service has started.
	Unattended UnattendedE
}

// UnattendedE is the unattended mode as the bar shows it.
type UnattendedE uint8

const (
	// UnattendedAbsent is a binary built without the mode: no segment.
	UnattendedAbsent UnattendedE = iota
	// UnattendedOff is a binary built with the mode, which is off.
	UnattendedOff
	// UnattendedOn is the host deciding in the person's place.
	UnattendedOn
)

// Render draws the snapshot as a single horizontal row of mono labels.
// Designed to nest inside a c.Horizontal()/MenuBar — does not open its own
// layout container. Under Input.Clickable each segment is a SelectableLabel
// and Result.Clicked names the one clicked.
func Render(in Input) (res Result) {
	if in.Snapshot == nil {
		return
	}
	if in.Ids == nil || !in.Clickable {
		in.render(nil, &res)
		return
	}
	if in.ScopeKey == "" {
		in.ScopeKey = "runtimestatus"
	}
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		in.render(in.Ids, &res)
	}
	return
}

func (in Input) render(ids *c.WidgetIdStack, res *Result) {
	s := in.Snapshot
	renderSegment(ids, "run:"+s.RunIdShort, CapRun, res)
	monoSpacer()
	renderSegment(ids, "facts:"+s.FactsBackend, CapFacts, res)
	monoSpacer()
	renderStatusSegment(ids, "bus", s.BusActive, CapBus, res)
	monoSpacer()
	renderStatusSegment(ids, "fs", s.FsBrokerActive, CapFs, res)
	monoSpacer()
	renderStatusSegment(ids, "adhoc", s.AdhocActive, CapAdhoc, res)
	monoSpacer()
	if s.PersistBackend == "" {
		renderStatusSegment(ids, "persist", false, CapPersist, res)
	} else {
		renderSegment(ids, "persist:"+s.PersistBackend, CapPersist, res)
	}
	switch s.Unattended {
	case UnattendedOff:
		monoSpacer()
		monoLabel("unattended:off")
	case UnattendedOn:
		// Never a SelectableLabel: the colour must survive the clickable
		// row, and the words carry it without colour (ADR-0031 §SD5).
		monoSpacer()
		col := color.Hex(styletokens.WarningDefault.AsHex())
		c.LabelAtoms(c.Atoms().
			BeginRichTextColored(col, color.Transparent, icons.PhWarning+" UNATTENDED — agents act without asking").Monospace().Strong().End().
			Keep()).Send()
	}
}

// renderSegment emits one label-shaped segment. With nil ids a monoLabel
// is used (no click overhead); otherwise a SelectableLabel captures clicks
// while preserving the inline label look.
func renderSegment(ids *c.WidgetIdStack, text, capId string, res *Result) {
	if ids == nil {
		monoLabel(text)
		return
	}
	if c.SelectableLabel(ids.PrepareStr(capId), false, text).
		SendResp().HasPrimaryClicked() {
		res.Clicked = capId
	}
}

// renderStatusSegment emits the "name ✓"/"name ✗" pair as either a
// plain label or a clickable selectable label. The colour applies in
// both modes via RichTextColored.
func renderStatusSegment(ids *c.WidgetIdStack, name string, active bool, capId string, res *Result) {
	if ids == nil {
		monoStatus(name, active)
		return
	}
	// SelectableLabel only takes plain text — drop the colour for
	// the clickable variant. The "✓"/"✗" glyph still differentiates.
	var glyph string
	if active {
		glyph = icons.PhCheck
	} else {
		glyph = icons.PhX
	}
	if c.SelectableLabel(ids.PrepareStr(capId), false, name+" "+glyph).
		SendResp().HasPrimaryClicked() {
		res.Clicked = capId
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
		glyph = icons.PhCheck
		col = color.Hex(styletokens.SuccessDefault.AsHex())
	} else {
		glyph = icons.PhX
		col = color.Hex(styletokens.ErrorDefault.AsHex())
	}
	c.LabelAtoms(
		c.Atoms().
			BeginRichText(name+" ").Monospace().End().
			BeginRichTextColored(col, color.Transparent, glyph).Monospace().End().
			Keep(),
	).Send()
}
