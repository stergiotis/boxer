package play

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

// RegisterPasses adds play's host-scoped entries to the shared pre-execute
// stage, beyond the standard set (passreg/defaults): a play-hosting process
// canonicalises every executed statement. Hosts (the standalone binary, the
// carousel) call this at their wiring site next to defaults.RegisterDefaults,
// keeping the process's rewrite set reviewable there (ADR-0108 §SD4).
//
// CanonicalizeFull runs first (Order 50, ahead of the standard entries), so
// the stage's later passes consume canonical shapes — the nanopass contract
// that downstream passes target canonical form. The quoted spellings it
// emits stay matchable and executable: identsql's expander and the
// column-handle resolver compare identifiers through DecodeIdentifier, and
// ClickHouse accepts quoted function and table-function names
// ("left"('a', 1), FROM "numbers"(1)) — verified against the server. The
// converse cost is that later passes' own output (macro expansions) ships
// uncanonicalised; that output is machine-generated and already uniform.
//
// The rewrite is result-schema-neutral: ClickHouse derives result column
// names from the parsed AST, so a sugared spelling and its canonical form
// name their columns identically ([1,2] and array(1,2) both name "[1, 2]";
// quoted function names do not leak into names). Like every entry of the
// stage it rewrites the shipped body only — editor and preview surfaces keep
// the user's original text.
func RegisterPasses(r *passreg.Registry) (err error) {
	return r.Register(passreg.Entry{
		Pass:        passes.CanonicalizeFull(100),
		Stage:       passreg.StagePreExecute,
		Order:       50,
		Description: "rewrite the statement into canonical form (sugar to function calls, canonical quoting and keyword case)",
		Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
	})
}
