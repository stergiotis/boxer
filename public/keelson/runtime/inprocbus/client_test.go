package inprocbus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

func TestClient_PublishDeniedWithoutCap(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub},
	})
	err := bus.Publish("ch.query.boxer", []byte("x"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionViolation)
}

func TestClient_PublishAllowedByCap(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionPub},
	})
	err := bus.Publish("fs.dialog.read", []byte("x"))
	require.NoError(t, err)
}

// TestClient_EnvelopeCarriesSenderInstance is ADR-0191 §SD2: the bus stamps
// the publisher's window on the envelope, so a service can attribute a
// request to a window instead of to an app.
//
// Two clients for the SAME app id is the case that matters — with one window
// open, Sender is already enough, and a test with one window would pass
// against a bus that stamped nothing.
func TestClient_EnvelopeCarriesSenderInstance(t *testing.T) {
	inst := newInst(t)
	caps := []app.SubjectFilter{{Pattern: "x.>", Direction: app.CapDirectionBoth}}
	sink := inst.NewClient("svc", caps)

	var got []uint64
	_, err := sink.Subscribe("x.y", func(msg *app.Msg) {
		assert.Equal(t, app.AppIdT("test.app"), msg.Sender)
		got = append(got, msg.SenderInstance)
	})
	require.NoError(t, err)

	w1 := inst.NewClient("test.app", caps)
	w1.SetInstanceKey(7)
	w2 := inst.NewClient("test.app", caps)
	w2.SetInstanceKey(9)

	require.NoError(t, w1.Publish("x.y", nil))
	require.NoError(t, w2.Publish("x.y", nil))
	assert.Equal(t, []uint64{7, 9}, got,
		"two windows of one app must be tellable apart on the envelope")
}

// TestClient_UnstampedClientPublishesZero pins what absence means. A
// service's own client, a CLI bootstrap and a test fake all skip
// SetInstanceKey; zero is "unattributed", and a reader must not read it as
// window zero.
func TestClient_UnstampedClientPublishesZero(t *testing.T) {
	inst := newInst(t)
	caps := []app.SubjectFilter{{Pattern: "x.>", Direction: app.CapDirectionBoth}}
	sink := inst.NewClient("svc", caps)

	var seen uint64 = 1
	_, err := sink.Subscribe("x.y", func(msg *app.Msg) { seen = msg.SenderInstance })
	require.NoError(t, err)

	require.NoError(t, inst.NewClient("test.app", caps).Publish("x.y", nil))
	assert.Zero(t, seen)
}

func TestClient_SubscribeDeniedWithoutCap(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "ch.>", Direction: app.CapDirectionPub},
	})
	_, err := bus.Subscribe("ch.query.boxer", func(msg *app.Msg) {})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionViolation)
}

func TestClient_SubscribeAllowedByCap(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "app.*.event.>", Direction: app.CapDirectionSub},
	})
	_, err := bus.Subscribe("app.play.event.row_selected", func(msg *app.Msg) {})
	require.NoError(t, err)
}

func TestClient_BothDirectionCoversPubAndSub(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "x.>", Direction: app.CapDirectionBoth},
	})
	err := bus.Publish("x.y", nil)
	require.NoError(t, err)
	_, err = bus.Subscribe("x.z", func(msg *app.Msg) {})
	require.NoError(t, err)
}

func TestClient_Request_PublishCapRequired(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "other.subject", Direction: app.CapDirectionPub},
	})
	_, err := bus.Request("ch.query.boxer", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionViolation)
}

