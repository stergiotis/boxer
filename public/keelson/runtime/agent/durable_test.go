package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent/agentfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Over clickhouse-local: every decision and every final phase lands as an
// agentAction row on boxer.facts (ADR-0269 §SD9), flushed off the call's
// path and at shutdown.
func TestActionRecordLandsOnFacts(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.Exec = true, exec })
	require.True(t, r.svc.Durable())
	g := r.grant(ModeAct)
	r.call(g, "q", "get_text", "{}")
	r.call(g, "w", "set_text", `{"text":"agent"}`)
	r.host.frame(7)
	r.host.frame(7)
	_, err = r.cli.Status(ctx, g.Handle, "w", 0)
	require.NoError(t, err)
	want := len(r.svc.Actions())
	r.svc.Close() // flushes what is buffered

	store := agentfacts.NewActionStore(exec, nil, agentfacts.ActionStoreConfig{})
	defer store.Close()
	var rows []agentfacts.AgentAction
	for ent, serr := range store.ScanAgentAction(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		if ent != nil && ent.AgentAction.Has {
			rows = append(rows, ent.AgentAction.Val)
		}
	}
	assert.Len(t, rows, want)
	var finals int
	for _, row := range rows {
		assert.Equal(t, "agentAction", row.Kind)
		assert.True(t, row.Test)
		if row.Decision == "final" {
			finals++
		}
	}
	assert.Equal(t, 2, finals, "the query and the command each reached a final phase")
}
