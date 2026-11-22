package sessiondata

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/db/fairlead/taskread"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// fakeDispatcher attests the calls it was told of and lists a grant's
// destinations; a task it was told ended is not live.
type fakeDispatcher struct {
	mu    sync.Mutex
	dests map[string][]string
	ended map[string]bool
}

func newDispatcher() (inst *fakeDispatcher) {
	return &fakeDispatcher{dests: map[string][]string{}, ended: map[string]bool{}}
}

func (inst *fakeDispatcher) grant(task string, dests ...string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.dests[task] = append(inst.dests[task], dests...)
}

func (inst *fakeDispatcher) end(task string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.ended[task] = true
}

func (inst *fakeDispatcher) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, known := inst.dests[task]; !known || inst.ended[task] {
		return cc, false, "no live task by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance}, true, ""
}

func (inst *fakeDispatcher) AllowDestination(task string, epoch uint64, destination string) (ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if slices.Contains(inst.dests[task], destination) {
		return true, ""
	}
	return false, "the task's grant does not list " + destination
}

func (inst *fakeDispatcher) TaskLive(task string) (live bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, known := inst.dests[task]
	return known && !inst.ended[task]
}

// fakeAdhoc serves ad-hoc datasets by alias; a read the reader's grant
// list does not cover is refused as the dataset service refuses one.
type fakeAdhoc struct {
	streams map[string][]byte
	reads   []string
}

func (inst *fakeAdhoc) Read(_ context.Context, alias string, _ adhocdata.Identity, obo *app.OnBehalfOf) (res adhocdata.ReadResult, err error) {
	inst.reads = append(inst.reads, alias)
	s, ok := inst.streams[alias]
	if !ok {
		return res, adhocdata.ErrNoLiveDataset
	}
	if obo != nil && !slices.Contains(obo.Destinations, adhocdata.DestinationDataset(alias)) {
		return res, &adhocdata.GrantError{Destination: adhocdata.DestinationDataset(alias), Reason: "the grant does not list " + adhocdata.DestinationDataset(alias)}
	}
	return adhocdata.ReadResult{Alias: alias, Handle: "adhoc_00000000000000aa", Revision: 3, Rows: 2, ArrowIPCStream: s, StreamDigest: "d1"}, nil
}

// itemsStream is a two-row Arrow stream (id UInt64, label String).
func itemsStream(t *testing.T) (stream []byte) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Uint64}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{1, 2}, nil)
	rb.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

type rig struct {
	t      *testing.T
	stub   *stubServer
	svc    *Service
	disp   *fakeDispatcher
	adhoc  *fakeAdhoc
	audits []AuditRecord
	amu    sync.Mutex
	reg    *introspect.Registry
}

var (
	person  = Identity{App: "github.com/x/play", Instance: 7}
	person2 = Identity{App: "github.com/x/notebook", Instance: 9}
)

func newRig(t *testing.T, limits Limits) (r *rig) {
	t.Helper()
	r = &rig{t: t, stub: newStub(t), disp: newDispatcher(), adhoc: &fakeAdhoc{streams: map[string][]byte{"items": itemsStream(t)}},
		reg: introspect.NewRegistry()}
	svc, err := NewService(Config{Log: zerolog.Nop(), Adhoc: r.adhoc, Limits: limits, SweepInterval: -1, Registry: r.reg})
	require.NoError(t, err)
	svc.SetDispatcher(r.disp)
	svc.setAuditHook(func(a AuditRecord) {
		r.amu.Lock()
		r.audits = append(r.audits, a)
		r.amu.Unlock()
	})
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	r.svc = svc
	return
}

func (inst *rig) endpoint() (e Endpoint) {
	return Endpoint{URL: inst.stub.url(), User: "u", Password: "p"}
}

func (inst *rig) host() (h string) { return EndpointHost(inst.stub.url()) }

// agent is the caller of a task's call routed to the play window, granted
// what dests list.
func (inst *rig) agent(task string, dests ...string) (c Caller) {
	inst.disp.grant(task, dests...)
	return Caller{By: person, OnBehalfOf: &app.OnBehalfOf{Task: task, Epoch: 1, Call: "c1", Destinations: dests}}
}

