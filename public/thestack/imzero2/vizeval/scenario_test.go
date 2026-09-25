package vizeval

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommittedScenariosParse keeps play's scenarios readable: a misspelt
// gate or an unknown sink fails here rather than at scoring time.
func TestCommittedScenariosParse(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "apps", "play", "vizeval", "*"+ScenarioSuffix))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, p := range paths {
		sc, err := ReadScenario(p)
		require.NoError(t, err, p)
		assert.NotEmpty(t, sc.Spec.Intent, p)
		assert.NotEmpty(t, sc.Spec.Questions, p)
		assert.True(t, strings.HasPrefix(sc.DatasetSQL(), "WITH base AS ("), "%s reads its data through the base CTE", p)
	}
}

const minimalScenario = "---\nvizeval:\n  size: 800x600\n  sinks: [card]\n" +
	"  questions:\n    - {id: q, prompt: p, answer: \"SELECT 1\"}\n" +
	"  gates:\n    text.clipped: {max: 0}\n---\n\n# x\n\n```sql\nSELECT 1\n```\n"

func TestParseScenario(t *testing.T) {
	sc, err := ParseScenario("x/a"+ScenarioSuffix, []byte(minimalScenario))
	require.NoError(t, err)
	assert.Equal(t, "a", sc.Name)
	assert.Equal(t, "SELECT 1", sc.DatasetSQL(), "no base fence, no CTE")
	assert.True(t, sc.Admits(SinkCard))
	assert.False(t, sc.Admits(SinkJSON))
	g := sc.Spec.Gates["text.clipped"]
	assert.True(t, g.Pass(0, true))
	assert.False(t, g.Pass(1, true))
	assert.False(t, g.Pass(0, false), "an absent metric fails its gate")

	withBase := strings.Replace(minimalScenario, "```sql\nSELECT 1\n```", "```sql base\nSELECT 2 AS v;\n```\n\n```sql\nSELECT v FROM base\n```", 1)
	sc, err = ParseScenario("x/a"+ScenarioSuffix, []byte(withBase))
	require.NoError(t, err)
	assert.Equal(t, "WITH base AS (\nSELECT 2 AS v\n)\nSELECT v FROM base", sc.DatasetSQL())
	assert.Equal(t, "WITH base AS (\nSELECT 2 AS v\n)\nSELECT 1", sc.AnswerSQL(sc.Spec.Questions[0]))

	for name, src := range map[string]string{
		"unknown key":      strings.Replace(minimalScenario, "  size:", "  sise: 1\n  size:", 1),
		"unknown sink":     strings.Replace(minimalScenario, "[card]", "[pie]", 1),
		"no sinks":         strings.Replace(minimalScenario, "[card]", "[]", 1),
		"gate w/o bound":   strings.Replace(minimalScenario, "{max: 0}", "{}", 1),
		"bad compare":      strings.Replace(minimalScenario, "answer: \"SELECT 1\"}", "answer: \"SELECT 1\", compare: fuzzy}", 1),
		"no dataset":       strings.Replace(minimalScenario, "```sql\nSELECT 1\n```\n", "", 1),
		"no vizeval key":   strings.Replace(minimalScenario, "vizeval:", "scene:", 1),
		"bad size":         strings.Replace(minimalScenario, "800x600", "big", 1),
		"question w/o sql": strings.Replace(minimalScenario, ", answer: \"SELECT 1\"", "", 1),
	} {
		_, err = ParseScenario("x/a"+ScenarioSuffix, []byte(src))
		assert.Error(t, err, name)
	}
	_, err = ParseScenario("x/a.scene.md", []byte(minimalScenario))
	assert.Error(t, err, "the suffix names the document")
}
