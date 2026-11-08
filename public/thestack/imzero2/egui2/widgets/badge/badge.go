// Package badge implements a compact labelled tag rendered as a Frame
// (rounded fill + optional stroke + padding) wrapping a styled LabelAtoms.
// The composition rides on existing FFFI2 primitives (Frame, Atoms,
// LabelAtoms) so no IDL or Rust changes are required.
//
// The widget covers four typical roles:
//
//   - Status indicator        (Tone + Send)
//   - Notification count      (Tone + numeric label + Send)
//   - Filter chip             (Tone + Selected + SendResp → toggle)
//   - Dismissible tag         (compose a badge in a Horizontal with a
//     small close-badge using VariantGhost, see the demo)
//
// Two terminals:
//
//   - Send()                                — fire-and-forget, no interaction.
//     Draws VariantSolid and VariantOutline as VariantSoft and ignores
//     Selected: those looks say "this can be clicked" (ADR-0273 SD5).
//   - SendResp() ResponseFlagsE             — emits with SenseClick; the
//     returned flags expose HasPrimaryClicked / HasHovered / etc., usable
//     immediately under the standard one-frame-lag contract. The badge
//     changes on hover and shows the pointer cursor, as a control does.
package badge

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ToneE aliases styletokens.Tone — the IDS semantic-role enumerator (info /
// success / warning / error / neutral / accent) promoted out of this widget
// into the design system (ADR-0031 §Updates 2026-06-06), where tone→token is
// colour policy shared with other painters (the gauge widget, ADR-0068). The
// alias plus the re-exported constants below keep badge's public surface and
// every existing call site (badge.ToneError, …) unchanged, while a badge
// picks up the same hues fleet-wide as plot annotations and status
// indicators. TonePrimary is the accent role (ADR-0031 forbids "primary" as a
// token name; the role enumerator keeps the name for source-compat).
type ToneE = styletokens.Tone //boxer:lint disable=CS008 reason="deliberate re-export alias of the promoted styletokens.Tone (ADR-0031 §Updates); keeps badge.ToneE and the badge.Tone* constants source-compatible across existing call sites — the textbook re-export use of a type alias"

const (
	ToneNeutral = styletokens.ToneNeutral
	TonePrimary = styletokens.TonePrimary
	ToneSuccess = styletokens.ToneSuccess
	ToneWarning = styletokens.ToneWarning
	ToneError   = styletokens.ToneError
	ToneInfo    = styletokens.ToneInfo
)

// VariantE chooses fill / stroke / fg combinations for a given tone.
//
//   - Solid    — fully filled with the tone colour, contrasting fg text.
//     SendResp only: the same colours as a Primary or Danger button.
//   - Soft     — translucent tone fill, tone-tinted fg. Default. Reads as
//     a category tag without dominating the surrounding layout.
//   - Outline  — transparent fill, tone stroke, tone fg. SendResp only: the
//     same colours as a Tertiary button. Good for filter chips, where
//     Selected swaps to Solid.
//   - Ghost    — transparent fill, no stroke, tone fg. Useful for inline
//     "close ×" affordances on a chip and other minimal markers.
//
// Send draws Solid and Outline as Soft.
type VariantE uint8

const (
	VariantSolid VariantE = iota
	VariantSoft
	VariantOutline
	VariantGhost
)

// SizeE controls inner padding, corner radius and font metrics. SizeSm, the
// default, sets the label at caption size, below a button's body-size label.
type SizeE uint8

const (
	SizeSm SizeE = iota
	SizeMd
	SizeLg
)

// Fluid is the chained builder for a badge. Zero value is not valid;
// always start from New(id, label).
type Fluid struct {
	idGen     c.WidgetIdCreatorI
	label     string
	icon      string
	tooltip   string
	tone      ToneE
	variant   VariantE
	size      SizeE
	selected  bool
	monospace bool
	strong    bool
	pill      bool
}

// New constructs a chip / status pill with sensible defaults
// (ToneNeutral + VariantSoft + SizeSm). Pass any
// WidgetIdCreatorI — typically `ids.PrepareStr("status")` for relative
// scoping, or `MakeAbsoluteIdHighEntropy(...)` for top-level overlays.
//
// Example:
//
//	badge.New(ids.PrepareStr("env"), "production").
//	    Tone(badge.ToneError).
//	    Send()
func New(id c.WidgetIdCreatorI, label string) Fluid {
	return Fluid{
		idGen:   id,
		label:   label,
		tone:    ToneNeutral,
		variant: VariantSoft,
		size:    SizeSm,
	}
}

func (inst Fluid) Tone(tn ToneE) Fluid       { inst.tone = tn; return inst }
func (inst Fluid) Variant(va VariantE) Fluid { inst.variant = va; return inst }
func (inst Fluid) Size(sz SizeE) Fluid       { inst.size = sz; return inst }

// Icon prefixes the label with a glyph (typically an `icons.IconXxx`
// rune from `keelson/runtime/icons` — Phosphor affordance or NFBrand
// brand mark) joined by a non-breaking space so it never wraps off
// the chip.
func (inst Fluid) Icon(glyph string) Fluid { inst.icon = glyph; return inst }

// Selected forces the chip into a "pressed" look (Solid fill + tone stroke +
// contrasting fg) regardless of Variant. SendResp only, for filter chips;
// Send ignores it.
func (inst Fluid) Selected(on bool) Fluid { inst.selected = on; return inst }

// Monospace renders the label in the egui monospace font — useful for codes,
// hashes or count strings where digit widths should align.
func (inst Fluid) Monospace() Fluid { inst.monospace = true; return inst }

// Strong renders the label bold.
func (inst Fluid) Strong() Fluid { inst.strong = true; return inst }

// Pill overrides the size-derived corner radius with a value larger than any
// realistic badge half-height (egui clamps corners to the smaller dimension),
// producing the fully-rounded "pill" shape commonly used for notification
// counts and status dots.
func (inst Fluid) Pill() Fluid { inst.pill = true; return inst }

// Tooltip wraps the badge in a HoverText scope so the given string appears as
// an egui hover-tooltip when the pointer rests over the chip. Empty strings
// are a no-op. Internally relies on egui's overlay-interact pattern (see
// SKILLS.md §12) so it works with both Send and SendResp.
func (inst Fluid) Tooltip(text string) Fluid { inst.tooltip = text; return inst }

// resolveTone maps (tone, variant, selected) into the four colour /
// stroke-width arguments needed by Frame + RichTextColored.
//
// A badge that cannot be clicked must not look like a button, so it draws
// Solid and Outline as Soft and is never selected (ADR-0273 SD5).
//
// A clickable badge always has a stroke, transparent where the variant draws
// none: egui counts the stroke in a Frame's size, so one that appeared on
// hover would move the layout. Hover follows the button kinds of ADR-0273 —
// a filled badge goes one emphasis lighter, an outline gains a soft fill, a
// ghost gains a soft fill alone.
func (inst Fluid) resolveTone(interactive, hovered bool) (fill color.Color, stroke color.Color, strokeW float32, fg color.Color) {
	base, soft, fgOnSolid, fgOnSoft := tonePalette(inst.tone)
	transparent := color.Transparent

	variant, selected := inst.variant, inst.selected
	if !interactive {
		selected = false
		if variant == VariantSolid || variant == VariantOutline {
			variant = VariantSoft
		}
	}

	switch variant {
	case VariantSolid:
		fill, stroke, strokeW, fg = base, transparent, 0, fgOnSolid
	case VariantOutline:
		fill, stroke, strokeW, fg = transparent, base, 1.0, fgOnSoft
	case VariantGhost:
		fill, stroke, strokeW, fg = transparent, transparent, 0, fgOnSoft
	default: // Soft
		fill, stroke, strokeW, fg = soft, transparent, 0, fgOnSoft
	}

	if selected {
		fill = base
		stroke = base
		strokeW = 1.0
		fg = fgOnSolid
	}
	if !interactive {
		return
	}
	strokeW = 1.0
	if !hovered {
		return
	}
	switch {
	case selected || variant == VariantSolid:
		fill, stroke = fgOnSoft, fgOnSoft
	case variant == VariantOutline:
		fill, stroke = soft, fgOnSoft
	case variant == VariantGhost:
		fill = soft
	default: // Soft
		stroke = base
	}
	return
}

// tonePalette bridges a tone's IDS semantic-role tokens (now owned by
// styletokens.Tone — ADR-0031 §Updates 2026-06-06) into drawable colours for
// the badge's four slots:
//
//   - base       — Tone.Fill()       — <role>.Default (L≈0.80) — the light
//     tint that drives the Solid / Selected fill.
//   - soft       — Tone.Soft()       — <role>.Subtle  (L≈0.20) — the Soft
//     variant fill; reads as a quiet tinted region on bg.panel.
//   - fgOnSolid  — Tone.TextOnFill() — neutral.bg_extreme — high-contrast dark
//     fg on the light Default fill (Lc ≈ -100 via APCA on the dark spine).
//   - fgOnSoft   — Tone.Strong()     — <role>.Strong  (L≈0.90) — the lighter
//     tone-coloured fg that reads on the Subtle background.
//
// The bridge from styletokens.RGBA8 to color.Color goes through
// color.Hex(token.AsHex()) to avoid tripping designlint L2 — the lint
// flags raw color.RGB / color.RGBA calls, not Hex, and styletokens cannot
// import color directly per ADR-0035's keelson/thestack layering.
func tonePalette(tone ToneE) (base, soft, fgOnSolid, fgOnSoft color.Color) {
	base = color.Hex(tone.Fill().AsHex())
	soft = color.Hex(tone.Soft().AsHex())
	fgOnSolid = color.Hex(tone.TextOnFill().AsHex())
	fgOnSoft = color.Hex(tone.Strong().AsHex())
	return
}

// resolveLayout returns the per-side inner margin in pixels, the corner
// radius in pixels, and a flag indicating whether the label should be
// styled with .Small() to match the chip's footprint. The radii are
// deliberately larger than the Rounding.Sm a button takes: the shape is what
// tells the two apart at a glance (ADR-0273 SD5).
func (inst Fluid) resolveLayout() (left, right, top, bottom, corner float32, useSmall bool) {
	switch inst.size {
	case SizeSm:
		return 5, 5, 1, 1, 6, true
	case SizeLg:
		return 12, 12, 4, 4, 10, false
	default: // Md
		return 8, 8, 2, 2, 8, false
	}
}

// emit is the shared rendering core. senseClick=true threads the
// FrameMethodIdSenseClick byte so the badge participates in the egui
// pointer-interaction snapshot via the §12 "overlay interact" pattern.
// Returns the Frame's effective widget id so SendResp can read its
// previous-frame response slot.
func (inst Fluid) emit(senseClick bool) (frameId uint64) {
	f := c.Frame(inst.idGen)
	frameId = f.Id()
	// The previous frame's hover, under the standard one-frame-lag contract.
	hovered := senseClick && c.CurrentApplicationState.StateManager.GetResponseByIdRaw(frameId).HasHovered()
	fill, stroke, strokeW, fg := inst.resolveTone(senseClick, hovered)
	left, right, top, bottom, corner, useSmall := inst.resolveLayout()
	if inst.pill {
		// Larger than any realistic chip half-height; egui clamps to fit.
		corner = 100.0
	}

	transparentBg := color.Transparent

	text := inst.label
	if inst.icon != "" {
		// U+00A0 NBSP — keeps the glyph and the label on the same line.
		text = inst.icon + " " + inst.label
	}

	a := c.Atoms().BeginRichTextColored(fg, transparentBg, text)
	if useSmall {
		a = a.Small()
	}
	if inst.strong {
		a = a.Strong()
	}
	if inst.monospace {
		a = a.Monospace()
	}
	atomsKept := a.End().Keep()

	f = f.
		Fill(fill).
		CornerRadius(corner).
		Stroke(strokeW, stroke).
		InnerMarginSides(left, right, top, bottom)
	if senseClick {
		f = f.SenseClick().HoverCursorPointer()
	}

	emitFrame := func() {
		for range f.KeepIter() {
			c.LabelAtoms(atomsKept).Send()
		}
	}
	if inst.tooltip != "" {
		for range c.HoverText(inst.tooltip).KeepIter() {
			emitFrame()
		}
	} else {
		emitFrame()
	}
	return
}

// Send emits a non-interactive badge.
func (inst Fluid) Send() {
	inst.emit(false)
}

// SendResp emits the badge with click sensing enabled and returns the
// previous-frame response flags. Inspect HasPrimaryClicked() to drive
// filter-chip toggles, HasHovered() for tooltips, etc.
func (inst Fluid) SendResp() c.ResponseFlagsE {
	fid := inst.emit(true)
	return c.CurrentApplicationState.StateManager.GetResponseByIdRaw(fid)
}
