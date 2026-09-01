package codelint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/gov/codelint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cases below are drawn from the CS013 backlog, and each one is the reason
// a rule in MessageWithoutDirectives exists. They are the record of which
// rewrites are safe to make without reading the surrounding function.
func TestMessageWithoutDirectives(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		want   string // "" means the site must be declined
	}{
		// The dominant shape: the value sits at the tail of a clause, so
		// deleting it leaves the clause whole.
		{"value before a colon", "open %s: %w", "open: %w"},
		{"value at end of message", "unknown profile %q", "unknown profile"},
		{"two tail values", "compose create table %s: index column: %w", "compose create table: index column: %w"},
		{"%w only is untouched", "open config: %w", "open config: %w"},
		{"no directives at all", "nothing to do", "nothing to do"},
		{"escaped percent is not a directive", "100%% full: %w", "100%% full: %w"},

		// Declined: the value is glued to punctuation, so removing it leaves a
		// dangling operator.
		{"glued to equals", "n=%d", ""},
		{"glued to a word", "id%dfailed", ""},

		// Declined: the value is mid-sentence. The sentence has to be
		// rewritten, which is not mechanical.
		{"value followed by a word", "open %s failed", ""},
		{"several mid-sentence values", "tier needs %d files, %s holds %d", ""},

		// Declined: a comma-separated list of values collapses to punctuation.
		{"comma-separated values", "raster columns must be UInt8 (got %s, %s, %s, %s)", ""},
		{"two values sharing a clause", "stored %s, computed %s: %w", ""},

		// Declined: the message would end on the word that introduced the
		// value, so it still parses and says the wrong thing.
		{"ends on an article phrase", "unable to open the staged %s: %w", ""},
		{"ends on a preposition", "could not locate repo root above %q", ""},
		{"ends on a transitive verb", "membership params index exceeds %d", ""},
		{"ends on a participle", "type info/sizes missing for package containing %q", ""},

		// Declined: a trailing colon with nothing left to introduce.
		{"trailing colon without a wrap", "adhocdata: publish rejected: %s", ""},
		// Declined: the value sat between two colons, so removing it collides
		// them. "(got)" fails the same way via the tail-word rule.
		{"colon collision", "adhocdata: publish rejected: %s: %w", ""},
		{"parenthesised introducing word", "raster columns must be UInt8 (got %s)", ""},

		// Declined: nothing would be left.
		{"message is only a directive", "%s", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := codelint.MessageWithoutDirectives(tc.format)
			if tc.want == "" {
				assert.False(t, ok, "expected %q to be declined, got %q", tc.format, got)
				return
			}
			assert.True(t, ok, "expected %q to be accepted", tc.format)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestUsefulFieldKey(t *testing.T) {
	for _, k := range []string{"path", "tableName", "idx", "pos", "id", "db", "wd", "ip", "os", "fd", "ok"} {
		assert.True(t, codelint.UsefulFieldKey(k), k)
	}
	// A field nobody can interpret is no better than the prose it came from.
	for _, k := range []string{"s", "h", "i", "n", "p", "x", "tt", "aa"} {
		assert.False(t, codelint.UsefulFieldKey(k), k)
	}
}

// TestCS013OffersAFixWhenMechanical pins the rewrite CS013 hands over, since
// the text is what someone pastes. The fourth case is the reason the key
// derivation consults the argument's type: an index expression has no name of
// its own, and "ids[0]" is not a field name.
func TestCS013OffersAFixWhenMechanical(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs013/fixable")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS013())

	byLine := map[int32]codelint.Finding{}
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		byLine[f.Line] = f
	}

	for _, tc := range []struct {
		line int32
		fix  string
	}{
		{23, `eb.Build().Str("path", path).Errorf("open SBOM: %w", ErrNotApplied)`},
		{29, `eb.Build().Stringer("patchHash", h).Errorf("patch: %w", ErrNotApplied)`},
		{35, `eb.Build().Str("nodeID", string(ids[0])).Errorf("duplicate node id")`},
		{42, `eb.Build().Uint8("stage", uint8(stage)).Errorf("unknown stage")`},
	} {
		f, found := byLine[tc.line]
		require.True(t, found, "expected a CS013 finding on line %d", tc.line)
		assert.Equal(t, tc.fix, f.Fix, "line %d", tc.line)
		assert.NotContains(t, f.Message, "[", "a fixable finding carries no decline reason")
	}
}

// TestCS013ReportsWhyItDeclined checks the triage labels reach the finding, so
// a backlog can be split by the kind of work each site needs.
func TestCS013ReportsWhyItDeclined(t *testing.T) {
	root, err := filepath.Abs("./testdata/cs013/bad")
	require.NoError(t, err)

	pkgs, err := codelint.LoadPackagesE(codelint.LoadConfig{}, root)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	linter := codelint.NewLinter()
	linter.Register(codelint.NewRuleCS013())

	reasons := map[string]int{}
	for f, runErr := range linter.Run(pkgs) {
		require.NoError(t, runErr)
		if i := strings.LastIndex(f.Message, "["); i >= 0 {
			reasons[strings.Trim(f.Message[i:], "[]")]++
		}
	}
	// bad.go's "unknown kind %d" sits mid-sentence in one case and names its
	// argument in others, so both labels have to appear for the split to mean
	// anything.
	assert.NotEmpty(t, reasons, "at least one declined finding must say why")
	for r := range reasons {
		assert.Contains(t, []string{
			codelint.DeclineNeedsMessageRewrite,
			codelint.DeclineNeedsFieldName,
			codelint.DeclineNoFieldForVerb,
			codelint.DeclineFormattedError,
			codelint.DeclineNotMechanical,
		}, r, "unexpected decline reason")
	}
}
