package regex_explorer

// Cheatsheet + showcase panel content.
//
// Static reference material rendered in the left panel: RE2 syntax
// tokens, ClickHouse regex function names, and a curated set of
// showcase (pattern, haystack) pairs. Syntax and showcase rows are
// clickable; the function rows are reference only:
//   - syntax tokens append to the last-focused text input via
//     [insertToken];
//   - showcase rows replace both pattern and haystack via [applyShowcase]
//     the query lanes pick the new inputs up on the same frame.
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

// cheatToken is one clickable reference row: the token text that gets
// inserted, plus a short description.
type cheatToken struct {
	Token string
	Desc  string
}

// cheatSection is one collapsible block of the cheatsheet. Rows are data
// rather than a run of hand-numbered calls: the sequence numbers that used
// to be written out by hand at every call site are now the slice index, so
// inserting a row in the middle can no longer silently collide two widget
// ids with a mistyped number.
type cheatSection struct {
	Id    string
	Title string
	Rows  []cheatToken
	// Reference marks a section of names to read, not tokens to insert:
	// a function signature appended to a pattern only breaks it.
	Reference bool
}

var cheatSections = []cheatSection{
	{
		Id:    "cs-classes",
		Title: "Character classes",
		Rows: []cheatToken{
			{`\d`, "digit [0-9]"},
			{`\D`, "non-digit"},
			{`\w`, "word [A-Za-z0-9_]"},
			{`\W`, "non-word"},
			{`\s`, "whitespace"},
			{`\S`, "non-whitespace"},
			{`.`, "any char"},
			{`[abc]`, "any of a, b, c"},
			{`[^abc]`, "none of a, b, c"},
			{`[a-z]`, "range"},
			{`\p{Greek}`, "Unicode property"},
		},
	},
	{
		Id:    "cs-anchors",
		Title: "Anchors",
		Rows: []cheatToken{
			{`^`, "start of line / text"},
			{`$`, "end of line / text"},
			{`\b`, "word boundary"},
			{`\B`, "non-boundary"},
			{`\A`, "start of text"},
			{`\z`, "end of text"},
		},
	},
	{
		Id:    "cs-quantifiers",
		Title: "Quantifiers",
		Rows: []cheatToken{
			{`*`, "zero or more"},
			{`+`, "one or more"},
			{`?`, "zero or one"},
			{`{n}`, "exactly n"},
			{`{n,}`, "n or more"},
			{`{n,m}`, "between n and m"},
			{`*?`, "lazy (smallest match)"},
		},
	},
	{
		Id:    "cs-groups",
		Title: "Groups & flags",
		Rows: []cheatToken{
			{`(...)`, "capturing group"},
			{`(?:...)`, "non-capturing"},
			{`(?P<n>...)`, "named capture"},
			{`(?i)`, "case-insensitive"},
			{`(?m)`, "multiline"},
			{`(?s)`, "dot-all"},
			{`a|b`, "alternation"},
		},
	},
	{
		Id:        "cs-ch-single",
		Title:     "ClickHouse RE2 functions",
		Reference: true,
		Rows: []cheatToken{
			{`match(h, p)`, "UInt8: 1 if anything matches"},
			{`countMatches(h, p)`, "UInt64: number of non-empty matches"},
			{`extract(h, p)`, "first match — or its group 1 if the pattern captures"},
			{`regexpExtract(h, p, i)`, "group i of the first match; 0 is the whole match"},
			// Deliberately not "full matches": ClickHouse returns capture
			// group 1 whenever the pattern captures, which is the single
			// most surprising thing about this function.
			{`extractAll(h, p)`, "Array(String): matches — or group 1 if the pattern captures; stops at the first empty match"},
			{`extractAllGroups(h, p)`, "Array(Array(String)): groups per match (needs a group)"},
			{`replaceRegexpOne(h, p, r)`, "replace the first match"},
			{`replaceRegexpAll(h, p, r)`, "replace every match"},
		},
	},
	{
		Id:        "cs-ch-multi",
		Title:     "ClickHouse VectorScan functions",
		Reference: true,
		Rows: []cheatToken{
			{`multiMatchAny(h, [p..])`, "UInt8: any pattern hit"},
			{`multiMatchAnyIndex(h, [p..])`, "UInt64: index of first hit"},
			{`multiMatchAllIndices(h, [p..])`, "Array(UInt64): all hit indices, unsorted"},
			{`multiFuzzyMatchAny(h, d, [p..])`, "fuzzy match with edit distance"},
		},
	},
}

// renderCheatsheet draws the left-panel cheatsheet: a Showcases section
// on top followed by RE2 syntax and ClickHouse function references.
// Clicking any row either inserts a token into the last-focused input
// (syntax/function rows) or replaces pattern+haystack (showcase rows).
func (inst *App) renderCheatsheet() {
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for range c.CollapsingHeader(inst.ids.PrepareStr("cs-showcases"), c.WidgetText().Text("Showcases").Keep()).DefaultOpen(true).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("showcase-scope")) {
				for i, sc := range showcaseCases {
					for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
						btnAtoms := c.Atoms().Text(sc.Title).Keep()
						if c.Button(inst.ids.PrepareStr("btn"), btnAtoms).Small().SendResp().HasPrimaryClicked() {
							inst.applyShowcase(sc.Pattern, sc.Haystack)
						}
					}
				}
			}
		}

		for si, sec := range cheatSections {
			for range c.CollapsingHeader(inst.ids.PrepareStr(sec.Id), c.WidgetText().Text(sec.Title).Keep()).KeepIter() {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(si))) {
					for i, row := range sec.Rows {
						if sec.Reference {
							referenceRow(row)
							continue
						}
						inst.cheatRow(uint64(i), row.Token, row.Desc)
					}
				}
			}
		}
	}
}

// referenceRow draws one function row: the signature in monospace and its
// description beneath, nothing to click.
func referenceRow(row cheatToken) {
	for rt := range c.RichTextLabel(row.Token) {
		rt.Monospace()
	}
	for rt := range c.RichTextLabel("    " + row.Desc) {
		rt.Weak()
	}
}

// cheatRow draws one clickable token row: a small button labelled with
// the token text, followed by a plain description. Clicking the button
// appends the token into the last-focused text input via [App.insertToken].
func (inst *App) cheatRow(seq uint64, token string, desc string) {
	for range c.IdScope(inst.ids.PrepareSeq(seq)) {
		for range c.Horizontal().KeepIter() {
			btnAtoms := c.Atoms().Text(token).Keep()
			if c.Button(inst.ids.PrepareStr("tok"), btnAtoms).Small().SendResp().HasPrimaryClicked() {
				inst.insertToken(token)
			}
			c.Label(desc).Send()
		}
	}
}
