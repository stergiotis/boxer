package play

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
)

// The corpus gate (the sqlapplet §SD6 pattern): play's prompt book is held
// to zero parse errors, and the three launch prompts are durably public
// identity with the scopes their wording assumes.
func TestModelPromptCorpus(t *testing.T) {
	defs, errs := promptbook.ParseBook(modelBookId, help.MustSub(modelPromptsFS, "prompts"))
	require.Empty(t, errs)
	scopes := map[string]promptbook.ScopeE{}
	for _, def := range defs {
		assert.NotEmpty(t, def.Title, def.Slug)
		assert.NotEmpty(t, def.Summary, def.Slug)
		assert.NotEmpty(t, def.System, def.Slug)
		scopes[def.Slug] = def.Scope
	}
	assert.Equal(t, promptbook.ScopeBuffer, scopes["explain"])
	assert.Equal(t, promptbook.ScopeBufferAndError, scopes["fix-error"])
	assert.Equal(t, promptbook.ScopeQuestion, scopes["ask"])
}

// The schema block groups columns under their table, carries comments,
// and stops at the byte cap with a marker rather than silently.
func TestFormatSchema(t *testing.T) {
	tsv := "db\tt1\ta\tUInt64\tthe key\ndb\tt1\tb\tString\t\ndb\tt2\tc\tDate\tday\n"
	got := formatSchema(tsv, 1<<20)
	assert.Equal(t, "db.t1:\n  a UInt64 -- the key\n  b String\n\ndb.t2:\n  c Date -- day\n", got)
	capped := formatSchema(tsv, 10)
	assert.Contains(t, capped, "schema truncated")
	assert.NotContains(t, capped, "db.t2")
}

// A fix prompt reads the statement and its error; without an error, the
// statement alone.
func TestFixInput(t *testing.T) {
	s := fixInput("SELECT 1", "Code: 62. Syntax error")
	assert.Contains(t, s, "```sql\nSELECT 1\n```")
	assert.Contains(t, s, "The error:\n\nCode: 62. Syntax error")
	assert.NotContains(t, fixInput("SELECT 1", " "), "The error:")
}

// The orchestrator's roles map onto the client's, and an unknown role is
// refused rather than sent as the user.
func TestChatMessage(t *testing.T) {
	for _, role := range []string{"system", "user", "assistant"} {
		_, ok := chatMessage(orchestrator.Message{Role: role, Content: "x"})
		assert.True(t, ok, role)
	}
	_, ok := chatMessage(orchestrator.Message{Role: "tool", Content: "x"})
	assert.False(t, ok)
	_, err := modelChat{}.Chat(context.Background(), "m", []orchestrator.Message{{Role: "weird"}})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown chat role"))
}

// The tools a model may call: the validate is pure and reads as the
// orchestrator's stages would; a read outside the granted tables is
// refused before any bus request; arguments that are not JSON are refused.
func TestModelTools(t *testing.T) {
	tools := modelTools{tables: modelToolTables}
	names := make([]string, 0)
	for _, tl := range tools.Tools() {
		names = append(names, tl.Name)
	}
	assert.ElementsMatch(t, []string{"list_tables", "describe_table", "validate_sql"}, names, "no keelson_query without a reads client")

	out, err := tools.Call(context.Background(), orchestrator.ToolCall{Name: "validate_sql", Arguments: `{"sql":"SELECT a FROM t"}`})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "valid"))
	out, err = tools.Call(context.Background(), orchestrator.ToolCall{Name: "validate_sql", Arguments: `{"sql":"SELECT FROM WHERE"}`})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "invalid"))

	_, err = tools.Call(context.Background(), orchestrator.ToolCall{Name: "validate_sql", Arguments: `not json`})
	require.Error(t, err)
	_, err = tools.Call(context.Background(), orchestrator.ToolCall{Name: "nope"})
	require.Error(t, err)
	_, err = tools.Call(context.Background(), orchestrator.ToolCall{Name: "list_tables"})
	require.Error(t, err, "no endpoint client")

	assert.Equal(t, "ab\n… (truncated)", capText("abc", 2))
}
