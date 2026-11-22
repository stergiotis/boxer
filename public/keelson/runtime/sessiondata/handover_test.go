package sessiondata

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// TestAHandOverAttachesOneWindowOnce hands a person's data session from one
// window to another over the bus: the token is claimed once, the claiming
// window reads the data sets, and neither ticket nor token is in a refusal
// or the log.
func TestAHandOverAttachesOneWindowOnce(t *testing.T) {
	stub := newStub(t)
	var logged bytes.Buffer
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(Config{Bus: bus, Log: zerolog.New(&logged), SweepInterval: -1})
	require.NoError(t, err)
	svc.SetDispatcher(newDispatcher())
	defer func() { _ = svc.Close(context.Background()) }()
	caps := []app.SubjectFilter{
		OpenCap("t"), DefineCap("t"), PrepareCap("t"), ReleaseCap("t"), ListCap("t"), HandOverCap("t"), ClaimCap("t"),
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub, Reason: "replies"},
	}
	opener := bus.NewClient(person.App, caps)
	opener.SetInstanceKey(person.Instance)
	opened := bus.NewClient(person2.App, caps)
	opened.SetInstanceKey(person2.Instance)
	ctx := context.Background()
	e := Endpoint{URL: stub.url(), User: "u", Password: "p"}

	ticket, err := Open(ctx, opener)
	require.NoError(t, err)
	_, err = Define(ctx, opener, DefineInput{Ticket: ticket, Endpoint: e, Name: "msgs", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)

	_, _, err = List(ctx, opened, ticket, nil)
	require.Error(t, err, "a window that holds nothing reads nothing")

	token, err := HandOver(ctx, opener, ticket)
	require.NoError(t, err)
	assert.True(t, IsHandOver(token))
	assert.NotEqual(t, ticket, token)

	got, err := Claim(ctx, opened, token)
	require.NoError(t, err)
	assert.Equal(t, ticket, got)
	infos, _, err := List(ctx, opened, got, nil)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "msgs", infos[0].Name)

	_, err = Claim(ctx, opened, token)
	require.Error(t, err, "a token is spent by its claim")
	assert.NotContains(t, err.Error(), token)

	_, err = HandOver(ctx, opened, "sds_00000000000000000000000000000000")
	require.Error(t, err)

	// A lapsed token is refused.
	late, err := HandOver(ctx, opener, ticket)
	require.NoError(t, err)
	svc.mu.Lock()
	h := svc.handOvers[late]
	h.expires = time.Now().Add(-time.Second)
	svc.handOvers[late] = h
	svc.mu.Unlock()
	_, err = Claim(ctx, opened, late)
	require.ErrorContains(t, err, "lapsed")

	// An agent's call may not hand over or claim.
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1"}
	_, err = request(ctx, opener, SubjectHandOver, withCaller(wireRequest{Ticket: ticket}, Caller{OnBehalfOf: obo}), 0)
	assert.ErrorContains(t, err, "the person's")

	assert.NotContains(t, logged.String(), ticket)
	assert.NotContains(t, logged.String(), token)
}

// TestATaskSessionIsNotHandedOver refuses to mint a token for a task's data
// session, whose ticket a define returns.
func TestATaskSessionIsNotHandedOver(t *testing.T) {
	r := newRig(t, Limits{})
	host := EndpointHost(r.stub.url())
	r.disp.grant("t1", DestinationServer(host), DestinationSession(host))
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1"}
	res, err := r.svc.Define(context.Background(), DefineInput{Caller: Caller{By: person, OnBehalfOf: obo}, Endpoint: Endpoint{URL: r.stub.url()}, Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	_, err = r.svc.HandOver(context.Background(), person, res.Ticket)
	require.ErrorContains(t, err, "task's data session")
}
