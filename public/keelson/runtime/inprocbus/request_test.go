package inprocbus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// A handler that blocks does not hold the requester past its timeout, and
// its reply after the timeout is dropped.
func TestRequest_ASlowHandlerDoesNotHoldTheRequester(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	release := make(chan struct{})
	replied := make(chan error, 1)
	_, err := bus.Subscribe("slow.svc", func(msg *app.Msg) {
		<-release
		replied <- bus.Publish(msg.Reply, []byte("late"))
	})
	require.NoError(t, err)

	t0 := time.Now()
	reply, err := bus.RequestWithTimeout("slow.svc", nil, 50*time.Millisecond)
	assert.ErrorIs(t, err, ErrTimeout)
	assert.Nil(t, reply)
	assert.Less(t, time.Since(t0), 5*time.Second)

	close(release)
	select {
	case <-replied:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never finished")
	}
	assert.Empty(t, inboxSubscriptions(inst), "the inbox is gone; the late reply landed nowhere")
}

// Handlers that return within the timeout have all returned when Request
// does, the observer as well as the responder: a request keeps a publish's
// ordering.
func TestRequest_ReturnsAfterEveryHandlerReturned(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	var observed atomic.Bool
	_, err := bus.Subscribe("svc.q", func(msg *app.Msg) {
		_ = bus.Publish(msg.Reply, []byte("ok"))
	})
	require.NoError(t, err)
	_, err = bus.Subscribe("svc.>", func(*app.Msg) {
		time.Sleep(20 * time.Millisecond)
		observed.Store(true)
	})
	require.NoError(t, err)

	reply, err := bus.Request("svc.q", nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(reply))
	assert.True(t, observed.Load())
}

// A reply sent from a goroutine of the handler's own, after the handler
// returned, is still waited for.
func TestRequest_WaitsForAReplySentLater(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("async.svc", func(msg *app.Msg) {
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = bus.Publish(msg.Reply, []byte("later"))
		}()
	})
	require.NoError(t, err)

	reply, err := bus.RequestWithTimeout("async.svc", nil, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "later", string(reply))
}

func TestRequestCtx_EndsWithTheContext(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(time.Minute)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("silent.svc", func(*app.Msg) {})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = bus.RequestCtx(ctx, "silent.svc", nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// A panicking responder costs the requester its timeout, not its process.
func TestRequest_APanickingHandlerIsATimeout(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("bad.svc", func(*app.Msg) { panic("boom") })
	require.NoError(t, err)
	_, err = bus.RequestWithTimeout("bad.svc", nil, 30*time.Millisecond)
	assert.ErrorIs(t, err, ErrTimeout)
}

func inboxSubscriptions(inst *Inst) (inboxes []string) {
	for _, s := range inst.Subscriptions() {
		if len(s.Pattern) >= len(InboxPrefix) && s.Pattern[:len(InboxPrefix)] == InboxPrefix {
			inboxes = append(inboxes, s.Pattern)
		}
	}
	return
}
