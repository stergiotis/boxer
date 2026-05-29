//go:build llm_generated_opus47

package validator_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/validator"
)

func TestValidateAcceptsTypicalExpressions(t *testing.T) {
	cases := []string{
		"anchor_now",
		"anchor_now - INTERVAL 5 MINUTE",
		"anchor_now - INTERVAL 24 HOUR",
		"toStartOfDay(anchor_now)",
		"toStartOfDay(anchor_now - INTERVAL 1 DAY)",
		"addDays(toStartOfWeek(anchor_now), 7)",
		"addMonths(toStartOfMonth(anchor_now), 1)",
	}
	for _, c := range cases {
		if err := validator.Validate(c); err != nil {
			t.Errorf("expected %q to validate, got err: %v", c, err)
		}
	}
}

func TestValidateRejectsEmpty(t *testing.T) {
	if err := validator.Validate(""); err == nil {
		t.Error("expected empty fragment to fail, got nil")
	}
}

func TestValidateRejectsSyntaxErrors(t *testing.T) {
	// The validator wraps the fragment in SELECT before parsing with
	// Grammar1, which is permissive in places (e.g. dangling keywords
	// like INTERVAL parse as bare identifiers in projection position).
	// Cases below pick errors Grammar1 *does* surface; the evaluator
	// catches the rest at Apply time when ClickHouse itself rejects
	// the rendered query.
	cases := []string{
		"toStartOfDay(",           // unclosed paren
		"); DROP TABLE x",         // leading `)` is invalid
		"SELECT * FROM y; SELECT", // trailing dangling SELECT
	}
	for _, c := range cases {
		if err := validator.Validate(c); err == nil {
			t.Errorf("expected %q to fail, got nil", c)
		}
	}
}