// fullAgent is an agent granted the endpoint and session tables on it.
func (inst *rig) fullAgent(task string) (c Caller) {
	return inst.agent(task, DestinationServer(inst.host()), DestinationSession(inst.host()))
}

func (inst *rig) define(c Caller, name string, sql string) (res DefineResult) {
	inst.t.Helper()
	res, err := inst.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: inst.endpoint(), Name: name, SQL: sql})
	require.NoError(inst.t, err)
	return
}

func (inst *rig) actions(action string) (out []AuditRecord) {
	inst.amu.Lock()
	defer inst.amu.Unlock()
	for _, a := range inst.audits {
		if a.Action == action {
			out = append(out, a)
		}
	}
	return
}

// TestDefineComposesTheCreateAndLeadsItAsItsOwn is the create the
// capability composes around a checked read, led at its own site with the
// task mark cleared, so the host's pass that refuses a task's non-read lets
// it through while it would refuse the same text marked.
func TestDefineComposesTheCreateAndLeadsItAsItsOwn(t *testing.T) {
	r := newRig(t, Limits{MaxRows: 1000})
	var mu sync.Mutex
	var seen []fairlead.Statement
	removeRec := fairlead.Install(fairlead.PassFunc(func(_ context.Context, st fairlead.Statement) (string, error) {
		mu.Lock()
		seen = append(seen, st)
		mu.Unlock()
		return st.SQL, nil
	}))
	defer removeRec()
	removePass := taskread.Install()
	defer removePass()

	c := r.fullAgent("t1")
	ctx := fairlead.WithTask(context.Background(), "t1") // a caller's context that carries the task's mark
	res, err := r.svc.Define(ctx, DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "msgs",
		SQL: "SELECT id, lower(body) AS b FROM enron.message WHERE id < 100;", OrderBy: []string{"id"}})
	require.NoError(t, err)
	assert.Equal(t, "msgs", res.Dataset.Name)
	assert.Equal(t, uint64(1), res.Dataset.Revision)
	assert.Equal(t, DatasetStateLive, res.Dataset.State)
	assert.Equal(t, "__sd_msgs_r1", res.Dataset.Table)
	assert.Equal(t, uint64(10), res.Dataset.Rows)
	assert.Equal(t, []Column{{Name: "x", Type: "UInt64"}, {Name: "s", Type: "String"}}, res.Dataset.Columns)
	assert.NotEmpty(t, res.Ticket)

	create := r.stub.lastCreate()
	assert.Equal(t, "CREATE TEMPORARY TABLE `__sd_msgs_r1` ENGINE = MergeTree ORDER BY (`id`) AS SELECT * FROM (\n"+
		"SELECT id, lower(body) AS b FROM enron.message WHERE id < 100\n) LIMIT 1001", create.SQL)
	assert.Equal(t, "2", create.Params["readonly"])
	assert.NotEmpty(t, create.Params["session_id"])
	assert.Equal(t, "900", create.Params["session_timeout"])
	assert.Equal(t, "1", create.Params["session_check"], "the probe opened the session; the create continues it")
	r.stub.mu.Lock()
	first := r.stub.requests[0]
	r.stub.mu.Unlock()
	assert.Equal(t, probeStatement, first.SQL)
	assert.Empty(t, first.Params["session_check"], "the first statement opens the session")
	stamp, ok := logcomment.Parse(create.LogComment)
	require.True(t, ok)
	assert.Equal(t, string(ServiceAppId), stamp.App)
	assert.Equal(t, "t1", stamp.Task)
	assert.Equal(t, "c1", stamp.TaskCall)

	mu.Lock()
	var sites []string
	for _, st := range seen {
		sites = append(sites, st.Site)
		assert.Empty(t, st.Task, "the capability's own text leaves unmarked: %s", st.SQL)
	}
	mu.Unlock()
	assert.Equal(t, []string{SiteProbe, SiteCreate, SiteProbe, SiteProbe}, sites)

	// The same create marked as the task's is refused by the host's pass.
	_, err = fairlead.Lead(fairlead.WithTask(context.Background(), "t1"), fairlead.ExitServer, "test", create.SQL)
	assert.Error(t, err)

	defs := r.actions(AuditDefine)
	require.Len(t, defs, 1)
	assert.Equal(t, "ok", defs[0].Outcome)
	assert.Equal(t, []string{"msgs@1"}, defs[0].Datasets)
	assert.Equal(t, "t1", defs[0].Context.Val.Task)
	assert.NotEmpty(t, defs[0].Stratum)
}

