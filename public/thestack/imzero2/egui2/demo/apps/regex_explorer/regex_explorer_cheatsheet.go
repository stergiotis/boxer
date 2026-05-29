//go:build llm_generated_opus47

package regex_explorer

// Cheatsheet + showcase panel content.
//
// Static reference material rendered in the left panel: RE2 syntax
// tokens, ClickHouse regex function names, and a curated set of
// showcase (pattern, haystack) pairs. All rows are clickable:
//   - syntax/function tokens append to the last-focused text input via
//     [insertToken];
//   - showcase rows replace both pattern and haystack via [applyShowcase]
//     and trigger the per-tab query cascade.
//
// Organised as CollapsingHeader sections so users can fold away the
// topics they don't need; all start closed to keep the initial panel
// compact.

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// showcaseCase is a (pattern, haystack) pair plus a short label, offered
// as a one-click seed example. International text is featured
// deliberately: both Go regexp and ClickHouse RE2 are UTF-8-aware, and
// showcasing that is more useful than a dozen ASCII variations.
type showcaseCase struct {
	Title    string
	Pattern  string
	Haystack string
}

var showcaseCases = []showcaseCase{
	{
		Title:    "digits",
		Pattern:  `\d+`,
		Haystack: "Order #123 shipped 2026-04-23 with 5 items at €19.95 each.",
	},
	{
		Title:    "email addresses",
		Pattern:  `[\w.+-]+@[\w-]+\.[\w.-]+`,
		Haystack: "Contact alice@example.com or bob+filter@sub.example.co.uk for details.",
	},
	{
		Title:    "IPv4 addresses",
		Pattern:  `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`,
		Haystack: "Primary 192.168.1.1, secondary 10.0.0.42, public 203.0.113.5.",
	},
	{
		Title:    "ISO dates",
		Pattern:  `\d{4}-\d{2}-\d{2}`,
		Haystack: "Releases: 2026-04-23, 2026-05-01, 2026-05-15 (tentative).",
	},
	{
		Title:    "capture groups (user@host)",
		Pattern:  `(\w+)@([\w.]+)`,
		Haystack: "alice@example.com bob@test.org carol@sub.domain.net",
	},
	{
		Title:    "Greek text",
		Pattern:  `\p{Greek}+`,
		Haystack: "Hello κόσμος, παρακαλώ πείτε ελληνικά.",
	},
	{
		Title:    "CJK (Han) characters",
		Pattern:  `\p{Han}+`,
		Haystack: "Order from 東京 to 北京 via 上海 arrived.",
	},
	{
		Title:    "hex colours",
		Pattern:  `#[0-9a-fA-F]{6}`,
		Haystack: "Palette: #ff6b35, #004e89, #1a936f, #ffcf56, plain red.",
	},
	{
		Title:    "URLs",
		Pattern:  `https?://[^\s]+`,
		Haystack: "See https://clickhouse.com/docs and http://example.org/path for references.",
	},
}

