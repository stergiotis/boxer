package widgets

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// RenderDemoIntro draws the canonical per-demo intro: kind + property
// badges (UX / DX / both for Mixed; Deterministic when the demo's
// captures are byte-stable; Needs network when it talks to the
// internet) + italic description label + clickable "view source on
// GitHub" Hyperlink + Separator. Both drivers (interactive shell,
// screenshot tour) call this before invoking the demo's Render
// closure, passing the caller's per-window WidgetIdStack. Embed (used
// by external hosts: profiler, debug shell) intentionally does not —
// those hosts own their own chrome.
//
// Skips silently when there is nothing to show (Kind unspecified AND
// empty Description AND unresolved SourceFile) so demos can adopt this
// gradually.
func RenderDemoIntro(ids *c.WidgetIdStack, d *registry.Demo) {
	if d.Kind == registry.DemoKindUnspecified && d.Description == "" && d.SourceFile == "" {
		return
	}
	renderDemoBadges(ids, d)
	if d.Description != "" {
		for rt := range c.RichTextLabel(d.Description) {
			rt.Italics()
		}
	}
	if d.SourceFile != "" {
		url := buildSourceURL(d.SourceFile, d.SourceLine)
		if url != "" {
			c.HyperlinkTo("view source on GitHub", url).OpenInNewTab(true).Send()
		}
	}
	c.Separator().Send()
}

// renderDemoBadges emits the kind chip (UX / DX / both for Mixed) plus
// flag-derived property chips on the same Horizontal row. Property
// chips currently cover:
//
//   - Deterministic — rendered when DemoFlagNonDeterministic is NOT
//     set, so the chip reads as a positive marker for the screenshot
//     tour's byte-stable subset (IMZERO2_SCREENSHOT_DETERMINISTIC=1
//     keeps exactly these demos).
//   - Needs network — rendered when DemoFlagNeedsNetwork IS set, as a
//     Warning chip so reviewers spot the offline-tour caveat at a
//     glance.
//
// Unspecified-kind demos still get the property row when any flag
// applies. The whole row collapses to a no-op when none of (Kind,
// property flags) contributes a badge.
func renderDemoBadges(ids *c.WidgetIdStack, d *registry.Demo) {
	hasUX := d.Kind == registry.DemoKindUX || d.Kind == registry.DemoKindMixed
	hasDX := d.Kind == registry.DemoKindDX || d.Kind == registry.DemoKindMixed
	deterministic := d.Flags&registry.DemoFlagNonDeterministic == 0
	needsNetwork := d.Flags&registry.DemoFlagNeedsNetwork != 0
	if !hasUX && !hasDX && !deterministic && !needsNetwork {
		return
	}
	for range c.Horizontal().KeepIter() {
		if hasUX {
			uxBadge(ids, d.Name).Send()
		}
		if hasDX {
			dxBadge(ids, d.Name).Send()
		}
		if deterministic {
			deterministicBadge(ids, d.Name).Send()
		}
		if needsNetwork {
			needsNetworkBadge(ids, d.Name).Send()
		}
	}
}

func uxBadge(ids *c.WidgetIdStack, demoName string) badge.Fluid {
	return badge.New(ids.PrepareStr("kind-ux-"+demoName), "UX showcase").
		Tone(badge.ToneInfo).Size(badge.SizeSm)
}

func dxBadge(ids *c.WidgetIdStack, demoName string) badge.Fluid {
	return badge.New(ids.PrepareStr("kind-dx-"+demoName), "DX example").
		Tone(badge.ToneWarning).Size(badge.SizeSm)
}

func deterministicBadge(ids *c.WidgetIdStack, demoName string) badge.Fluid {
	return badge.New(ids.PrepareStr("flag-det-"+demoName), "Deterministic").
		Tone(badge.ToneSuccess).Size(badge.SizeSm).
		Tooltip("Tour captures are byte-stable across runs.")
}

func needsNetworkBadge(ids *c.WidgetIdStack, demoName string) badge.Fluid {
	return badge.New(ids.PrepareStr("flag-net-"+demoName), "Needs network").
		Tone(badge.ToneWarning).Size(badge.SizeSm).
		Tooltip("Demo fetches data over the network; tour mode may produce blanks offline.")
}

// RenderDemoOutro draws optional per-demo trailing chrome. For DemoKindDX
// and DemoKindMixed it renders a closed-by-default CollapsingHeader
// "source code" containing the resolved function body in monospace, so
// reading the source is one click away even when the visual is
// uninformative on its own. UX gets nothing — its pixels are the artifact.
//
// Closed-by-default keeps screenshot-tour stages stable: no open animation
// is triggered, sidestepping the 4-frame footgun (CLAUDE.md / SKILLS.md
// §12).
func RenderDemoOutro(ids *c.WidgetIdStack, d *registry.Demo) {
	if (d.Kind != registry.DemoKindDX && d.Kind != registry.DemoKindMixed) || d.SourceFile == "" {
		return
	}
	fullSrc, firstLine, lastLine := extractFunctionSource(d.SourceFile, d.SourceLine)
	if firstLine == 0 {
		return
	}
	for range c.CollapsingHeader(
		ids.PrepareStr("source-"+d.Name),
		c.WidgetText().Text("source code").Keep(),
	).KeepIter() {
		// PrepareGoLines: this body runs every frame the header is expanded,
		// over an unchanging source (ADR-0125).
		c.CodeView(
			ids.PrepareStr("source-view-"+d.Name),
			codeview.PrepareGoLines(fullSrc, firstLine, lastLine),
		).Send()
	}
}
