// Package canonicaltypesummary is a reusable, tethered value-inspector for
// a single leeway canonical type
// ([canonicaltypes]). It is the inspector half of ADR-0067 and is built on
// the shared ADR-0046 inspector infrastructure, modelled directly on
// [distsummary] / [regexsummary].
//
//   - Level 1 (anchor): a compact inline row — a brackets icon + the
//     canonical string in monospace (truncated to a configurable cap) + a
//     small validity dot (green when the type parses and [canonicaltypes.AstNodeI.IsValid]
//     accepts it, red otherwise, elided when the string is empty) + a terse
//     "N fields · K B" footprint trailer — paired with the standard
//     [inspector.AnchorToggle] glyph. Every instance carries the toggle by
//     default.
//   - Level 2 (inspector window): a draggable [c.Window] sized to
//     [styletokens.SurfaceInspector] containing a three-tab body — Layout
//     (a byte-footprint strip), Members (a decomposed table, one row per
//     [canonicaltypes.AstNodeI.IterateMembers] entry), and Go codec (the
//     per-node [canonicaltypes.PrimitiveAstNodeI.GenerateGoCode] output in a
//     syntax-highlighted [codeview] block) — plus the optional
//     [inspector.ProvenanceChip]. A bezier connector (via
//     [inspector.AnchorTether]) tethers the toggle to the open window.
//
// Input is the canonical string; it is parsed once per change into a primitive,
// a flat group, or a signature (groups joined by '_'). An AstNodeI overload is
// deferred per ADR-0067. The widget is read-only — editing a type is
// [canonicaltypeedit]'s job.
//
// Each idPrefix combined with the per-call idGen scope names one logical
// instance: the pinned open/closed flag, the selected tab, the cached parse,
// and the retained code-view holder live in a package-level state map keyed
// by that scope, so the value-receiver / fluent-builder pattern stays intact
// and the same Renderer can be called multiple times in one frame (e.g. once
// per row of a schema list) without colliding on toggle / window / tab ids.
package canonicaltypesummary

