package linkhost

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

const testAppId app.AppIdT = "example.org/apps/sheet"

type sqlArgs struct {
	Sql string
}

type cellArgs struct {
	Row int64
}

var sqlCodec = applink.Codec[sqlArgs]{
	Format: func(v sqlArgs) (string, error) {
		return applink.Tail{Query: map[string][]string{"sql": {v.Sql}}}.String(), nil
	},
	Parse: func(tail string) (v sqlArgs, err error) {
		t, err := applink.ParseTail(tail)
		if err != nil {
			return
		}
		if err = t.OnlyKeys("sql"); err != nil {
			return
		}
		v.Sql, _ = t.Value("sql")
		return
	},
	Examples: []sqlArgs{{Sql: "SELECT 1"}},
}

var cellCodec = applink.Codec[cellArgs]{
	Format: func(v cellArgs) (string, error) {
		return "/" + strings.Repeat("x", int(v.Row)), nil
	},
	Parse: func(tail string) (v cellArgs, err error) {
		v.Row = int64(len(strings.TrimPrefix(tail, "/")))
		return
	},
	Examples: []cellArgs{{Row: 2}},
}

func testCatalog() *app.OperationsCatalog {
	return &app.OperationsCatalog{
		Resources: []app.ResourceSpec{{Name: "sql", Summary: "the statement"}, {Name: "cursor", Summary: "the selected row"}},
		Operations: []app.OperationSpec{
			{Name: "select_row", Version: 1, Summary: "select a row", Class: app.OperationClassCommand, Effect: app.OperationEffectView,
				Writes: []string{"cursor"}, Args: reflect.TypeFor[cellArgs](), Agents: true, Link: cellCodec},
			{Name: "set_sql", Version: 1, Summary: "set the statement", Class: app.OperationClassCommand, Effect: app.OperationEffectDocument,
				Writes: []string{"sql"}, Args: reflect.TypeFor[sqlArgs](), Agents: true, Link: sqlCodec},
			{Name: "run_sql", Version: 2, Summary: "run a statement", Class: app.OperationClassCommand, Effect: app.OperationEffectRun,
				Writes: []string{"sql"}, Args: reflect.TypeFor[sqlArgs](), Agents: true, Link: sqlCodec, LinkStage: "set_sql"},
			{Name: "run_unstaged", Version: 1, Summary: "run without a stage", Class: app.OperationClassCommand, Effect: app.OperationEffectRun,
				Writes: []string{"sql"}, Args: reflect.TypeFor[sqlArgs](), Agents: true, Link: sqlCodec},
			{Name: "get_sql", Version: 1, Summary: "read the statement", Class: app.OperationClassQuery, Effect: app.OperationEffectNone,
				Reads: []string{"sql"}, Agents: true},
		},
	}
}

type submitted struct {
	key uint64
	op  string
	req opwire.CallRequest
}

type fakeHost struct {
	mu        sync.Mutex
	nextKey   uint64
	windows   []windowhost.WindowInfo
	opened    []uint64
	raised    []uint64
	submitted []submitted
}

func (inst *fakeHost) OpsOpen(appId app.AppIdT, kind string, cfg []byte) (key uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.nextKey++
	key = 100 + inst.nextKey
	inst.windows = append(inst.windows, windowhost.WindowInfo{Key: windowhost.WindowKeyT(key), AppId: appId})
	inst.opened = append(inst.opened, key)
	return
}

func (inst *fakeHost) OpsRaise(key uint64) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.raised = append(inst.raised, key)
	return
}

func (inst *fakeHost) OpsInstances() (out []opwire.InstanceInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range inst.windows {
		out = append(out, opwire.InstanceInfo{App: w.AppId, Alias: app.AliasOf(w.AppId), Key: uint64(w.Key), Ops: true, Load: opwire.LoadReady})
	}
	return
}

func (inst *fakeHost) OpsRevisions(key uint64) (revs map[string]uint64, ok bool) {
	return map[string]uint64{"sql": 4, "cursor": 9}, true
}

func (inst *fakeHost) OpsStatus(key uint64, callId string) (out opwire.Outcome, ok bool) {
	return opwire.Outcome{Phase: opwire.PhaseRendered}, true
}

func (inst *fakeHost) OpsSubmit(ctx context.Context, key uint64, op string, req opwire.CallRequest) (out opwire.Outcome, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.submitted = append(inst.submitted, submitted{key: key, op: op, req: req})
	return opwire.Outcome{Phase: opwire.PhaseAccepted}, true
}

func (inst *fakeHost) ResolveLaunchKind(kind string) (id app.AppIdT, err error) {
	return testAppId, nil
}

func (inst *fakeHost) WindowInfos() (out []windowhost.WindowInfo) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append(out, inst.windows...)
}

type fakeFollower struct {
	live     bool
	followed []agent.LinkClick
}

func (inst *fakeFollower) FollowLink(ctx context.Context, c agent.LinkClick) (out agent.LinkFollowed) {
	inst.followed = append(inst.followed, c)
	if !inst.live {
		out.Reason = "the task that made the link has ended"
		return
	}
	return agent.LinkFollowed{Authorised: true, Window: 55, Outcome: opwire.Outcome{Phase: opwire.PhaseRendered}}
}

