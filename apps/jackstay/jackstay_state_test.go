package jackstay

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	jkcli "github.com/stergiotis/boxer/public/app/commands/jackstay"
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// testPlan is a plan that passes Validate, between two made-up servers.
func testPlan(tables ...jk.PlanTable) (p jk.Plan) {
	return jk.Plan{
		FormatVersion: jk.PlanFormatVersion,
		Source:        jk.Endpoint{URL: "http://src.example:8123/", User: "default"},
		Target:        jk.Endpoint{URL: "http://dst.example:8123/", User: "default"},
		Tables:        tables,
	}
}

func table(db string, name string) (t jk.PlanTable) {
	t.Source = datacatalog.TableRef{Database: db, Name: name}
	t.Target = t.Source
	t.Verdict = jk.VerdictIdentical
	return
}

// withFiles gives inst a data area on an in-process bus, served by a real fs
// broker over a temporary directory.
func withFiles(t *testing.T, inst *App) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	svc, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	svc.SetAppDataRoot(t.TempDir())
	inst.files = fsbroker.NewAppDataClient(bus.NewClient(AppId, []app.SubjectFilter{
		{Pattern: fsbroker.SubjectAppDataPrefix + ">", Direction: app.CapDirectionPub, Reason: "test data area"},
	}))
}

func writePlan(t *testing.T, inst *App, name string, p jk.Plan) {
	t.Helper()
	data, err := p.Marshal()
	require.NoError(t, err)
	_, err = inst.files.Write(name, data)
	require.NoError(t, err)
}

func settled(t *testing.T, running func() bool) {
	t.Helper()
	require.Eventually(t, func() bool { return !running() }, 5*time.Second, 5*time.Millisecond)
}

// Every job that reads or writes the plan, or the servers it is planned
// against, makes the window busy, and the busy window starts no other; the
// read-only pre-flight does not.
func TestEveryPlanJobMakesTheWindowBusy(t *testing.T) {
	block := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	type job struct {
		name   string
		start  func(inst *App)
		cancel func(inst *App)
	}
	jobs := []job{
		{"discover", func(inst *App) {
			inst.discoverJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*discovered, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.discoverJob.Cancel() }},
		{"structure", func(inst *App) {
			inst.structureJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*stepResult, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.structureJob.Cancel() }},
		{"diff", func(inst *App) {
			inst.diffJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*stepResult, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.diffJob.Cancel() }},
		{"sync", func(inst *App) {
			inst.syncJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*stepResult, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.syncJob.Cancel() }},
		{"file", func(inst *App) {
			inst.fileJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*fileResult, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.fileJob.Cancel() }},
		{"reload", func(inst *App) {
			inst.reloadJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*fileResult, error) { return nil, block(ctx) })
		}, func(inst *App) { inst.reloadJob.Cancel() }},
	}
	for _, j := range jobs {
		inst := phaseApp()
		p := testPlan(table("a", "t"))
		inst.adoptPlan(&p, planSaved{})
		require.True(t, inst.canCompare(), j.name)
		j.start(inst)
		assert.True(t, inst.planBusy(), j.name)
		assert.False(t, inst.canCompare(), j.name)
		j.cancel(inst)
		settled(t, inst.planBusy)
	}

	inst := phaseApp()
	inst.previewJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*preflightResult, error) { return nil, block(ctx) })
	assert.False(t, inst.planBusy(), "the pre-flight reads a copy of the plan")
	inst.previewJob.Cancel()
}

// Compare and Start refuse a stale plan, as the engine would; a plan with
// nothing in line has nothing to compare.
func TestCompareWaitsForAPlanItCanCompare(t *testing.T) {
	inst := phaseApp()
	p := testPlan(table("a", "t"))
	inst.adoptPlan(&p, planSaved{})
	assert.True(t, inst.canCompare())
	inst.stale = []string{"a.t: moved"}
	assert.False(t, inst.canCompare())

	pending := table("a", "u")
	pending.Verdict, pending.DDL = jk.VerdictCreate, []string{"CREATE TABLE a.u"}
	q := testPlan(pending)
	inst.adoptPlan(&q, planSaved{})
	assert.False(t, inst.canCompare(), "no table is in line before the DDL")
}

