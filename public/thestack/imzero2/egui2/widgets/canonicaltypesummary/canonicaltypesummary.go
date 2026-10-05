// Package canonicaltypesummary is an immediate-mode widget (ADR-0267): a
// tethered value-inspector for a single leeway canonical type
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
// The host owns everything that survives a frame — the pinned open/closed
// flag, the selected tab, the cached parse and the retained code-view
// holder — in a [State] it passes on every [Render]. Ids are the host's
// [c.WidgetIdStack] under a ScopeKey, so two summaries in one host differ by
// ScopeKey alone and a row of them is a per-row IdScope away.
package canonicaltypesummary

import (
	"errors"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// tabE selects which body the inspector window renders.
type tabE uint8

const (
	// tabLayout is the zero value so a fresh State lands on the
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

// defaultScopeKey names the widget when Input.ScopeKey is empty.
const defaultScopeKey = "canonicaltypesummary"

// ErrNeedsIdsAndState is Result.Err when Input.Ids or Input.State is nil:
// the widget has nowhere to derive its ids or record whether its
// inspector is open. Drawn in place of the row.
var ErrNeedsIdsAndState = errors.New("canonicaltypesummary: Input.Ids and Input.State are required")

// State is the host-owned part of one summary that survives a frame: the
// inspector's open flag, the selected tab, the cached parse result (so an
// unchanged string is not re-parsed every frame), and the retained Go
// code-view holder (rebuilt only when the generated source changes). The
// zero value is a closed inspector on the Layout tab. Hold one per summary —
// two summaries sharing a State share an inspector.
type State struct {
	// Pinned is whether the inspector window is open. Exported so a host
	// can persist it or open the inspector from code; the window's
	// title-bar X writes it back through an R10 databinding.
	Pinned bool
	// pinnedInit guards the one-time seed of Pinned from Input.DefaultOpen
	// at first render, so a caller-requested start-open does not re-assert
	// itself after the user later closes the window.
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

// Input is one frame's declaration of the summary.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two summaries in one host need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this summary within the host's id space; empty uses
	// "canonicaltypesummary". It also names the inspector window unless
	// Title is set.
	ScopeKey string
	// Canonical is the type as a string. Empty renders a weak "(empty
	// type)" placeholder with no toggle. A non-empty string is parsed once
	// per change; a parse failure renders a red dot and surfaces the error
	// inside the window.
	Canonical string
	// State is the host-owned open flag, tab and caches. Required.
	State *State

	// Provenance, when non-zero, renders the standard
	// [inspector.ProvenanceChip] at the top of the inspector window so
	// operators can see which subject / source produced the type this
	// widget is summarising.
	Provenance inspector.Provenance
	// Title is the inspector window's title; empty reads "type: " plus
	// the ScopeKey.
	Title string
	// PopupWidth and PopupHeight set the inspector window's first-open
	// envelope in points; zero takes [styletokens.SurfaceInspector], the
	// compact accessory archetype created for the tethered-inspector role
	// (ADR-0065). The body is resizable, so this only affects the initial
	// draw.
	PopupWidth  float32
	PopupHeight float32
	// NameMaxLen caps the level-1 inline canonical-string display in
	// runes; zero (or a negative value) takes 48. The full string is always
	// shown in the inspector window header.
	NameMaxLen int
	// HideIcon drops the brackets affordance icon from the level-1 row.
	HideIcon bool
	// DefaultOpen seeds State.Pinned on the first frame a State is
	// rendered: when true the inspector window starts open without a
	// click. The seed is one-shot — once the user closes the window it
	// stays closed. Useful for always-expanded inspectors and for
	// screenshot demos.
	DefaultOpen bool
}

// Result is what one Render reports.
type Result struct {
	// Toggled is true on the frame the anchor toggle was clicked; the new
	// open state is in State.Pinned.
	Toggled bool
	// Valid is whether Canonical parsed and passes IsValid this frame;
	// false for an empty string.
	Valid bool
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
// [inspector.AnchorToggle]. Clicking the toggle opens the inspector window
// containing the Layout / Members / Go-codec tab body; clicking it again or
// the window's title-bar X closes it. A bezier connector ties the toggle to
// the open window via [inspector.AnchorTether]. Call once per frame inside
// the host's layout; every widget id is derived under one IdScope keyed by
// ScopeKey.
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
	if in.Canonical == "" {
		c.LabelAtoms(c.Atoms().BeginRichText("(empty type)").Monospace().Weak().End().Keep()).Send()
		return
	}
	if !st.pinnedInit {
		st.pinnedInit = true
		st.Pinned = in.DefaultOpen
	}
	if st.inSrc != in.Canonical {
		st.inSrc = in.Canonical
		st.ast, st.parseErr = parseType(in.Canonical)
	}
	ok := st.parseErr == nil && st.ast != nil
	valid := ok && st.ast.IsValid()
	res.Valid = valid
	var fixedBytes, count int
	var anyVar bool
	if ok {
		fixedBytes, anyVar, count = footprint(st.ast)
	}

	// The tether infrastructure keys its rect captures by a string; it is
	// derived from this scope's id so two summaries never share a slot.
	tether := inspector.NewAnchorTether(ids.PrepareStr("tether").Derive())
	for range c.Horizontal().KeepIter() {
		in.renderLevel1(ok, valid, fixedBytes, anyVar, count)
		res.Toggled = inspector.AnchorToggle(ids.PrepareStr("anchor-toggle"), &st.Pinned)
		tether.CaptureToggle()
	}

	if !st.Pinned {
		return
	}
	in.renderPinnedWindow(tether, ok, valid)
	tether.Paint()
	return
}

// renderLevel1 emits the inline icon + truncated canonical string + validity
// dot + footprint trailer. Order: icon, string, dot, trailer — the eye lands
// on the affordance, then reads the type, then catches the status indicator.
func (in Input) renderLevel1(ok, valid bool, fixedBytes int, anyVar bool, count int) {
	transparentBg := color.Transparent
	if !in.HideIcon {
		accent := color.Hex(styletokens.AccentDefault.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(accent, transparentBg, icons.PhBracketsAngle).Monospace().End().Keep()).Send()
	}
	c.LabelAtoms(c.Atoms().BeginRichText(truncate(in.Canonical, in.NameMaxLen)).Monospace().End().Keep()).Send()

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
//
// The window is the one floating surface this widget owns, so it takes the
// one absolute id the contract allows (ADR-0267 W6), derived from this
// summary's scope.
func (in Input) renderPinnedWindow(tether inspector.AnchorTether, ok, valid bool) {
	st := in.State
	winId := c.MakeAbsoluteIdHighEntropy(in.Ids.PrepareStr("window").Derive())
	title := in.Title
	if title == "" {
		title = "type: " + in.scopeKey()
	}
	w, h := in.PopupWidth, in.PopupHeight
	if w <= 0 {
		w = float32(styletokens.SurfaceInspector.W)
	}
	if h <= 0 {
		h = float32(styletokens.SurfaceInspector.H)
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
		in.renderLevel2Body(ok, valid)
	}
}

// renderLevel2Body lays the inspector body out top-to-bottom: provenance chip
// (when bound) → canonical-string header → parse-error / invalid banner →
// tab bar → active tab. A parse failure short-circuits to the error message;
// an invalid (but parseable) type still shows the tabs under a warning.
func (in Input) renderLevel2Body(ok, valid bool) {
	ids, st := in.Ids, in.State
	transparentBg := color.Transparent
	if !in.Provenance.IsZero() {
		inspector.ProvenanceChip(in.Provenance)
		c.Separator().Horizontal().Send()
	}
	c.LabelAtoms(c.Atoms().BeginRichText(in.Canonical).Monospace().Strong().End().Keep()).Send()

	if !ok {
		errCol := color.Hex(styletokens.ErrorDefault.AsHex())
		c.AddSpace(styletokens.PaddingInner(styletokens.ActiveDensity()))
		msg := "parse error: " + firstLine(st.parseErr.Error())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(errCol, transparentBg, msg).Small().End().Keep()).Send()
		return
	}
	if !valid {
		errCol := color.Hex(styletokens.ErrorDefault.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(errCol, transparentBg, "⚠ type is not valid").Small().End().Keep()).Send()
	}

	c.Separator().Horizontal().Send()
	renderTabBar(ids, st)
	c.Separator().Horizontal().Send()
	switch st.tab {
	case tabMembers:
		renderMembersTab(st.ast)
	case tabCodec:
		renderCodecTab(ids, st)
	default:
		renderLayoutTab(ids, st.ast)
	}
}

// renderTabBar emits the three-tab selector, scoped under the summary so
// two summaries in one host drive independent tab state.
func renderTabBar(ids *c.WidgetIdStack, st *State) {
	selector.Segmented(ids, "tab", &st.tab).
		Style(selector.StyleSelectable).
		Gap(styletokens.GapInline(styletokens.ActiveDensity())).
		Option(tabLayout, "Layout").
		Option(tabMembers, "Members").
		Option(tabCodec, "Go codec").
		SendResp()
}

// renderLayoutTab draws the byte-footprint strip: one framed segment per
// member, left-to-right, fixed-width members sized roughly in proportion to
// their byte footprint, variable-length members at a fixed width with a
// muted "var" label. The footprint is type-level (see the honest caveat
// below the strip), not a byte-exact runtime encoding for non-network types.
func renderLayoutTab(ids *c.WidgetIdStack, ast canonicaltypes.AstNodeI) {
	density := styletokens.ActiveDensity()
	fixedBytes, anyVar, count := footprint(ast)
	c.LabelAtoms(c.Atoms().BeginRichText(footprintHeader(count, fixedBytes, anyVar)).Small().Weak().End().Keep()).Send()
	c.AddSpace(styletokens.PaddingInner(density))

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
		for range c.IdScope(ids.PrepareStr("layout")) {
			for i, it := range stripItems(ast) {
				if it.sep != "" {
					col := muted
					if it.sep == canonicaltypes.SignatureSeparator {
						col = accent
					}
					c.AddSpace(styletokens.PaddingHair(density))
					c.LabelAtoms(c.Atoms().BeginRichTextColored(col, transparentBg, it.sep).Monospace().Strong().End().Keep()).Send()
					c.AddSpace(styletokens.PaddingHair(density))
					continue
				}
				info := it.info
				for range c.Frame(ids.PrepareSeq(uint64(i))).Fill(fill).InnerMargin(styletokens.PaddingTight(density)).CornerRadius(styletokens.RoundingMd).KeepIter() {
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
	}
	c.AddSpace(styletokens.PaddingHair(density))
	c.Separator().Horizontal().Send()
	c.AddSpace(styletokens.PaddingInner(density))
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
// (cached on the State), so an open-but-static inspector re-tokenises
// nothing per frame.
func renderCodecTab(ids *c.WidgetIdStack, st *State) {
	src := generateGoSource(st.ast)
	if st.goViewSrc != src {
		st.goView = codeview.BuildGo(src)
		st.goViewSrc = src
	}
	c.CodeView(ids.PrepareStr("codec-view"), st.goView).Wrap().Send()
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
