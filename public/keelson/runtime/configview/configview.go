//go:build llm_generated_opus47

// Package configview renders the boxer/public/config/env registry as
// a read-only inspector. Layout: a search/Only-set/Reveal-sensitive
// header above a ScrollArea of category sections; each section is a
// CollapsingHeader carrying a Phosphor icon + set/total count, with
// per-var rows below.
//
// Each var row is two lines:
//
//   ●  [str]  🔒 NAME    value             --cliFlag
//             description, small + muted, wraps to row width
//
// Status dot is accent-coloured when env.Lookup() reports set,
// muted when unset. Type chip tone differentiates string/bool/int/
// duration/path/categorial-string. Lock icon (Warning-coloured)
// prefixes sensitive vars. Value is monospace; sensitive values
// mask to "********" unless the operator toggles "Reveal sensitive".
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

// descIndent budgets the left padding before the description line so
// it lines up under the variable name rather than the status dot.
const descIndent = 28.0

// Token-derived colors cached at package init. Re-resolving each
// frame would burn 40+ Color.Hex parses every frame for no benefit;
// these tokens never change at runtime.
var (
	fgPrimary    = color.Hex(styletokens.NeutralTextPrimary.AsHex())
	fgMuted      = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	fgAccent     = color.Hex(styletokens.AccentDefault.AsHex())
	fgWarning    = color.Hex(styletokens.WarningDefault.AsHex())
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
	// CollapsingHeader memory.
	expandedCat env.CategoryE
}

var _ app.AppI = (*App)(nil)

func newInstance(m app.Manifest) (out *App) {
	out = &App{
		manifest: m,
		ids:      c.NewWidgetIdStack(),
		density:  styletokens.DensityFromEnv(),
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
		// AutoShrink(false, false): collapse/expand of a category
		// CollapsingHeader mustn't ripple the wrapping Window.
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			specs := applyFilter(env.All(), inst.filter)
			if len(specs) == 0 {
				inst.renderEmptyState()
				return
			}
			buckets := groupByCategory(specs)
			for ci, b := range buckets {
				inst.renderCategory(ci, b)
			}
		}
	}
	return
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

// renderCategory draws one section: header (icon + name + N/total
// counts) plus a per-var row below the header when expanded.
func (inst *App) renderCategory(idx int, b bucket) {
	setCount := 0
	for _, s := range b.specs {
		if isSet(s) {
			setCount++
		}
	}
	icon := categoryIcon(b.cat)
	title := fmt.Sprintf("%s  %s  (%d / %d set)", icon, b.cat, setCount, len(b.specs))
	hdrId := inst.ids.PrepareStr(fmt.Sprintf("cat-%d", idx))
	defaultOpen := inst.expandedCat != "" && b.cat == inst.expandedCat
	for range c.CollapsingHeader(hdrId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(defaultOpen).
		KeepIter() {
		for vi, s := range b.specs {
			inst.renderVarRow(idx, vi, s)
		}
	}
}

// renderVarRow draws the two-line var entry. Line 1 packs the
// quick-scan signals (set/type/sensitive/name/value/flag); line 2
// is the description (muted, wraps).
func (inst *App) renderVarRow(catIdx, vIdx int, s env.Spec) {
	raw, set := lookupValue(s)
	valueText := truncate(maskValue(s, raw, set, inst.filter.RevealSensitive), valueDisplayMax)

	for range c.Horizontal().KeepIter() {
		// Status dot — accent for set, muted for unset.
		inst.renderStatusDot(set)
		c.AddSpace(styletokens.GapInline(inst.density))

		// Type chip — tone differentiates the typed-handle family
		// at a glance (Success for bool, Info for numeric/categorial,
		// Neutral for string/path).
		badge.New(inst.ids.PrepareStr(fmt.Sprintf("t-%d-%d", catIdx, vIdx)),
			typeShortLabel(s.Type)).
			Tone(typeTone(s.Type)).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Monospace().
			Tooltip(string(s.Type)).
			Send()
		c.AddSpace(styletokens.GapInline(inst.density))

		// Lock icon for sensitive vars — Warning-coloured glyph,
		// rendered via the Phosphor font (ADR-0044). Drawing
		// attention here so a screenshot/screenshare reviewer knows
		// to be careful with what's masked.
		if s.Sensitive {
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(fgWarning, bgTransparent, icons.PhLock).End().
				Keep()).Send()
			c.AddSpace(styletokens.GapInline(inst.density))
		}

		// Name — Strong + monospace so it lines up with the value.
		c.LabelAtoms(c.Atoms().
			BeginRichText(s.Name).Strong().Monospace().End().
			Keep()).Send()
		c.AddSpace(styletokens.GapItems(inst.density))

		// Value — colour cues: muted for <unset>, primary for set.
		// Wrapped in a HoverText scope when there's a default or a
		// declared origin to surface; the dense row deliberately
		// omits both for layout calm, so the tooltip is where the
		// operator picks them up.
		valueColor := fgPrimary
		if !set {
			valueColor = fgMuted
		}
		valueAtomsKept := c.Atoms().
			BeginRichTextColored(valueColor, bgTransparent, valueText).Monospace().End().
			Keep()
		tip := valueTooltip(s, inst.filter.RevealSensitive)
		if tip != "" {
			for range c.HoverText(tip).KeepIter() {
				c.LabelAtoms(valueAtomsKept).Send()
			}
		} else {
			c.LabelAtoms(valueAtomsKept).Send()
		}

		// CLI flag chip — only when the spec declares one; small +
		// muted so it doesn't compete with the value.
		if s.CliFlagName != "" {
			c.AddSpace(styletokens.GapItems(inst.density))
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(fgMuted, bgTransparent, s.CliFlagName).Small().Monospace().End().
				Keep()).Send()
		}
	}

	if s.Description != "" {
		for range c.Horizontal().KeepIter() {
			c.AddSpace(descIndent)
			c.LabelAtoms(c.Atoms().
				BeginRichTextColored(fgMuted, bgTransparent, s.Description).Small().End().
				Keep()).Wrap().Send()
		}
	}
	c.AddSpace(styletokens.PaddingHair(inst.density))
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
