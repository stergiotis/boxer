package chstore

import "github.com/stergiotis/boxer/public/config/env"

// BOXER_LOG_FACTS / BOXER_LOG_FACTS_URL are declared here (not in
// the thestack imzero2 main that originally read them) so they
// register with the env catalogue regardless of which binary links
// chstore. Package main was not importable from envgen, leaving
// these specs invisible to `boxer env list` and `doc/env-vars.md`
// (ADR-0058 §4) until the relocation.
var (
	// LogFactsEnabled gates whether the host installs a logbridge sink
	// over this ClickHouse-backed store (non-empty and != "0") or
	// persists no log lines at all (empty or "0"). Consumed by
	// upper-layer wiring (loghost); chstore.New itself ignores the env
	// value and honours whatever Config it is handed.
	LogFactsEnabled = env.NewString(env.Spec{
		Name:        "BOXER_LOG_FACTS",
		Description: "non-empty (and not \"0\") persists the host's log lines to boxer.facts through the ClickHouse store, which is where the log viewer reads them; empty/0 persists none, and the log viewer says so",
		Category:    env.CategoryObservability,
	})

	// LogFactsURL overrides the URL ConfigFromEnv resolved (so it wins
	// over CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL) when the upper layer is
	// wiring chstore.New(). chstore.New itself takes the URL from
	// Config.URL — this spec is purely the convention for the override
	// env var. Credentials are not part of it: the user and password come
	// from the CLICKHOUSE_* entries in either case.
	LogFactsURL = env.NewString(env.Spec{
		Name:        "BOXER_LOG_FACTS_URL",
		Description: "overrides the chstore default ClickHouse URL when BOXER_LOG_FACTS is enabled",
		Category:    env.CategoryObservability,
	})
)

// LogFactsOn says BOXER_LOG_FACTS asks for the host's log lines on
// boxer.facts: non-empty and not "0".
func LogFactsOn() (on bool) {
	v := LogFactsEnabled.Get()
	return v != "" && v != "0"
}
