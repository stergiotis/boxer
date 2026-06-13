package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// =============================================================================
// Badge / Chip widget demos
//
// Split into three focused demos so each fits the screenshot tour's ~694 px
// viewport ceiling:
//
//   - demoBadgeStyle        — tones × variants matrix + sizes (design vocabulary)
//   - demoBadgeExtras       — icons + pill shape + hover tooltips
//   - demoBadgeInteractive  — filter chips + status pills + counts + dismissible
//
// demoBadgeStyle and demoBadgeExtras are read-only — they don't keep
// per-window state. demoBadgeInteractive carries badgesInteractiveState
// (per-window filter map, notification counts, tag list) so two open
// gallery windows have independent chip toggles and counters.
// =============================================================================

// badgeFilters is the (immutable) list of filter labels shown across
// every gallery window. Stays package-level because nothing mutates
// it.
var badgeFilters = []string{"go", "rust", "ui", "egui", "fffi", "ffi", "imzero2", "imgui"}

// badgesInteractiveState carries the per-window state for
// demoBadgeInteractive. filterSelected is a map so the demo can toggle
// chips by key; tags is a slice the dismissible-tag demo mutates with
// append/cut; the three notify counters drive monospace badge labels.
type badgesInteractiveState struct {
	filterSelected map[string]bool
	tags           []string
	notifyUnread   int
	notifyAlerts   int
	notifyMsgs     int
}

func newBadgesInteractiveState() (st *badgesInteractiveState) {
	st = &badgesInteractiveState{
		filterSelected: map[string]bool{
			"go":      true,
			"rust":    true,
			"imzero2": true,
		},
		tags:         []string{"go", "rust", "ui", "egui", "imzero2"},
		notifyUnread: 12,
		notifyAlerts: 3,
		notifyMsgs:   142,
	}
	return
}

func init() {
	registry.Register(registry.Demo{
		Name:        "badges-interactive",
		Category:    "Design system",
		Title:       icons.IconTag + " badges (interactive)",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindMixed,
		Description: "Real-world Badge patterns: clickable filter chips with selected state, status pills, monospace notification counts and a manually-composed dismissible tag list.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = newBadgesInteractiveState()
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoBadgeInteractive(ids, state.(*badgesInteractiveState))
		},
		SourceFunc: demoBadgeInteractive,
	})
}

// badgeSection emits the standard "Separator + bold heading + caption" block
// shared by every demo subsection. Spacing is density-resolved via IDS
// tokens; callers thread their resolved DensityE through `d`.
func badgeSection(d styletokens.DensityE, title, caption string) {
	c.Separator().Send()
	for rt := range c.RichTextLabel(title) {
		rt.Strong()
	}
	if caption != "" {
		c.Label(caption).Send()
	}
	c.AddSpace(styletokens.PaddingHair(d))
}

// -----------------------------------------------------------------------------
// Demo 1: design vocabulary — tones × variants + sizes
// -----------------------------------------------------------------------------

func demoBadgeStyle(ids *c.WidgetIdStack) {
	d := styletokens.DensityFromEnv()
	c.Label("Badge / Chip — a Frame + LabelAtoms composition over the existing").Send()
	c.Label("FFFI2 primitives. No new IDL or Rust opcodes were added.").Send()
	c.AddSpace(styletokens.PaddingInner(d))

	badgeSection(d, "tones × variants", "rows = tone, columns = Solid / Soft / Outline / Ghost")
	tones := []struct {
		tone  badge.ToneE
		label string
	}{
		{badge.ToneNeutral, "neutral"},
		{badge.TonePrimary, "primary"},
		{badge.ToneSuccess, "success"},
		{badge.ToneWarning, "warning"},
		{badge.ToneError, "error"},
		{badge.ToneInfo, "info"},
	}
	variants := []badge.VariantE{
		badge.VariantSolid, badge.VariantSoft, badge.VariantOutline, badge.VariantGhost,
	}
	for ti, t := range tones {
		for range c.IdScope(ids.PrepareSeq(uint64(0xba0e0000 + ti))) {
			for range c.Horizontal().KeepIter() {
				for vi, v := range variants {
					badge.New(ids.PrepareSeq(uint64(vi)), t.label).
						Tone(t.tone).Variant(v).Send()
				}
			}
		}
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "sizes", "Sm / Md / Lg adjust inner padding, corner radius and font scale")
	for range c.Horizontal().KeepIter() {
		badge.New(ids.PrepareStr("sz-sm"), "small").
			Tone(badge.TonePrimary).Variant(badge.VariantSolid).Size(badge.SizeSm).Send()
		badge.New(ids.PrepareStr("sz-md"), "medium").
			Tone(badge.TonePrimary).Variant(badge.VariantSolid).Size(badge.SizeMd).Send()
		badge.New(ids.PrepareStr("sz-lg"), "large").
			Tone(badge.TonePrimary).Variant(badge.VariantSolid).Size(badge.SizeLg).Send()
	}
}

