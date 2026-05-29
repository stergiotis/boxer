//go:build llm_generated_opus47

// Package regexsummary implements a two-level summary widget for a
// single regular-expression value.
//
//   - Level 1 (anchor): a compact inline row — magnifying-glass icon +
//     the pattern in monospace (truncated to a configurable cap) + a
//     small compile-status dot (green when Go's regexp accepts the
//     pattern, red when it doesn't, dim when the pattern is empty) —
//     paired with the standard [inspector.AnchorToggle] glyph. Every
//     regexsummary instance carries the toggle by default; there is no
//     opt-in.
//   - Level 2 (inspector window): a draggable [c.Window] containing the
//     full [regex_explorer] body (cheatsheet panel, pattern + haystack
//     + multi-pattern inputs, Test / List / Replace tabs, bottom status
//     bar) plus the standard [inspector.ProvenanceChip]. Opened by
//     clicking the toggle and closed by clicking it again or the
//     window's title-bar X. A bezier connector (via
//     [inspector.AnchorTether]) visually tethers the toggle to the
//     open window.
//
// Pattern seeding is one-way: each false→true open transition seeds
// the embedded explorer's pattern field from the host-supplied
// `pattern` argument. Subsequent edits inside the inspector stay
// local to the EmbeddedApp and do not flow back to the host; the
// "this will be added once bidirectional inspectors are available"
// roadmap is tracked in the same place as the rest of the inspector
// infrastructure (ADR-0046).
//
// Each idPrefix names one logical regexsummary instance: the pinned
// open/closed state, the lazily-constructed [regex_explorer.EmbeddedApp]
// and the last seeded pattern are held in a package-level state map
// keyed by idPrefix combined with the caller's per-call-site idGen,
// so the value-receiver / fluent-builder pattern stays intact and the
// same Renderer can be called multiple times in one frame (e.g. once
// per row of a list) without colliding on toggle / window / embedded-
// explorer ids.
//
// BusI handoff: the optional [Renderer.Bus] fluent setter attaches a
// clickhouse-local-capable BusI (typically the host's
// MountContext.Bus()) to every embedded explorer the Renderer spawns;
// when no bus is attached the inspector falls back to the Go-side
// preview only and CH-backed tabs surface a clear "no bus attached"
// error. The Provenance chip mirrors distsummary's optional-by-default
// posture.
package regexsummary

