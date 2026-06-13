// Package l4rounding implements IDS Tier 1 rule L4 — flag literal numeric
// values in rounding-aware API calls outside the styletokens allowlist.
//
// Rule rationale: ADR-0032 §SD3 — the rounding ladder is fixed at
// 0 / 2 / 4 / 6 px (RoundingNone / RoundingSm / RoundingMd / RoundingLg).
// A raw literal in .CornerRadius() bypasses the ladder, so a panel that
// should adopt RoundingMd (4 px cards/dialogs) instead drifts to whatever
// happened to be typed at the call site. The ladder is density-independent
// per ADR-0032 SD3, so unlike L3 there is no density-resolution argument —
// the case is purely "literal numbers fragment the visual system".
//
// v1 implementation note: detection is syntactic. We trigger on selector
// names matching the rounding-aware API surface (CornerRadius) and
// inspect the first positional argument; a numeric literal (INT or FLOAT)
// that is not in the no-op allowlist (0 / 0.0) raises the finding.
// Variable-bound arguments (the canonical `styletokens.RoundingSm` /
// `visuals.CornerRadius` forms) never trigger because they're not BasicLit
// nodes.
//
// Allowlist files: the styletokens module itself, where the ladder
// constants legitimately live. Line-level ignore via
// `// designlint:ignore=L4 (reason)` handles the rare case where a
// non-token-source literal is intentional.
//
// The traversal is shared with the other literal-in-selector-call rules via
// [literalrule]; this file supplies only L4's trigger set, allowlist, and
// message.
package l4rounding

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/internal/literalrule"
)

// Analyzer is the L4 default analyzer used by the designlint binary.
var Analyzer = literalrule.NewAnalyzer(literalrule.Spec{
	Name:   "l4rounding",
	Doc:    "L4: flag literal numeric values in rounding-aware API calls outside the styletokens module (ADR-0032 §SD3).",
	RuleID: "L4",
	// Rounding-aware method names that take a numeric corner-radius (px) arg.
	// Detection is by suffix selector name only — works regardless of the
	// FrameFluid / ProgressBarFluid / TintedScopeFluid receiver and aliasing.
	Triggers: map[string]bool{
		"CornerRadius": true,
	},
	// Only 0 (sharp corners, the Swiss default per ADR-0032 §SD3) may appear
	// raw; it has no token-form replacement. All other ladder values must reach
	// for styletokens.RoundingSm / RoundingMd / RoundingLg by name.
	Allowed: map[string]bool{
		"0":   true,
		"0.0": true,
	},
	// Package-path tails where literal rounding values legitimately live — the
	// styletokens module's ladder constants.
	AllowlistedPkgPathSuffixes: []string{
		"/public/keelson/designsystem/styletokens",
	},
	MessageFormat: "L4: raw literal %s in rounding-aware call .%s(); use a styletokens accessor (RoundingSm / RoundingMd / RoundingLg — ADR-0032 §SD3); annotate with // designlint:ignore=L4 (reason) if intentional",
})
