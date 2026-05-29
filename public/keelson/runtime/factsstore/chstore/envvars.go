//go:build llm_generated_opus47

package chstore

import "github.com/stergiotis/boxer/public/config/env"

// BOXER_LOG_FACTS / BOXER_LOG_FACTS_URL are declared here (not in
// the thestack imzero2 main that originally read them) so they
// register with the env catalogue regardless of which binary links
// chstore. Package main was not importable from envgen, leaving
// these specs invisible to `boxer env list` and `doc/env-vars.md`
// (ADR-0058 §4) until the relocation.
var (
	// LogFactsEnabled gates whether the logbridge sink routes through
	// this ClickHouse-backed store (non-empty and != "0") versus the
	// in-memory fallback (empty or "0"). Consumed by upper-layer
	// wiring; chstore.New itself ignores the env value and honours
	// whatever Config it is handed.
	LogFactsEnabled = env.NewString(env.Spec{
		Name:        "BOXER_LOG_FACTS",
		Description: "non-empty (and not \"0\") enables the ClickHouse-backed facts store for the logbridge sink; empty/0 selects the in-memory fallback",
		Category:    env.CategoryObservability,
	})

	// LogFactsURL overrides chstore.Defaults().URL when the upper
	// layer is wiring chstore.New(). chstore.New itself takes the
	// URL from Config.URL — this spec is purely the convention for
	// the override env var.
	LogFactsURL = env.NewString(env.Spec{
		Name:        "BOXER_LOG_FACTS_URL",
		Description: "overrides the chstore default ClickHouse URL when BOXER_LOG_FACTS is enabled",
		Category:    env.CategoryObservability,
	})
)
