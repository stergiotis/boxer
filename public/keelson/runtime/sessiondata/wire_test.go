package sessiondata

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

func TestReferencesAndRewrite(t *testing.T) {
	ds, host := References("WITH x AS (SELECT * FROM session('a')) SELECT * FROM x JOIN session(b) USING id JOIN keelson('items') USING id JOIN session('a') USING id")
	assert.Equal(t, []string{"a", "b"}, ds)
	assert.Equal(t, []string{"items"}, host)
	ds, host = References("SELECT session('a')")
	assert.Nil(t, ds, "a scalar call is not a table")
	assert.Nil(t, host)
	ds, _ = References("SELEKT")
	assert.Nil(t, ds)

	pr, err := parseStatement("SELECT * FROM session('a') AS s JOIN keelson('k') AS k USING id")
	require.NoError(t, err)
	calls, err := findMacroCalls(pr)
	require.NoError(t, err)
	got := rewriteCalls(pr, calls, func(c macroCall) string {
		if c.session {
			return tableName(c.name, 2)
		}
		return externalName(0)
	})
	assert.Equal(t, "SELECT * FROM `__sd_a_r2` AS s JOIN `__adhoc_0` AS k USING id", got)
}

func TestCheckReadKeepsTheSelectOnly(t *testing.T) {
	cr, err := checkRead("SELECT a FROM t UNION ALL SELECT b FROM u;")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t UNION ALL SELECT b FROM u", cr.inner)
	cr, err = checkRead("WITH s AS (SELECT 1 AS x) SELECT x FROM s ORDER BY x LIMIT 3")
	require.NoError(t, err)
	assert.Equal(t, "WITH s AS (SELECT 1 AS x) SELECT x FROM s ORDER BY x LIMIT 3", cr.inner)
	assert.Equal(t, "CREATE TEMPORARY TABLE `t` ENGINE = MergeTree ORDER BY tuple() AS SELECT * FROM (\nSELECT 1\n) LIMIT 5",
		composeCreate(createSpec{table: "t", limit: 5}, checkedRead{inner: "SELECT 1"}))
}

// TestTheBusRoundTrip drives the subjects through the bus: the sender is
// the envelope's window, a refusal for a grant comes back as a GrantError
// naming the destination, and a closed window ends its session.
func TestTheBusRoundTrip(t *testing.T) {
	stub := newStub(t)
	disp := newDispatcher()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(Config{Bus: bus, Log: zerolog.Nop(), SweepInterval: -1})
	require.NoError(t, err)
	svc.SetDispatcher(disp)
	defer func() { _ = svc.Close(context.Background()) }()

	window := bus.NewClient(person.App, []app.SubjectFilter{
		OpenCap("t"), AttachCap("t"), CloseCap("t"), DefineCap("t"), DropCap("t"), PrepareCap("t"), ReleaseCap("t"), ListCap("t"),
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub, Reason: "replies"},
	})
	window.SetInstanceKey(person.Instance)
	ctx := context.Background()
	e := Endpoint{URL: stub.url(), User: "u", Password: "p"}
	host := EndpointHost(stub.url())

	ticket, err := Open(ctx, window)
	require.NoError(t, err)
	res, err := Define(ctx, window, DefineInput{Ticket: ticket, Endpoint: e, Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	assert.Equal(t, "__sd_a_r1", res.Dataset.Table)
	b, err := Prepare(ctx, window, PrepareInput{Ticket: ticket, Endpoint: e, SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM `__sd_a_r1`", b.SQL)
	assert.NotEmpty(t, b.Lease)
	require.NoError(t, Release(ctx, window, b.Lease))
	infos, got, err := List(ctx, window, ticket, nil)
	require.NoError(t, err)
	assert.Equal(t, ticket, got)
	require.Len(t, infos, 1)

	disp.grant("t1", DestinationServer(host))
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1"}
	_, err = Define(ctx, window, DefineInput{Caller: Caller{OnBehalfOf: obo}, Endpoint: e, Name: "b", SQL: "SELECT 1 AS x"})
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, DestinationSession(host), ge.Destination)
	_, err = request(ctx, window, SubjectOpen, withCaller(wireRequest{}, Caller{OnBehalfOf: obo}), 0)
	assert.ErrorContains(t, err, "the person's")

	dropped, err := Drop(ctx, window, DropInput{Ticket: ticket, Name: "a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a@1"}, dropped)

	payload, err := buscodec.Encode(instanceclosed.InstanceClosed{AppId: string(person.App), InstanceKey: person.Instance})
	require.NoError(t, err)
	host2 := bus.NewClient("runtime.windowhost", []app.SubjectFilter{{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionPub, Reason: "t"}})
	require.NoError(t, host2.Publish(app.SubjectInstanceClosed, payload))
	_, _, err = List(ctx, window, ticket, nil)
	assert.ErrorContains(t, err, "no data session", "the session ended with its last window")
}

// TestARefusalCarriesTheNamesItIsAbout: what a refusal's facts say — the
// name asked for, what the session holds — crosses the bus in its text.
func TestARefusalCarriesTheNamesItIsAbout(t *testing.T) {
	r := newRig(t, Limits{})
	ticket, err := r.svc.Open(context.Background(), person)
	require.NoError(t, err)
	e := Endpoint{URL: r.stub.url()}
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: e, Name: "msgs", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: e, SQL: "SELECT * FROM session('nope')"})
	require.Error(t, err)
	text := refusalText(err)
	assert.Contains(t, text, "name: nope")
	assert.Contains(t, text, "available: msgs")
	assert.NotContains(t, text, ticket)
	assert.Equal(t, "plain", refusalText(errors.New("plain")))
}
