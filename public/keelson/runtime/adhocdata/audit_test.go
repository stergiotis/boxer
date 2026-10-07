package adhocdata

import (
	"context"
	"encoding/hex"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// fakeDispatcher attests one call, sent to one window, as the agent
// dispatcher would; it records what it was asked.
type fakeDispatcher struct {
	mu     sync.Mutex
	call   app.CallContext
	asked  []app.CallContext
	refuse string
	grants []string
}

func (inst *fakeDispatcher) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.asked = append(inst.asked, app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance})
	switch {
	case inst.refuse != "":
		return cc, false, inst.refuse
	case task != inst.call.Task || epoch != inst.call.Epoch || call != inst.call.Call:
		return cc, false, "the task has no call by that id"
	case sender != inst.call.App || senderInstance != inst.call.Instance:
		return cc, false, "the call was routed to another window"
	}
	return inst.call, true, ""
}

// AllowDestination grants what grants lists for the dispatcher's task.
func (inst *fakeDispatcher) AllowDestination(task string, epoch uint64, destination string) (ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if task == inst.call.Task && slices.Contains(inst.grants, destination) {
		return true, ""
	}
	return false, "the task's grant does not list " + destination
}

var _ DispatcherI = (*fakeDispatcher)(nil)

func agentCall() app.CallContext {
	return app.CallContext{Task: "task-1", Epoch: 3, Call: "task-1-7", Conversation: "conv-9", Turn: "turn-2",
		ModelCall: "llm-4", ToolCall: "call_a", ToolIndex: 1, App: windowA.App, Instance: windowA.Instance, Operation: "publish_result"}
}

func oboOf(cc app.CallContext) *app.OnBehalfOf {
	return &app.OnBehalfOf{Task: cc.Task, Epoch: cc.Epoch, Call: cc.Call}
}

func TestAnAgentsPublishCarriesTheDispatchersContext(t *testing.T) {
	svc := newTestService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	res, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: oboOf(d.call)})
	require.NoError(t, err)
	require.True(t, res.Context.Has)
	assert.Equal(t, "turn-2", res.Context.Val.Turn, "the turn is the dispatcher's, the publisher never sent one")

	audits := svc.auditRecords()
	require.Len(t, audits, 1)
	a := audits[0]
	assert.Equal(t, AuditPublish, a.Operation)
	assert.Equal(t, AuditApplied, a.Outcome)
	require.True(t, a.Context.Has)
	assert.Equal(t, "conv-9", a.Context.Val.Conversation)
	assert.Equal(t, "llm-4", a.Context.Val.ModelCall)
	assert.Equal(t, []string{"orders", "regions"}, a.LocalNames)
	assert.Equal(t, []uint64{3, 1}, a.Rows)
	assert.Equal(t, res.DocumentDigest, a.DocumentDigest)

	for i, h := range a.Handles {
		plain, _ := readAll(t, svc.reg, h)
		sum := blake3.Sum256(plain)
		assert.Equal(t, hex.EncodeToString(sum[:16]), a.StreamDigests[i], "the digest is over the bytes every reader reads")
	}

	for _, r := range svc.catalogRows() {
		assert.Equal(t, "task-1-7", r.call)
		assert.Equal(t, "turn-2", r.turn)
	}
	rows := svc.bundleCatalogRows()
	require.Len(t, rows, 1)
	assert.Equal(t, "conv-9", rows[0].conversation)

	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: oboOf(d.call)})
	require.NoError(t, err)
	assert.Equal(t, AuditRepublish, svc.auditRecords()[1].Operation)
}

func TestAnUnattestedContextIsRefusedAndAudited(t *testing.T) {
	svc := newTestService(t)
	call := agentCall()

	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: oboOf(call)})
	assert.ErrorIs(t, err, ErrUnattested, "no dispatcher, nothing can confirm the claim")

	d := &fakeDispatcher{call: call}
	svc.SetDispatcher(d)
	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowB, OnBehalfOf: oboOf(call)})
	assert.ErrorIs(t, err, ErrUnattested, "another window cannot publish under the call")
	assert.Equal(t, 0, svc.LiveCount())

	audits := svc.auditRecords()
	require.Len(t, audits, 2)
	for _, a := range audits {
		assert.Equal(t, AuditRefused, a.Outcome)
		assert.False(t, a.Context.Has, "a refused claim is not recorded as the call's work")
	}
	assert.Contains(t, audits[1].Reason, "another window")
	assert.Equal(t, windowB, audits[1].By)
}