// An opened plan brings its databases, their targets and its row filters, so
// planning it again plans the same tables the same way.
func TestAnOpenedPlanKeepsItsChoices(t *testing.T) {
	inst := phaseApp()
	tb := table("a", "t")
	tb.Target.Database, tb.Filter = "a2", "x > 1"
	p := testPlan(tb)
	p.Selection = jk.Selection{Databases: []string{"a"}, DatabaseMap: map[string]string{"a": "a2"},
		Filters: map[string]string{"a.t": "x > 1"}, LeewayOnly: true}
	inst.filters["b.stale"] = new("y = 2")
	inst.adoptPlan(&p, planSaved{name: "p.json"})

	inst.disc = &discovered{src: jk.Inventory{Databases: []jk.DatabaseInfo{{Name: "a"}, {Name: "b"}, {Name: "system"}}}}
	inst.seedDatabases()
	sel := inst.selection()
	assert.Equal(t, []string{"a"}, sel.Databases, "a database the discovery adds starts unticked")
	assert.Equal(t, map[string]string{"a": "a2"}, sel.DatabaseMap)
	assert.Equal(t, map[string]string{"a.t": "x > 1"}, sel.Filters, "the previous plan's filters are gone")
	assert.True(t, sel.LeewayOnly)

	// A plan that chose every database names them through its tables.
	q := testPlan(table("c", "t"))
	inst.adoptPlan(&q, planSaved{name: "q.json"})
	inst.disc = &discovered{src: jk.Inventory{Databases: []jk.DatabaseInfo{{Name: "c"}, {Name: "d"}}}}
	inst.seedDatabases()
	assert.Equal(t, []string{"c"}, inst.selection().Databases)
}

// A database whose name holds a dot keeps its tables' filters.
func TestAFilterOfADottedDatabaseIsKept(t *testing.T) {
	inst := phaseApp()
	tb := table("a.b", "t")
	tb.Filter = "x > 1"
	p := testPlan(tb)
	p.Selection = jk.Selection{Databases: []string{"a.b"}, Filters: map[string]string{"a.b.t": "x > 1"}}
	inst.adoptPlan(&p, planSaved{name: "p.json"})
	inst.disc = &discovered{src: jk.Inventory{Databases: []jk.DatabaseInfo{{Name: "a.b"}, {Name: "a"}}}}
	inst.seedDatabases()
	assert.Equal(t, map[string]string{"a.b.t": "x > 1"}, inst.selection().Filters)
}

// A new plan replaces whatever the window knew about the previous one.
func TestAdoptingAPlanForgetsThePrevious(t *testing.T) {
	inst := phaseApp()
	p := testPlan(table("a", "t"))
	inst.adoptPlan(&p, planSaved{name: "a.json"})
	inst.stale = []string{"a.t: the sorting key changed"}
	inst.skipped = []string{"a.t: skipped"}
	inst.syncStopped = true
	inst.syncStarted, inst.syncFinished = time.Now(), time.Now()
	inst.logChunk(jk.ChunkResult{Table: "a.t"})
	inst.lastDisks = &jk.DiskReport{}
	inst.selected = p.Tables[0].Source
	inst.applyArmed, inst.syncArmed = true, true
	inst.restart = true
	require.Equal(t, phaseStale, inst.observePhase())

	q := testPlan(table("b", "t"))
	inst.adoptPlan(&q, planSaved{name: "b.json"})
	assert.Equal(t, phasePlanned, inst.observePhase(), "the previous plan's stale list does not make this one stale")
	assert.Empty(t, inst.skipped)
	assert.False(t, inst.syncStopped)
	assert.True(t, inst.syncStarted.IsZero() && inst.syncFinished.IsZero())
	assert.Empty(t, inst.chunkLogSnapshot())
	assert.Nil(t, inst.lastDisks)
	assert.Equal(t, datacatalog.TableRef{}, inst.selected)
	assert.False(t, inst.applyArmed || inst.syncArmed || inst.restart)
	assert.Equal(t, "b.json", inst.planName)
}

// The sync mode follows the opened plan's recommendation, and the
// existing-rows policy starts at its default.
func TestAnOpenedPlanTakesTheRecommendedMode(t *testing.T) {
	inst := phaseApp()
	inst.syncMode, inst.syncModeChosen, inst.existing = jk.SyncModeSample, true, jk.ExistingPolicyAppend
	tb := table("a", "t")
	tb.Diff = &jk.TableDiff{ComputedAt: time.Now(), Differing: []jk.ChunkDiff{{}}}
	p := testPlan(tb)
	inst.adoptPlan(&p, planSaved{})
	assert.Equal(t, jk.SyncModeRepair, inst.syncMode)
	assert.False(t, inst.syncModeChosen)
	assert.Equal(t, jk.ExistingPolicyRefuse, inst.existing)
}

