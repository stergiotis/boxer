package sessiondata

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

const itemsRef = "items@adhoc_00000000000000aa#3:d1"

// ownClearRig is a rig whose dataset service answers a read of items as
// the own publication at TLP:CLEAR of task t1, and holds a dataset other
// that is nobody's own.
func ownClearRig(t *testing.T) (r *rig, c Caller, e Endpoint) {
	t.Helper()
	r = newRig(t, Limits{})
	r.adhoc.streams["other"] = itemsStream(t)
	r.adhoc.ownClear = map[string]string{"items": "t1"}
	c = r.agent("t1", DestinationServer(r.host()), DestinationSession(r.host()), adhocdata.DestinationDataset("items"), adhocdata.DestinationDataset("other"))
	e = r.endpoint()
	e.ServesConfined = true
	return
}

// A statement that attaches only the reading task's own publication at
// TLP:CLEAR is bound as confined, to an endpoint declared for sealed data,
// and marked as leaving no label; the trail says which attachment was
// taken so. Anything else sealed in the statement, another task's read and
// the person's read keep the label.
func TestAnOwnClearAttachmentIsBoundConfinedAndUnlabelled(t *testing.T) {
	r, c, e := ownClearRig(t)
	ctx := context.Background()

	b, err := r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: e, SQL: "SELECT m.id FROM mail.messages AS m JOIN keelson('items') AS i USING id"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity, "sealed data is attached, whoever published it")
	assert.True(t, b.OwnClear, "and it is the reading task's own publication at TLP:CLEAR")
	req := queryengine.Request{SQL: b.SQL}
	require.NoError(t, b.Apply(&req))
	assert.Equal(t, queryengine.SensitivityConfined, req.Sensitivity, "the request is sent as confined, so the engine's own refusal still applies")
	att := r.actions(AuditAttach)
	require.Len(t, att, 1)
	assert.Equal(t, []string{itemsRef}, att[0].Attachments)
	assert.Equal(t, []string{itemsRef}, att[0].OwnClear, "the trail names the read taken as the task's own")
	assert.Equal(t, auditLabelOwnClear, att[0].Label)

	reads := len(r.adhoc.reads)
	_, err = r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM keelson('items')"})
	assert.ErrorContains(t, err, "not declared as allowed to see sealed data", "the wall is the same for a task's own publication")
	assert.Len(t, r.adhoc.reads, reads, "and the dataset is not even read")

	b, err = r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: e, SQL: "SELECT * FROM keelson('items') AS i JOIN keelson('other') AS o USING id"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.False(t, b.OwnClear, "one dataset that is not the task's own labels the statement")
	att = r.actions(AuditAttach)
	require.Len(t, att, 2)
	assert.Len(t, att[1].Attachments, 2)
	assert.Equal(t, []string{itemsRef}, att[1].OwnClear)
	assert.Equal(t, auditLabelConfined, att[1].Label)

	other := r.agent("t2", DestinationServer(r.host()), adhocdata.DestinationDataset("items"))
	b, err = r.svc.Prepare(ctx, PrepareInput{Caller: other, Endpoint: e, SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	assert.False(t, b.OwnClear, "another task's read")
	assert.Empty(t, r.actions(AuditAttach)[2].OwnClear)

	b, err = r.svc.Prepare(ctx, PrepareInput{Caller: Caller{By: person}, Endpoint: e, SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.False(t, b.OwnClear, "the person's read")

	// Even a dataset service that answered every read as own-clear lifts
	// no label for a request that is no attested call of a task.
	r.adhoc.ownClear["items"] = "*"
	b, err = r.svc.Prepare(ctx, PrepareInput{Caller: Caller{By: person}, Endpoint: e, SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	assert.False(t, b.OwnClear)
	assert.False(t, Binding{Sensitivity: queryengine.SensitivityConfined}.OwnClear, "the zero value keeps the label")
}

// A data set a task keeps from its own clear publications alone is held
// behind the wall and reads back unlabelled for that task; anything else
// sealed in its definition, or in a statement beside it, labels.
func TestAKeptDataSetOfOwnClearDataIsNotLabelled(t *testing.T) {
	r, c, e := ownClearRig(t)
	ctx := context.Background()
	define := func(name string, sql string) (d DatasetInfo) {
		res, err := r.svc.Define(ctx, DefineInput{Caller: c, Endpoint: e, Name: name, SQL: sql})
		require.NoError(t, err)
		return res.Dataset
	}
	prepare := func(sql string) (b Binding) {
		b, err := r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: e, SQL: sql})
		require.NoError(t, err)
		require.NoError(t, r.svc.Release(person, b.Lease))
		return
	}

	d := define("mine", "SELECT * FROM keelson('items')")
	assert.True(t, d.Confined, "it holds sealed data: the wall applies to every statement reading it")
	assert.True(t, d.OwnClear, "all of it the task's own publication at TLP:CLEAR")
	made := r.actions(AuditDefine)
	assert.Equal(t, auditLabelOwnClear, made[len(made)-1].Label)
	assert.Equal(t, []string{itemsRef}, made[len(made)-1].OwnClear)

	b := prepare("SELECT * FROM session('mine')")
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.True(t, b.OwnClear)
	_, err := r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('mine')"})
	assert.ErrorContains(t, err, "derived from sealed data", "an endpoint not declared for sealed data does not read it")

	assert.True(t, define("mine2", "SELECT count() AS n FROM session('mine')").OwnClear, "what reads it alone is the task's own too")
	mixed := define("mixed", "SELECT * FROM session('mine') AS m JOIN keelson('other') AS o USING id")
	assert.True(t, mixed.Confined)
	assert.False(t, mixed.OwnClear, "a dataset that is not the task's own confines the data set")
	made = r.actions(AuditDefine)
	assert.Equal(t, auditLabelConfined, made[len(made)-1].Label)
	assert.Empty(t, made[len(made)-1].OwnClear)
	assert.False(t, define("mixed2", "SELECT * FROM session('mixed')").OwnClear, "and what reads that")

	assert.False(t, prepare("SELECT * FROM session('mine') AS a JOIN session('mixed') AS b USING id").OwnClear, "a confined data set beside an own-clear one labels the statement")
	assert.False(t, prepare("SELECT * FROM session('mine') AS a JOIN keelson('other') AS o USING id").OwnClear)
	assert.True(t, prepare("SELECT * FROM session('mine') AS a JOIN keelson('items') AS i USING id").OwnClear)

	plain := define("plain", "SELECT 1 AS x")
	assert.False(t, plain.Confined)
	assert.False(t, plain.OwnClear, "a data set that holds nothing sealed has no label to lift")
	b = prepare("SELECT * FROM session('plain')")
	assert.Equal(t, queryengine.SensitivityOrdinary, b.Sensitivity)
	assert.False(t, b.OwnClear)

	for _, row := range r.svc.catalogRows() {
		assert.False(t, row.info.OwnClear, "the operator's catalog shows %s as sealed", row.info.Name)
	}
}

// A kept data set's label follows what a replay reads: once a replay finds
// the dataset is no longer the task's own clear publication the data set
// is confined, and it stays so until a replay reads the task's own clear
// publication again.
func TestAReplayJudgesAKeptDataSetOnWhatItReads(t *testing.T) {
	r, c, e := ownClearRig(t)
	ctx := context.Background()
	_, err := r.svc.Define(ctx, DefineInput{Caller: c, Endpoint: e, Name: "mine", SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	_, err = r.svc.Define(ctx, DefineInput{Caller: c, Endpoint: e, Name: "top", SQL: "SELECT count() AS n FROM session('mine')"})
	require.NoError(t, err)
	read := func() (b Binding) {
		b, err = r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: e, SQL: "SELECT * FROM session('top')"})
		require.NoError(t, err)
		require.NoError(t, r.svc.Release(person, b.Lease))
		return
	}
	listed := func(name string) (d DatasetInfo) {
		infos, _, lErr := r.svc.List(ctx, c, "")
		require.NoError(t, lErr)
		for _, i := range infos {
			if i.Name == name {
				return i
			}
		}
		t.Fatalf("no data set %s", name)
		return
	}
	assert.True(t, read().OwnClear)

	// Someone else republishes the dataset. The tables still hold the
	// task's own rows, so nothing changes until a replay reads again.
	r.adhoc.ownClear["items"] = ""
	b := read()
	assert.Empty(t, b.Replayed)
	assert.True(t, b.OwnClear, "the table is a cache of what was read when it was made")

	r.stub.expireAll()
	b = read()
	assert.Equal(t, []string{"mine@1", "top@1"}, b.Replayed)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.False(t, b.OwnClear, "the replay read a revision that is not the task's own")
	assert.False(t, listed("mine").OwnClear)
	assert.False(t, listed("top").OwnClear, "and what reads it was re-made from it")
	replays := r.actions(AuditReplay)
	require.Len(t, replays, 2)
	assert.Equal(t, auditLabelConfined, replays[0].Label, "the replay's row says the data set is confined from here")
	assert.Empty(t, replays[0].OwnClear)
	assert.Equal(t, auditLabelConfined, replays[1].Label)

	// The task publishes at TLP:CLEAR again: no table was re-made, so the
	// data sets stay confined.
	r.adhoc.ownClear["items"] = "t1"
	b = read()
	assert.Empty(t, b.Replayed)
	assert.False(t, b.OwnClear, "never back without a fresh read of the task's own clear publication")
	assert.False(t, listed("mine").OwnClear)

	r.stub.expireAll()
	b = read()
	assert.Equal(t, []string{"mine@1", "top@1"}, b.Replayed)
	assert.True(t, b.OwnClear, "re-made from the task's own clear publication alone")
	replays = r.actions(AuditReplay)
	assert.Equal(t, auditLabelOwnClear, replays[len(replays)-1].Label)

	// A replay on an endpoint not declared for sealed data fails as before.
	r.stub.expireAll()
	_, err = r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('top')"})
	assert.ErrorContains(t, err, "not declared as allowed to see sealed data")
}

// The binding and the catalog carry the answer across the bus, to the
// window the call was routed to.
func TestTheLabelCrossesTheBus(t *testing.T) {
	stub := newStub(t)
	disp := newDispatcher()
	bus := inprocbus.NewInst(zerolog.Nop())
	adhoc := &fakeAdhoc{streams: map[string][]byte{"items": itemsStream(t)}, ownClear: map[string]string{"items": "t1"}}
	svc, err := NewService(Config{Bus: bus, Log: zerolog.Nop(), Adhoc: adhoc, SweepInterval: -1})
	require.NoError(t, err)
	svc.SetDispatcher(disp)
	defer func() { _ = svc.Close(context.Background()) }()
	window := bus.NewClient(person.App, []app.SubjectFilter{
		DefineCap("t"), PrepareCap("t"), ReleaseCap("t"), ListCap("t"),
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub, Reason: "replies"},
	})
	window.SetInstanceKey(person.Instance)
	ctx := context.Background()
	host := EndpointHost(stub.url())
	e := Endpoint{URL: stub.url(), User: "u", Password: "p", ServesConfined: true}
	dests := []string{DestinationServer(host), DestinationSession(host), adhocdata.DestinationDataset("items")}
	disp.grant("t1", dests...)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: dests}

	b, err := Prepare(ctx, window, PrepareInput{Caller: Caller{OnBehalfOf: obo}, Endpoint: e, SQL: "SELECT * FROM t JOIN keelson('items') AS i USING id"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.True(t, b.OwnClear)
	res, err := Define(ctx, window, DefineInput{Caller: Caller{OnBehalfOf: obo}, Endpoint: e, Name: "mine", SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	assert.True(t, res.Dataset.Confined)
	assert.True(t, res.Dataset.OwnClear)
	infos, _, err := List(ctx, window, "", obo)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.True(t, infos[0].OwnClear)

	b, err = Prepare(ctx, window, PrepareInput{Endpoint: e, SQL: "SELECT * FROM t JOIN keelson('items') AS i USING id"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.False(t, b.OwnClear, "the person's statement")
}
