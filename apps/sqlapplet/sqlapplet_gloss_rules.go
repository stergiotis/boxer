package sqlapplet

import (
	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// The standing glosses of the shipped books (ADR-0186, its 2026-08-15
// Update: rules are code). The books spell their measures by suffix —
// `text_bytes`, `self_ns`, `age_ms` — so a suffix rule shows a byte count
// as one and a duration in the unit its name declares, in every applet's
// Table and Detail, without a directive in each buffer. A buffer's own
// `-- play: gloss` line still outranks these; `gloss/raw` there switches
// one off for that applet.
//
// Declared, validated and bound at package init — a rule that names an
// unknown gloss or a parameter its gloss does not take fails the process
// at startup, not the first applet that happens to open.
var bookRules = gloss.Rules("sqlapplet-books").
	Rule("byte counts").
	When(gloss.NameMatches(`(^|_)bytes$`)).
	Show(gloss.MediaTypeBytes).
	Rule("nanosecond durations").
	When(gloss.NameMatches(`_ns$`)).
	Show(gloss.MediaTypeDuration, gloss.Unit("ns")).
	Rule("millisecond durations").
	When(gloss.NameMatches(`_ms$`)).
	Show(gloss.MediaTypeDuration, gloss.Unit("ms"))

// bookRepository is the rule repository every standalone applet window is
// built over: the default catalog and bookRules. An embedder that wants
// its own passes EmbedConfig.Rules instead.
var bookRepository = func() *gloss.Repository {
	repo := gloss.NewRepository(nil)
	repo.MustRegister(bookRules)
	return repo
}()
