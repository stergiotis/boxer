package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// captureObserver records the first TaskCreated it sees and signals.
type captureObserver struct {
	mu      sync.Mutex
	created taskcreated.TaskCreated
	got     bool
	done    chan struct{}
}

func newCaptureObserver() (o *captureObserver) {
	o = &captureObserver{done: make(chan struct{}, 1)}
	return
}

func (inst *captureObserver) OnCreated(c taskcreated.TaskCreated) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.got {
		return
	}
	inst.created = c
	inst.got = true
	select {
	case inst.done <- struct{}{}:
	default:
	}
}
func (inst *captureObserver) OnProgress(taskprogress.TaskProgress) {}
func (inst *captureObserver) OnDone(taskdone.TaskDone) {}
func (inst *captureObserver) OnError(taskerror.TaskError) {}
func (inst *captureObserver) OnCancel(taskcancel.TaskCancel) {}

func TestForApp_InjectsIdentityIntoTaskCreated(t *testing.T) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	producerC := busInst.NewClient("test.app", ProducerCaps())
	observerC := busInst.NewClient("test.observer", ObserverCaps())

	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, producerC, nil)
	mc.SetInstanceKey(42)
	mc.SetRunId("run-abc")

	obs := newCaptureObserver()
	unsub, err := WatchAll(observerC, obs)
	require.NoError(t, err)
	defer unsub()

	api := ForApp(mc)
	require.Equal(t, app.AppIdT("test.app"), api.AppId())
	require.EqualValues(t, 42, api.InstanceKey())
	require.Equal(t, "run-abc", api.RunId())

	h, err := api.Spawn(context.Background(), SpawnOpts{Kind: "test.kind"})
	require.NoError(t, err)

	select {
	case <-obs.done:
	case <-time.After(time.Second):
		t.Fatal("no TaskCreated observed within 1s")
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	assert.Equal(t, "test.app", obs.created.OwnerAppId)
	assert.EqualValues(t, 42, obs.created.OwnerTileKey)
	assert.Equal(t, "run-abc", obs.created.OwnerRunId)

	require.NoError(t, h.Done(nil))
}

func TestForApp_MountCancelPropagatesToHandle(t *testing.T) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	producerC := busInst.NewClient("test.app", ProducerCaps())

	mountCancel := make(chan struct{})
	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, producerC, mountCancel)

	api := ForApp(mc)
	h, err := api.Spawn(context.Background(), SpawnOpts{Kind: "k"})
	require.NoError(t, err)
	require.False(t, h.Cancelled(), "fresh handle is not cancelled")

	close(mountCancel)
	// The TaskApi observes mountCancel on a goroutine; give it a beat
	// to propagate before the assertion.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if h.Cancelled() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.True(t, h.Cancelled(), "mountCancel should cancel the handle's ctx")
	require.NoError(t, h.Done(nil))
}

func TestForApp_NoopFallbackWhenBusIsNoop(t *testing.T) {
	mc := app.NewStaticMountContext("test.app", zerolog.Nop(), nil, nil, nil)
	api := ForApp(mc)

	_, err := api.Spawn(context.Background(), SpawnOpts{Kind: "k"})
	// NoopBus errors at the cancel-subscription step; Spawn returns
	// that error directly.
	assert.Error(t, err)
}

func TestNoopTaskApi_AllMethodsError(t *testing.T) {
	api := &NoopTaskApi{}
	_, err := api.Spawn(context.Background(), SpawnOpts{Kind: "k"})
	assert.Error(t, err)
	_, err = api.WatchAll(nil)
	assert.Error(t, err)
	err = api.RequestCancel("id", "reason")
	assert.Error(t, err)
	_, err = api.ListInflight()
	assert.Error(t, err)
	assert.Empty(t, string(api.AppId()))
	assert.Zero(t, api.InstanceKey())
	assert.Empty(t, api.RunId())
}
