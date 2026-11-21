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
	reply, err := app.RequestWithin(t.Context(), bus, 50*time.Millisecond, "slow.svc", nil)
	assert.ErrorIs(t, err, ErrTimeout)
	assert.Nil(t, reply)
	assert.Less(t, time.Since(t0), 5*time.Second)

	close(release)
	select {
	case perr := <-replied:
		assert.NoError(t, perr, "a reply to a gone inbox is dropped, not refused")
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

	reply, err := bus.Request(t.Context(), "svc.q", nil)
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

	reply, err := app.RequestWithin(t.Context(), bus, 5*time.Second, "async.svc", nil)
	require.NoError(t, err)
	assert.Equal(t, "later", string(reply))
}

func TestRequest_EndsWithTheContext(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(time.Minute)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("silent.svc", func(*app.Msg) {})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = bus.Request(ctx, "silent.svc", nil)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, err, ErrTimeout, "a wait ended by ctx's deadline is a timeout")
}

// A handler of a request sees the requester's ctx, bounded by the wait and
// carrying its values, and it ends when the request returns.
func TestRequest_TheHandlerSeesTheRequestersContext(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	type key struct{}
	seen := make(chan context.Context, 1)
	_, err := bus.Subscribe("ctx.svc", func(msg *app.Msg) {
		seen <- msg.Context()
		_ = bus.Publish(msg.Reply, nil)
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), key{}, "v"), time.Minute)
	defer cancel()
	_, err = bus.Request(ctx, "ctx.svc", nil)
	require.NoError(t, err)
	hctx := <-seen
	assert.Equal(t, "v", hctx.Value(key{}))
	_, has := hctx.Deadline()
	assert.True(t, has)
	assert.Error(t, hctx.Err(), "the handler's ctx ends when the request returns")
}

// A panicking responder costs the requester its timeout, not its process.
func TestRequest_APanickingHandlerIsATimeout(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("bad.svc", func(*app.Msg) { panic("boom") })
	require.NoError(t, err)
	_, err = app.RequestWithin(t.Context(), bus, 30*time.Millisecond, "bad.svc", nil)
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
