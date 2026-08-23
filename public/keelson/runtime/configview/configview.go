// Package configview renders the boxer/public/config/env registry as
// a read-only inspector. Layout: a search/Only-set/Reveal-sensitive
// header above an outline of category sections, each carrying a
// Phosphor icon + set/total count and holding its variables.
//
// Each var is one row across three columns:
//
//	●  [str]  🔒 NAME  │  value      --cliFlag  │  description
//
// Status dot is accent-coloured when env.Lookup() reports set,
// muted when unset. Type chip tone differentiates string/bool/int/
// duration/path/categorial-string. Lock icon (Warning-coloured)
// prefixes sensitive vars. Value is monospace; sensitive values
// mask to "********" unless the operator toggles "Reveal sensitive".
// The description truncates to its column with the full text on
// hover — it wrapped to a second line before the widget moved to the
// native tree (ADR-0176 M3), whose rows are one line high.
//
// Read-only by design (v1). Re-reads env.All() and env.LookupVar()
// each frame (~40 entries; cheap).
package configview

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/config/env"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// maskedSensitive replaces a sensitive variable's value/default when
// reveal is off. Fixed width so the rendered length doesn't telegraph
// the secret length.
const maskedSensitive = "********"

// unsetMarker is what the value column shows when Lookup() reports
// set==false. Brackets keep it distinct from a literal value of
// "unset" (which would render without brackets).
const unsetMarker = "<unset>"

// valueDisplayMax bounds the rendered value before truncation. Long
// URLs / paths otherwise push the row past the ScrollArea and force
// horizontal scrolling; 96 keeps the row inside the 720-pt window.
const valueDisplayMax = 96

// Column widths. The name column fits the longest BOXER_* / IMZERO2_*
// identifier plus its dot, chip and lock; the value column fits a
// typical host:port or path. Both are resizable, so these are starting
// points rather than rules. The description takes whatever is left of
// the measured panel width, floored at descColMinWidth so it does not
// vanish in a narrow window — below that the table scrolls sideways
// instead.
const (
	nameColWidth        = 340.0
	valueColWidth       = 260.0
	descColMinWidth     = 220.0
	varsScrollbarGutter = 18.0
)

// varsPaneProbeSalt namespaces the panel-size probe's r21 register slot
// (ADR-0009 §probe seq), derived under the instance's id stack so two
// inspectors in one frame measure their own panels.
const varsPaneProbeSalt uint64 = 0xc0f1_6117_0176_0001

// idBadgeBase namespaces the per-row type chips' widget ids. The high
// half is the ADR number, which makes a stray id recognisable in a
// checkId warning.
const idBadgeBase uint64 = 0x0176_0a00_0000_0000

// Token-derived colors cached at package init. Re-resolving each
// frame would burn 40+ Color.Hex parses every frame for no benefit;
// these tokens never change at runtime.
var (
	fgPrimary     = color.Hex(styletokens.NeutralTextPrimary.AsHex())
	fgMuted       = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	fgAccent      = color.Hex(styletokens.AccentDefault.AsHex())
	fgWarning     = color.Hex(styletokens.WarningDefault.AsHex())
	bgTransparent = color.Transparent
)

// Filter holds the operator's narrowing choices. Re-applied each
// frame against env.All().
type Filter struct {
	Query           string
	OnlySet         bool
	RevealSensitive bool
}