func (inst *fakeFollower) TaskLive(taskId string) bool { return inst.live }

type rig struct {
	svc  *Service
	host *fakeHost
}

func newRig(t *testing.T) *rig {
	t.Helper()
	reg := app.NewRegistry()
	require.NoError(t, reg.RegisterFactory(app.Manifest{Id: testAppId, Alias: "sheet", Display: "Sheet", Summary: "a sheet",
		Surface: app.SurfaceWindowed, Topics: []app.TopicT{app.Topics()[0]}, Operations: testCatalog()},
		func() (app.AppI, error) { return nil, nil }))
	host := &fakeHost{}
	svc, err := New(Config{Bus: inprocbus.NewInst(zerolog.Nop()), Host: host, Registry: reg, Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	return &rig{svc: svc, host: host}
}

var modelText = applink.Author{Level: applink.LevelOwnModel}

func TestAuthorless_DecidesByEffectAndWindow(t *testing.T) {
	cat := testCatalog()
	look := func(n string) app.OperationSpec { s, _ := cat.Lookup(n); return s }
	cases := []struct {
		op       string
		fresh    bool
		decision string
		calls    string
	}{
		{"select_row", false, applink.DecisionApplied, "select_row"},
		{"select_row", true, applink.DecisionApplied, "select_row"},
		{"set_sql", true, applink.DecisionApplied, "set_sql"},
		{"set_sql", false, applink.DecisionRefused, ""},
		{"run_sql", true, applink.DecisionStaged, "set_sql"},
		{"run_sql", false, applink.DecisionRefused, ""},
		{"run_unstaged", true, applink.DecisionRefused, ""},
		{"get_sql", true, applink.DecisionRefused, ""},
	}
	for _, c := range cases {
		op, d, _ := authorless(look(c.op), true, c.fresh)
		assert.Equal(t, c.decision, d, c.op)
		assert.Equal(t, c.calls, op, c.op)
	}
	_, d, _ := authorless(app.OperationSpec{}, false, false)
	assert.Equal(t, applink.DecisionReached, d)
}

func TestFollow_InlineDocumentOpensAWindowAndMarksTheCall(t *testing.T) {
	r := newRig(t)
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: "boxer:open/app/sheet/set_sql@1?sql=SELECT+2", Author: modelText}, "chat")
	require.Equal(t, applink.DecisionApplied, rep.Decision, rep.Reason)
	require.Len(t, r.host.submitted, 1)
	s := r.host.submitted[0]
	assert.Equal(t, "set_sql", s.op)
	assert.Equal(t, opwire.WriterPerson, s.req.Writer)
	assert.Nil(t, s.req.OnBehalfOf, "an authorless call is not agent-caused")
	require.NotNil(t, s.req.Link)
	assert.Equal(t, applink.LevelOwnModel, s.req.Link.Level)
	assert.Equal(t, app.AppIdT("chat"), s.req.Link.Surface)
	assert.Empty(t, s.req.Expects, "an authorless call read nothing and expects nothing")
	args, err := buscodec.Decode[sqlArgs](s.req.Args)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", args.Sql)
}

func TestFollow_AuthorlessRunIsStaged(t *testing.T) {
	r := newRig(t)
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: "boxer:open/app/sheet/run_sql@2?sql=DROP+TABLE+x", Author: modelText}, "chat")
	require.Equal(t, applink.DecisionStaged, rep.Decision, rep.Reason)
	require.Len(t, r.host.submitted, 1)
	assert.Equal(t, "set_sql", r.host.submitted[0].op, "the run is staged, never started")
}

func TestFollow_RaiseTakesOnlyViewEffectsIntoAnOpenWindow(t *testing.T) {
	r := newRig(t)
	r.host.windows = []windowhost.WindowInfo{
		{Key: 3, AppId: testAppId, Geom: windowhost.WindowGeom{Stack: 1}},
		{Key: 4, AppId: testAppId, Geom: windowhost.WindowGeom{Stack: 7}},
	}
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: "boxer:raise/app/sheet/set_sql@1?sql=x", Author: modelText}, "chat")
	assert.Equal(t, applink.DecisionRefused, rep.Decision)
	assert.Empty(t, r.host.submitted)
	assert.Empty(t, r.host.opened, "a refused link opens nothing")

	rep = r.svc.follow(t.Context(), applink.FollowRequest{Link: "boxer:raise/app/sheet/select_row@1/xx", Author: modelText}, "chat")
	require.Equal(t, applink.DecisionApplied, rep.Decision, rep.Reason)
	assert.Equal(t, uint64(4), rep.Window, "the topmost window")
	assert.Equal(t, []uint64{4}, r.host.raised)
}

func TestFollow_RaiseWithNoWindowOpensOne(t *testing.T) {
	r := newRig(t)
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: "boxer:raise/app/sheet/set_sql@1?sql=x", Author: modelText}, "chat")
	require.Equal(t, applink.DecisionApplied, rep.Decision, rep.Reason)
	assert.Len(t, r.host.opened, 1)
}