import (
	"regexp"
	"strconv"
	"sync"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

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

// Renderer is the configured regexsummary widget. Values are immutable
// after construction; fluent setters return modified copies. Per-instance
// pinned-window state, lazily-allocated EmbeddedApp, and last-seeded
// pattern are held in the [instanceStates] package map keyed by the
// per-call scope — so the value-receiver / fluent chain pattern can
// stay intact while still giving every Renderer a persistent inspector
// toggle and a persistent embedded explorer that survives across frames.
type Renderer struct {
	idPrefix      string
	popupWidth    float32
	popupHeight   float32
	patternMaxLen int
	showIcon      bool
	showPattern   bool
	showStatusDot bool

	// bus, when non-nil, is forwarded to every [regex_explorer.EmbeddedApp]
	// the Renderer spawns so the embedded explorer's clickhouse-local
	// queries route through the host's bus client. nil falls back to
	// [runtimeapp.NoopBus] (the Go-side preview still works; CH-backed
	// tabs surface a clear "no bus attached" error).
	bus runtimeapp.BusI

	// provenance, when non-zero, renders the standard
	// [inspector.ProvenanceChip] at the top of the inspector window so
	// operators can see which subject / source-app produced the regex
	// this widget is summarising.
	provenance inspector.Provenance
}

// instanceState carries the per-regexsummary pinned-window open flag,
// the embedded explorer state, and the last pattern the embedded
// explorer was seeded with. Lives in [instanceStates] keyed by per-call
// scope so the value-receiver Renderer can stay stateless while still
// driving a real per-instance toggle + embedded explorer.
//
// The embedded explorer is lazily allocated on the first false→true
// open transition: Renderers that are never opened pay no allocation
// cost; once opened, the explorer state (pattern, haystack, flags,
// query results, tripwire) persists across close/reopen cycles within
// the same widget instance. Subsequent opens re-seed the pattern from
// the host-supplied argument so the inspector tracks the host source
// at each open without losing the user's intra-session edits to
// haystack / replacement / flags / multi-patterns.
type instanceState struct {
	pinned         bool
	embedded       *regex_explorer.EmbeddedApp
	lastSeededPat  string
	seededAtLeast1 bool
}

// instanceStates is the package-level state map, keyed by per-call
// scope. Mirrors [distsummary] (composite_widget_state memory): one
// entry per unique scope ever rendered, never reclaimed — acceptable
// for typical app shapes; apps that dynamically mount/unmount short-
// lived regexsummary instances with one-shot idPrefixes leak O(mounts)
// memory.
var instanceStates sync.Map // map[string]*instanceState

func getInstanceState(scope string) (s *instanceState) {
	actual, _ := instanceStates.LoadOrStore(scope, &instanceState{})
	s = actual.(*instanceState)
	return
}

// New constructs a Renderer with IDS-aligned defaults.
//
// Defaults:
//
//   - popup size: 1100×720 (matches [regex_explorer.manifest]'s
//     SurfaceHints so the embedded body has room for the cheatsheet
//     panel and the tabbed result area)
//   - pattern truncation cap: 32 characters
//   - all three inline elements (magnifying-glass icon, pattern,
//     compile-status dot) are shown by default
//   - no bus attached — CH-backed tabs in the inspector return the
//     NoopBus "no bus attached" error until [Renderer.Bus] is called
//   - no provenance chip — call [Renderer.Provenance] to bind one
//
// idPrefix scopes any widget-id-bearing primitive emitted by Render —
// pass a stable short string (e.g. "match-rule", "user-search-regex").
func New(idPrefix string) (inst Renderer) {
	inst = Renderer{
		idPrefix:      idPrefix,
		popupWidth:    defaultPopupW,
		popupHeight:   defaultPopupH,
		patternMaxLen: defaultPatternMaxLen,
		showIcon:      true,
		showPattern:   true,
		showStatusDot: true,
	}
	return
}

// Bus attaches a clickhouse-local-capable BusI to every embedded
// regex explorer this Renderer spawns. Typically the host's
// MountContext.Bus(). Passing nil is a no-op; pass an explicit
// [runtimeapp.NoopBus] to revert to "no CH" semantics if a previously-
// attached bus needs to be detached (rare).
func (inst Renderer) Bus(bus runtimeapp.BusI) (out Renderer) {
	inst.bus = bus
	out = inst
	return
}

// PopupSize sets the inspector window's first-open envelope (width,
// height) in points. The body itself is resizable, so this only
// affects the initial draw; large enough to fit the cheatsheet + tab
// area without scrolling is the goal.
func (inst Renderer) PopupSize(w, h float32) (out Renderer) {
	inst.popupWidth = w
	inst.popupHeight = h
	out = inst
	return
}

// PatternMaxLen sets the truncation cap (in runes) for the level-1
// inline pattern display. Patterns longer than the cap render as
// "<first n-1 chars>…". Values below 1 are clamped to
// [defaultPatternMaxLen]. The full pattern is always available inside
// the inspector window regardless of cap.
func (inst Renderer) PatternMaxLen(n int) (out Renderer) {
	if n < 1 {
		n = defaultPatternMaxLen
	}
	inst.patternMaxLen = n
	out = inst
	return
}

// ShowIcon toggles the magnifying-glass affordance icon on the
// level-1 row. Default true.
func (inst Renderer) ShowIcon(b bool) (out Renderer) {
	inst.showIcon = b
	out = inst
	return
}

// ShowPattern toggles the inline pattern display on the level-1 row.
// Default true. Useful when the host already shows the pattern
// elsewhere and the level-1 should collapse to icon + dot + toggle.
func (inst Renderer) ShowPattern(b bool) (out Renderer) {
	inst.showPattern = b
	out = inst
	return
}

// ShowStatusDot toggles the green/red compile-status dot on the
// level-1 row. Default true. The status is computed via Go's
// [regexp.Compile]; CH-side compile errors (rare — RE2 in Go and
// libre2 in CH almost always agree, see [regex_explorer]'s SD1
// tripwire) are not reflected here.
func (inst Renderer) ShowStatusDot(b bool) (out Renderer) {
	inst.showStatusDot = b
	out = inst
	return
}

// Provenance binds the regex to its source value's
// [inspector.Provenance] identity card. When set (non-zero), the
// level-2 inspector window renders the standard
// [inspector.ProvenanceChip] at the top so operators can see which
// subject / source-app produced this regex. Zero value (default)
// suppresses the chip.
func (inst Renderer) Provenance(p inspector.Provenance) (out Renderer) {
	inst.provenance = p
	out = inst
	return
}

// Render emits the level-1 inline row paired with the standard
// [inspector.AnchorToggle]. Clicking the toggle opens the inspector
// window containing the embedded regex explorer; clicking the toggle
// again or the window's title-bar X closes it. A bezier connector ties
// the toggle to the open window via [inspector.AnchorTether]. The
// pinned open/closed state and the lazily-allocated embedded explorer
// are held in the package-level [instanceStates] map keyed by the
// per-call scope so they survive across Render calls without forcing
// a pointer-receiver API.
//
//   - idGen is consumed exactly once via [c.WidgetIdCreatorI.Derive]
//     so the caller's WidgetIdStack state-machine contract
//     (Initial → Prepared → Initial) holds in one hop. Toggle, window,
//     and embedded-explorer ids are derived from
//     [c.MakeAbsoluteIdStr] over the per-call scope instead, because
//     they must match across frames for response-by-id lookups (a
//     stack-prepared id XORs the surrounding stack top in and would
//     silently miss).
//   - pattern is the host's regex source. Read each frame to drive the
//     level-1 display + compile-status dot, and copied into the
//     embedded explorer's pattern field on each false→true open
//     transition.
func (inst Renderer) Render(idGen c.WidgetIdCreatorI, pattern string) {
	callId := idGen.Derive()
	scope := callScope(inst.idPrefix, callId)
	state := getInstanceState(scope)

	tether := inspector.NewAnchorTether(scope)
	toggleId := c.MakeAbsoluteIdStr(scope + "-anchor-toggle")
	wasPinned := state.pinned
	for range c.Horizontal().KeepIter() {
		inst.renderLevel1Atoms(pattern)
		inspector.AnchorToggle(toggleId, &state.pinned)
		tether.CaptureToggle()
	}

	if !state.pinned {
		return
	}

	// false→true transition (or first-ever open): lazy-allocate the
	// embedded explorer and seed its pattern field. The seed honours
	// the "interaction does not change source" contract — every open
	// starts mirrored to the host pattern, intra-session edits stay
	// local. Subsequent frames within the same open session do not
	// reseed.
	if !wasPinned || !state.seededAtLeast1 {
		inst.ensureEmbedded(state, scope)
		if !state.seededAtLeast1 || pattern != state.lastSeededPat {
			state.embedded.SetPattern(pattern)
			state.lastSeededPat = pattern
			state.seededAtLeast1 = true
		}
	}
	// Bus may change across frames (e.g. host re-mounted with a new
	// bus); push it through on every open frame so the embedded
	// explorer's CH queries route through the current bus.
	state.embedded.SetBus(inst.bus)

	inst.renderPinnedWindow(scope, tether, state)
	tether.Paint()
}

// callScope combines the developer-supplied idPrefix with the per-call
// disambiguator derived from idGen. Format: "idPrefix#<hex>". Stable
// across frames for the same call site (idGen.Derive is deterministic
// on the same prepared id under the same surrounding IdScope), so the
// derived toggle / window / state ids stay put while still being
// unique across multiple .Render(...) calls on the same Renderer.
func callScope(idPrefix string, callId uint64) (scope string) {
	scope = idPrefix + "#" + strconv.FormatUint(callId, 16)
	return
}

// renderLevel1Atoms emits the inline icon + truncated pattern +
// compile-status dot triplet. Order: icon, pattern, dot — left-to-
// right so the eye lands on the affordance (icon) first, then reads
// the pattern, then catches the status indicator before the anchor
// toggle. Each element is gated by its respective Show* flag so
// callers can collapse the row to whatever subset they prefer.
//
// The atoms are stitched as one rich-text label per element rather
// than one composite string because the status dot needs an
// independent foreground colour (green/red) — building three small
// labels in the same Horizontal is the lowest-friction way to mix
// colours under the current Atoms API.
func (inst Renderer) renderLevel1Atoms(pattern string) {
	transparentBg := color.Transparent
	if inst.showIcon {
		accent := color.Hex(styletokens.AccentDefault.AsHex())
		atoms := c.Atoms().
			BeginRichTextColored(accent, transparentBg, icons.PhMagnifyingGlass).
			Monospace().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
	if inst.showPattern {
		display := truncatePattern(pattern, inst.patternMaxLen)
		atoms := c.Atoms().
			BeginRichText(display).
			Monospace().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
	if inst.showStatusDot {
		dotColor, ok := compileStatusColor(pattern)
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
// would refuse to lay out.
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

// ensureEmbedded lazily allocates the per-instance EmbeddedApp on the
// first open. Subsequent opens reuse the same embedded state so the
// user's intra-session edits to haystack / replacement / flags
// persist across close/reopen cycles within the same widget instance.
//
// The seed for the embedded app is derived from a per-scope absolute
// widget id so two regexsummary instances on the same screen draw
// under independent id namespaces — without this, the embedded
// explorer's PrepareStr-derived ids would collide at the top of the
// stack.
func (inst Renderer) ensureEmbedded(state *instanceState, scope string) {
	if state.embedded != nil {
		return
	}
	seed := uint64(c.MakeAbsoluteIdStr(scope + "-embedded"))
	state.embedded = regex_explorer.NewEmbedded(seed)
}

// renderPinnedWindow emits the c.Window holding the embedded regex
// explorer. Mirrors [distsummary.Renderer.renderPinnedWindow]: native
// title-bar X is wired to the same pinned flag via OpenBound + R10
// databinding (fsmview pattern) so closing through egui's chrome flips
// the toggle the same way clicking the anchor would. The tether's
// CaptureWindow runs at the top of the body so the bezier "to"
// endpoint anchors on the window's content rect (title bar excluded).
//
// The window id is scoped by the per-call `scope`, not idPrefix alone,
// so two .Render(...) calls on the same Renderer open two independent
// inspector windows backed by two independent embedded explorers.
func (inst Renderer) renderPinnedWindow(scope string, tether inspector.AnchorTether, state *instanceState) {
	winId := c.MakeAbsoluteIdStr(scope + "-anchor-window")
	title := "regex: " + inst.idPrefix
	win := c.Window(winId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		AlwaysOnTop(true).
		DefaultSize(inst.popupWidth, inst.popupHeight)
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &state.pinned)
	for range win.KeepIter() {
		tether.CaptureWindow()
		if !inst.provenance.IsZero() {
			inspector.ProvenanceChip(inst.provenance)
			c.Separator().Horizontal().Send()
		}
		state.embedded.Render()
	}
}