// App is the per-window configview instance.
type App struct {
	manifest app.Manifest
	ids      *c.WidgetIdStack
	density  styletokens.DensityE

	filter Filter

	// expandedCat seeds the open/closed state of category headers.
	// Empty (the interactive default) means every category starts
	// collapsed; matching a CategoryE pre-expands that one section.
	// The screenshot tour mutates this to capture a stable
	// "category-expanded" scene without depending on persisted
	// widget memory.
	expandedCat env.CategoryE

	// selected is the variable name of the highlighted row. Name-keyed
	// rather than indexed because the filter rebuilds the hierarchy on
	// every keystroke — see [App.syncTree].
	selected string
	// navState is the tree widget's view state, and the authority for
	// which categories are open: navKeys gives the widget a stable
	// identity per node, so a section's open state outlives the rebuild
	// that every filter keystroke triggers. Only its selection is
	// projected, from the field above.
	//
	// navLabels / navParents / navKeys / navNodes are [App.buildTree]'s
	// retained scratch; navKeys holds a category name for a section row
	// and a variable name for a var row.
	navState   tree.State
	navLabels  []string
	navParents []int32
	navKeys    []string
	navNodes   []varNode
}

var _ app.AppI = (*App)(nil)

func newInstance(m app.Manifest) (out *App) {
	out = &App{
		manifest: m,
		ids:      c.NewWidgetIdStack(),
		density:  styletokens.ActiveDensity(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest)                { m = inst.manifest; return }
func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	return inst.render()
}

func (inst *App) render() (err error) {
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).
		DefaultSize(54).
		Resizable(false).
		KeepIter() {
		inst.renderFilterRow()
	}
	for range c.PanelCentralInside().KeepIter() {
		specs := applyFilter(env.All(), inst.filter)
		if len(specs) == 0 {
			inst.renderEmptyState()
			return
		}
		// There is no ScrollArea here any more: the tree renders through
		// an etable, which brings its own scroll and culls the rows
		// outside it. Wrapping it in one would give the panel two
		// scrollbars and hand the table an unbounded parent, which is
		// the case its 400px auto-fit cap exists for.
		availW, availH, _ := c.CapturePaneSize(inst.ids.PrepareHighEntropy(varsPaneProbeSalt).Derive())
		inst.renderVarTree(groupByCategory(specs), availW, availH)
	}
	return
}

// renderVarTree draws the category / variable outline.
//
// availW / availH are the panel's measured size. The height keeps the table
// from falling back to its 400px auto-fit; the width goes to the description
// column, which takes whatever the name and value columns leave. The probe
// answers one frame late and not at all on the first, where both fall back to
// the constants.
func (inst *App) renderVarTree(buckets []bucket, availW, availH float32) {
	inst.buildTree(buckets)
	inst.syncTree()
	descW := float32(descColMinWidth)
	if w := availW - nameColWidth - valueColWidth - varsScrollbarGutter; w > descW {
		descW = w
	}
	res := tree.Render(tree.Input{
		Ids:      inst.ids,
		ScopeKey: "vars",
		Tree:     inst.navTree(),
		State:    &inst.navState,
		Outline: tree.Column{
			Header:    "variable",
			Width:     nameColWidth,
			Resizable: true,
			Cell:      inst.renderNameCell,
		},
		Columns: []tree.Column{
			{Header: "value", Width: valueColWidth, Resizable: true, Cell: inst.renderValueCell},
			{Header: "description", Width: descW, Cell: inst.renderDescCell},
		},
		MaxHeight: availH,
	})
	inst.applyTree(res)
}

// renderEmptyState surfaces a muted line when the filter narrows
// the registry to zero matches — distinguishes "the filter is too
// tight" from "the body just hasn't rendered yet".
func (inst *App) renderEmptyState() {
	c.AddSpace(styletokens.PaddingOuter(inst.density))
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(fgMuted, bgTransparent, "(no vars match the current filter)").Small().End().
		Keep()).Send()
}