// renderCheatsheet draws the left-panel cheatsheet: a Showcases section
// on top followed by RE2 syntax and ClickHouse function references.
// Clicking any row either inserts a token into the last-focused input
// (syntax/function rows) or replaces pattern+haystack (showcase rows).
func renderCheatsheet() {
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for range c.CollapsingHeader(app.ids.PrepareStr("cs-showcases"), c.WidgetText().Text("Showcases").Keep()).DefaultOpen(true).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("showcase-scope")) {
				for i, sc := range showcaseCases {
					for range c.IdScope(app.ids.PrepareSeq(uint64(i))) {
						btnAtoms := c.Atoms().Text(sc.Title).Keep()
						if c.Button(app.ids.PrepareStr("btn"), btnAtoms).Small().SendResp().HasPrimaryClicked() {
							applyShowcase(sc.Pattern, sc.Haystack)
						}
					}
				}
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-classes"), c.WidgetText().Text("Character classes").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-classes-scope")) {
				cheatRow(0, `\d`, "digit [0-9]")
				cheatRow(1, `\D`, "non-digit")
				cheatRow(2, `\w`, "word [A-Za-z0-9_]")
				cheatRow(3, `\W`, "non-word")
				cheatRow(4, `\s`, "whitespace")
				cheatRow(5, `\S`, "non-whitespace")
				cheatRow(6, `.`, "any char")
				cheatRow(7, `[abc]`, "any of a, b, c")
				cheatRow(8, `[^abc]`, "none of a, b, c")
				cheatRow(9, `[a-z]`, "range")
				cheatRow(10, `\p{Greek}`, "Unicode property")
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-anchors"), c.WidgetText().Text("Anchors").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-anchors-scope")) {
				cheatRow(0, `^`, "start of line / text")
				cheatRow(1, `$`, "end of line / text")
				cheatRow(2, `\b`, "word boundary")
				cheatRow(3, `\B`, "non-boundary")
				cheatRow(4, `\A`, "start of text")
				cheatRow(5, `\z`, "end of text")
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-quantifiers"), c.WidgetText().Text("Quantifiers").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-quantifiers-scope")) {
				cheatRow(0, `*`, "zero or more")
				cheatRow(1, `+`, "one or more")
				cheatRow(2, `?`, "zero or one")
				cheatRow(3, `{n}`, "exactly n")
				cheatRow(4, `{n,}`, "n or more")
				cheatRow(5, `{n,m}`, "between n and m")
				cheatRow(6, `*?`, "lazy (smallest match)")
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-groups"), c.WidgetText().Text("Groups & flags").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-groups-scope")) {
				cheatRow(0, `(...)`, "capturing group")
				cheatRow(1, `(?:...)`, "non-capturing")
				cheatRow(2, `(?P<n>...)`, "named capture")
				cheatRow(3, `(?i)`, "case-insensitive")
				cheatRow(4, `(?m)`, "multiline")
				cheatRow(5, `(?s)`, "dot-all")
				cheatRow(6, `a|b`, "alternation")
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-ch-single"), c.WidgetText().Text("ClickHouse RE2 fns").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-ch-single-scope")) {
				cheatRow(0, `match(h, p)`, "UInt8: 1 if match, else 0")
				cheatRow(1, `extractAll(h, p)`, "Array(String): full matches")
				cheatRow(2, `extractAllGroups(h, p)`, "Array(Array(String)): groups per match")
				cheatRow(3, `replaceRegexpAll(h, p, r)`, "replace every match")
				cheatRow(4, `replaceRegexpOne(h, p, r)`, "replace first match")
				cheatRow(5, `countMatches(h, p)`, "number of matches")
			}
		}

		for range c.CollapsingHeader(app.ids.PrepareStr("cs-ch-multi"), c.WidgetText().Text("ClickHouse VectorScan fns").Keep()).KeepIter() {
			for range c.IdScope(app.ids.PrepareStr("cs-ch-multi-scope")) {
				cheatRow(0, `multiMatchAny(h, [p..])`, "UInt8: any pattern hit")
				cheatRow(1, `multiMatchAnyIndex(h, [p..])`, "UInt64: index of first hit")
				cheatRow(2, `multiMatchAllIndices(h, [p..])`, "Array(UInt64): all hit indices")
				cheatRow(3, `multiFuzzyMatchAny(h, d, [p..])`, "fuzzy match with edit distance")
			}
		}
	}
}

// cheatRow draws one clickable token row: a small button labelled with
// the token text, followed by a plain description. Clicking the button
// appends the token into the last-focused text input via [insertToken].
func cheatRow(seq uint64, token string, desc string) {
	for range c.IdScope(app.ids.PrepareSeq(seq)) {
		for range c.Horizontal().KeepIter() {
			btnAtoms := c.Atoms().Text(token).Keep()
			if c.Button(app.ids.PrepareStr("tok"), btnAtoms).Small().SendResp().HasPrimaryClicked() {
				insertToken(token)
			}
			c.Label(desc).Send()
		}
	}
}