// TestDefineRefusesWhatIsNotAPlainRead is the check of the task's text
// before it is wrapped.
func TestDefineRefusesWhatIsNotAPlainRead(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	for name, sql := range map[string]string{
		"insert":   "INSERT INTO t SELECT 1",
		"ddl":      "CREATE TABLE t (x UInt8) ENGINE = Memory",
		"url":      "SELECT * FROM url('http://example.invalid/x', CSV, 'x String')",
		"remote":   "SELECT * FROM remote('h:9000', db.t)",
		"file":     "SELECT * FROM file('/etc/passwd', LineAsString)",
		"script":   "SELECT 1; SELECT 2",
		"set":      "SET param_x = 1; SELECT {x:UInt8}",
		"format":   "SELECT 1 FORMAT JSON",
		"unparsed": "SELEKT 1",
		"self":     "SELECT * FROM session('me')",
		"args":     "SELECT * FROM session('a', 'b')",
	} {
		_, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "me", SQL: sql})
		assert.Error(t, err, name)
	}
	assert.Empty(t, r.stub.creates(), "nothing refused reached the server")
}

// TestDefineRefusesWithTheDestinationToAskFor holds a task's grant: the
// endpoint and the right to keep session tables on it are each a
// destination, refused by name when missing.
func TestDefineRefusesWithTheDestinationToAskFor(t *testing.T) {
	r := newRig(t, Limits{})
	h := r.host()
	_, err := r.svc.Define(context.Background(), DefineInput{Caller: r.agent("t1", DestinationServer(h)), Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, DestinationSession(h), ge.Destination)

	_, err = r.svc.Define(context.Background(), DefineInput{Caller: r.agent("t2", DestinationSession(h)), Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, DestinationServer(h), ge.Destination)

	unattested := Caller{By: person, OnBehalfOf: &app.OnBehalfOf{Task: "nobody", Epoch: 1, Call: "c"}}
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: unattested, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	assert.ErrorIs(t, err, ErrUnattested)
	assert.Empty(t, r.stub.creates())
	denied := r.actions(AuditDefine)
	require.NotEmpty(t, denied)
	assert.Equal(t, "denied", denied[0].Outcome)
}

// TestReplayAfterExpiryInDependencyOrder: the server dropped the session;
// the next statement reading the last of a chain re-makes the chain,
// oldest first, in a new server session, and says so.
func TestReplayAfterExpiryInDependencyOrder(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT number AS x FROM numbers(10)")
	r.define(c, "b", "SELECT x * 2 AS y FROM session('a')")
	r.define(c, "c", "SELECT count() AS n FROM session('b') JOIN session('a') ON y = x")
	r.define(c, "other", "SELECT 1 AS z")
	first := r.stub.lastCreate().Params["session_id"]
	r.stub.expireAll()

	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('c') AS c"})
	require.NoError(t, err)
	defer func() { require.NoError(t, r.svc.Release(person, b.Lease)) }()
	assert.Equal(t, []string{"a@1", "b@1", "c@1"}, b.Replayed)
	assert.Equal(t, "SELECT * FROM `__sd_c_r1` AS c", b.SQL)
	assert.Equal(t, []string{"c@1"}, b.Datasets)
	second := b.Settings["session_id"]
	assert.NotEqual(t, first, second, "a lost session is opened afresh under a new id")
	assert.Equal(t, "1", b.Settings["session_check"])
	assert.ElementsMatch(t, []string{"__sd_a_r1", "__sd_b_r1", "__sd_c_r1"}, r.stub.tablesOf(second), "only what the statement reads is replayed")
	creates := r.stub.creates()
	assert.Equal(t, []string{"__sd_a_r1", "__sd_b_r1", "__sd_c_r1"}, creates[len(creates)-3:])

	infos, _, err := r.svc.List(context.Background(), c, "")
	require.NoError(t, err)
	for _, d := range infos {
		switch d.Name {
		case "other":
			assert.Equal(t, DatasetStateExpired, d.State)
			assert.Equal(t, "the server session was lost", d.Reason)
		default:
			assert.Equal(t, DatasetStateLive, d.State, d.Name)
			assert.Equal(t, uint64(1), d.Replays, d.Name)
		}
	}
	assert.Len(t, r.actions(AuditReplay), 3)
	require.Len(t, r.actions(AuditExpire), 1)
	assert.ElementsMatch(t, []string{"a@1", "b@1", "c@1", "other@1"}, r.actions(AuditExpire)[0].Datasets)
}

// TestRedefinitionLeavesDependantsStaleAndRebuildsThem.
func TestRedefinitionLeavesDependantsStaleAndRebuildsThem(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	r.define(c, "b", "SELECT x FROM session('a')")
	res := r.define(c, "a", "SELECT 2 AS x")
	assert.Equal(t, uint64(2), res.Dataset.Revision)
	assert.Equal(t, "__sd_a_r2", res.Dataset.Table)
	assert.Equal(t, []string{"__sd_a_r1", "__sd_b_r1"}, r.stub.drops(), "the old table and the stale dependant's go")
	infos, _, err := r.svc.List(context.Background(), c, "")
	require.NoError(t, err)
	assert.Equal(t, DatasetStateStale, infos[1].State)

	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('b')"})
	require.NoError(t, err)
	require.NoError(t, r.svc.Release(person, b.Lease))
	assert.Equal(t, []string{"b@1"}, b.Replayed)
	assert.Contains(t, r.stub.lastCreate().SQL, "FROM `__sd_a_r2`", "the dependant is rebuilt from the new revision")

	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT x FROM session('b')"})
	assert.ErrorContains(t, err, "reads it", "a redefinition may not close a cycle")
}

// TestBoundsRefuseAndDropWhatWasMade covers the count, row and byte
// bounds of one data session.
func TestBoundsRefuseAndDropWhatWasMade(t *testing.T) {
	r := newRig(t, Limits{MaxDatasets: 2, MaxRows: 25, MaxBytes: 1000})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	r.define(c, "b", "SELECT 2 AS x")
	_, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "c", SQL: "SELECT 3 AS x"})
	assert.ErrorContains(t, err, "as many data sets")

	r2 := newRig(t, Limits{MaxRows: 25})
	c2 := r2.fullAgent("t1")
	r2.define(c2, "a", "SELECT 1 AS x") // 10 rows
	r2.define(c2, "b", "SELECT 2 AS x") // 20
	_, err = r2.svc.Define(context.Background(), DefineInput{Caller: c2, Endpoint: r2.endpoint(), Name: "c", SQL: "SELECT 3 AS x"})
	assert.ErrorContains(t, err, "more rows")
	assert.Contains(t, r2.stub.lastCreate().SQL, "LIMIT 6", "the create is bounded by what the session may still hold, plus one")
	assert.Contains(t, r2.stub.drops(), "__sd_c_r1", "a table over the bound is dropped")

	r3 := newRig(t, Limits{MaxBytes: 100})
	c3 := r3.fullAgent("t1")
	r3.stub.bytesPerRow = 6
	r3.define(c3, "a", "SELECT 1 AS x") // 60 bytes
	_, err = r3.svc.Define(context.Background(), DefineInput{Caller: c3, Endpoint: r3.endpoint(), Name: "b", SQL: "SELECT 1 AS x"})
	assert.ErrorContains(t, err, "more bytes")
	assert.Contains(t, r3.stub.drops(), "__sd_b_r1")
	infos, _, _ := r3.svc.List(context.Background(), c3, "")
	assert.Len(t, infos, 1, "a refused definition is not recorded")
}

