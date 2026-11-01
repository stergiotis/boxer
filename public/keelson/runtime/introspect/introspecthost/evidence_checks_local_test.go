package introspecthost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Over clickhouse-local: L34 holds on a host whose trail shares boxer.facts
// with other writers under the same origin. Every writer of the table
// carries the run (ADR-0277's Update of 2026-10-30), and the law is over
// the trail's own kinds (ADR-0327's Update of the same date), so a metrics
// row of the run is not a row the trail failed to seal.
func TestEvidenceChecksL34HoldsBesideOtherWritersOfTheRun(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")
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
	const run = "run-shared"
	rec := trail.NewRecorder(exec, run, zerolog.Nop())
	t.Cleanup(rec.Close)
	require.NoError(t, rec.LlmCall(time.Now(), trail.Context{Origin: rec.OriginOf("apps/chat", 2)}, trail.LlmCall{CallId: "c1"}))
	require.NoError(t, rec.Flush(ctx))

	sysm := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(sysm.Close)
	require.NoError(t, sysm.Begin(77, time.Now().UTC(), sysmfacts.SysmetricsEnvelope{NaturalKey: []byte("host-a")}).
		AddOrigin(sysmfacts.Origin{Id: 77, Run: run}).
		AddSysMem(sysmfacts.SysMem{Id: 77, NaturalKey: []byte("host-a"), Kind: "sysMem", Host: "host-a"}).Commit())
	_, err = sysm.Flush(ctx)
	require.NoError(t, err)

	rows := readEvidenceChecks(t, Deps{Trail: rec, Run: run})
	assert.True(t, rows["trail/"+run+"/"+LawTrailDurable].holds)
	l34 := rows["trail/"+run+"/L34"]
	assert.True(t, l34.holds, "a metrics row of the run is not the trail's: %s", l34.why)
	assert.Empty(t, l34.why)
}
