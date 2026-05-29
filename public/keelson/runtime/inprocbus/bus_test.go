//go:build llm_generated_opus47

package inprocbus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func newInst(t *testing.T) (inst *Inst) {
	t.Helper()
	inst = NewInst(zerolog.Nop())
	return
}

// fullCaps grants pub+sub on every subject — used by tests that aren't
// exercising permission enforcement.
func fullCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: ">", Direction: app.CapDirectionBoth},
	}
	return
}

func TestInst_PublishSubscribe_Delivers(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	var got atomic.Pointer[app.Msg]
	unsub, err := bus.Subscribe("fs.dialog.read", func(msg *app.Msg) {
		got.Store(msg)
	})
	require.NoError(t, err)
	defer unsub()

	err = bus.Publish("fs.dialog.read", []byte("hello"))
	require.NoError(t, err)

	m := got.Load()
	require.NotNil(t, m)
	assert.Equal(t, "fs.dialog.read", m.Subject)
	assert.Equal(t, []byte("hello"), m.Payload)
}

func TestInst_PublishMatchesWildcards(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	var hits atomic.Int32
	unsub, err := bus.Subscribe("fs.>", func(msg *app.Msg) {
		hits.Add(1)
	})
	require.NoError(t, err)
	defer unsub()

	require.NoError(t, bus.Publish("fs.dialog.read", nil))
	require.NoError(t, bus.Publish("fs.handle.abc.read", nil))
	require.NoError(t, bus.Publish("kafka.produce.topic", nil)) // no match

	assert.Equal(t, int32(2), hits.Load())
}

func TestInst_PublishNoSubscribers_NoError(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	err := bus.Publish("fs.dialog.read", []byte("x"))
	require.NoError(t, err)
}

func TestInst_PublishInvalidSubject_Errors(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	err := bus.Publish("fs.>", nil) // wildcard in subject is invalid
	require.Error(t, err)
}

func TestInst_MultipleSubscribersAllReceive(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	var a, b atomic.Int32
	unsubA, err := bus.Subscribe("ch.query.boxer", func(msg *app.Msg) { a.Add(1) })
	require.NoError(t, err)
	defer unsubA()
	unsubB, err := bus.Subscribe("ch.>", func(msg *app.Msg) { b.Add(1) })
	require.NoError(t, err)
	defer unsubB()

	require.NoError(t, bus.Publish("ch.query.boxer", nil))
	assert.Equal(t, int32(1), a.Load())
	assert.Equal(t, int32(1), b.Load())
}

func TestInst_UnsubscribeStopsDelivery(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	var hits atomic.Int32
	unsub, err := bus.Subscribe("x.y", func(msg *app.Msg) { hits.Add(1) })
	require.NoError(t, err)

	require.NoError(t, bus.Publish("x.y", nil))
	unsub()
	require.NoError(t, bus.Publish("x.y", nil))

	assert.Equal(t, int32(1), hits.Load())
}

func TestInst_RequestReply_RoundTrip(t *testing.T) {
	inst := newInst(t)
	server := inst.NewClient("server", fullCaps())
	clientB := inst.NewClient("client", fullCaps())

	unsub, err := server.Subscribe("ch.query.boxer", func(msg *app.Msg) {
		require.NotEmpty(t, msg.Reply)
		_ = server.Publish(msg.Reply, append([]byte("ok:"), msg.Payload...))
	})
	require.NoError(t, err)
	defer unsub()

	reply, err := clientB.Request("ch.query.boxer", []byte("SELECT 1"))
	require.NoError(t, err)
	assert.Equal(t, "ok:SELECT 1", string(reply))
}

func TestInst_Request_TimesOutWhenNoResponder(t *testing.T) {
	inst := newInst(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	bus := inst.NewClient("test.app", fullCaps())

	_, err := bus.Request("nobody.listening", []byte("?"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTimeout)
}

func TestInst_Subscribe_RejectsMalformedPattern(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("a..b", func(msg *app.Msg) {})
	require.Error(t, err)
}

func TestInst_Subscribe_NilHandlerRejected(t *testing.T) {
	inst := newInst(t)
	bus := inst.NewClient("test.app", fullCaps())
	_, err := bus.Subscribe("a.b", nil)
	require.Error(t, err)
}