// -----------------------------------------------------------------------------
// Demo 2: decoration knobs — icons + pill + tooltip
// -----------------------------------------------------------------------------

func demoBadgeExtras(ids *c.WidgetIdStack) {
	d := styletokens.DensityFromEnv()
	c.Label("Three orthogonal knobs that decorate any tone × variant × size").Send()
	c.Label("combination: a leading glyph, a fully-rounded pill shape, and a").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("hover tooltip.").Send()                                                  // designlint:ignore=L1 (continuation of preceding line)
	c.AddSpace(styletokens.PaddingInner(d))

	badgeSection(d, "icons", "any string accepted; `icons.IconXxx` glyphs read well at every size")
	for range c.Horizontal().KeepIter() {
		badge.New(ids.PrepareStr("ic-ok"), "passing").
			Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Icon(icons.IconCheck).Send()
		badge.New(ids.PrepareStr("ic-warn"), "deprecated").
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Icon(icons.IconWarning).Send()
		badge.New(ids.PrepareStr("ic-err"), "failed").
			Tone(badge.ToneError).Variant(badge.VariantSolid).Strong().Icon(icons.IconError).Send()
		badge.New(ids.PrepareStr("ic-info"), "preview").
			Tone(badge.ToneInfo).Variant(badge.VariantOutline).Icon(icons.IconInfo).Send()
		badge.New(ids.PrepareStr("ic-tag"), "release").
			Tone(badge.TonePrimary).Variant(badge.VariantSoft).Icon(icons.IconTag).Send()
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "pill shape", ".Pill() overrides the size-derived corner radius for the fully-rounded look")
	for range c.Horizontal().KeepIter() {
		badge.New(ids.PrepareStr("pl-1"), "1").
			Tone(badge.TonePrimary).Variant(badge.VariantSolid).Size(badge.SizeSm).Pill().Monospace().Send()
		badge.New(ids.PrepareStr("pl-12"), "12").
			Tone(badge.ToneError).Variant(badge.VariantSolid).Size(badge.SizeSm).Pill().Monospace().Strong().Send()
		badge.New(ids.PrepareStr("pl-99"), "99+").
			Tone(badge.ToneError).Variant(badge.VariantSolid).Size(badge.SizeSm).Pill().Monospace().Strong().Send()
		badge.New(ids.PrepareStr("pl-live"), "LIVE").
			Tone(badge.ToneSuccess).Variant(badge.VariantSolid).Pill().Strong().Send()
		badge.New(ids.PrepareStr("pl-beta"), "BETA").
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Pill().Strong().Send()
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "hover tooltips", "Tooltip(\"…\") wraps the chip in a HoverText scope — hover to see the tip")
	for range c.Horizontal().KeepIter() {
		badge.New(ids.PrepareStr("tt-1"), "ready").
			Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Icon(icons.IconCheck).
			Tooltip("all 12 health checks passing").Send()
		badge.New(ids.PrepareStr("tt-2"), "deprecated").
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Icon(icons.IconWarning).
			Tooltip("removed in v3.0 — see migration guide").Send()
		badge.New(ids.PrepareStr("tt-3"), "down").
			Tone(badge.ToneError).Variant(badge.VariantSolid).Strong().Icon(icons.IconError).
			Tooltip("connection refused (last seen 4m ago)").Send()
	}
}

// -----------------------------------------------------------------------------
// Demo 3: real-world patterns — filter chips + status + counts + dismissible
// -----------------------------------------------------------------------------