func (inst *App) renderFilterRow() {
	c.AddSpace(styletokens.PaddingHair(inst.density))
	for range c.Horizontal().KeepIter() {
		c.Label("Search:").Send()
		c.AddSpace(styletokens.GapInline(inst.density))
		c.TextEdit(inst.ids.PrepareStr("q"), inst.filter.Query, false).
			HintText("name or description…").
			DesiredWidth(280).
			SendRespVal(&inst.filter.Query)

		c.AddSpace(styletokens.GapPanels(inst.density))
		c.Checkbox(inst.ids.PrepareStr("only-set"), inst.filter.OnlySet, "Only set").
			SendRespVal(&inst.filter.OnlySet)
		c.AddSpace(styletokens.GapInline(inst.density))
		c.Checkbox(inst.ids.PrepareStr("reveal"), inst.filter.RevealSensitive, "Reveal sensitive").
			SendRespVal(&inst.filter.RevealSensitive)
	}
	c.AddSpace(styletokens.PaddingHair(inst.density))
}

// categoryLabel is a section row's text: the category's Phosphor icon, its
// name, and how many of its variables are set. The count is what makes a
// collapsed section still worth reading.
func categoryLabel(cat env.CategoryE, setCount, total int) string {
	return fmt.Sprintf("%s  %s  (%d / %d set)", categoryIcon(cat), cat, setCount, total)
}

// renderNameCell draws the outline column: the quick-scan signals that used to
// open the row's first line — set dot, type chip, sensitivity lock — and then
// the variable name. A category row draws its own label, since the tree hands
// the whole outline column to this one function.
//
// Every label is emitted Selectable(false): a selectable label senses
// click-and-drag and is registered after the row's own sense region, so it
// would sit over it and swallow clicks on its own rect (ADR-0176 SD7). The
// type chip is a real button and legitimately takes the pointer over its
// rect — that is the price of its tooltip, and it is 30px of a row.
func (inst *App) renderNameCell(r tree.Row) {
	node := r.Node
	n := &inst.navNodes[node]
	if !n.isVar {
		c.Label(inst.navLabels[node]).Selectable(false).Truncate().Send()
		return
	}
	s := n.spec
	_, set := lookupValue(s)

	inst.renderStatusDot(set)
	c.AddSpace(styletokens.GapInline(inst.density))

	// Type chip — tone differentiates the typed-handle family at a glance
	// (Success for bool, Info for numeric/categorial, Neutral for
	// string/path).
	badge.New(inst.ids.PrepareSeq(idBadgeBase+uint64(node)), typeShortLabel(s.Type)).
		Tone(typeTone(s.Type)).
		Variant(badge.VariantSoft).
		Size(badge.SizeSm).
		Monospace().
		Tooltip(string(s.Type)).
		Send()
	c.AddSpace(styletokens.GapInline(inst.density))

	// Lock icon for sensitive vars — Warning-coloured glyph, rendered via the
	// Phosphor font (ADR-0044). Drawing attention here so a screenshot /
	// screenshare reviewer knows to be careful with what's masked.
	if s.Sensitive {
		c.LabelAtoms(c.Atoms().
			BeginRichTextColored(fgWarning, bgTransparent, icons.PhLock).End().
			Keep()).Selectable(false).Send()
		c.AddSpace(styletokens.GapInline(inst.density))
	}

	c.LabelAtoms(c.Atoms().
		BeginRichText(s.Name).Strong().Monospace().End().
		Keep()).Selectable(false).Truncate().Send()
}

// renderValueCell draws the value and, after it, the CLI flag that also sets
// this variable. Colour cues: muted for <unset>, primary for set. The tooltip
// carries the default and the declared origin, which the dense row leaves out
// for layout calm.
func (inst *App) renderValueCell(r tree.Row) {
	node := r.Node
	n := &inst.navNodes[node]
	if !n.isVar {
		return
	}
	s := n.spec
	raw, set := lookupValue(s)
	valueColor := fgPrimary
	if !set {
		valueColor = fgMuted
	}
	atoms := c.Atoms().
		BeginRichTextColored(valueColor, bgTransparent,
			truncate(maskValue(s, raw, set, inst.filter.RevealSensitive), valueDisplayMax)).
		Monospace().End().
		Keep()
	if tip := valueTooltip(s, inst.filter.RevealSensitive); tip != "" {
		for range c.HoverText(tip).KeepIter() {
			c.LabelAtoms(atoms).Selectable(false).Truncate().Send()
		}
	} else {
		c.LabelAtoms(atoms).Selectable(false).Truncate().Send()
	}
	if s.CliFlagName == "" {
		return
	}
	c.AddSpace(styletokens.GapItems(inst.density))
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(fgMuted, bgTransparent, s.CliFlagName).Small().Monospace().End().
		Keep()).Selectable(false).Truncate().Send()
}

