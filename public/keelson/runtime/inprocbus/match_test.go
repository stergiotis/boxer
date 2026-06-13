package inprocbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatch_ExactSubject(t *testing.T) {
	assert.True(t, Match("ch.query.boxer", "ch.query.boxer"))
	assert.False(t, Match("ch.query.boxer", "ch.query.spinnaker"))
	assert.False(t, Match("ch.query.boxer", "ch.query"))
	assert.False(t, Match("ch.query.boxer", "ch.query.boxer.extra"))
}

func TestMatch_StarOneToken(t *testing.T) {
	assert.True(t, Match("ch.*.boxer", "ch.query.boxer"))
	assert.True(t, Match("ch.*.boxer", "ch.stream.boxer"))
	assert.False(t, Match("ch.*.boxer", "ch.query.spinnaker"))
	assert.False(t, Match("ch.*.boxer", "ch.boxer"))
	assert.False(t, Match("ch.*.boxer", "ch.query.boxer.extra"))
}

func TestMatch_GtRestOfSubject(t *testing.T) {
	assert.True(t, Match("fs.>", "fs.dialog.read"))
	assert.True(t, Match("fs.>", "fs.handle.uuid.read"))
	assert.False(t, Match("fs.>", "fs"))            // > requires at least one trailing token
	assert.False(t, Match("fs.>", "kafka.produce")) // wrong prefix
}

func TestMatch_GtAlone(t *testing.T) {
	assert.True(t, Match(">", "fs.dialog.read"))
	assert.True(t, Match(">", "x"))
	assert.False(t, Match(">", "")) // empty subject never matches
}

func TestMatch_StarThenGt(t *testing.T) {
	assert.True(t, Match("app.*.event.>", "app.play.event.row_selected"))
	assert.True(t, Match("app.*.event.>", "app.play.event.col_changed.detail"))
	assert.False(t, Match("app.*.event.>", "app.play.event")) // > needs trailing
	assert.False(t, Match("app.*.event.>", "app.play.deep.event.x"))
}

func TestMatch_EmptyInputs(t *testing.T) {
	assert.False(t, Match("", "x"))
	assert.False(t, Match("x", ""))
	assert.False(t, Match("", ""))
}

func TestValidatePattern_OK(t *testing.T) {
	cases := []string{
		"fs.dialog.read",
		"fs.>",
		"app.*.event.>",
		"ch.query.boxer",
		">",
		"a",
	}
	for _, p := range cases {
		err := ValidatePattern(p)
		require.NoError(t, err, "pattern=%s", p)
	}
}

func TestValidatePattern_Reject(t *testing.T) {
	cases := map[string]string{
		"":         "empty",
		".x":       "empty token",
		"a..b":     "empty token",
		"a.>.b":    "'>' must be last",
		"a.@.b":    "invalid char",
		"a.b ":     "invalid char", // trailing space
	}
	for p, hint := range cases {
		err := ValidatePattern(p)
		require.Error(t, err, "pattern=%s", p)
		assert.Contains(t, err.Error(), hint, "pattern=%s", p)
	}
}

func TestValidateSubject_OK(t *testing.T) {
	cases := []string{
		"fs.dialog.read",
		"a.b.c.d.e",
		"single",
	}
	for _, s := range cases {
		err := ValidateSubject(s)
		require.NoError(t, err, "subject=%s", s)
	}
}

func TestValidateSubject_RejectsWildcards(t *testing.T) {
	cases := []string{"fs.>", "a.*.c", ">"}
	for _, s := range cases {
		err := ValidateSubject(s)
		require.Error(t, err, "subject=%s", s)
	}
}