// A step begun on one plan lands nothing once another plan is opened.
func TestAResultForAnotherPlanIsDropped(t *testing.T) {
	inst := phaseApp()
	a := testPlan(table("a", "t"))
	inst.adoptPlan(&a, planSaved{name: "a.json"})
	epoch := inst.planEpoch
	release := make(chan struct{})
	inst.structureJob.Start(nil, bgjob.Spec{}, func(ctx context.Context) (*stepResult, error) {
		<-release
		return &stepResult{plan: a, saved: planSaved{name: "a.json"}, epoch: epoch, replanned: true}, nil
	})
	b := testPlan(table("b", "t"))
	inst.adoptPlan(&b, planSaved{name: "b.json"})
	close(release)
	settled(t, inst.structureJob.Running)
	inst.takeResults()
	assert.Equal(t, "b", inst.plan.Tables[0].Source.Database)
	assert.Equal(t, "b.json", inst.planName)
}

// Only a seed's missing file names a plan still to be written; a plan that
// exists and cannot be read leaves the window without a name to save over it.
func TestOpenOutcome(t *testing.T) {
	missing := errors.Join(errors.New("unable to read plan"), fs.ErrNotExist)
	r, err := openOutcome("p.json", true, jk.Plan{}, missing)
	require.NoError(t, err)
	assert.Equal(t, "p.json", r.create)

	r, err = openOutcome("p.json", false, jk.Plan{}, missing)
	require.NoError(t, err)
	assert.ErrorIs(t, r.saved.err, errPlanGone)

	_, err = openOutcome("p.json", true, jk.Plan{}, errors.New("unable to decode plan"))
	assert.Error(t, err)

	pack := testPlan()
	pack.Source = jk.Endpoint{Pack: "dir"}
	_, err = openOutcome("p.json", true, pack, nil)
	assert.ErrorIs(t, err, errPackPlan)

	r, err = openOutcome("p.json", true, testPlan(), nil)
	require.NoError(t, err)
	assert.Empty(t, r.create)
}

// The start-up plan, through the data area: a missing file keeps its name, an
// unreadable one does not, a good one opens.
func TestTheSeedPlanIsNotOverwrittenUnread(t *testing.T) {
	inst := phaseApp()
	withFiles(t, inst)

	inst.startOpen("new.json", true)
	settled(t, inst.fileJob.Running)
	inst.takeResults()
	assert.Equal(t, "new.json", inst.planName, "a missing plan is written under the seed's name")
	assert.Nil(t, inst.plan)

	inst = phaseApp()
	withFiles(t, inst)
	_, err := inst.files.Write("bad.json", []byte("{not a plan"))
	require.NoError(t, err)
	inst.startOpen("bad.json", true)
	settled(t, inst.fileJob.Running)
	inst.takeResults()
	assert.Empty(t, inst.planName, "a plan that cannot be read is not the window's to save over")
	assert.Equal(t, bgjob.StateFailed, inst.fileJob.Snapshot().State)

	writePlan(t, inst, "good.json", testPlan(table("a", "t")))
	inst.startOpen("good.json", true)
	settled(t, inst.fileJob.Running)
	inst.takeResults()
	assert.Equal(t, "good.json", inst.planName)
	require.NotNil(t, inst.plan)
}

// A plan for other servers is a new plan with a name of its own.
func TestPlanningOtherServersStartsANewPlan(t *testing.T) {
	name, fresh := structurePlanName(true, true, "a.json")
	assert.Equal(t, "a.json", name)
	assert.False(t, fresh)
	name, fresh = structurePlanName(true, false, "a.json")
	assert.Empty(t, name, "the open plan's file is not overwritten")
	assert.True(t, fresh)
	name, fresh = structurePlanName(false, false, "seed.json")
	assert.Equal(t, "seed.json", name)
	assert.True(t, fresh)

	inst := phaseApp()
	p := testPlan()
	inst.adoptPlan(&p, planSaved{})
	assert.True(t, inst.planServes(jk.Endpoint{URL: "src.example:8123", User: "default"}, jk.Endpoint{URL: "http://dst.example:8123", User: "default"}))
	assert.False(t, inst.planServes(jk.Endpoint{URL: "other:8123", User: "default"}, p.Target))
}

// endSync runs a sync job whose worker does what compute does, with the
// bookkeeping startSync does around it, and takes the frame's results.
func endSync(t *testing.T, inst *App, compute func(ctx context.Context) (*stepResult, error), cancel bool) {
	t.Helper()
	inst.syncEpoch = inst.planEpoch
	inst.syncJob.Start(nil, bgjob.Spec{}, compute)
	inst.syncWasRunning = true
	if cancel {
		inst.syncJob.Cancel()
	}
	settled(t, inst.syncJob.Running)
	inst.takeResults()
}