// TestPrepareAttachesAnAdhocDatasetAsExternalData is a host dataset and a
// server table in one statement, and a data set derived from host data.
func TestPrepareAttachesAnAdhocDatasetAsExternalData(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.agent("t1", DestinationServer(r.host()), DestinationSession(r.host()), adhocdata.DestinationDataset("items"))
	e := r.endpoint()
	e.ServesConfined = true

	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: e,
		SQL: "SELECT m.id, i.label FROM enron.message AS m JOIN keelson('items') AS i USING id"})
	require.NoError(t, err)
	assert.Equal(t, "SELECT m.id, i.label FROM enron.message AS m JOIN `__adhoc_0` AS i USING id", b.SQL)
	require.Len(t, b.External, 1)
	assert.Equal(t, "ArrowStream", b.External[0].Format)
	assert.Equal(t, "`id` UInt64, `label` String", b.External[0].Structure)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity)
	assert.Empty(t, b.Lease, "a statement reading no data set holds no server session")
	assert.Empty(t, b.Settings)
	require.Len(t, r.actions(AuditAttach), 1)
	assert.Equal(t, []string{"items@adhoc_00000000000000aa#3:d1"}, r.actions(AuditAttach)[0].Attachments)

	req := queryengine.Request{SQL: b.SQL}
	require.NoError(t, b.Apply(&req))
	assert.Equal(t, queryengine.SensitivityConfined, req.Sensitivity)
	require.NoError(t, req.Validate())

	res, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "joined",
		SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	assert.True(t, res.Dataset.Confined, "a data set derived from sealed data is confined")
	assert.Equal(t, []string{"items"}, res.Dataset.Attachments)
	create := r.stub.lastCreate()
	assert.Contains(t, create.Files, "__adhoc_0")
	assert.Equal(t, "ArrowStream", create.Params["__adhoc_0_format"])
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "derived", SQL: "SELECT count() AS n FROM session('joined')"})
	assert.ErrorContains(t, err, "derived from sealed data", "a call that does not declare the endpoint does not reach confined data on it")
	derived, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "derived", SQL: "SELECT count() AS n FROM session('joined')"})
	require.NoError(t, err)
	assert.True(t, derived.Dataset.Confined)

	b, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: e, SQL: "SELECT * FROM session('derived')"})
	require.NoError(t, err)
	assert.Equal(t, queryengine.SensitivityConfined, b.Sensitivity, "confinement is carried to what a confined data set feeds")
	require.NoError(t, r.svc.Release(person, b.Lease))
}

