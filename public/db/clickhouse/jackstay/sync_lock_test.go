package jackstay

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOsFiles_Lock(t *testing.T) {
	name := filepath.Join(t.TempDir(), "plan.json")
	unlock, err := OsFiles{}.Lock(name)
	require.NoError(t, err)
	_, err = OsFiles{}.Lock(name)
	assert.ErrorContains(t, err, "another run holds the plan", "this process still runs")
	require.NoError(t, unlock())
	_, err = os.Stat(name + ".lock")
	assert.ErrorIs(t, err, os.ErrNotExist)

	host, _ := os.Hostname()
	write := func(h lockHolder) {
		data, merr := json.Marshal(h)
		require.NoError(t, merr)
		require.NoError(t, os.WriteFile(name+".lock", data, 0o644))
	}

	// Left by a process of this host that has ended: taken over.
	write(lockHolder{Pid: 1 << 30, Host: host, At: time.Unix(1, 0)})
	unlock, err = OsFiles{}.Lock(name)
	require.NoError(t, err)
	require.NoError(t, unlock())

	// Held on another host, or unreadable: refused, and left in place.
	write(lockHolder{Pid: 1 << 30, Host: host + "-elsewhere", At: time.Unix(1, 0)})
	_, err = OsFiles{}.Lock(name)
	assert.ErrorContains(t, err, "another run holds the plan")
	require.NoError(t, os.WriteFile(name+".lock", []byte("{"), 0o644))
	_, err = OsFiles{}.Lock(name)
	assert.ErrorContains(t, err, "another run holds the plan")
	_, err = os.Stat(name + ".lock")
	assert.NoError(t, err)
}

// A run of a plan another run holds is refused before anything is written,
// and a run releases its lock when it ends, on error too.
func TestRunSync_Lock(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	newPrep := func() (prep SyncPrepared) {
		prep = SyncPrepared{Plan: Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://s/"}, Target: Endpoint{URL: "http://d/"},
			Tables: []PlanTable{*pt}, SyncRun: &SyncRun{RunId: "run"}}}
		prep.Chosen = []*PlanTable{&prep.Plan.Tables[0]}
		return
	}
	quiet := quietClient()

	unlock, err := OsFiles{}.Lock(planPath)
	require.NoError(t, err)
	prep := newPrep()
	_, err = RunSync(context.Background(), ServerSource(quiet), quiet, &prep, planPath, DefaultSyncOptions(), time.Now)
	assert.ErrorContains(t, err, "another run holds the plan")
	_, serr := os.Stat(JournalPath(planPath))
	assert.ErrorIs(t, serr, os.ErrNotExist, "no journal was opened")
	require.NoError(t, unlock())

	// Refused for its settings after the lock was taken: released anyway.
	j, err := OpenJournal(JournalPath(planPath), "run")
	require.NoError(t, err)
	require.NoError(t, j.RecordStart("s.t", false, TableSync{Mode: SyncModeRepair}, "", time.Now()))
	require.NoError(t, j.Close())
	prep = newPrep()
	_, err = RunSync(context.Background(), ServerSource(quiet), quiet, &prep, planPath, DefaultSyncOptions(), time.Now)
	assert.ErrorContains(t, err, "other settings")
	_, serr = os.Stat(planPath + ".lock")
	assert.ErrorIs(t, serr, os.ErrNotExist)
}

// The prepared request's Restart begins a new run.
func TestRunSync_RestartFromRequest(t *testing.T) {
	pt := singleChunkTable("t", "t")
	pt.Sync = &TableSync{Mode: SyncModeFull, Existing: ExistingPolicyAppend}
	planPath := filepath.Join(t.TempDir(), "plan.json")
	prep := SyncPrepared{Request: SyncRequest{Restart: true}, Plan: Plan{FormatVersion: PlanFormatVersion, Source: Endpoint{URL: "http://s/"}, Target: Endpoint{URL: "http://d/"},
		Tables: []PlanTable{*pt}, SyncRun: &SyncRun{RunId: "run"}}}
	prep.Chosen = []*PlanTable{&prep.Plan.Tables[0]}
	// The old run's start entry would refuse these settings; a new run has
	// none.
	j, err := OpenJournal(JournalPath(planPath), "run")
	require.NoError(t, err)
	require.NoError(t, j.RecordStart("s.t", false, TableSync{Mode: SyncModeRepair}, "", time.Now()))
	require.NoError(t, j.Close())
	quiet := quietClient()
	out, err := RunSync(context.Background(), ServerSource(quiet), quiet, &prep, planPath, DefaultSyncOptions(), time.Now)
	require.NoError(t, err)
	assert.False(t, out.Resumed)
	assert.NotEqual(t, "run", out.Run.RunId)
}
