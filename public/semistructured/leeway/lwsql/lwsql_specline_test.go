package lwsql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SpecLines is the read-direction dual of Composer: the tokens that minted a
// column come back out, in the fixed order, with the constructor family's
// own spellings (ADR-0186 §SD3, §Verification).
func TestSpecLines_ClosureWithComposer(t *testing.T) {
	c := newDefaultComposer(t)
	names := make([]string, 0, 5)
	for _, f := range []func() (string, error){
		func() (string, error) {
			return c.PlainColumn("id", "u64", []string{"item:id", "enc:delta-encoding", "sem:scale-of-measurement-nominal"})
		},
		func() (string, error) {
			return c.TaggedValueColumn("sensor", "temperature", "f64", []string{"sem:measured", "sem:secret", "use:tlp-amber"})
		},
		func() (string, error) { return c.MembershipColumn("sensor", "low-card-ref") },
		func() (string, error) { return c.SupportColumn("sensor", "lrcard") },
	} {
		n, err := f()
		require.NoError(t, err)
		names = append(names, n)
	}
	lines := SpecLines(names)
	require.Len(t, lines, len(names))

	// Backbone: name, item, ct, aspects; no section, no role, no use.
	assert.Equal(t, "name:id item:id ct:u64 enc:delta-encoding sem:scale-of-measurement-nominal", lines[0])

	// Tagged value: name, section, role, ct, then sem/use in vocabulary order.
	assert.Equal(t, "name:temperature section:sensor role:val ct:f64 sem:measured sem:secret use:tlp-amber", lines[1])

	// A membership lane and a support lane carry their role.
	assert.True(t, strings.HasPrefix(lines[2], "name:"), lines[2])
	assert.Contains(t, lines[2], " section:sensor ")
	assert.Contains(t, lines[2], " role:lr")
	assert.Contains(t, lines[3], " role:lrcard")

	// The one rule the design exists for: sem:secret is one token, matchable
	// with a word boundary; the value column has it, the lanes do not.
	assert.Regexp(t, `\bsem:secret\b`, lines[1])
	assert.NotRegexp(t, `\bsem:secret\b`, lines[2])
}

// A non-leeway set gets `name:<column>` per column and nothing else, so a
// rule keyed on the name works on both kinds of result.
func TestSpecLines_NonLeeway(t *testing.T) {
	lines := SpecLines([]string{"temp_c", "count()", "notes@text/markdown"})
	assert.Equal(t, []string{"name:temp_c", "name:count()", "name:notes@text/markdown"}, lines)
	assert.Empty(t, SpecLines(nil))
}
