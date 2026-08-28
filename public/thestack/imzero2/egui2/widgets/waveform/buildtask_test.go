package waveform_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/track"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/waveform"
)

// A recording task API: Spawn hands out one handle whose reports and
// terminal calls the test inspects, and whose context the test can cancel
// as a bus cancel would.
type fakeTasks struct {
	handle *fakeHandle
	opts   task.SpawnOpts
}

func (inst *fakeTasks) Spawn(ctx context.Context, opts task.SpawnOpts) (h task.HandleI, err error) {
	inst.opts = opts
	c, cancel := context.WithCancel(ctx)
	inst.handle = &fakeHandle{ctx: c, cancel: cancel}
	return inst.handle, nil
}
func (inst *fakeTasks) WatchAll(_ task.ObserverI) (unsubscribe func(), err error) { return nil, nil }
func (inst *fakeTasks) RequestCancel(_ task.TaskIdT, _ string) (err error)        { return nil }
func (inst *fakeTasks) ListInflight() (entries []task.InflightSnapshotEntry, err error) {
	return nil, nil
}
func (inst *fakeTasks) AppId() (id app.AppIdT)    { return }
func (inst *fakeTasks) InstanceKey() (key uint64) { return }
func (inst *fakeTasks) RunId() (id string)        { return }

type fakeHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	report []task.ProgressReport
	done   bool
	failed error
}

func (inst *fakeHandle) Id() (id task.TaskIdT)      { return "t1" }
func (inst *fakeHandle) Ctx() (ctx context.Context) { return inst.ctx }
func (inst *fakeHandle) Cancelled() (b bool)        { return inst.ctx.Err() != nil }
func (inst *fakeHandle) Report(p task.ProgressReport) {
	inst.mu.Lock()
	inst.report = append(inst.report, p)
	inst.mu.Unlock()
}
func (inst *fakeHandle) Note(_ string) {}
func (inst *fakeHandle) Done(_ []byte) (err error) {
	inst.mu.Lock()
	inst.done = true
	inst.mu.Unlock()
	inst.cancel()
	return nil
}
func (inst *fakeHandle) Error(err error, _ string) (rerr error) {
	inst.mu.Lock()
	inst.failed = err
	inst.mu.Unlock()
	inst.cancel()
	return nil
}
func (inst *fakeHandle) snapshot() (n int, last task.ProgressReport, done bool, failed error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = len(inst.report)
	if n > 0 {
		last = inst.report[n-1]
	}
	return n, last, inst.done, inst.failed
}

// slowSource delays every read so a short track builds over observable time.
type slowSource struct {
	pcm.SourceI
	delay time.Duration
}

func (inst slowSource) ReadFramesAtE(ctx context.Context, off int64, dst []float32) (n int, err error) {
	time.Sleep(inst.delay)
	return inst.SourceI.ReadFramesAtE(ctx, off, dst)
}

func openSlowBackgroundTrack(t *testing.T) (tr *track.Track) {
	t.Helper()
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	frames := format.DurationToFrames(20 * time.Second)
	src, err := pcm.NewSynthSourceE(format, frames, pcm.Silence())
	require.NoError(t, err)
	tr, err = track.OpenE(context.Background(), slowSource{SourceI: src, delay: 5 * time.Millisecond}, track.Options{
		Background: true, ChunkFrames: 4000, NoCache: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.CloseE() })
	return tr
}

func TestSpawnBuildTaskReportsAndCompletes(t *testing.T) {
	tr := openSlowBackgroundTrack(t)
	tasks := &fakeTasks{}
	h, err := waveform.SpawnBuildTask(context.Background(), tasks, tr, "test peaks")
	require.NoError(t, err)
	require.NotNil(t, h)
	require.Equal(t, waveform.BuildTaskKind, tasks.opts.Kind)
	require.True(t, tasks.opts.Cancellable)

	deadline := time.Now().Add(15 * time.Second)
	for {
		_, _, done, failed := tasks.handle.snapshot()
		require.NoError(t, failed)
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	n, last, done, _ := tasks.handle.snapshot()
	require.True(t, done, "the task did not complete")
	require.Greater(t, n, 1, "progress was reported along the way")
	require.Equal(t, last.Total, last.Current, "the final report is full")
	require.Equal(t, task.UnitItems, last.Unit)
	bp := tr.BuildProgress()
	require.True(t, bp.Complete)
	require.Positive(t, bp.Elapsed)
}

func TestSpawnBuildTaskCancelStopsTheBuild(t *testing.T) {
	tr := openSlowBackgroundTrack(t)
	tasks := &fakeTasks{}
	h, err := waveform.SpawnBuildTask(context.Background(), tasks, tr, "")
	require.NoError(t, err)
	require.NotNil(t, h)
	time.Sleep(60 * time.Millisecond)
	tasks.handle.cancel() // as a task.<id>.cancel from the bus would

	deadline := time.Now().Add(5 * time.Second)
	for tr.BuildProgress().Err == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	bp := tr.BuildProgress()
	require.ErrorIs(t, bp.Err, context.Canceled)
	require.False(t, bp.Complete)
	require.Less(t, bp.BuiltFrames, bp.TotalFrames)
	// The partial pyramid is still drawable.
	mins, maxs := make([]int8, 8), make([]int8, 8)
	require.GreaterOrEqual(t, tr.Peaks().Columns(0, bp.BuiltFrames, 0, mins, maxs), 0)
}

func TestSpawnBuildTaskOnACompleteBuildIsNoop(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	src, err := pcm.NewSynthSourceE(format, 8000, pcm.Silence())
	require.NoError(t, err)
	tr, err := track.OpenE(context.Background(), src, track.Options{NoCache: true})
	require.NoError(t, err)
	defer func() { _ = tr.CloseE() }()
	h, err := waveform.SpawnBuildTask(context.Background(), &fakeTasks{}, tr, "")
	require.NoError(t, err)
	require.Nil(t, h)
}

func TestBuildProgressEta(t *testing.T) {
	require.Equal(t, int64(0), track.EstimateEtaMs(0, 0, 100))
	require.Equal(t, int64(0), track.EstimateEtaMs(time.Second, 100, 100))
	// Half done after two seconds → two seconds to go.
	require.Equal(t, int64(2000), track.EstimateEtaMs(2*time.Second, 50, 100))
}
