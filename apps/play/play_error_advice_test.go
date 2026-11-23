package play

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// An alias named like the column it aggregates makes the server nest or
// misplace the aggregate; the advice names the alias. An aggregate nested
// as written, or an alias the aggregate does not read, gets none.
func TestRunErrorAdvice(t *testing.T) {
	const tail = " (ILLEGAL_AGGREGATION) (version 26.9.13.15 (official build))"
	for _, c := range []struct{ text, alias string }{
		{"clickhouse http 500: Code: 184. DB::Exception: Aggregate function any(tot) AS tot is found inside another aggregate function in query." + tail, "tot"},
		{"Code: 184. DB::Exception: Aggregate function max(number) AS number is found in GROUP BY in query." + tail, "number"},
		{"Code: 184. DB::Exception: Aggregate function sumIf(t.n, t.k = 1) AS n is found in WHERE in query." + tail, "n"},
	} {
		advice := runErrorAdvice(c.text)
		assert.Contains(t, advice, "The alias "+c.alias+" has the name of a column it aggregates", c.text)
		assert.Equal(t, c.text+" — "+advice, withRunErrorAdvice(c.text))
	}
	for _, text := range []string{
		"Code: 184. DB::Exception: Aggregate function max(number) is found inside another aggregate function in query." + tail,
		"Code: 184. DB::Exception: Aggregate function sum(total) AS tot is found inside another aggregate function in query." + tail,
		"Code: 184. DB::Exception: Aggregate function count() AS n is found in WHERE in query." + tail,
		"Code: 184. DB::Exception: Aggregate function n(x) AS n is found in WHERE in query." + tail,
		"Code: 47. DB::Exception: Unknown expression identifier `w`. (UNKNOWN_IDENTIFIER)",
		"",
	} {
		assert.Empty(t, runErrorAdvice(text), text)
		assert.Equal(t, text, withRunErrorAdvice(text))
	}
}

// A keep the server refuses for that reason carries the advice too, and
// stays the error it wraps.
func TestSessionErrorCarriesTheAdvice(t *testing.T) {
	cause := errors.New("create the data set's table: clickhouse http 500: Code: 184. DB::Exception: Aggregate function any(ta) AS ta is found inside another aggregate function in query. (ILLEGAL_AGGREGATION)")
	err := sessionError("the data set was not kept", cause)
	assert.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "the data set was not kept: create the data set's table")
	assert.Contains(t, err.Error(), "Give the alias another name")
	plain := sessionError("the data set was not kept", errors.New("clickhouse http 404: Code: 47. Unknown identifier"))
	assert.NotContains(t, plain.Error(), "alias")
}
