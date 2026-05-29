//go:build llm_generated_opus47

package capbroker

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func newBus(t *testing.T) (inst *inprocbus.Inst) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	return
}

// requesterCaps is the minimum the test apps need to call Request on
// runtime.cap.request.
func requesterCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: RequestSubject, Direction: app.CapDirectionPub, Reason: "test app may request caps"},
	}
	return
}

func TestNewBroker_NilInstRejected(t *testing.T) {
	_, err := NewBroker(nil, zerolog.Nop(), &AutoApprovePolicy{})
	require.Error(t, err)
}

func TestNewBroker_NilPolicyDefaultsToDenyAll(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), nil)
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())
	payload, err := MarshalRequest(GrantRequest{
		AppId:         "test.app",
		SubjectFilter: app.SubjectFilter{Pattern: "x.y", Direction: app.CapDirectionPub},
	})
	require.NoError(t, err)
	replyBytes, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	reply, err := UnmarshalReply(replyBytes)
	require.NoError(t, err)
	assert.False(t, reply.Granted)
	assert.Contains(t, reply.Reason, "deny-all")
}

func TestBroker_AutoApprove_GrantsAndExtendsCaps(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &AutoApprovePolicy{})
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())

	// Pre-grant: ch.query.boxer is denied.
	err = bus.Publish("ch.query.boxer", []byte("before"))
	require.Error(t, err)
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation)

	payload, err := MarshalRequest(GrantRequest{
		AppId: "test.app",
		SubjectFilter: app.SubjectFilter{
			Pattern:   "ch.query.boxer",
			Direction: app.CapDirectionPub,
			Reason:    "test wants to publish CH queries",
		},
	})
	require.NoError(t, err)
	replyBytes, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	reply, err := UnmarshalReply(replyBytes)
	require.NoError(t, err)
	assert.True(t, reply.Granted)
	assert.NotEmpty(t, reply.GrantId)

	// Post-grant: ch.query.boxer publishes succeed.
	err = bus.Publish("ch.query.boxer", []byte("after"))
	require.NoError(t, err)

	grants := broker.Grants()
	require.Len(t, grants, 1)
	assert.Equal(t, app.AppIdT("test.app"), grants[0].AppId)
	assert.Equal(t, "ch.query.boxer", grants[0].Filter.Pattern)
}

func TestBroker_DenyAll_ReplyCarriesReason(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &DenyAllPolicy{})
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId:         "test.app",
		SubjectFilter: app.SubjectFilter{Pattern: "x.y", Direction: app.CapDirectionPub},
	})
	replyBytes, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	reply, err := UnmarshalReply(replyBytes)
	require.NoError(t, err)
	assert.False(t, reply.Granted)
	assert.Contains(t, reply.Reason, "deny-all")
	assert.Empty(t, broker.Grants(), "deny should not record a grant")
}

func TestBroker_FuncPolicy_PerRequestDecision(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	policy := FuncPolicy(func(req GrantRequest) (d GrantDecision) {
		if req.SubjectFilter.Pattern == "ch.query.boxer" {
			d = GrantDecision{Granted: true, Reason: "CH query allowed"}
			return
		}
		d = GrantDecision{Granted: false, Reason: "default deny"}
		return
	})
	broker, err := NewBroker(inst, zerolog.Nop(), policy)
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())

	yesPayload, _ := MarshalRequest(GrantRequest{
		AppId: "test.app",
		SubjectFilter: app.SubjectFilter{
			Pattern: "ch.query.boxer", Direction: app.CapDirectionPub,
		},
	})
	yesReplyBytes, err := bus.Request(RequestSubject, yesPayload)
	require.NoError(t, err)
	yesReply, _ := UnmarshalReply(yesReplyBytes)
	assert.True(t, yesReply.Granted)

	noPayload, _ := MarshalRequest(GrantRequest{
		AppId: "test.app",
		SubjectFilter: app.SubjectFilter{
			Pattern: "fs.dialog.read", Direction: app.CapDirectionPub,
		},
	})
	noReplyBytes, err := bus.Request(RequestSubject, noPayload)
	require.NoError(t, err)
	noReply, _ := UnmarshalReply(noReplyBytes)
	assert.False(t, noReply.Granted)
}