func TestFollow_Refusals(t *testing.T) {
	r := newRig(t)
	for name, link := range map[string]string{
		"wrong version":   "boxer:open/app/sheet/set_sql@2?sql=x",
		"unknown app":     "boxer:open/app/nosuch",
		"unknown key":     "boxer:open/app/sheet/set_sql@1?bad=x",
		"expired handle":  "boxer:l/" + strings.Repeat("ab", applink.HandleBytes),
		"malformed":       "boxer:open/app/sheet/set_sql",
		"unknown op":      "boxer:open/app/sheet/nosuch@1",
		"a query by link": "boxer:open/app/sheet/get_sql@1",
	} {
		rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: link, Author: modelText}, "chat")
		assert.Equal(t, applink.DecisionRefused, rep.Decision, name)
		assert.NotEmpty(t, rep.Reason, name)
	}
	assert.Empty(t, r.host.opened)
}

func TestMint_ContentAddressedAndBounded(t *testing.T) {
	r := newRig(t)
	rec := applink.Record{Verb: applink.VerbOpen, App: "sheet", Task: "task-1", Epoch: 1}
	a, err := r.svc.MintTask(rec)
	require.NoError(t, err)
	b, err := r.svc.MintTask(rec)
	require.NoError(t, err)
	assert.Equal(t, a, b, "the same record gets the same handle")
	rec.Epoch = 2
	c, err := r.svc.MintTask(rec)
	require.NoError(t, err)
	assert.NotEqual(t, a, c)
	for i := 0; i < MaxRecords; i++ {
		_, err = r.svc.mint(applink.Record{Verb: applink.VerbOpen, App: "sheet", Window: uint64(i + 1)})
		require.NoError(t, err)
	}
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: a}, "chat")
	assert.Equal(t, applink.DecisionRefused, rep.Decision)
	assert.Equal(t, expiredReason, rep.Reason, "the oldest record was evicted")
}

func TestFollow_MintedRunsUnderTheGrantOrFallsBack(t *testing.T) {
	r := newRig(t)
	f := &fakeFollower{live: true}
	r.svc.SetFollower(f)
	args, err := buscodec.Encode(sqlArgs{Sql: "SELECT 3"})
	require.NoError(t, err)
	link, err := r.svc.MintTask(applink.Record{Verb: applink.VerbOpen, App: "sheet", Operation: "run_sql", Version: 2, Args: args,
		AuthorLevel: applink.LevelOwnModel, Task: "task-1", Epoch: 1})
	require.NoError(t, err)

	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: link, Author: applink.Author{Level: applink.LevelPerson}}, "mdedit")
	require.Equal(t, applink.DecisionApplied, rep.Decision, rep.Reason)
	assert.False(t, rep.Authorless)
	assert.Equal(t, uint64(55), rep.Window)
	require.Len(t, f.followed, 1)
	assert.Empty(t, r.host.submitted, "the dispatcher made the call, not the host")

	f.live = false
	rep = r.svc.follow(t.Context(), applink.FollowRequest{Link: link, Author: applink.Author{Level: applink.LevelPerson}}, "mdedit")
	require.Equal(t, applink.DecisionStaged, rep.Decision, rep.Reason)
	assert.True(t, rep.Authorless)
	require.Len(t, r.host.submitted, 1)
	assert.Equal(t, applink.LevelOwnModel, r.host.submitted[0].req.Link.Level, "the record's author, not the text the handle was pasted in")
}

func TestDescribe_SaysWhatAClickWouldDo(t *testing.T) {
	r := newRig(t)
	d := r.svc.Describe("boxer:open/app/sheet/run_sql@2?sql=SELECT+1", modelText)
	assert.Empty(t, d.Refused)
	assert.Equal(t, "Sheet", d.Display)
	assert.Equal(t, "run a statement", d.Summary)
	assert.Contains(t, d.Args, "SELECT 1")
	assert.Equal(t, "a model", d.Author)
	assert.Contains(t, d.Would, "puts the work in place")
	assert.Contains(t, d.Text(), "Sheet — run a statement")

	d = r.svc.Describe("boxer:open/app/sheet/set_sql@9", modelText)
	assert.NotEmpty(t, d.Refused)
}

func TestWindowLink_RaisesOnlyItsWindow(t *testing.T) {
	r := newRig(t)
	r.host.windows = []windowhost.WindowInfo{{Key: 3, AppId: testAppId}}
	link, err := r.svc.MintWindow(3)
	require.NoError(t, err)
	rep := r.svc.follow(t.Context(), applink.FollowRequest{Link: link, Author: modelText}, "chat")
	require.Equal(t, applink.DecisionReached, rep.Decision, rep.Reason)
	assert.Equal(t, []uint64{3}, r.host.raised)
	r.host.windows = nil
	rep = r.svc.follow(t.Context(), applink.FollowRequest{Link: link}, "chat")
	assert.Equal(t, applink.DecisionRefused, rep.Decision)
}