func TestThePersonsOperationsAreAuditedWithoutAContext(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowA})
	require.NoError(t, err)
	_, err = svc.ResolveBundle("sales", windowB, nil)
	require.NoError(t, err)
	require.NoError(t, svc.RetractBundle("sales", windowA, nil))
	audits := svc.auditRecords()
	require.Len(t, audits, 2, "a resolve no agent caused is not a trail row")
	assert.Equal(t, AuditPublish, audits[0].Operation)
	assert.Equal(t, AuditRetract, audits[1].Operation)
	assert.Equal(t, []string{"sales__orders", "sales__regions"}, audits[1].Aliases, "a retract names what it withdrew")
	for _, a := range audits {
		assert.False(t, a.Context.Has)
	}
}

func TestAnAgentsResolveIsAudited(t *testing.T) {
	svc := newTestService(t)
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)
	_, err := svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t), By: windowB})
	require.NoError(t, err)
	_, err = svc.ResolveBundle("sales", windowA, oboOf(d.call))
	require.NoError(t, err)
	audits := svc.auditRecords()
	require.Len(t, audits, 2)
	assert.Equal(t, AuditResolve, audits[1].Operation)
	assert.True(t, audits[1].Context.Has)
	assert.Equal(t, windowB, audits[1].Owner)
}

func TestAClosedWindowsBundleIsAuditedAsWithdrawn(t *testing.T) {
	logger := testLogger(t)
	bus := inprocbus.NewInst(logger)
	svc, err := NewService(Config{Bus: bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	d := &fakeDispatcher{call: agentCall()}
	d.call.App, d.call.Instance = "test.app", 5
	svc.SetDispatcher(d)
	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	window := bus.NewClient("test.app", caps)
	window.SetInstanceKey(5)

	_, err = PublishBundleRequest(window, BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		OnBehalfOf: oboOf(d.call)})
	require.NoError(t, err)
	d.mu.Lock()
	require.Len(t, d.asked, 1)
	assert.Equal(t, app.AppIdT("test.app"), d.asked[0].App, "the sender is the envelope's")
	assert.Equal(t, uint64(5), d.asked[0].Instance)
	assert.Equal(t, "task-1-7", d.asked[0].Call, "the call travelled the wire")
	d.mu.Unlock()

	require.NoError(t, window.Close())
	audits := svc.auditRecords()
	require.Len(t, audits, 2)
	assert.True(t, audits[0].Context.Has)
	assert.Equal(t, AuditWithdraw, audits[1].Operation)
	assert.True(t, audits[1].By.IsRuntime())
	assert.Equal(t, Identity{App: "test.app", Instance: 5}, audits[1].Owner)
}

// The audit lands on the trail as an AdhocDataset row with the attested
// components (ADR-0288 (proposed) §SD5).
func TestTheAuditLandsOnTheTrail(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}
	rec := trail.NewRecorder(exec, "run-1", testLogger(t))
	t.Cleanup(rec.Close)
	svc, err := NewService(Config{Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: testLogger(t), Trail: rec})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	d := &fakeDispatcher{call: agentCall()}
	svc.SetDispatcher(d)

	_, err = svc.PublishBundle(BundlePublishInput{Alias: "sales", Document: []byte(testDoc), Datasets: twoDatasets(t),
		By: windowA, OnBehalfOf: oboOf(d.call)})
	require.NoError(t, err)
	require.NoError(t, rec.Flush(ctx))
	ents, err := rec.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAdhocDataset(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	require.Len(t, ents, 1)
	e := ents[0]
	assert.Equal(t, string(windowA.App), e.Origin.Val.App)
	assert.Equal(t, "task-1-7", e.Delegation.Val.Call.Val)
	assert.Equal(t, "turn-2", e.Conversation.Val.Turn.Val)
	assert.Equal(t, "llm-4", e.Cause.Val.ModelCall)
	assert.Equal(t, "sales", e.AdhocDataset.Val.Bundle)
	assert.Equal(t, svc.auditRecords()[0].StreamDigests, e.AdhocDataset.Val.StreamDigests)
	assert.True(t, e.AdhocDataset.Val.Attested)
}