func demoBadgeInteractive(ids *c.WidgetIdStack, st *badgesInteractiveState) {
	d := styletokens.DensityFromEnv()
	c.Label("Patterns built from Badge: clickable filter chips, status pills,").Send()
	c.Label("monospace notification counts and a manually-composed dismissible").Send() // designlint:ignore=L1 (continuation of preceding line)
	c.Label("tag list (\"chip + ×\" = two adjacent badges in an IdScope).").Send()      // designlint:ignore=L1 (continuation of preceding line)
	c.AddSpace(styletokens.PaddingInner(d))

	badgeSection(d, "filter chips (interactive)", "click any chip to toggle Selected — the visual switches Outline ↔ Solid")
	for range c.IdScope(ids.PrepareStr("filter-row")) {
		for range c.Horizontal().KeepIter() {
			for i, key := range badgeFilters {
				sel := st.filterSelected[key]
				if badge.New(ids.PrepareSeq(uint64(i)), key).
					Tone(badge.TonePrimary).
					Variant(badge.VariantOutline).
					Selected(sel).
					SendResp().
					HasPrimaryClicked() {
					st.filterSelected[key] = !sel
				}
			}
		}
	}
	c.AddSpace(styletokens.PaddingHair(d))
	{
		var picked []string
		for _, k := range badgeFilters {
			if st.filterSelected[k] {
				picked = append(picked, k)
			}
		}
		readout := "selected: (none)"
		if len(picked) > 0 {
			readout = "selected: " + fmt.Sprint(picked)
		}
		for rt := range c.RichTextLabel(readout) {
			rt.Italics()
		}
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "status indicators", "typical \"row of status pills\" layout — service health / build state / etc.")
	for range c.IdScope(ids.PrepareStr("status-row")) {
		for range c.Horizontal().KeepIter() {
			c.Label("API").Send()
			badge.New(ids.PrepareStr("api"), "online").
				Tone(badge.ToneSuccess).Variant(badge.VariantSoft).
				Size(badge.SizeSm).Icon(icons.IconCircle).Send()
			c.AddSpace(styletokens.GapItems(d))
			c.Label("Worker").Send()
			badge.New(ids.PrepareStr("worker"), "degraded").
				Tone(badge.ToneWarning).Variant(badge.VariantSoft).
				Size(badge.SizeSm).Icon(icons.IconCircle).Send()
			c.AddSpace(styletokens.GapItems(d))
			c.Label("DB").Send()
			badge.New(ids.PrepareStr("db"), "down").
				Tone(badge.ToneError).Variant(badge.VariantSolid).Strong().
				Size(badge.SizeSm).Icon(icons.IconCircle).Send()
		}
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "notification counts", "monospace label keeps digit widths aligned across re-renders")
	for range c.IdScope(ids.PrepareStr("notify-row")) {
		for range c.Horizontal().KeepIter() {
			c.Label("Inbox").Send()
			badge.New(ids.PrepareStr("inbox"), fmt.Sprintf("%d", st.notifyUnread)).
				Tone(badge.TonePrimary).Variant(badge.VariantSolid).
				Size(badge.SizeSm).Pill().Monospace().Send()
			c.AddSpace(styletokens.GapItems(d))
			c.Label("Alerts").Send()
			badge.New(ids.PrepareStr("alerts"), fmt.Sprintf("%d", st.notifyAlerts)).
				Tone(badge.ToneError).Variant(badge.VariantSolid).
				Size(badge.SizeSm).Pill().Monospace().Strong().Send()
			c.AddSpace(styletokens.GapItems(d))
			c.Label("Messages").Send()
			badge.New(ids.PrepareStr("messages"), fmt.Sprintf("%d", st.notifyMsgs)).
				Tone(badge.ToneNeutral).Variant(badge.VariantSoft).
				Size(badge.SizeSm).Pill().Monospace().Send()
		}
	}
	c.AddSpace(styletokens.PaddingHair(d))
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("inc-unread"), c.Atoms().Text("+1 unread").Keep()).
			SendResp().HasPrimaryClicked() {
			st.notifyUnread++
		}
		if c.Button(ids.PrepareStr("clear-unread"), c.Atoms().Text("clear unread").Keep()).
			SendResp().HasPrimaryClicked() {
			st.notifyUnread = 0
		}
		if c.Button(ids.PrepareStr("inc-alerts"), c.Atoms().Text("+1 alert").Keep()).
			SendResp().HasPrimaryClicked() {
			st.notifyAlerts++
		}
	}

	c.AddSpace(styletokens.GapInline(d))
	badgeSection(d, "dismissible tags", "v1 keeps Badge atomic; \"chip + ×\" is composed from two adjacent badges")
	for range c.IdScope(ids.PrepareStr("tag-row")) {
		for range c.Horizontal().KeepIter() {
			removeAt := -1
			for i, tag := range st.tags {
				for range c.IdScope(ids.PrepareSeq(uint64(i))) {
					badge.New(ids.PrepareStr("tag"), tag).
						Tone(badge.TonePrimary).Variant(badge.VariantSoft).
						Size(badge.SizeSm).Send()
					if badge.New(ids.PrepareStr("close"), icons.IconClose).
						Tone(badge.TonePrimary).Variant(badge.VariantGhost).
						Size(badge.SizeSm).
						SendResp().HasPrimaryClicked() {
						removeAt = i
					}
				}
				c.AddSpace(styletokens.PaddingInner(d))
			}
			if removeAt >= 0 {
				st.tags = append(st.tags[:removeAt], st.tags[removeAt+1:]...)
			}
		}
	}
	if c.Button(ids.PrepareStr("reset-tags"), c.Atoms().Text("reset tags").Keep()).
		SendResp().HasPrimaryClicked() {
		st.tags = []string{"go", "rust", "ui", "egui", "imzero2"}
	}
}