// renderDescCell draws the description, which before the port was a second
// wrapped line under the name. A tree row is one line high, so it truncates
// and carries the full text as a tooltip — the trade the port makes for
// descriptions that line up down the pane.
func (inst *App) renderDescCell(r tree.Row) {
	node := r.Node
	n := &inst.navNodes[node]
	if !n.isVar || n.spec.Description == "" {
		return
	}
	atoms := c.Atoms().
		BeginRichTextColored(fgMuted, bgTransparent, n.spec.Description).Small().End().
		Keep()
	for range c.HoverText(n.spec.Description).KeepIter() {
		c.LabelAtoms(atoms).Selectable(false).Truncate().Send()
	}
}

// renderStatusDot draws ●/○ via the Phosphor font with the
// set/unset semantic colour. PhDot (filled) for set so the eye
// catches active rows when scanning a long category; PhCircle
// (outline) for unset so they read as available-but-empty.
func (inst *App) renderStatusDot(set bool) {
	glyph := icons.PhCircle
	col := fgMuted
	if set {
		glyph = icons.PhDot
		col = fgAccent
	}
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(col, bgTransparent, glyph).End().
		Keep()).Send()
}

// bucket pairs a Category with its specs in the order applyFilter
// produced. Exposed for tests; the App path consumes it only
// internally.
type bucket struct {
	cat   env.CategoryE
	specs []env.Spec
}

// groupByCategory buckets adjacent same-Category specs from a
// pre-sorted slice. applyFilter sorts (Category, Name), so a single
// pass suffices.
func groupByCategory(specs []env.Spec) (out []bucket) {
	for _, s := range specs {
		if n := len(out); n > 0 && out[n-1].cat == s.Category {
			out[n-1].specs = append(out[n-1].specs, s)
			continue
		}
		out = append(out, bucket{cat: s.Category, specs: []env.Spec{s}})
	}
	return
}

