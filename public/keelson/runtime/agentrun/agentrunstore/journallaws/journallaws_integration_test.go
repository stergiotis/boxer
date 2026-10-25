//go:build integration

package journallaws_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journallaws"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/journalviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// The laws as queries, run over a journal written through the SQL store
// onto clickhouse local: each returns the planted breaches and nothing
// else, and a clean run returns none.
func TestTheLawsReturnThePlantedBreaches(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "chdb")
	exec, err := chexec.NewLocalExecutor(dir, memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	const db = "jltest"
	layout := agentrunstore.Layout{Database: db}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	for _, stmt := range lwsqlsurface.AllStatements("") {
		require.NoError(t, exec.Exec(ctx, stmt))
	}
	st := agentrun.NewSqlStore(exec, layout)
	defer st.Close()
	person := agentrun.Label{Level: agentrun.LevelPerson}
	model := agentrun.Label{Level: agentrun.LevelOwnModel, Origin: []string{"r"}}

	// A clean run: request and reply paired, the reply at least the
	// request's label, one compaction from 0 whose summary joins its span.
	clean, err := agentrun.Open(ctx, st, agentrun.Run{Cell: "cell", Conversation: "clean", OwnerApp: "chat", Coordinator: "w1"})
	require.NoError(t, err)
	_, err = clean.Append(ctx,
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "q", Turn: "t1", Label: person},
		agentrun.Entry{Kind: agentrun.KindRequest, Key: "k1", Turn: "t1", Label: agentrun.Label{Tier: agentrun.TierGreen}},
		agentrun.Entry{Kind: agentrun.KindReply, Key: "k1", Role: "assistant", Body: "a", Turn: "t1", Label: model.Join(agentrun.Label{Tier: agentrun.TierGreen})},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "c1", Name: "op", Effect: agentrun.EffectView, Turn: "t1", Label: model},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "c1", Name: "op", Body: "{}", Turn: "t1", Label: person},
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "q2", Turn: "t2", Label: person},
		agentrun.Entry{Kind: agentrun.KindCompact, Target: 6, From: 0, Body: "s", Turn: "t2", Label: model.Join(agentrun.Label{Tier: agentrun.TierGreen})})
	require.NoError(t, err)

	// A run with one breach of each law planted.
	bad, err := agentrun.Open(ctx, st, agentrun.Run{Cell: "cell", Conversation: "bad", OwnerApp: "chat", Coordinator: "w2"})
	require.NoError(t, err)
	_, err = bad.Append(ctx,
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "q", Turn: "t1", Label: person.Join(agentrun.Label{Tier: agentrun.TierAmber})},         // 1, AMBER: what the summary at 7 replaces
		agentrun.Entry{Kind: agentrun.KindRequest, Key: "k1", Turn: "t1", Label: agentrun.Label{Tier: agentrun.TierAmber}},                                 // 2
		agentrun.Entry{Kind: agentrun.KindReply, Key: "k1", Role: "assistant", Body: "a", Turn: "t1", Label: model},                                        // 3: L6, GREEN under an AMBER request
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "c9", Name: "op", Body: "{}", Turn: "t1", Label: person},                                           // 4: L1, no intent
		agentrun.Entry{Kind: agentrun.KindReply, Key: "k9", Role: "assistant", Body: "a", Turn: "t1", Label: model},                                        // 5: L1, no request
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "q2", Turn: "t2", Label: person},                                                       // 6
		agentrun.Entry{Kind: agentrun.KindCompact, Target: 6, From: 0, Body: "s", Turn: "t2", Label: model},                                                // 7: L3, CLEAR over the AMBER message at 1
		agentrun.Entry{Kind: agentrun.KindUser, Role: "user", Body: "q3", Turn: "t3", Label: person},                                                       // 8
		agentrun.Entry{Kind: agentrun.KindCompact, Target: 6, From: 5, Body: "s", Turn: "t3", Label: model.Join(agentrun.Label{Tier: agentrun.TierAmber})}) // 9: L22 from 5 != 6, L23 cut 6 not > 6
	require.NoError(t, err)
	badRun := bad.Run().Id

	views, err := journalviews.Compose(db)
	require.NoError(t, err)
	for _, v := range views {
		require.NoError(t, exec.Exec(ctx, v.Sql), v.Name)
	}
	laws, err := journallaws.Compose(db)
	require.NoError(t, err)
	type breach struct {
		Run     uint64 `json:"run"`
		Ordinal uint32 `json:"ordinal"`
		Why     string `json:"why"`
	}
	run := func(sql string) (rows []breach) {
		cmd, cerr := extbin.ClickHouseLocal.Command(ctx, extbin.Opts{}, "--path", dir, "--output_format_json_quote_64bit_integers=0", "--query", sql+" FORMAT JSONEachRow")
		require.NoError(t, cerr)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		require.NoError(t, cmd.Run(), stderr.String())
		dec := json.NewDecoder(&out)
		for dec.More() {
			var row breach
			require.NoError(t, dec.Decode(&row))
			rows = append(rows, row)
		}
		return
	}
	want := map[string][]uint32{"L1": {4, 5}, "L3": {7}, "L6": {3}, "L22": {9}, "L23": {9}}
	for _, l := range laws {
		rows := run(l.Sql)
		var ordinals []uint32
		for _, r := range rows {
			assert.Equal(t, badRun, r.Run, "%s names a breach outside the planted run: %+v", l.Name, r)
			assert.Contains(t, r.Why, l.Name+": ", l.Name)
			ordinals = append(ordinals, r.Ordinal)
		}
		assert.Equal(t, want[l.Name], ordinals, l.Name)
	}
}