// TestConfinedDataIsRefusedAnEndpointNotDeclaredForIt is the ADR-0145
// wall: sealed data reaches a server only where the caller declared it may.
func TestConfinedDataIsRefusedAnEndpointNotDeclaredForIt(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.agent("t1", DestinationServer(r.host()), DestinationSession(r.host()), adhocdata.DestinationDataset("items"))
	_, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM keelson('items')"})
	assert.ErrorContains(t, err, "not declared as allowed to see sealed data")
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT * FROM keelson('items')"})
	assert.ErrorContains(t, err, "not declared as allowed to see sealed data")
	assert.Empty(t, r.adhoc.reads, "the dataset is not even read")

	e := r.endpoint()
	e.ServesConfined = true
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: e, Name: "c2", SQL: "SELECT * FROM keelson('items')"})
	require.NoError(t, err)
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('c2')"})
	assert.ErrorContains(t, err, "derived from sealed data", "the declaration is the call's, not the session's")

	noGrant := r.agent("t2", DestinationServer(r.host()))
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: noGrant, Endpoint: e, SQL: "SELECT * FROM keelson('items')"})
	var ge *GrantError
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, adhocdata.DestinationDataset("items"), ge.Destination)
}

// TestOneStatementAtATimeInAServerSession: a second prepare waits for the
// first binding's release, so the server never sees two statements in one
// session at once.
func TestOneStatementAtATimeInAServerSession(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	b1, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)

	got := make(chan Binding, 1)
	go func() {
		b2, pErr := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
		assert.NoError(t, pErr)
		got <- b2
	}()
	select {
	case <-got:
		t.Fatal("the second statement was prepared while the first held the session")
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, r.svc.Release(person, b1.Lease))
	select {
	case b2 := <-got:
		require.NoError(t, r.svc.Release(person, b2.Lease))
	case <-time.After(5 * time.Second):
		t.Fatal("the second statement was not prepared after the release")
	}
	assert.Error(t, r.svc.Release(person, b1.Lease), "a lease is released once")
	assert.Zero(t, r.stub.overlaps)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	b3, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	_, err = r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	assert.ErrorIs(t, err, context.DeadlineExceeded, "a caller waits under its own context")
	assert.Error(t, r.svc.Release(person2, b3.Lease), "a lease is its window's")
	require.NoError(t, r.svc.Release(person, b3.Lease))
}