import (
	"strconv"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// tabE selects which body the inspector window renders.
type tabE uint8

const (
	// tabLayout is the zero value so a fresh instanceState lands on the
	// byte-footprint strip without an explicit initialiser.
	tabLayout tabE = iota
	tabMembers
	tabCodec
)

// defaultNameMaxLen caps the level-1 inline canonical-string display. 48
// keeps the row narrow enough to sit beside a field label while still
// showing a typical primitive or short group in full; the untruncated
// string is always available in the inspector window header.
const defaultNameMaxLen = 48

// Renderer is the configured canonicaltypesummary widget. Values are
// immutable after construction; fluent setters return modified copies.
// Per-instance pinned-window state, selected tab, cached parse, and the
// retained code-view holder live in the [instanceStates] package map keyed
// by the per-call scope — so the value-receiver / fluent chain pattern can
// stay intact while still giving every Renderer a persistent inspector.
type Renderer struct {
	idPrefix    string
	popupWidth  float32
	popupHeight float32
	nameMaxLen  int
	showIcon    bool
	defaultOpen bool

	// provenance, when non-zero, renders the standard
	// [inspector.ProvenanceChip] at the top of the inspector window so
	// operators can see which subject / source produced the type this
	// widget is summarising. Zero value (default) suppresses the chip.
	provenance inspector.Provenance
}

// instanceState carries the per-instance pinned-window flag, the selected
// tab, the cached parse result (so an unchanged string is not re-parsed
// every frame), and the retained Go code-view holder (rebuilt only when the
// generated source changes). Lives in [instanceStates] keyed by per-call
// scope so the value-receiver Renderer can stay stateless while still
// driving a real per-instance inspector.
type instanceState struct {
	pinned bool
	// pinnedInit guards the one-time seed of pinned from the Renderer's
	// DefaultOpen at first render, so a caller-requested start-open does not
	// re-assert itself after the user later closes the window.
	pinnedInit bool
	tab        tabE

	// inSrc is the canonical string the cached ast / parseErr were derived
	// from. A mismatch on the next Render triggers a re-parse.
	inSrc    string
	ast      canonicaltypes.AstNodeI
	parseErr error

	// goView is the retained, syntax-highlighted Go code-view holder for
	// the codec tab; goViewSrc is the generated source it was built from so
	// it is rebuilt only when the type (hence the source) changes.
	goView    typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	goViewSrc string
}

// instanceStates is the package-level state map, keyed by per-call scope.
// Mirrors [distsummary] / [regexsummary]: one entry per unique scope ever
// rendered, never reclaimed — acceptable for typical app shapes; apps that
// dynamically mount/unmount short-lived instances with one-shot idPrefixes
// leak O(mounts) memory.
var instanceStates sync.Map // map[string]*instanceState

func getInstanceState(scope string) (s *instanceState) {
	actual, _ := instanceStates.LoadOrStore(scope, &instanceState{})
	s = actual.(*instanceState)
	return
}

// New constructs a Renderer with IDS-aligned defaults:
//
//   - popup size: [styletokens.SurfaceInspector] (420×560) — the compact
//     accessory archetype created for the tethered-inspector role (ADR-0065)
//   - canonical-string truncation cap: 48 runes
//   - the brackets affordance icon is shown
//   - no provenance chip — call [Renderer.Provenance] to bind one
//
// idPrefix scopes any widget-id-bearing primitive emitted by Render — pass a
// stable short string (e.g. "col-type", "fact-section").
func New(idPrefix string) (inst Renderer) {
	inst = Renderer{
		idPrefix:    idPrefix,
		popupWidth:  float32(styletokens.SurfaceInspector.W),
		popupHeight: float32(styletokens.SurfaceInspector.H),
		nameMaxLen:  defaultNameMaxLen,
		showIcon:    true,
	}
	return
}

// PopupSize overrides the inspector window's first-open envelope (width,
// height) in points. The body is resizable, so this only affects the
// initial draw. Default [styletokens.SurfaceInspector].
func (inst Renderer) PopupSize(w, h float32) (out Renderer) {
	inst.popupWidth = w
	inst.popupHeight = h
	out = inst
	return
}

// NameMaxLen sets the truncation cap (in runes) for the level-1 inline
// canonical-string display. Values below 1 are clamped to the default. The
// full string is always shown in the inspector window header.
func (inst Renderer) NameMaxLen(n int) (out Renderer) {
	if n < 1 {
		n = defaultNameMaxLen
	}
	inst.nameMaxLen = n
	out = inst
	return
}

// ShowIcon toggles the brackets affordance icon on the level-1 row. Default
// true.
func (inst Renderer) ShowIcon(b bool) (out Renderer) {
	inst.showIcon = b
	out = inst
	return
}

// DefaultOpen seeds the pinned state on the first frame an instance is
// rendered: when true the inspector window starts open without a click.
// Default false (closed, like distsummary / regexsummary). The seed is
// one-shot — once the user closes the window it stays closed. Useful for
// always-expanded inspectors and for screenshot demos.
func (inst Renderer) DefaultOpen(b bool) (out Renderer) {
	inst.defaultOpen = b
	out = inst
	return
}

// Provenance binds the type to its source's [inspector.Provenance] identity
// card. When set (non-zero), the inspector window renders the standard
// [inspector.ProvenanceChip] at the top. Zero value (default) suppresses it.
func (inst Renderer) Provenance(p inspector.Provenance) (out Renderer) {
	inst.provenance = p
	out = inst
	return
}

// Render emits the level-1 inline row paired with the standard
// [inspector.AnchorToggle]. Clicking the toggle opens the inspector window
// containing the Layout / Members / Go-codec tab body; clicking it again or
// the window's title-bar X closes it. A bezier connector ties the toggle to
// the open window via [inspector.AnchorTether]. The pinned state, selected
// tab, cached parse, and code-view holder live in [instanceStates] keyed by
// the per-call scope so they survive across Render calls without a
// pointer-receiver API.
//
//   - idGen is consumed exactly once via [c.WidgetIdCreatorI.Derive] so the
//     caller's WidgetIdStack state-machine contract holds in one hop.
//     Toggle, window, and tab ids are derived from [c.MakeAbsoluteIdStr]
//     over the per-call scope instead, because they must match across frames
//     for response-by-id lookups.
//   - canonical is the type as a string. Empty renders a weak "(empty type)"
//     placeholder (and still consumes idGen) with no toggle. A non-empty
//     string is parsed once per change; a parse failure renders a red dot and
//     surfaces the error inside the window.
func (inst Renderer) Render(idGen c.WidgetIdCreatorI, canonical string) {
	callId := idGen.Derive()
	if canonical == "" {
		c.LabelAtoms(c.Atoms().BeginRichText("(empty type)").Monospace().Weak().End().Keep()).Send()
		return
	}

	scope := callScope(inst.idPrefix, callId)
	state := getInstanceState(scope)
	if !state.pinnedInit {
		state.pinnedInit = true
		state.pinned = inst.defaultOpen
	}
	if state.inSrc != canonical {
		state.inSrc = canonical
		state.ast, state.parseErr = parseType(canonical)
	}
	ok := state.parseErr == nil && state.ast != nil
	valid := ok && state.ast.IsValid()
	var fixedBytes, count int
	var anyVar bool
	if ok {
		fixedBytes, anyVar, count = footprint(state.ast)
	}

	tether := inspector.NewAnchorTether(scope)
	toggleId := c.MakeAbsoluteIdStr(scope + "-anchor-toggle")
	for range c.Horizontal().KeepIter() {
		inst.renderLevel1(canonical, ok, valid, fixedBytes, anyVar, count)
		inspector.AnchorToggle(toggleId, &state.pinned)
		tether.CaptureToggle()
	}

	if !state.pinned {
		return
	}
	inst.renderPinnedWindow(scope, tether, state, canonical, ok, valid)
	tether.Paint()
}

// callScope combines the developer-supplied idPrefix with the per-call
// disambiguator derived from idGen. Format: "idPrefix#<hex>". Stable across
// frames for the same call site, so the derived toggle / window / tab ids
// stay put while still being unique across multiple .Render(...) calls on
// the same Renderer.
func callScope(idPrefix string, callId uint64) string {
	return idPrefix + "#" + strconv.FormatUint(callId, 16)
}

// renderLevel1 emits the inline icon + truncated canonical string + validity
// dot + footprint trailer. Order: icon, string, dot, trailer — the eye lands
// on the affordance, then reads the type, then catches the status indicator.
func (inst Renderer) renderLevel1(canonical string, ok, valid bool, fixedBytes int, anyVar bool, count int) {
	transparentBg := color.Transparent
	if inst.showIcon {
		accent := color.Hex(styletokens.AccentDefault.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(accent, transparentBg, icons.PhBracketsAngle).Monospace().End().Keep()).Send()
	}
	c.LabelAtoms(c.Atoms().BeginRichText(truncate(canonical, inst.nameMaxLen)).Monospace().End().Keep()).Send()

	var dot color.Color
	if ok && valid {
		dot = color.Hex(styletokens.SuccessDefault.AsHex())
	} else {
		dot = color.Hex(styletokens.ErrorDefault.AsHex())
	}
	c.LabelAtoms(c.Atoms().BeginRichTextColored(dot, transparentBg, icons.PhDot).Monospace().End().Keep()).Send()

	if ok {
		c.LabelAtoms(c.Atoms().BeginRichText(footprintTrailer(count, fixedBytes, anyVar)).Small().Weak().End().Keep()).Send()
	}
}

// renderPinnedWindow emits the c.Window holding the inspector body. The
// native title-bar X is wired to the same pinned flag via OpenBound + R10
// databinding (distsummary / fsmview pattern) so closing through egui's
// chrome flips the toggle. The tether's CaptureWindow runs at the top of the
// body so the bezier "to" endpoint anchors on the window content rect.
func (inst Renderer) renderPinnedWindow(scope string, tether inspector.AnchorTether, state *instanceState, canonical string, ok, valid bool) {
	winId := c.MakeAbsoluteIdStr(scope + "-anchor-window")
	title := "type: " + inst.idPrefix
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
		inst.renderLevel2Body(scope, state, canonical, ok, valid)
	}
}

