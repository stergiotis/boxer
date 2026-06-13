package task

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubjectFormatters_RoundTripThroughParse(t *testing.T) {
	id := TaskIdT("abc123_DEF-456")
	cases := []struct {
		name    string
		subject string
		verb    string
	}{
		{"created", SubjectCreated(id), VerbCreated},
		{"progress", SubjectProgress(id), VerbProgress},
		{"cancel", SubjectCancel(id), VerbCancel},
		{"done", SubjectDone(id), VerbDone},
		{"error", SubjectError(id), VerbError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotId, gotVerb, ok := ParseSubject(tc.subject)
			require.True(t, ok, "ParseSubject(%q) should succeed", tc.subject)
			assert.Equal(t, id, gotId)
			assert.Equal(t, tc.verb, gotVerb)
		})
	}
}

func TestParseSubject_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"task.",
		"task.id",
		"task.id.",
		"fs.dialog.read",
		"taskid.verb",
		"task..verb",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			_, _, ok := ParseSubject(s)
			assert.False(t, ok)
		})
	}
}

func TestPatternAll_CoversAllVerbs(t *testing.T) {
	// PatternAll is task.> — the > wildcard matches one or more tokens.
	// This test documents the contract; if the prefix or wildcard
	// changes, downstream observers break.
	assert.Equal(t, "task.>", PatternAll)
	assert.Equal(t, "task.*.cancel", PatternCancelAll)
}
