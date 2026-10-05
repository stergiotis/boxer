// Package regexsummary is an immediate-mode widget (ADR-0267): a two-level
// summary of a single regular-expression value.
//
//   - Level 1 (anchor): a compact inline row — magnifying-glass icon +
//     the pattern in monospace (truncated to a configurable cap) + a
//     small compile-status dot (green when Go's regexp accepts the
//     pattern, red when it doesn't, dim when the pattern is empty) —
//     paired with the standard [inspector.AnchorToggle] glyph. Every
//     regexsummary instance carries the toggle by default; there is no
//     opt-in.
//   - Level 2 (inspector window): a draggable [c.Window] containing the
//     full [regex_explorer] body (cheatsheet panel, pattern + haystack +
//     multi-pattern inputs, Test / List / Replace tabs, bottom status
//     bar) plus the standard [inspector.ProvenanceChip]. Opened by
//     clicking the toggle and closed by clicking it again or the
//     window's title-bar X. A bezier connector (via
//     [inspector.AnchorTether]) visually tethers the toggle to the
//     open window.
//
// Pattern seeding is one-way: each false→true open transition seeds
// the embedded explorer's pattern field from the host-supplied
// pattern. Subsequent edits inside the inspector stay local to the
// EmbeddedApp and do not flow back to the host; the "this will be added
// once bidirectional inspectors are available" roadmap is tracked in the
// same place as the rest of the inspector infrastructure (ADR-0046).
//
// The host owns everything that survives a frame: the pinned open/closed
// flag, the lazily-constructed [regex_explorer.EmbeddedApp] and the last
// seeded pattern live in a [State] the host holds and passes on every
// [Render]. Ids are the host's [c.WidgetIdStack] under a ScopeKey, so two
// summaries in one host differ by ScopeKey alone and a row of them is a
// per-row IdScope away.
//
// BusI handoff: the optional [Input.Bus] attaches a clickhouse-local-capable
// BusI (typically the host's MountContext.Bus()) to the embedded explorer;
// when no bus is attached the inspector falls back to the Go-side preview
// only and CH-backed tabs surface a clear "no bus attached" error.
package regexsummary

import (
	"errors"
	"regexp"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// ChLocalCapPattern is the capability an app hosting this widget must
// declare in its Manifest.Caps for the inspector's ClickHouse tabs — and
// its SD1 tripwire — to run: the widget carries no manifest of its own,
// and a bus without the grant is a bus whose every request the broker
// denies. Hosts that want the Go-side preview only can leave it out.
const ChLocalCapPattern = regex_explorer.ChLocalCapPattern

// defaultPatternMaxLen is the default truncation cap for the level-1
// inline pattern display. 32 keeps the row narrow enough to sit beside
// a typical label without forcing the host's Horizontal to wrap, while
// still showing enough of the pattern to disambiguate at a glance.
const defaultPatternMaxLen = 32

// defaultPopupW / defaultPopupH size the level-2 inspector window's
// first-open envelope. The values match [regex_explorer.manifest]'s
// SurfaceHints (1100×720) so the embedded body sits in roughly the
// same envelope it gets when launched as a standalone window — the
// cheatsheet + tabbed body need this much horizontal width to look
// uncramped.
const (
	defaultPopupW float32 = 1100
	defaultPopupH float32 = 720
)

// defaultScopeKey names the widget when Input.ScopeKey is empty.
const defaultScopeKey = "regexsummary"

// ErrNeedsIdsAndState is Result.Err when Input.Ids or Input.State is nil:
// the widget has nowhere to derive its ids or record whether its
// inspector is open. Drawn in place of the row.
var ErrNeedsIdsAndState = errors.New("regexsummary: Input.Ids and Input.State are required")

// State is the host-owned part of one summary that survives a frame: the
// inspector's open flag, the embedded explorer and what it was last seeded
// with. The zero value is a closed inspector. Hold one per summary — two
// summaries sharing a State share an inspector.
//
// The embedded explorer is lazily allocated on the first false→true
// open transition: a summary that is never opened pays no allocation
// cost; once opened, the explorer state (pattern, haystack, flags,
// query results, tripwire) persists across close/reopen cycles for as
// long as the host keeps the State. Subsequent opens re-seed the pattern
// from the host-supplied argument so the inspector tracks the host source
// at each open without losing the user's intra-session edits to
// haystack / replacement / flags / multi-patterns.
type State struct {
	// Pinned is whether the inspector window is open. Exported so a host
	// can persist it or open the inspector from code; the window's
	// title-bar X writes it back through an R10 databinding.
	Pinned bool

	embedded       *regex_explorer.EmbeddedApp
	lastSeededPat  string
	seededAtLeast1 bool
}

// Input is one frame's declaration of the summary.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two summaries in one host need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this summary within the host's id space; empty uses
	// "regexsummary". It also names the inspector window unless Title is
	// set.
	ScopeKey string
	// Pattern is the host's regex source. Read each frame to drive the
	// level-1 display + compile-status dot, and copied into the embedded
	// explorer's pattern field on each false→true open transition.
	Pattern string
	// State is the host-owned open flag and embedded explorer. Required.
	State *State

	// Bus, when non-nil, is forwarded to the embedded explorer so its
	// clickhouse-local queries route through the host's bus client. nil
	// falls back to [runtimeapp.NoopBus] (the Go-side preview still works;
	// CH-backed tabs surface a clear "no bus attached" error).
	Bus runtimeapp.BusI
	// Provenance, when non-zero, renders the standard
	// [inspector.ProvenanceChip] at the top of the inspector window so
	// operators can see which subject / source-app produced the regex
	// this widget is summarising.
	Provenance inspector.Provenance
	// Title is the inspector window's title; empty reads "regex: " plus
	// the ScopeKey.
	Title string

	// PopupWidth and PopupHeight set the inspector window's first-open
	// envelope in points; zero takes 1100×720, the standalone explorer's
	// surface hint. The body itself is resizable, so this only affects
	// the initial draw.
	PopupWidth  float32
	PopupHeight float32
	// PatternMaxLen caps the level-1 inline pattern display in runes; a
	// longer pattern renders as "<first n-1 chars>…". Zero (or a negative
	// value) takes 32. The full pattern is always available inside the
	// inspector window.
	PatternMaxLen int
	// HideIcon drops the magnifying-glass affordance from the level-1
	// row; HidePattern drops the inline pattern (for a host that already
	// shows it, collapsing the row to icon + dot + toggle); HideStatusDot
	// drops the compile-status dot. The status is Go's [regexp.Compile];
	// CH-side compile errors (rare — RE2 in Go and libre2 in CH almost
	// always agree, see [regex_explorer]'s SD1 tripwire) are not
	// reflected here.
	HideIcon      bool
	HidePattern   bool
	HideStatusDot bool
}