// renderLevel2Body lays the inspector body out top-to-bottom: provenance chip
// (when bound) → canonical-string header → parse-error / invalid banner →
// tab bar → active tab. A parse failure short-circuits to the error message;
// an invalid (but parseable) type still shows the tabs under a warning.
func (inst Renderer) renderLevel2Body(scope string, state *instanceState, canonical string, ok, valid bool) {
	transparentBg := color.Transparent
	if !inst.provenance.IsZero() {
		inspector.ProvenanceChip(inst.provenance)
		c.Separator().Horizontal().Send()
	}
	c.LabelAtoms(c.Atoms().BeginRichText(canonical).Monospace().Strong().End().Keep()).Send()

	if !ok {
		errCol := color.Hex(styletokens.ErrorDefault.AsHex())
		c.AddSpace(4)
		msg := "parse error: " + firstLine(state.parseErr.Error())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(errCol, transparentBg, msg).Small().End().Keep()).Send()
		return
	}
	if !valid {
		errCol := color.Hex(styletokens.ErrorDefault.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(errCol, transparentBg, "⚠ type is not valid").Small().End().Keep()).Send()
	}

	c.Separator().Horizontal().Send()
	inst.renderTabBar(scope, state)
	c.Separator().Horizontal().Send()
	switch state.tab {
	case tabMembers:
		renderMembersTab(state.ast)
	case tabCodec:
		renderCodecTab(scope, state)
	default:
		renderLayoutTab(scope, state.ast)
	}
}

