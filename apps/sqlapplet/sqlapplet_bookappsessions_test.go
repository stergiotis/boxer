package sqlapplet

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// sessionsTrail is a facts store whose trail holds fixed sessions.
type sessionsTrail struct {
	*factsstore.InMemoryFactsStore
	runs []factsstore.AppRunRow
}

func (inst sessionsTrail) AppRuns(context.Context, factsstore.AppTrailFilter) ([]factsstore.AppRunRow, error) {
	return inst.runs, nil
}
func (inst sessionsTrail) AppLogTail(context.Context, factsstore.AppTrailFilter, uint32) ([]factsstore.AppLogRow, error) {
	return nil, nil
}
func (inst sessionsTrail) AppAuditSummary(context.Context, factsstore.AppTrailFilter) ([]factsstore.AppAuditRow, error) {
	return nil, nil
}

// setPrelude splits a buffer's `SET param_x = '…';` prelude into params, the
// form the engine binds them in.
func setPrelude(sql string) (body string, params map[string]string) {
	re := regexp.MustCompile(`(?m)^SET param_(\w+) = '([^']*)';\s*$`)
	params = map[string]string{}
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		params[m[1]] = m[2]
	}
	return strings.TrimSpace(re.ReplaceAllString(sql, "")), params
}

// Every ending the buffer distinguishes, drawn from fixed sessions: a closed
// one, one cut short with a later heartbeat, one with none, and one whose
// start fell before the look-back. The `apps` knob drops a lane by name.
func TestAppSessionsBookExecutes(t *testing.T) {
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	defs, errs := ParseBook("sqlapplet", help.MustSub(bookFS, "book"))
	require.Empty(t, errs)
	var def *AppletDef
	for _, d := range defs {
		if d.Slug == "app-sessions" {
			def = d
		}
	}
	require.NotNil(t, def)

	now := time.Now().UTC().Truncate(time.Second)
	at := func(m int) time.Time { return now.Add(time.Duration(m) * time.Minute) }
	trail := sessionsTrail{InMemoryFactsStore: factsstore.NewInMemoryFactsStore(), runs: []factsstore.AppRunRow{
		{RunId: "r1", AppId: "x/play", InstanceKey: 1, StartedAt: at(-60), StoppedAt: at(-50), StopReason: "user-close", RunSeenAt: at(-40)},
		{RunId: "r2", AppId: "x/play", InstanceKey: 1, StartedAt: at(-30), RunSeenAt: at(-20)},
		{RunId: "r3", AppId: "x/tally", InstanceKey: 2, StartedAt: at(-10)},
		{RunId: "r4", AppId: "x/launcher", InstanceKey: 1, StoppedAt: at(-5), StopReason: "shutdown"},
	}}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(60 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterAppTrail(reg, trail))
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: bus.NewClient("test.bookappsessions.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})}, logger)
	require.NoError(t, err)

	body, params := setPrelude(def.SQL)
	require.Equal(t, map[string]string{"apps": "all"}, params)
	run := func(apps string) []string {
		t.Helper()
		out, _, qErr := e.QueryParams(context.Background(), "SELECT app, seconds, ending FROM ("+body+")", "TabSeparated", map[string]string{"apps": apps})
		require.NoError(t, qErr)
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	rows := run("all")
	require.Len(t, rows, 4)
	assert.Equal(t, "play\t600\tclosed: user-close", rows[0])
	assert.Equal(t, "play\t600\tno close: process last seen", rows[1], "ends at the heartbeat, not the next session")
	assert.Equal(t, "tally\t1\tno close: no later sign of the process", rows[2])
	assert.Equal(t, "launcher\t1\tno start in the look-back; closed: shutdown", rows[3],
		"a start outside the look-back is a mark at the close, not a bar from the look-back's edge")

	assert.Len(t, run("-launcher"), 3)
	assert.Len(t, run("tally"), 1)
}