// A sync that fails or is cancelled saved its run in the plan file; the
// window reads it back, so the next start resumes that run, and shows the
// sync as stopped.
func TestAStoppedSyncReadsItsRunBack(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		inst := phaseApp()
		withFiles(t, inst)
		p := testPlan(table("a", "t"))
		inst.adoptPlan(&p, planSaved{name: "p.json"})
		saved := p
		saved.SyncRun = &jk.SyncRun{RunId: "run-1", StartedAt: time.Now().UTC()}
		writePlan(t, inst, "p.json", saved)

		endSync(t, inst, func(ctx context.Context) (*stepResult, error) {
			if cancel {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return nil, errors.New("connection reset")
		}, cancel)
		assert.True(t, inst.syncStopped, "cancel=%v", cancel)
		assert.Equal(t, phaseIncomplete, inst.observePhase(), "cancel=%v", cancel)
		settled(t, inst.reloadJob.Running)
		inst.takeResults()
		require.NotNil(t, inst.plan.SyncRun, "cancel=%v", cancel)
		assert.Equal(t, "run-1", inst.plan.SyncRun.RunId)
		assert.Equal(t, phaseIncomplete, inst.observePhase())
	}

	inst := phaseApp()
	p := testPlan(table("a", "t"))
	inst.adoptPlan(&p, planSaved{name: "p.json"})
	epoch := inst.planEpoch
	endSync(t, inst, func(ctx context.Context) (*stepResult, error) {
		return &stepResult{plan: p, note: "nothing to sync", epoch: epoch}, nil
	}, false)
	assert.False(t, inst.syncStopped, "a sync that hands back a result did not stop")
	assert.False(t, inst.reloadJob.Running())
	assert.False(t, inst.syncFinished.IsZero())
}

// The confirmation is for the choices it was armed under.
func TestTheSyncConfirmationFollowsItsChoices(t *testing.T) {
	inst := phaseApp()
	p := testPlan(table("a", "t"))
	inst.adoptPlan(&p, planSaved{})
	inst.syncArmed, inst.syncArmedKey = true, inst.armKey()
	inst.keepArmed()
	assert.True(t, inst.syncArmed)
	inst.compression = "gzip"
	inst.keepArmed()
	assert.False(t, inst.syncArmed, "a changed choice disarms")

	inst.syncArmed, inst.applyArmed = true, true
	inst.takeStepResult(&stepResult{plan: p, epoch: inst.planEpoch})
	assert.False(t, inst.syncArmed || inst.applyArmed, "a new plan state disarms")
}

// The pre-flight waits for the choices to settle before it runs.
func TestThePreflightWaitsForTheChoicesToSettle(t *testing.T) {
	inst := phaseApp()
	p := testPlan(table("a", "t"))
	inst.adoptPlan(&p, planSaved{})
	inst.step = stepSync
	// A sample that does not parse stops the pre-flight before any server.
	inst.syncMode, inst.sampleText = jk.SyncModeSample, "1/"
	t0 := time.Now()
	wait := inst.schedulePreflight(t0)
	assert.Equal(t, preflightSettle, wait)
	assert.Empty(t, inst.preflightKey)
	inst.sampleText = "1/1"
	assert.Equal(t, preflightSettle, inst.schedulePreflight(t0.Add(400*time.Millisecond)), "a keystroke restarts the wait")
	inst.sampleText = "1/"
	inst.schedulePreflight(t0.Add(500 * time.Millisecond))
	assert.Zero(t, inst.schedulePreflight(t0.Add(time.Second)))
	assert.Equal(t, inst.settingsKey(), inst.preflightKey)
	assert.NotEmpty(t, inst.lastError)
	assert.False(t, inst.previewJob.Running())
}