// TestALeaseNotReleasedLapses.
func TestALeaseNotReleasedLapses(t *testing.T) {
	r := newRig(t, Limits{LeaseTimeout: 50 * time.Millisecond})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	_, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := r.svc.Prepare(ctx, PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	require.NoError(t, r.svc.Release(person, b.Lease))
}

// TestPersonSessionsByTicketAndTheirEnd: a person's data session is
// reached by its ticket from a window that holds it, handed to another
// window by attaching, and ends with the last window holding it.
func TestPersonSessionsByTicketAndTheirEnd(t *testing.T) {
	r := newRig(t, Limits{})
	ctx := context.Background()
	ticket, err := r.svc.Open(ctx, person)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ticket, "sds_") && len(ticket) == 36)
	me := Caller{By: person}
	other := Caller{By: person2}
	_, err = r.svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	_, _, err = r.svc.List(ctx, other, ticket)
	assert.ErrorContains(t, err, "does not hold")
	_, _, err = r.svc.List(ctx, me, "sds_nope")
	assert.Error(t, err)

	require.NoError(t, r.svc.Attach(ctx, person2, ticket))
	infos, got, err := r.svc.List(ctx, other, ticket)
	require.NoError(t, err)
	assert.Equal(t, ticket, got)
	require.Len(t, infos, 1)
	assert.Equal(t, "SELECT 1 AS x", infos[0].SQL)
	assert.Empty(t, infos[0].Task)

	r.svc.instanceClosed(ctx, person)
	_, _, err = r.svc.List(ctx, other, ticket)
	require.NoError(t, err, "the session lives while a window holds it")
	r.svc.instanceClosed(ctx, person2)
	_, _, err = r.svc.List(ctx, other, ticket)
	assert.Error(t, err)
	assert.Equal(t, []string{"__sd_a_r1"}, r.stub.drops(), "its tables are dropped")
	require.Len(t, r.actions(AuditClose), 1)
}

// TestATaskSessionIsTheTasksAndEndsWithIt: every window of the task
// reaches one data session without being told of it, a ticket cannot steer
// an agent's call elsewhere, and the sweep ends it once the task ends.
func TestATaskSessionIsTheTasksAndEndsWithIt(t *testing.T) {
	r := newRig(t, Limits{})
	ctx := context.Background()
	c := r.fullAgent("t1")
	res := r.define(c, "a", "SELECT 1 AS x")
	ticket, err := r.svc.Open(ctx, person2)
	require.NoError(t, err)

	inOther := Caller{By: person2, OnBehalfOf: c.OnBehalfOf}
	infos, got, err := r.svc.List(ctx, inOther, ticket)
	require.NoError(t, err)
	assert.Equal(t, res.Ticket, got, "the call reaches the task's session, whatever ticket it names")
	require.Len(t, infos, 1)
	assert.Equal(t, "t1", infos[0].Task)

	other, _, err := r.svc.List(ctx, r.fullAgent("t2"), "")
	require.NoError(t, err)
	assert.Empty(t, other, "another task sees nothing of it")

	r.disp.end("t1")
	assert.Equal(t, 1, r.svc.SweepEndedTasks(ctx))
	assert.Equal(t, []string{"__sd_a_r1"}, r.stub.drops())
	assert.Nil(t, r.svc.lookupTicket(res.Ticket))
}

