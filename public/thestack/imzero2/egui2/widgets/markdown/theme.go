//go:build llm_generated_opus47

package markdown

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Inline highlight palette — "highlighter pen" effect using the IDS accent
// role (ADR-0031 §SD2 reserves accent for "branded highlights, selection,
// focus rings"). AccentDefault is the bright L≈0.80 highlight area;
// NeutralBgExtreme is the high-contrast dark text. Promoted to package-level
// vars so the bridge (token → uint32 hex → color.Color) runs once per
// process rather than per markdown render.
var (
	highlightFg = color.Hex(styletokens.NeutralBgExtreme.AsHex())
	highlightBg = color.Hex(styletokens.AccentDefault.AsHex())
)

// calloutThemeE selects a callout color family. Obsidian's vocabulary
// is wide (note, info, tip, abstract, todo, success, question,
// warning, failure, danger, bug, example, quote, …); we collapse it
// into five visually-distinct families plus a default. The mapping
// from string type to family lives in [calloutTheme].
type calloutThemeE uint8

const (
	calloutThemeDefault calloutThemeE = iota
	calloutThemeNote
	calloutThemeWarning
	calloutThemeDanger
	calloutThemeTip
	calloutThemeQuote
)

// calloutTheme classifies an Obsidian callout type string into a
// theme family and selects an emoji glyph for the title row. The
// match is case-insensitive; unknown types fall through to
// calloutThemeDefault with a generic glyph.
func calloutTheme(typ string) (theme calloutThemeE, glyph string) {
	switch strings.ToLower(typ) {
	case "note":
		theme = calloutThemeNote
		glyph = "📝"
	case "info":
		theme = calloutThemeNote
		glyph = "ℹ"
	case "abstract", "summary", "tldr":
		theme = calloutThemeNote
		glyph = "📑"
	case "todo":
		theme = calloutThemeNote
		glyph = "☐"
	case "example":
		theme = calloutThemeNote
		glyph = "📋"
	case "tip", "hint", "important":
		theme = calloutThemeTip
		glyph = "💡"
	case "success", "check", "done":
		theme = calloutThemeTip
		glyph = "✓"
	case "question", "help", "faq":
		theme = calloutThemeNote
		glyph = "❓"
	case "warning", "caution", "attention":
		theme = calloutThemeWarning
		glyph = "⚠"
	case "failure", "fail", "missing":
		theme = calloutThemeDanger
		glyph = "✗"
	case "danger", "error":
		theme = calloutThemeDanger
		glyph = "⛔"
	case "bug":
		theme = calloutThemeDanger
		glyph = "🐞"
	case "quote", "cite":
		theme = calloutThemeQuote
		glyph = "❝"
	default:
		theme = calloutThemeDefault
		glyph = "•"
	}
	return
}

// calloutColors returns the (border, fill) pair for a callout family,
// sourced from the IDS semantic palette (ADR-0031 §SD2):
//
//   - note     → Info     (informational, blue family)
//   - warning  → Warning  (caution, amber family)
//   - danger   → Error    (failure, red family)
//   - tip      → Success  (positive, green family)
//   - quote    → Neutral  (no chroma)
//   - default  → Neutral  (catchall for unknown callout types)
//
// border carries the family identity at full opacity (<role>.Default at
// L≈0.80) and reads against the bg.panel substrate; fill is the matching
// Subtle tint (L≈0.20) so the callout block reads as a quiet tinted
// region rather than competing with surrounding text. Replaces the
// pre-IDS hand-picked hex values from the Obsidian-style palette.
func calloutColors(theme calloutThemeE) (border, fill color.Color) {
	var borderT, fillT styletokens.RGBA8
	switch theme {
	case calloutThemeNote:
		borderT, fillT = styletokens.InfoDefault, styletokens.InfoSubtle
	case calloutThemeWarning:
		borderT, fillT = styletokens.WarningDefault, styletokens.WarningSubtle
	case calloutThemeDanger:
		borderT, fillT = styletokens.ErrorDefault, styletokens.ErrorSubtle
	case calloutThemeTip:
		borderT, fillT = styletokens.SuccessDefault, styletokens.SuccessSubtle
	case calloutThemeQuote:
		borderT, fillT = styletokens.NeutralDefault, styletokens.NeutralSubtle
	default:
		borderT, fillT = styletokens.NeutralDefault, styletokens.NeutralSubtle
	}
	border = color.Hex(borderT.AsHex())
	fill = color.Hex(fillT.AsHex())
	return
}

// calloutTitleText composes the title row text from the callout's
// glyph + explicit Title field, falling back to the type name (with a
// leading capital letter) when no title is given so plain
// `> [!warning]` callouts still get a header.
func calloutTitleText(typ, title, glyph string) (s string) {
	body := title
	if body == "" {
		if typ == "" {
			body = "Callout"
		} else {
			body = strings.ToUpper(typ[:1]) + typ[1:]
		}
	}
	s = glyph + " " + body
	return
}
