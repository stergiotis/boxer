package agent

import (
	"bytes"
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// e2eQueryEndpoint stands in for the host's introspection endpoint; nothing
// is queried over it here, the windows only move to it.
const e2eQueryEndpoint = "http://127.0.0.1:1/query"

// The ADR-0288 (proposed) M5 lane: a chat-like coordinator, under a grant
// the person approved, has one play window publish its result as a bundle
// — the person confirms — and another open it; a third party reads the
// dataset whole. Every hop's bytes are the producer's, and every hop is a
// trail row that joins the dispatcher's action rows on (task, call) and
// carries the conversation and turn the coordinator stated.
func TestABundleCrossesWindowsWithItsProvenance(t *testing.T) {
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
	rec := trail.NewRecorder(exec, "run-e2e", zerolog.Nop())
	t.Cleanup(rec.Close)
	prevEndpoint := introspect.LocalQueryEndpoint()
	introspect.SetLocalQueryEndpoint(e2eQueryEndpoint)
	t.Cleanup(func() { introspect.SetLocalQueryEndpoint(prevEndpoint) })

	manifest := (&play.PlayLauncher{}).Manifest()
	r := newRigWith(t, func(cfg *Config) {
		cfg.Coordinators = []string{"test.coordinator"}
		cfg.Trail = rec
		require.NoError(t, cfg.Registry.RegisterFactory(manifest, func() (app.AppI, error) { return nil, nil }))
	})
	svc, err := adhocdata.NewService(adhocdata.Config{Bus: r.bus, Registry: introspect.NewRegistry(), Dir: t.TempDir(),
		Log: zerolog.Nop(), Trail: rec})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(ctx) })
	svc.SetDispatcher(r.svc)

	caps := []app.SubjectFilter{{Pattern: "adhoc.>", Direction: app.CapDirectionBoth, Reason: "test"}}
	window := func(key uint64) (l *play.PlayLauncher) {
		c := r.bus.NewClient(manifest.Id, caps)
		c.SetInstanceKey(key)
		l = play.NewHeadlessLauncherForTest(c, zerolog.Nop(), "http://ch.example:8123/")
		e := opengine.New(manifest.Operations, l.Operations())
		r.host.mu.Lock()
		r.host.engines[key] = e
		if r.host.apps == nil {
			r.host.apps = map[uint64]app.AppIdT{}
		}
		r.host.apps[key] = manifest.Id
		r.host.mu.Unlock()
		e.SetListener(func(entry opengine.LogEntry) { r.svc.Listener()(key, entry) })
		r.host.frame(key)
		return
	}
	const producerKey, consumerKey = 20, 21
	producer, consumer := window(producerKey), window(consumerKey)
	producer.SetMainResultForTest(e2eInts(t, 0, 1, 2, 3, 4), "SELECT number AS n FROM numbers(5)")
	frameUntil := func(key uint64, l *play.PlayLauncher, cond func() bool) {
		t.Helper()
		require.Eventually(t, func() bool {
			r.host.frame(key)
			l.SyncForTest()
			return cond()
		}, 5*time.Second, 5*time.Millisecond)
	}

	got := make(chan Grant, 1)
	go func() {
		g, gErr := r.cli.Request(ctx, GrantRequest{Plan: "move a result between windows", Conversation: "conv-e2e",
			Entries: []GrantEntry{{Instance: producerKey, Mode: ModeAct}, {Instance: consumerKey, Mode: ModeAct}}})
		assert.NoError(t, gErr)
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	require.NotEmpty(t, g.Handle)

	// The producer publishes; publishing is consequential, so the person
	// confirms it even in act mode.
	out, err := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: producerKey, Operation: "publish_result",
		Args: `{"bundle":"counts","sql":"SELECT n FROM keelson('result') ORDER BY n","tabs":["chart"]}`, Key: "pub",
		Turn: "turn-1", ModelCall: "llm-1", ToolCall: "call_pub", ToolIndex: 0})
	require.NoError(t, err)
	require.Equal(t, "proposed", out.Phase, out.Reason)
	r.decideProposal(true, true)
	frameUntil(producerKey, producer, func() bool {
		last := producer.LastPublishForTest()
		return last.Revision == 1 && !last.Pending
	})
	require.Empty(t, producer.LastPublishForTest().Error)

	// The consumer opens it, in a later turn; a command reads what it
	// writes first (ADR-0269 §SD1), and get_state is that read.
	out, err = r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: consumerKey, Operation: "get_state", Args: `{}`, Key: "state",
		Turn: "turn-2", ModelCall: "llm-2", ToolCall: "call_state", ToolIndex: 0})
	require.NoError(t, err)
	require.Equal(t, "completed", out.Phase, out.Reason)
	out, err = r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: consumerKey, Operation: "open_bundle",
		Args: `{"alias":"counts"}`, Key: "open", Turn: "turn-2", ModelCall: "llm-2", ToolCall: "call_open", ToolIndex: 1})
	require.NoError(t, err)
	require.Contains(t, []string{"accepted", "applied"}, out.Phase, out.Reason)
	frameUntil(consumerKey, consumer, func() bool { return len(consumer.DatasetBindingsForTest()) == 1 })
	assert.Equal(t, "SELECT n FROM keelson('result') ORDER BY n", consumer.BufferForTest())

	// A third party reads the dataset whole.
	reader := r.bus.NewClient("test.notebook", caps)
	read, err := adhocdata.ReadAllE(reader, "counts__result", nil)
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1, 2, 3, 4}, e2eValues(t, read.ArrowIPCStream), "the producer's rows, as play held them")
	assert.Equal(t, consumer.DatasetBindingsForTest()["result"], read.Handle, "the consumer bound the dataset that was read")

	// The provenance, from the trail alone.
	require.NoError(t, rec.Flush(ctx))
	scan := func(pick func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error]) []*trail.TrailEntity {
		ents, sErr := rec.Scan(pick)
		require.NoError(t, sErr)
		return ents
	}
	bundleRows := scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAdhocDataset(ctx, recordstore.ScanOpts{})
	})
	actions := scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAgentAction(ctx, recordstore.ScanOpts{})
	})
	byOp := map[string]*trail.TrailEntity{}
	for _, e := range bundleRows {
		byOp[e.AdhocDataset.Val.Operation] = e
	}
	callOf := map[string]string{}
	for _, a := range actions {
		if a.Delegation.Has && a.Delegation.Val.Call.Has {
			callOf[a.AgentAction.Val.Operation] = a.Delegation.Val.Call.Val
		}
	}

	pub := byOp[adhocdata.AuditPublish]
	require.NotNil(t, pub, "the publish is a trail row")
	assert.True(t, pub.AdhocDataset.Val.Attested)
	assert.Equal(t, callOf["publish_result"], pub.Delegation.Val.Call.Val, "it joins the publish_result action on (task, call)")
	assert.Equal(t, "conv-e2e", pub.Conversation.Val.Conversation)
	assert.Equal(t, "turn-1", pub.Conversation.Val.Turn.Val)
	assert.Equal(t, "llm-1", pub.Cause.Val.ModelCall)
	assert.Equal(t, []string{"counts__result"}, pub.AdhocDataset.Val.Aliases)

	open := byOp[adhocdata.AuditResolve]
	require.NotNil(t, open, "the agent's open resolved the bundle, and that is a trail row")
	assert.Equal(t, callOf["open_bundle"], open.Delegation.Val.Call.Val)
	assert.Equal(t, "turn-2", open.Conversation.Val.Turn.Val)

	rd := byOp[adhocdata.AuditRead]
	require.NotNil(t, rd, "the read is a trail row")
	assert.False(t, rd.AdhocDataset.Val.Attested, "no agent's call caused it")
	assert.Equal(t, pub.AdhocDataset.Val.StreamDigests, rd.AdhocDataset.Val.StreamDigests,
		"the read names the bytes the publish sealed")
	assert.Equal(t, read.StreamDigest, rd.AdhocDataset.Val.StreamDigests[0])
}

func e2eInts(t *testing.T, vals ...int64) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	return rb.NewRecordBatch()
}

func e2eValues(t *testing.T, stream []byte) (vals []int64) {
	t.Helper()
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	defer rdr.Release()
	for rdr.Next() {
		vals = append(vals, rdr.RecordBatch().Column(0).(*array.Int64).Int64Values()...)
	}
	require.NoError(t, rdr.Err())
	return
}