// TestDropRefusesWhatOthersReadUnlessCascaded.
func TestDropRefusesWhatOthersReadUnlessCascaded(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	r.define(c, "b", "SELECT x FROM session('a')")
	_, err := r.svc.Drop(context.Background(), DropInput{Caller: c, Name: "a"})
	assert.ErrorContains(t, err, "other data sets read it")
	dropped, err := r.svc.Drop(context.Background(), DropInput{Caller: c, Name: "a", Cascade: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"a@1", "b@1"}, dropped)
	assert.ElementsMatch(t, []string{"__sd_a_r1", "__sd_b_r1"}, r.stub.drops())
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	assert.ErrorContains(t, err, "no data set by that name")
}

// TestCatalogShowsEverySessionWithoutTicketsOrText.
func TestCatalogShowsEverySessionWithoutTicketsOrText(t *testing.T) {
	r := newRig(t, Limits{})
	r.define(r.fullAgent("t1"), "a", "SELECT 1 AS x")
	p, ok := r.reg.Lookup(CatalogTableName)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	assert.Equal(t, int64(1), rec.NumRows())
	idx := rec.Schema().FieldIndices("state")
	require.Len(t, idx, 1)
	assert.Equal(t, "live", rec.Column(idx[0]).(*array.String).Value(0))
	for _, f := range rec.Schema().Fields() {
		assert.NotEqual(t, "sql", f.Name)
		assert.NotEqual(t, "ticket", f.Name)
	}
}

// TestPrepareLeavesAStatementWithoutMacrosAlone.
func TestPrepareLeavesAStatementWithoutMacrosAlone(t *testing.T) {
	r := newRig(t, Limits{})
	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: Caller{By: person}, SQL: "SELECT 1"})
	require.NoError(t, err)
	assert.Equal(t, Binding{SQL: "SELECT 1"}, b)
	b, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: Caller{By: person}, SQL: "SELEKT nonsense"})
	require.NoError(t, err, "an unparseable statement without a macro is the server's to refuse")
	assert.Equal(t, "SELEKT nonsense", b.SQL)
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: Caller{By: person}, SQL: "SELEKT * FROM session('a')"})
	assert.Error(t, err)
}

// TestAnAgentsPreparedStatementIsARead.
func TestAnAgentsPreparedStatementIsARead(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	_, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(),
		SQL: "SELECT * FROM session('a') JOIN url('http://example.invalid', CSV, 'x UInt8') USING x"})
	assert.ErrorContains(t, err, "not a plain read")
}

// TestALostSessionMidStatementIsRetriedOnce: the server drops the session
// between the probe and the create; the capability opens a fresh one and
// replays what the definition reads.
func TestALostSessionMidStatementIsRetriedOnce(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.define(c, "a", "SELECT 1 AS x")
	var once sync.Once
	r.stub.afterProbe = func() { once.Do(r.stub.expireAll) }
	res := r.define(c, "b", "SELECT x FROM session('a')")
	assert.Equal(t, []string{"a@1"}, res.Replayed)
	assert.Equal(t, DatasetStateLive, res.Dataset.State)
}

// TestCloseDropsEveryTable is the host stopping.
func TestCloseDropsEveryTable(t *testing.T) {
	r := newRig(t, Limits{})
	r.define(r.fullAgent("t1"), "a", "SELECT 1 AS x")
	ticket, err := r.svc.Open(context.Background(), person)
	require.NoError(t, err)
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: r.endpoint(), Name: "p", SQL: "SELECT 2 AS x"})
	require.NoError(t, err)
	require.NoError(t, r.svc.Close(context.Background()))
	assert.ElementsMatch(t, []string{"__sd_a_r1", "__sd_p_r1"}, r.stub.drops())
	_, err = r.svc.Open(context.Background(), person)
	assert.Error(t, err)
}
