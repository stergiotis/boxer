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
