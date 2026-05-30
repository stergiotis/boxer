//go:build llm_generated_opus47

// Package l3spacing implements IDS Tier 1 rule L3 — flag literal floats
// in spacing-aware API calls outside the styletokens allowlist.
//
// Rule rationale: ADR-0032 §SD2 — spacing tokens drive density resolution.
// A raw literal in c.AddSpace / Frame.InnerMargin / Frame.OuterMargin
// bypasses the density preset, so a panel that should adapt to Tight
// stays at Standard.
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the spacing-aware API surface (AddSpace / InnerMargin /
// OuterMargin) and inspect the first positional argument; a numeric
// literal (INT or FLOAT) that is not in the hairline allowlist (0 / 0.0 /
// 1 / 1.0) raises the finding. Variable-bound arguments (the canonical
// `styletokens.PaddingDefault(d)` form) never trigger because they're
// not BasicLit nodes.
//
// Allowlist files: the styletokens module itself, where the PX_TABLE
// numeric literals legitimately live. Line-level ignore via
// `// designlint:ignore=L3 (reason)` handles the rare case where a
// non-token-source literal is intentional.
//
// The traversal is shared with the other literal-in-selector-call rules via
// [literalrule]; this file supplies only L3's trigger set, allowlist, and
// message.
package l3spacing

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/literalrule"
)

// Analyzer is the L3 default analyzer used by the designlint binary.
var Analyzer = literalrule.NewAnalyzer(literalrule.Spec{
	Name:   "l3spacing",
	Doc:    "L3: flag literal floats in spacing-aware API calls outside the styletokens module (ADR-0032 §SD2).",
	RuleID: "L3",
	// Spacing-aware method names that take a numeric px arg. Detection is by
	// suffix selector name only — works regardless of the c.* / ui.* receiver
	// and import-alias variations.
	Triggers: map[string]bool{
		"AddSpace":    true,
		"InnerMargin": true,
		"OuterMargin": true,
	},
	// Values that may appear raw in spacing positions without referring to a
	// token — 0 (no-op) and 1 (hairline strokes per ADR-0029 §SD6 / ADR-0032 §SD4).
	Allowed: map[string]bool{
		"0":   true,
		"0.0": true,
		"1":   true,
		"1.0": true,
	},
	// Package-path tails where literal spacing values legitimately live — the
	// styletokens module's PX_TABLE and the purpose-based helper bodies.
	AllowlistedPkgPathSuffixes: []string{
		"/public/keelson/designsystem/styletokens",
	},
	MessageFormat: "L3: raw literal %s in spacing-aware call .%s(); use a styletokens accessor (PaddingDefault / GapItems / etc. — ADR-0032 §SD2); annotate with // designlint:ignore=L3 (reason) if intentional",
})