// Result is what one Render reports.
type Result struct {
	// Toggled is true on the frame the anchor toggle was clicked; the new
	// open state is in State.Pinned.
	Toggled bool
	// Err is [ErrNeedsIdsAndState] when the Input cannot be drawn; the
	// message is drawn in place of the row.
	Err error
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// Render emits the level-1 inline row paired with the standard
// [inspector.AnchorToggle]. Clicking the toggle opens the inspector
// window containing the embedded regex explorer; clicking the toggle
// again or the window's title-bar X closes it. A bezier connector ties
// the toggle to the open window via [inspector.AnchorTether]. Call once
// per frame inside the host's layout; every widget id is derived under
// one IdScope keyed by ScopeKey.
func Render(in Input) (res Result) {
	if in.Ids == nil || in.State == nil {
		res.Err = ErrNeedsIdsAndState
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		res = in.render()
	}
	return
}

func (in Input) render() (res Result) {
	ids, st := in.Ids, in.State
	// The tether infrastructure keys its rect captures by a string; it is
	// derived from this scope's id so two summaries never share a slot.
	tether := inspector.NewAnchorTether(ids.PrepareStr("tether").Derive())
	wasPinned := st.Pinned
	for range c.Horizontal().KeepIter() {
		in.renderLevel1Atoms()
		res.Toggled = inspector.AnchorToggle(ids.PrepareStr("anchor-toggle"), &st.Pinned)
		tether.CaptureToggle()
	}

	if !st.Pinned {
		return
	}

	// false→true transition (or first-ever open): lazy-allocate the
	// embedded explorer and seed its pattern field. The seed honours
	// the "interaction does not change source" contract — every open
	// starts mirrored to the host pattern, intra-session edits stay
	// local. Subsequent frames within the same open session do not
	// reseed.
	if !wasPinned || !st.seededAtLeast1 {
		in.ensureEmbedded()
		if !st.seededAtLeast1 || in.Pattern != st.lastSeededPat {
			st.embedded.SetPattern(in.Pattern)
			st.lastSeededPat = in.Pattern
			st.seededAtLeast1 = true
		}
	}
	// Bus may change across frames (e.g. host re-mounted with a new
	// bus); push it through on every open frame so the embedded
	// explorer's CH queries route through the current bus.
	st.embedded.SetBus(in.Bus)

	in.renderPinnedWindow(tether)
	tether.Paint()
	return
}

// renderLevel1Atoms emits the inline icon + truncated pattern +
// compile-status dot triplet. Order: icon, pattern, dot — left-to-
// right so the eye lands on the affordance (icon) first, then reads
// the pattern, then catches the status indicator before the anchor
// toggle. Each element is gated by its Hide* flag so a host can
// collapse the row to whatever subset it prefers.
//
// The atoms are stitched as one rich-text label per element rather
// than one composite string because the status dot needs an
// independent foreground colour (green/red) — building three small
// labels in the same Horizontal is the lowest-friction way to mix
// colours under the current Atoms API.
func (in Input) renderLevel1Atoms() {
	transparentBg := color.Transparent
	if !in.HideIcon {
		accent := color.Hex(styletokens.AccentDefault.AsHex())
		atoms := c.Atoms().
			BeginRichTextColored(accent, transparentBg, icons.PhMagnifyingGlass).
			Monospace().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
	if !in.HidePattern {
		display := truncatePattern(in.Pattern, in.PatternMaxLen)
		atoms := c.Atoms().
			BeginRichText(display).
			Monospace().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
	if !in.HideStatusDot {
		dotColor, ok := compileStatusColor(in.Pattern)
		if !ok {
			// Empty pattern — render no dot at all rather than a dim
			// "indeterminate" glyph; an empty regex is a valid state
			// the user is about to type into and the host will
			// usually elide the status feedback for it.
			return
		}
		atoms := c.Atoms().
			BeginRichTextColored(dotColor, transparentBg, icons.PhDot).
			Monospace().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
}

// compileStatusColor returns the dot foreground colour for the level-1
// status indicator: green ([styletokens.SuccessDefault]) when the
// pattern compiles under Go's regexp engine, red
// ([styletokens.ErrorDefault]) otherwise. Returns ok=false for empty
// patterns so the caller can elide the dot entirely.
func compileStatusColor(pattern string) (dotColor color.Color, ok bool) {
	if pattern == "" {
		return
	}
	_, err := regexp.Compile(pattern)
	if err != nil {
		dotColor = color.Hex(styletokens.ErrorDefault.AsHex())
	} else {
		dotColor = color.Hex(styletokens.SuccessDefault.AsHex())
	}
	ok = true
	return
}

// truncatePattern caps the pattern to maxLen runes, appending a single
// horizontal ellipsis when truncation occurs. Counts in runes (not
// bytes) so multi-byte characters in patterns (e.g. unicode-class
// shortcuts) don't produce mid-codepoint cuts that egui's text shaper
// would refuse to lay out. maxLen < 1 takes the default cap.
func truncatePattern(pattern string, maxLen int) (display string) {
	if maxLen < 1 {
		maxLen = defaultPatternMaxLen
	}
	runes := []rune(pattern)
	if len(runes) <= maxLen {
		display = pattern
		return
	}
	display = string(runes[:maxLen-1]) + "…"
	return
}

// ensureEmbedded lazily allocates the per-summary EmbeddedApp on the
// first open. Subsequent opens reuse the same embedded state so the
// user's intra-session edits to haystack / replacement / flags
// persist across close/reopen cycles for the life of the State.
//
// The embedded app's id seed is derived under this summary's scope so
// two summaries on the same screen draw under independent id namespaces —
// without this, the embedded explorer's PrepareStr-derived ids would
// collide at the top of the stack.
func (in Input) ensureEmbedded() {
	if in.State.embedded != nil {
		return
	}
	in.State.embedded = regex_explorer.NewEmbedded(in.Ids.PrepareStr("embedded").Derive())
}

// renderPinnedWindow emits the c.Window holding the embedded regex
// explorer. The native title-bar X is wired to the same pinned flag via
// OpenBound + R10 databinding (fsmview pattern) so closing through
// egui's chrome flips the toggle the same way clicking the anchor would.
// The tether's CaptureWindow runs at the top of the body so the bezier
// "to" endpoint anchors on the window's content rect (title bar
// excluded).
//
// The window is the one floating surface this widget owns, so it takes
// the one absolute id the contract allows (ADR-0267 W6), derived from
// this summary's scope.
func (in Input) renderPinnedWindow(tether inspector.AnchorTether) {
	st := in.State
	winId := c.MakeAbsoluteIdHighEntropy(in.Ids.PrepareStr("window").Derive())
	title := in.Title
	if title == "" {
		title = "regex: " + in.scopeKey()
	}
	w, h := in.PopupWidth, in.PopupHeight
	if w <= 0 {
		w = defaultPopupW
	}
	if h <= 0 {
		h = defaultPopupH
	}
	win := c.Window(winId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		AlwaysOnTop(true).
		DefaultSize(w, h)
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &st.Pinned)
	for range win.KeepIter() {
		tether.CaptureWindow()
		if !in.Provenance.IsZero() {
			inspector.ProvenanceChip(in.Provenance)
			c.Separator().Horizontal().Send()
		}
		st.embedded.Render()
	}
}
