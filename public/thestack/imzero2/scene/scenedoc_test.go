package scene

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleDoc = "---\n" +
	"type: reference\n" +
	"audience: contributor\n" +
	"status: draft\n" +
	"scene:\n" +
	"  launch: play\n" +
	"  size: 1600x1000\n" +
	"  env:\n" +
	"    BOXER_PLAY_FOCUS_TABLE: \"1\"\n" +
	"  requires: [clickhouse]\n" +
	"---\n\n" +
	"# A result table\n\n" +
	"The table pane after a run.\n\n" +
	"```sql\nSELECT 1 AS a,\n       2 AS b\n```\n\n" +
	"An example that is not the buffer:\n\n" +
	"```sql\nSELECT 2\n```\n\n" +
	"```jsonl trace\n" +
	"# wait for the run\n" +
	"{\"do\":\"wait\",\"name\":\"Run\"}\n" +
	"{\"do\":\"capture\",\"text\":\"table\"}\n" +
	"```\n"

func TestParseDoc(t *testing.T) {
	doc, err := ParseDoc("x/10_table.scene.md", []byte(sampleDoc))
	require.NoError(t, err)
	assert.Equal(t, "10_table", doc.Name)
	assert.Equal(t, "play", doc.Spec.Launch)
	assert.Equal(t, "1", doc.Spec.Env["BOXER_PLAY_FOCUS_TABLE"])
	assert.Equal(t, []string{"clickhouse"}, doc.Spec.Requires)
	assert.Equal(t, "SELECT 1 AS a,\n       2 AS b", doc.SQL, "the first role-less sql fence is the buffer")
	require.Len(t, doc.Steps, 2)
	assert.Equal(t, []string{"table"}, doc.Captures())
	assert.Contains(t, doc.Prose, "The table pane after a run.")
	assert.NotContains(t, doc.Prose, "SELECT", "fences are not prose")
	w, h, err := doc.Spec.Dimensions()
	require.NoError(t, err)
	assert.Equal(t, 1600, w)
	assert.Equal(t, 1000, h)
}

func TestParseDocWithoutATraceIsOneCapture(t *testing.T) {
	doc, err := ParseDoc("a.scene.md", []byte("---\nscene:\n  launch: widgets\n---\nprose\n"))
	require.NoError(t, err)
	require.Len(t, doc.Steps, 1)
	assert.Equal(t, "capture", doc.Steps[0].Do)
	assert.Equal(t, "a", doc.Steps[0].Text)
}

func TestParseDocRejects(t *testing.T) {
	for name, src := range map[string]string{
		"no frontmatter":    "prose only\n",
		"no scene key":      "---\ntype: reference\n---\n",
		"no launch":         "---\nscene:\n  size: 10x10\n---\n",
		"misspelt spec key": "---\nscene:\n  launch: play\n  sise: 10x10\n---\n",
		"bad size":          "---\nscene:\n  launch: play\n  size: big\n---\n",
		"bad trace":         "---\nscene:\n  launch: play\n---\n```jsonl trace\n{\"name\":\"Run\"}\n```\n",
		"two traces":        "---\nscene:\n  launch: play\n---\n```jsonl trace\n{\"do\":\"note\"}\n```\n```jsonl trace\n{\"do\":\"note\"}\n```\n",
	} {
		_, err := ParseDoc("a.scene.md", []byte(src))
		require.Error(t, err, name)
	}
	_, err := ParseDoc("a.md", []byte("---\nscene:\n  launch: play\n---\n"))
	require.Error(t, err, "wrong suffix")
}