// applyFilter narrows specs by needle + OnlySet and sorts by
// (Category, Name). Pure function — tests exercise it without
// touching env state.
func applyFilter(specs []env.Spec, f Filter) (out []env.Spec) {
	needle := strings.ToLower(strings.TrimSpace(f.Query))
	out = make([]env.Spec, 0, len(specs))
	for _, s := range specs {
		if needle != "" {
			if !strings.Contains(strings.ToLower(s.Name), needle) &&
				!strings.Contains(strings.ToLower(s.Description), needle) {
				continue
			}
		}
		if f.OnlySet && !isSet(s) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return
}

// lookupValue resolves the live env value via the registered VarI.
// Returns ("", false) when no var was registered with this spec's
// Name (shouldn't happen for specs coming from env.All(), but the
// fallback keeps the renderer robust to test fixtures).
func lookupValue(s env.Spec) (raw string, set bool) {
	v, ok := env.LookupVar(s.Name)
	if !ok {
		return
	}
	raw, set = v.Lookup()
	return
}

// isSet is the OnlySet-filter predicate; small wrapper around
// lookupValue for clarity at the call sites.
func isSet(s env.Spec) (out bool) {
	_, out = lookupValue(s)
	return
}

// maskValue produces the value column. Unset wins over sensitive —
// revealing "<unset>" carries no secret, so an unset sensitive var
// shows <unset> regardless of the reveal toggle.
func maskValue(s env.Spec, raw string, set bool, reveal bool) (out string) {
	if !set {
		out = unsetMarker
		return
	}
	if s.Sensitive && !reveal {
		out = maskedSensitive
		return
	}
	out = raw
	return
}

// maskDefault produces the default-column string for the value
// tooltip. Empty default stays empty even for a sensitive var —
// "no default" is metadata, not a secret.
func maskDefault(s env.Spec, reveal bool) (out string) {
	if s.Sensitive && !reveal && s.Default != "" {
		out = maskedSensitive
		return
	}
	out = s.Default
	return
}

// valueTooltip composes the hover string shown over the value
// column. Returns "" when neither default nor origin contribute
// usable info — the caller then skips the HoverText wrap and
// avoids paying for an empty deferred-block scope.
func valueTooltip(s env.Spec, reveal bool) (out string) {
	parts := make([]string, 0, 2)
	def := maskDefault(s, reveal)
	if def != "" {
		parts = append(parts, "default: "+def)
	}
	if s.Origin.Package != "" {
		parts = append(parts, "declared in: "+s.Origin.Package)
	}
	out = strings.Join(parts, "\n")
	return
}

// truncate caps a value display at max runes, appending an
// ellipsis-plus-rune-count suffix so the operator knows how much
// was elided. 0 disables truncation. Walks the string by runes so
// the slice never lands mid-codepoint — env values are usually
// ASCII, but a stray multibyte char in a description-style override
// would corrupt the display under a byte slice.
func truncate(s string, max int) (out string) {
	if max <= 0 {
		out = s
		return
	}
	runeCount := utf8.RuneCountInString(s)
	if runeCount <= max {
		out = s
		return
	}
	i := 0
	for byteIdx := range s {
		if i == max {
			out = fmt.Sprintf("%s… (%d chars)", s[:byteIdx], runeCount)
			return
		}
		i++
	}
	out = s
	return
}

// typeShortLabel collapses env.TypeE into the badge's pill text;
// keeps the chip narrow so the name column starts at a predictable
// offset across rows.
func typeShortLabel(t env.TypeE) (out string) {
	switch t {
	case env.TypeString:
		out = "str"
	case env.TypeBool:
		out = "bool"
	case env.TypeInt64:
		out = "int"
	case env.TypeDuration:
		out = "dur"
	case env.TypePath:
		out = "path"
	case env.TypeCategorialString:
		out = "enum"
	default:
		out = string(t)
	}
	return
}

// typeTone maps env.TypeE → badge tone. Mapping is intentionally
// arbitrary-but-stable: any colour family that distinguishes types
// at a glance works; what matters is consistency across rows.
func typeTone(t env.TypeE) (out badge.ToneE) {
	switch t {
	case env.TypeBool:
		out = badge.ToneSuccess
	case env.TypeInt64, env.TypeDuration:
		out = badge.ToneInfo
	case env.TypeCategorialString:
		out = badge.ToneWarning
	default:
		out = badge.ToneNeutral
	}
	return
}

// categoryIcon maps the boxer-declared and pebble2impl-declared
// categories to Phosphor glyphs. Unknown categories fall back to
// PhCircle so the layout column width stays consistent.
func categoryIcon(cat env.CategoryE) (glyph string) {
	switch cat {
	case env.CategoryObservability:
		glyph = icons.PhWaveform
	case env.CategoryDev:
		glyph = icons.PhCode
	case env.CategoryDocgen:
		glyph = icons.PhFileText
	case env.CategoryLLM:
		glyph = icons.PhBrain
	case env.CategoryDatabase:
		glyph = icons.PhDatabase
	case env.CategorySystem:
		glyph = icons.PhDesktop
	case env.CategoryTestIntegration:
		glyph = icons.PhTestTube
	default:
		// pebble2impl-local categories declared outside boxer.
		switch string(cat) {
		case "anchor":
			glyph = icons.PhAnchor
		case "krypto":
			glyph = icons.PhKey
		case "runinfo":
			glyph = icons.PhTag
		default:
			glyph = icons.PhCircle
		}
	}
	return
}