func TestClient_Request_InboxSubscriptionBypasses_Cap(t *testing.T) {
	// Even when the client has no explicit cap covering _INBOX.*, Request
	// must succeed because the inbox subscribe path bypasses cap checks.
	inst := newInst(t)
	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionPub},
	})
	server := inst.NewClient("server", []app.SubjectFilter{
		{Pattern: "ch.query.boxer", Direction: app.CapDirectionSub},
		{Pattern: InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	unsub, err := server.Subscribe("ch.query.boxer", func(msg *app.Msg) {
		_ = server.Publish(msg.Reply, []byte("pong"))
	})
	require.NoError(t, err)
	defer unsub()

	reply, err := bus.Request("ch.query.boxer", []byte("ping"))
	require.NoError(t, err)
	assert.Equal(t, "pong", string(reply))
}

func TestInst_AuditSink_RecordsRequestOk(t *testing.T) {
	inst := newInst(t)
	sink := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(sink)

	server := inst.NewClient("server", fullCaps())
	bus := inst.NewClient("test.app", fullCaps())

	unsub, err := server.Subscribe("ch.query.boxer", func(msg *app.Msg) {
		_ = server.Publish(msg.Reply, []byte("pong"))
	})
	require.NoError(t, err)
	defer unsub()

	_, err = bus.Request("ch.query.boxer", []byte("ping"))
	require.NoError(t, err)

	require.Equal(t, 1, sink.Len())
	rec := sink.Records()[0]
	assert.Equal(t, app.AppIdT("test.app"), rec.AppId)
	assert.Equal(t, "ch.query.boxer", rec.Subject)
	assert.Equal(t, audit.AuditResultOk, rec.Result)
	assert.Equal(t, uint32(4), rec.RequestSizeB)
	assert.Equal(t, uint32(4), rec.ResponseSizeB)
}

// TestInst_AuditSink_RecordsTheRequestingWindow is ADR-0191 §SD4 on the
// audit path: the record carries the window that asked, read off the client
// the host stamped at Open.
//
// Audit is the highest-volume app-attributed kind and the one that most
// needed this — with two windows of one app open, AppId alone cannot say
// which spoke, so the test uses two clients for one app id rather than one.
func TestInst_AuditSink_RecordsTheRequestingWindow(t *testing.T) {
	inst := newInst(t)
	sink := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(sink)

	server := inst.NewClient("server", fullCaps())
	unsub, err := server.Subscribe("ch.query.boxer", func(msg *app.Msg) {
		_ = server.Publish(msg.Reply, []byte("pong"))
	})
	require.NoError(t, err)
	defer unsub()

	w1 := inst.NewClient("test.app", fullCaps())
	w1.SetInstanceKey(2)
	w2 := inst.NewClient("test.app", fullCaps())
	w2.SetInstanceKey(5)

	_, err = w1.Request("ch.query.boxer", []byte("ping"))
	require.NoError(t, err)
	_, err = w2.Request("ch.query.boxer", []byte("ping"))
	require.NoError(t, err)

	require.Equal(t, 2, sink.Len())
	recs := sink.Records()
	assert.Equal(t, uint64(2), recs[0].InstanceKey)
	assert.Equal(t, uint64(5), recs[1].InstanceKey)
	assert.Equal(t, recs[0].AppId, recs[1].AppId, "same app, and only the window tells them apart")
}

// TestInst_AuditSink_ServiceClientHasNoWindow pins zero as unattributed
// rather than as window zero. A service mints its own client and never gets
// an instance key, which is most of what an idle runtime's audit trail is.
func TestInst_AuditSink_ServiceClientHasNoWindow(t *testing.T) {
	inst := newInst(t)
	sink := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(sink)

	server := inst.NewClient("server", fullCaps())
	unsub, err := server.Subscribe("ch.query.boxer", func(msg *app.Msg) {
		_ = server.Publish(msg.Reply, nil)
	})
	require.NoError(t, err)
	defer unsub()

	_, err = inst.NewClient("runtime.appletstore", fullCaps()).Request("ch.query.boxer", nil)
	require.NoError(t, err)

	require.Equal(t, 1, sink.Len())
	assert.Zero(t, sink.Records()[0].InstanceKey)
}

func TestInst_AuditSink_RecordsRequestDenied(t *testing.T) {
	inst := newInst(t)
	sink := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(sink)

	bus := inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: "fs.>", Direction: app.CapDirectionPub},
	})
	_, err := bus.Request("ch.query.boxer", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionViolation)

	require.Equal(t, 1, sink.Len())
	assert.Equal(t, audit.AuditResultDenied, sink.Records()[0].Result)
}

func TestInst_AuditSink_RecordsRequestTimeout(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(20 * time.Millisecond)
	sink := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(sink)

	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Request("nobody.here", nil)
	require.Error(t, err)

	require.Equal(t, 1, sink.Len())
	assert.Equal(t, audit.AuditResultTimeout, sink.Records()[0].Result)
}

func TestInst_AuditSink_NilDoesNotPanic(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(20 * time.Millisecond)
	bus := inst.NewClient("test.app", fullCaps())
	// No audit sink set — should not panic.
	_, _ = bus.Request("nobody", nil)
}

func TestInst_AuditSink_SwapAtRuntime(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(20 * time.Millisecond)
	first := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(first)
	bus := inst.NewClient("test.app", fullCaps())
	_, _ = bus.Request("nobody1", nil)
	second := audit.NewInMemoryAuditSink()
	inst.SetAuditSink(second)
	_, _ = bus.Request("nobody2", nil)
	assert.Equal(t, 1, first.Len())
	assert.Equal(t, 1, second.Len())
}

func TestClient_AllocateInbox_UniquePerCall(t *testing.T) {
	inst := newInst(t)
	c := &Client{inst: inst, appId: "x"}
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		inbox := c.allocateInbox()
		_, dup := seen[inbox]
		require.False(t, dup, "duplicate inbox %s", inbox)
		seen[inbox] = struct{}{}
	}
}

// TestRequestWithTimeout_HonoursTheCallerSTimeout is the regression this
// method was written to fix and then reintroduced by renaming the function
// without using its new parameter — the select still waited the Inst default,
// so a caller asking for longer got the default and a file dialog failed while
// the picker was still on screen. Compiles and vets clean either way; only a
// test that actually waits can tell.
func TestRequestWithTimeout_HonoursTheCallerSTimeout(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(20 * time.Millisecond)

	responder := inst.NewClient("responder", []app.SubjectFilter{
		{Pattern: "slow.op", Direction: app.CapDirectionSub, Reason: "test"},
		{Pattern: InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "test"},
	})
	// The reply goes out from a GOROUTINE, not inline. inprocbus dispatches a
	// publish to its subscribers synchronously on the caller's own goroutine,
	// so an inline reply is already sitting in the channel by the time Request
	// reaches its select — the timeout branch is never taken and the test
	// passes whatever the timeout says. Answering off-thread is what makes the
	// wait real.
	_, err := responder.Subscribe("slow.op", func(msg *app.Msg) {
		reply := msg.Reply
		go func() {
			// Well after the Inst default, well inside what the caller asks.
			time.Sleep(120 * time.Millisecond)
			_ = responder.Publish(reply, []byte("late but welcome"))
		}()
	})
	require.NoError(t, err)

	caller := inst.NewClient("caller", []app.SubjectFilter{
		{Pattern: "slow.op", Direction: app.CapDirectionPub, Reason: "test"},
	})

	// The default would have given up at 20ms.
	reply, err := caller.RequestWithTimeout("slow.op", nil, 3*time.Second)
	require.NoError(t, err, "the caller's longer timeout must be the one that applies")
	assert.Equal(t, "late but welcome", string(reply))

	// And a non-positive duration still means "use the default", which the
	// slow responder outlasts.
	_, err = caller.RequestWithTimeout("slow.op", nil, 0)
	require.Error(t, err, "d <= 0 must fall back to the Inst default")
}