// Every edge the window can cause on success is declared; a failed or
// cancelled step returns to its start along declared edges.
func TestEveryEdgeTheWindowCausesIsDeclared(t *testing.T) {
	m := newPhaseMachine()
	declared := make(map[[2]phaseE]bool, 64)
	for e := range m.Edges() {
		declared[[2]phaseE{e.From, e.To}] = true
	}
	settledPlan := []phaseE{phaseDDLPending, phasePlanned, phaseDiffers, phaseIdentical, phaseSynced, phaseIncomplete}
	edges := [][2]phaseE{
		{phaseIdle, phaseDiscovering}, {phaseIdle, phaseOpening},
		{phaseDiscovering, phaseDiscovered},
		{phaseDiscovered, phaseDiscovering}, {phaseDiscovered, phasePlanning}, {phaseDiscovered, phaseOpening},
		{phaseOpening, phaseIdle},
		{phasePlanning, phaseDDLPending}, {phasePlanning, phasePlanned}, {phasePlanning, phaseStale},
		{phaseComparing, phaseDiffers}, {phaseComparing, phaseIdentical}, {phaseComparing, phaseStale},
		{phaseSyncing, phaseSynced}, {phaseSyncing, phaseIncomplete}, {phaseSyncing, phaseStale},
		{phaseStale, phasePlanning}, {phaseStale, phaseOpening},
	}
	for _, ph := range settledPlan {
		// Open, import, plan again, compare and start from every settled phase
		// with a plan; a sync that copies nothing leaves the phase as it was.
		edges = append(edges, [2]phaseE{phaseOpening, ph}, [2]phaseE{ph, phaseOpening}, [2]phaseE{ph, phasePlanning},
			[2]phaseE{ph, phaseComparing}, [2]phaseE{ph, phaseSyncing})
		if ph != phaseSynced && ph != phaseIncomplete {
			edges = append(edges, [2]phaseE{phaseSyncing, ph})
		}
	}
	for _, e := range edges {
		assert.True(t, declared[e], "%s → %s", e[0], e[1])
	}
	for _, back := range [][2]phaseE{
		{phaseDiscovering, phaseIdle}, {phaseOpening, phaseDiscovered}, {phasePlanning, phaseDiscovered},
		{phasePlanning, phaseDiffers}, {phaseComparing, phasePlanned}, {phaseComparing, phaseSynced},
	} {
		assert.True(t, m.CanReach(back[0], back[1]), "%s back to %s", back[0], back[1])
	}
}

// Every shell hint reproduces the window's choices, and the CLI takes it.
func TestEveryShellHintParses(t *testing.T) {
	inst := phaseApp()
	tb := table("a", "t")
	tb.Filter = "x > 1 AND s = 'q'"
	p := testPlan(tb)
	p.Selection = jk.Selection{Databases: []string{"a"}, DatabaseMap: map[string]string{"a": "a2"}, Filters: map[string]string{"a.t": tb.Filter}}
	inst.adoptPlan(&p, planSaved{})
	inst.disc = &discovered{src: jk.Inventory{Databases: []jk.DatabaseInfo{{Name: "a"}}}}
	inst.seedDatabases()

	structure := inst.structureCommand()
	assert.Contains(t, structure, `--filter 'a.t=x > 1 AND s = '\''q'\'''`)
	assert.Contains(t, structure, "--map a=a2")

	inst.syncMode, inst.sampleText, inst.existing, inst.compression, inst.restart = jk.SyncModeSample, "1/10", jk.ExistingPolicyAppend, "", true
	sample := inst.syncCommand()
	assert.Contains(t, sample, "--mode sample --sample 1/10 --existing append --compression none --restart")
	inst.syncMode, inst.compression, inst.restart = jk.SyncModeRepair, "zstd", false
	repair := inst.syncCommand()
	assert.NotContains(t, repair, "--existing")
	assert.NotContains(t, repair, "--compression")

	inst.final = true
	for _, cmd := range []string{"discover" + inst.endpointArgs(), structure, sample, repair, "diff --plan {plan} --final",
		"apply-ddl --plan {plan} --confirm", "status --plan {plan} --disk"} {
		args := append([]string{"boxer"}, shellSplit(t, strings.ReplaceAll(cmd, "{plan}", "plan.json"))...)
		root := jkcli.NewCliCommand()
		ran := ""
		for _, sub := range root.Subcommands {
			sub.Action = func(c *cli.Context) error { ran = c.Command.Name; return nil }
		}
		a := &cli.App{Commands: root.Subcommands, ExitErrHandler: func(*cli.Context, error) {}}
		require.NoError(t, a.Run(args), cmd)
		assert.Equal(t, args[1], ran, cmd)
	}
}

// shellSplit splits a command line as a POSIX shell would, for the quoting
// shellQuote writes: words, and single-quoted runs.
func shellSplit(t *testing.T, s string) (words []string) {
	t.Helper()
	var cur strings.Builder
	in, has, escaped := false, false, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\'':
			in, has = !in, true
		case r == ' ' && !in:
			if has {
				words = append(words, cur.String())
			}
			cur.Reset()
			has = false
		case r == '\\' && !in:
			escaped, has = true, true
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	require.False(t, in, "unbalanced quote in %q", s)
	if has {
		words = append(words, cur.String())
	}
	return
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "http://a.example:8123/", shellQuote("http://a.example:8123/"))
	assert.Equal(t, "''", shellQuote(""))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
}