func TestBroker_UnknownApp_ErrorReply(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &AutoApprovePolicy{})
	require.NoError(t, err)
	defer broker.Close()

	// Requester is "test.app" but request targets "ghost.app" which has
	// no registered Client.
	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId:         "ghost.app",
		SubjectFilter: app.SubjectFilter{Pattern: "x.y", Direction: app.CapDirectionPub},
	})
	replyBytes, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	reply, _ := UnmarshalReply(replyBytes)
	assert.False(t, reply.Granted)
	assert.Contains(t, reply.Reason, "unknown app")
}

func TestBroker_MalformedPayload_ErrorReply(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &AutoApprovePolicy{})
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())
	replyBytes, err := bus.Request(RequestSubject, []byte("not-json"))
	require.NoError(t, err)
	reply, _ := UnmarshalReply(replyBytes)
	assert.False(t, reply.Granted)
	assert.Contains(t, reply.Reason, "decode")
}

func TestBroker_SetPolicy_AppliesToNextRequest(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &DenyAllPolicy{})
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId:         "test.app",
		SubjectFilter: app.SubjectFilter{Pattern: "x.y", Direction: app.CapDirectionPub},
	})

	// First request: denied.
	r1, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	rep1, _ := UnmarshalReply(r1)
	assert.False(t, rep1.Granted)

	// Swap policy.
	broker.SetPolicy(&AutoApprovePolicy{})

	// Second request: approved.
	r2, err := bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	rep2, _ := UnmarshalReply(r2)
	assert.True(t, rep2.Granted)
}

func TestBroker_WithFactsStore_PersistsGrantRow(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &AutoApprovePolicy{})
	require.NoError(t, err)
	defer broker.Close()

	fs := factsstore.NewInMemoryFactsStore()
	broker.SetFactsStore(fs)

	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId: "test.app",
		SubjectFilter: app.SubjectFilter{
			Pattern: "ch.query.boxer", Direction: app.CapDirectionPub, Reason: "test reason", Sticky: true,
		},
	})
	_, err = bus.Request(RequestSubject, payload)
	require.NoError(t, err)

	rows := fs.Grants()
	require.Len(t, rows, 1)
	assert.Equal(t, app.AppIdT("test.app"), rows[0].AppId)
	assert.Equal(t, "ch.query.boxer", rows[0].Pattern)
	assert.Equal(t, app.CapDirectionPub, rows[0].Direction)
	assert.True(t, rows[0].Sticky)
	assert.Equal(t, "policy", rows[0].GrantedVia)
}

func TestBroker_WithFactsStore_DenyDoesNotPersist(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &DenyAllPolicy{})
	require.NoError(t, err)
	defer broker.Close()

	fs := factsstore.NewInMemoryFactsStore()
	broker.SetFactsStore(fs)

	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId:         "test.app",
		SubjectFilter: app.SubjectFilter{Pattern: "x.y", Direction: app.CapDirectionPub},
	})
	_, err = bus.Request(RequestSubject, payload)
	require.NoError(t, err)

	assert.Empty(t, fs.Grants())
}

func TestBroker_GrantRecordPopulated(t *testing.T) {
	inst := newBus(t)
	inst.SetRequestTimeout(50 * time.Millisecond)
	broker, err := NewBroker(inst, zerolog.Nop(), &AutoApprovePolicy{})
	require.NoError(t, err)
	defer broker.Close()

	bus := inst.NewClient("test.app", requesterCaps())
	payload, _ := MarshalRequest(GrantRequest{
		AppId: "test.app",
		SubjectFilter: app.SubjectFilter{
			Pattern: "x.y", Direction: app.CapDirectionPub, Reason: "test reason",
		},
	})
	before := time.Now().Add(-time.Second)
	_, err = bus.Request(RequestSubject, payload)
	require.NoError(t, err)
	after := time.Now().Add(time.Second)

	grants := broker.Grants()
	require.Len(t, grants, 1)
	assert.Equal(t, uint64(1), grants[0].Id)
	assert.True(t, grants[0].At.After(before) && grants[0].At.Before(after))
	assert.Equal(t, "test reason", grants[0].Filter.Reason)
}
