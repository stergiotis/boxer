//go:build llm_generated_opus47

// Package validator wraps boxer's nanopass.Parse (ClickHouse Grammar1)
// to provide sub-millisecond in-process syntax validation for the
// time range picker's expression fields. The picker calls Validate on
// every keystroke; on parse error it surfaces a red underline in the
// field. Evaluation against clickhouse-local is deferred until Apply
// (see the evaluator sub-package).
package validator

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Validate parses the given ClickHouse SQL fragment using boxer's
// Grammar1 parser. Returns nil on parse success; returns an error
// describing the first syntax error otherwise.
//
// The fragment is wrapped in a SELECT before parsing because Grammar1
// expects a QueryStmt at the root, while user expressions are bare
// relative-expressions. A fragment "anchor_now - INTERVAL 5 MINUTE"
// is parsed as if the user wrote "SELECT anchor_now - INTERVAL 5
// MINUTE", which is a valid QueryStmt with a single projection.
//
// Empty fragments are rejected with a dedicated message rather than
// being passed to the parser (which would surface a less helpful
// "expected expression" error).
func Validate(fragment string) (err error) {
	if fragment == "" {
		err = eh.Errorf("empty expression")
		return
	}
	wrapped := "SELECT " + fragment
	_, parseErr := nanopass.Parse(wrapped)
	if parseErr != nil {
		err = eh.Errorf("validate: %w", parseErr)
		return
	}
	return
}