// renderTabBar emits the three-tab selector. SelectableLabels carry an
// AbsoluteWidgetId derived from scope so multiple .Render(...) calls on the
// same Renderer drive independent tab state.
func (inst Renderer) renderTabBar(scope string, state *instanceState) {
	gap := styletokens.GapInline(styletokens.DensityFromEnv())
	layoutID := c.MakeAbsoluteIdStr(scope + "-tab-layout")
	membersID := c.MakeAbsoluteIdStr(scope + "-tab-members")
	codecID := c.MakeAbsoluteIdStr(scope + "-tab-codec")
	for range c.Horizontal().KeepIter() {
		if c.SelectableLabel(layoutID, state.tab == tabLayout, "Layout").SendResp().HasPrimaryClicked() {
			state.tab = tabLayout
		}
		c.AddSpace(gap)
		if c.SelectableLabel(membersID, state.tab == tabMembers, "Members").SendResp().HasPrimaryClicked() {
			state.tab = tabMembers
		}
		c.AddSpace(gap)
		if c.SelectableLabel(codecID, state.tab == tabCodec, "Go codec").SendResp().HasPrimaryClicked() {
			state.tab = tabCodec
		}
	}
}

// renderLayoutTab draws the byte-footprint strip: one framed segment per
// member, left-to-right, fixed-width members sized roughly in proportion to
// their byte footprint, variable-length members at a fixed width with a
// muted "var" label. The footprint is type-level (see the honest caveat
// below the strip), not a byte-exact runtime encoding for non-network types.
func renderLayoutTab(scope string, ast canonicaltypes.AstNodeI) {
	fixedBytes, anyVar, count := footprint(ast)
	c.LabelAtoms(c.Atoms().BeginRichText(footprintHeader(count, fixedBytes, anyVar)).Small().Weak().End().Keep()).Send()
	c.AddSpace(4)

	fill := color.Hex(styletokens.AccentSubtle.AsHex())
	muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	accent := color.Hex(styletokens.AccentDefault.AsHex())
	transparentBg := color.Transparent
	// A compact "slot-machine" reel: every member is a uniform fixed-width cell
	// split by a rule into a canonical register over a byte-size register, so
	// the strip reads as a rhythmic packed row rather than a ragged run. The
	// '-'/'_' boundaries sit between cells as reel dividers.
	const slotW float32 = 80
	for range c.Horizontal().KeepIter() {
		for i, it := range stripItems(ast) {
			if it.sep != "" {
				col := muted
				if it.sep == canonicaltypes.SignatureSeparator {
					col = accent
				}
				c.AddSpace(2)
				c.LabelAtoms(c.Atoms().BeginRichTextColored(col, transparentBg, it.sep).Monospace().Strong().End().Keep()).Send()
				c.AddSpace(2)
				continue
			}
			info := it.info
			segId := c.MakeAbsoluteIdStr(scope + "-seg-" + strconv.Itoa(i))
			for range c.Frame(segId).Fill(fill).InnerMargin(6).CornerRadius(4).KeepIter() {
				for range c.Vertical().KeepIter() {
					c.UiSetMinWidth(slotW)
					c.UiSetMaxWidth(slotW)
					c.LabelAtoms(c.Atoms().BeginRichText(info.canonical).Monospace().Strong().End().Keep()).Send()
					c.Separator().Horizontal().Send()
					sizeLbl := "var"
					if !info.variable && info.bytes > 0 {
						sizeLbl = strconv.Itoa(info.bytes) + " B"
					}
					c.LabelAtoms(c.Atoms().BeginRichTextColored(muted, transparentBg, sizeLbl).Small().End().Keep()).Send()
				}
			}
		}
	}
	c.AddSpace(2)
	c.Separator().Horizontal().Send()
	c.AddSpace(4)
	c.LabelAtoms(c.Atoms().BeginRichTextColored(muted, transparentBg, "footprint is type-level; non-network runtime encoding may differ").Small().End().Keep()).Send()
}

