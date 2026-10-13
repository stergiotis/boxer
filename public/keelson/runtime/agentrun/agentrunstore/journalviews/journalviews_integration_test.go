//go:build integration

package journalviews_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// A journal written through the SQL store reads back through the views:
// one row per run as it stands, a rewritten run collapsed to its newest
// row, a deleted one carrying its tombstone, and one row per entry with
// its metadata and none of its content.
func TestViewsReadBackAJournal(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "chdb")
	exec, err := chexec.NewLocalExecutor(dir, memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	const db = "jvtest"
	layout := agentrunstore.Layout{Database: db}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	for _, stmt := range lwsqlsurface.AllStatements("") {
		require.NoError(t, exec.Exec(ctx, stmt))
	}
	st := agentrun.NewSqlStore(exec, layout)
	defer st.Close()

	w, err := agentrun.Open(ctx, st, agentrun.Run{Cell: "cell", Conversation: "c1", OwnerApp: "chat", Coordinator: "w1"})
	require.NoError(t, err)
	_, err = w.Append(ctx,
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "secret words", Turn: "t1", Label: agentrun.Label{Level: agentrun.LevelPerson}},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "llm-1", Role: "assistant", Body: "an answer", Turn: "t1", Label: agentrun.Label{Origin: []string{"openai"}}})
	require.NoError(t, err)
	require.NoError(t, w.Update(ctx, func(r *agentrun.Run) { r.Fingerprint = "fp" }))
	first := w.Run().Id

	other, err := agentrun.Open(ctx, st, agentrun.Run{Cell: "cell", Conversation: "c2", OwnerApp: "chat", Coordinator: "w2"})
	require.NoError(t, err)
	require.NoError(t, other.Close(ctx, agentrun.StateClosed))
	require.NoError(t, other.Delete(ctx, "person"))

	views, err := journalviews.Compose(db)
	require.NoError(t, err)
	byName := map[string]journalviews.View{}
	for _, v := range views {
		require.NoError(t, exec.Exec(ctx, v.Sql), v.Name)
		byName[v.Name] = v
	}
	query := func(sql string) (rows []map[string]any) {
		cmd, cerr := extbin.ClickHouseLocal.Command(ctx, extbin.Opts{}, "--path", dir, "--output_format_json_quote_64bit_integers=0", "--query", sql+" FORMAT JSONEachRow")
		require.NoError(t, cerr)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		dec := json.NewDecoder(&out)
		for dec.More() {
			row := map[string]any{}
			require.NoError(t, dec.Decode(&row))
			rows = append(rows, row)
		}
		return
	}
	read := func(view string, names []string, rest string) []map[string]any {
		phys := map[string]string{}
		for _, c := range byName[view].Columns {
			phys[c.Name] = c.Physical
		}
		items := make([]string, 0, len(names))
		for _, n := range names {
			p, ok := phys[n]
			require.True(t, ok, "%s has no column %s", view, n)
			items = append(items, `"`+p+`" AS "`+n+`"`)
		}
		for _, n := range names {
			rest = strings.ReplaceAll(rest, "{"+n+"}", `"`+phys[n]+`"`)
		}
		return query("SELECT " + strings.Join(items, ", ") + " FROM " + db + "." + view + " " + rest)
	}

	runs := read(journalviews.ViewRuns, []string{"run", "conversation", "owner-app", "state", "fingerprint", "last-ordinal", "deleted-by"}, "ORDER BY {conversation}")
	require.Len(t, runs, 2, "latest-wins: one row per run")
	require.EqualValues(t, first, runs[0]["run"])
	require.Equal(t, "fp", runs[0]["fingerprint"], "the rewrite is the row")
	require.Equal(t, "chat", runs[0]["owner-app"])
	require.EqualValues(t, 2, runs[0]["last-ordinal"])
	require.Equal(t, "", runs[0]["deleted-by"])
	require.Equal(t, "person", runs[1]["deleted-by"], "a deletion is the run's newest row")

	entries := read(journalviews.ViewEntries, []string{"run", "ordinal", "kind", "role", "key", "turn", "label-origin"}, "WHERE {run} = "+jsonNumber(first)+" ORDER BY {ordinal}")
	require.Len(t, entries, 2)
	require.Equal(t, agentrun.KindUser.String(), entries[0]["kind"])
	require.Equal(t, "assistant", entries[1]["role"])
	require.Equal(t, "llm-1", entries[1]["key"])
	require.Equal(t, []any{"openai"}, entries[1]["label-origin"])

	for _, v := range views {
		for _, c := range v.Columns {
			for _, content := range []string{"body", "calls", "arguments", "content"} {
				require.NotEqual(t, content, c.Name, "%s carries no content", v.Name)
			}
		}
	}
}

func jsonNumber(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
