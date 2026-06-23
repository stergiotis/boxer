package play

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// newAffordanceEvaluator builds the analytical FunctionEvaluator that the
// SQL editor's debounced format pipeline runs alongside the canonicalisers.
// It registers no-op handlers for SQL functions whose call sites we want
// to surface to editor-side affordances (regex testers, time-range
// pickers, geo-cell editors, …); each handler returns ControlFlow with
// the discard sentinel so the runner forwards the editor's input
// unchanged — the call sites are preserved verbatim in the canonical-form
// preview, and observations land via OnObservation.
//
// The sink pointer is appended to on every visited call site; the caller
// (PlayApp) clears it before each pipeline run so the slice mirrors the
// current SQL.
func newAffordanceEvaluator(sink *[]nanopass.Observation) *passes.FunctionEvaluator {
	eval := passes.NewFunctionEvaluator()

	discard := func(args []any) (any, error) {
		return marshalling.ControlFlow{Sentinel: nanopass.PassDiscardOutput}, nil
	}
	// Register the whole multi-pattern regex family (multiMatch* and the
	// fuzzy multiFuzzyMatch*). Registration is case-insensitive; the
	// affordance matcher keys off the lowercased obs.Name. See
	// multiMatchRegexFamily (play_affordance.go) for the canonical set —
	// multiSearch* is deliberately absent (substring, not regex).
	for _, spec := range multiMatchRegexFamily {
		eval.Register(spec.display, discard, true)
	}

	eval.OnObservation(func(obs nanopass.Observation) {
		log.Info().
			Str("name", obs.Name).
			Bool("evaluated", obs.Evaluated).
			Int("src.start", obs.Src.Start).
			Int("src.end", obs.Src.End).
			Interface("args", obs.Args).
			Msg("play: SQL affordance observation")
		*sink = append(*sink, obs)
	})
	return eval
}