// renderMembersTab draws the decomposed member table: a header row plus one
// row per [canonicaltypes.AstNodeI.IterateMembers] entry. Columns are pinned
// to fixed widths via cellLabel so they line up without the table widget.
func renderMembersTab(ast canonicaltypes.AstNodeI) {
	widths := []float32{150, 70, 96, 56, 52, 60, 52, 120}
	headers := []string{"type", "family", "base", "width", "order", "shape", "bytes", "note"}
	for range c.Horizontal().KeepIter() {
		for ci, h := range headers {
			cellLabel(h, widths[ci], true)
		}
	}
	c.Separator().Horizontal().Send()
	for m := range ast.IterateMembers() {
		info := describeMember(m)
		for range c.Horizontal().KeepIter() {
			cellLabel(info.canonical, widths[0], false)
			cellLabel(info.family, widths[1], false)
			cellLabel(info.base, widths[2], false)
			cellLabel(widthStr(info), widths[3], false)
			cellLabel(emptyDash(info.byteOrder), widths[4], false)
			cellLabel(info.scalar, widths[5], false)
			cellLabel(bytesStr(info), widths[6], false)
			cellLabel(emptyDash(info.note), widths[7], false)
		}
	}
}

// renderCodecTab shows the type as compilable Go via [codeview]. The
// highlighted holder is rebuilt only when the generated source changes
// (cached on the instanceState), so an open-but-static inspector re-tokenises
// nothing per frame.
func renderCodecTab(scope string, state *instanceState) {
	src := generateGoSource(state.ast)
	if state.goViewSrc != src {
		state.goView = codeview.BuildGo(src)
		state.goViewSrc = src
	}
	viewId := c.MakeAbsoluteIdStr(scope + "-codec-view")
	c.CodeView(viewId, state.goView).Wrap().Send()
}

// cellLabel renders one fixed-width monospace table cell. The width is pinned
// (min == max) so columns align across rows without the table widget; header
// cells render bold.
func cellLabel(text string, w float32, strong bool) {
	for range c.Vertical().KeepIter() {
		c.UiSetMinWidth(w)
		c.UiSetMaxWidth(w)
		rt := c.Atoms().BeginRichText(text).Monospace()
		if strong {
			rt = rt.Strong()
		}
		c.LabelAtoms(rt.End().Keep()).Send()
	}
}
