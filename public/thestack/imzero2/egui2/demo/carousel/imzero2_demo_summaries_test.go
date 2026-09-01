package demo

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// summaryMaxRunes is the hard ceiling the tree is held to. The authoring
// target in Manifest.Summary's doc comment is ~60 — deliberately below this,
// because a hard cap set at the target turns the target into a limit and
// every author writes exactly to it. The headroom absorbs a line that reads
// better slightly long; past this the launcher row truncates and the summary
// stops doing its job.
const summaryMaxRunes = 72

// TestEverySummaryMeetsTheStyleBudget is ADR-0214 §SD4's style gate.
//
// Manifest.Validate deliberately checks non-emptiness only: RegisterFactory
// answers a validation failure by logging at Warn and dropping the app, so a
// length rule enforced there would silently remove an applet from the
// launcher over a house-style matter. Enforcing it here makes the same
// violation a red build instead — which is the whole reason the check lives
// in a test rather than in the constructor.
//
// The applet half is what this really covers. A Go manifest's missing summary
// is a compile-visible edit in a file someone is already looking at; a
// document's missing or over-long `summary:` frontmatter is neither.
func TestEverySummaryMeetsTheStyleBudget(t *testing.T) {
	mintCorpusOnce(t)

	manifests := runtimeapp.AllManifests()
	require.NotEmpty(t, manifests, "the carousel blank-imports every app; an empty registry means that broke")

	for _, m := range manifests {
		if m.Surface != runtimeapp.SurfaceWindowed {
			continue
		}
		// Non-emptiness is Validate's (asserted wholesale by
		// TestEveryRegisteredManifestIsClassifiable); skipping it here keeps
		// one owner per rule.
		if m.Summary == "" {
			continue
		}
		s := m.Summary
		assert.LessOrEqual(t, utf8.RuneCountInString(s), summaryMaxRunes,
			"%s: summary is %d runes, over the %d-rune budget: %q",
			m.Id, utf8.RuneCountInString(s), summaryMaxRunes, s)
		assert.Equal(t, strings.TrimSpace(s), s,
			"%s: summary has leading or trailing whitespace: %q", m.Id, s)
		assert.NotContains(t, s, "\n", "%s: summary must be one line: %q", m.Id, s)
		assert.False(t, strings.HasSuffix(s, "."),
			"%s: summary is a label, not a sentence — drop the full stop: %q", m.Id, s)
		// The row already shows Display directly above the summary, so a
		// summary that opens by restating it spends its whole budget saying
		// nothing ("Terrain scope — a scope for terrain").
		if m.Display != "" {
			assert.False(t, strings.HasPrefix(strings.ToLower(s), strings.ToLower(m.Display)),
				"%s: summary repeats Display %q, which the row renders directly above it: %q",
				m.Id, m.Display, s)
		}
		assert.NotEqual(t, strings.ToLower(m.Title), strings.ToLower(s),
			"%s: summary duplicates Title", m.Id)
	}
}
