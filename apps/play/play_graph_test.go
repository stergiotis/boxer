package play

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stretchr/testify/require"
)

// mockExecutor is a deterministic nodeExecutorI: it counts calls, records the SQL
// it saw, and returns a freshly built record from build(sql). Ownership of the
// returned record transfers to the graph.
type mockExecutor struct {
	calls int
	sqls  []string
	build func(sql string) arrow.RecordBatch
}

func (inst *mockExecutor) execute(ctx context.Context, sql string, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	inst.calls++
	inst.sqls = append(inst.sqls, sql)
	rec = inst.build(sql)
	schema = rec.Schema()
	return
}

// int64Rec builds a one-column Int64 record the caller (the graph) then owns.
func int64Rec(col string, vals ...int64) (rec arrow.RecordBatch) {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: col, Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	defer arr.Release()
	rec = array.NewRecord(schema, []arrow.Array{arr}, int64(len(vals)))
	return
}

// selectParamX is a node whose compiled SQL embeds signal "x" — a stand-in for
// the param substitution slice 3 does via nanopass.
func selectParamX(sig SignalEnvI) (sql string, err error) {
	p, _ := sig.Get("x")
	sql = "SELECT " + p.Raw
	return
}

// minimality: unchanged inputs hit the memo; a changed signal re-executes.
func TestGraphMinimalityMemoizesUnchangedInputs(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	g := newQueryGraph(exec, memory.NewGoAllocator())
	defer g.close()
	g.addNode(&Node{ID: "main", Compile: selectParamX})
	g.setSignal("x", env.Param{Name: "x", Raw: "1"})

	r1, err := g.demand(context.Background(), "main")
	require.NoError(t, err)
	require.NoError(t, r1.err)
	require.NotNil(t, r1.rec)
	require.Equal(t, 1, exec.calls)

	_, err = g.demand(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, 1, exec.calls, "unchanged inputs must not re-execute")

	g.setSignal("x", env.Param{Name: "x", Raw: "2"})
	_, err = g.demand(context.Background(), "main")
	require.NoError(t, err)
	require.Equal(t, 2, exec.calls, "a changed signal must re-execute")
	require.Equal(t, []string{"SELECT 1", "SELECT 2"}, exec.sqls)
}

// demand: only an observed node executes; an undemanded node never runs.
func TestGraphDemandDrivenSkipsUnobservedNode(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return int64Rec("n", 7) }}
	g := newQueryGraph(exec, memory.NewGoAllocator())
	defer g.close()
	g.addNode(&Node{ID: "a", Compile: func(SignalEnvI) (string, error) { return "SELECT 1", nil }})
	g.addNode(&Node{ID: "b", Compile: func(SignalEnvI) (string, error) { return "SELECT 2", nil }})

	g.beginFrame()
	_, err := g.demand(context.Background(), "a")
	require.NoError(t, err)

	require.Equal(t, 1, exec.calls, "only the demanded node executes")
	require.True(t, g.isDemanded("a"))
	require.False(t, g.isDemanded("b"), "an undemanded node must not execute (demand-driven)")
	require.Equal(t, []string{"SELECT 1"}, exec.sqls)
}

// early cutoff: a re-execution yielding content-identical bytes keeps the same
// fingerprint, so a downstream observer would not be re-invalidated.
func TestGraphEarlyCutoffStableFingerprintOnIdenticalResult(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return int64Rec("n", 1, 2, 3) }}
	g := newQueryGraph(exec, memory.NewGoAllocator())
	defer g.close()
	g.addNode(&Node{ID: "main", Compile: selectParamX})

	g.setSignal("x", env.Param{Name: "x", Raw: "1"})
	r1, err := g.demand(context.Background(), "main")
	require.NoError(t, err)
	fp1 := r1.fingerprint

	g.setSignal("x", env.Param{Name: "x", Raw: "2"})
	r2, err := g.demand(context.Background(), "main")
	require.NoError(t, err)

	require.Equal(t, 2, exec.calls, "a changed signal re-executes the node")
	require.Equal(t, fp1, r2.fingerprint, "content-identical result ⇒ stable fingerprint (early cutoff)")

	// Control: genuinely different content ⇒ different fingerprint.
	a := int64Rec("n", 1, 2, 3)
	b := int64Rec("n", 9)
	require.NotEqual(t, fingerprintRecord(a), fingerprintRecord(b))
	a.Release()
	b.Release()
}

// revision discipline: the revision advances only on a real signal change.
func TestGraphSignalRevisionAdvancesOnlyOnChange(t *testing.T) {
	g := newQueryGraph(&mockExecutor{build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}, memory.NewGoAllocator())
	defer g.close()

	require.Equal(t, uint64(0), g.signals().Revision())
	g.setSignal("x", env.Param{Name: "x", Raw: "1"})
	require.Equal(t, uint64(1), g.signals().Revision())
	g.setSignal("x", env.Param{Name: "x", Raw: "1"})
	require.Equal(t, uint64(1), g.signals().Revision(), "an unchanged signal must not advance the revision")
	g.setSignal("x", env.Param{Name: "x", Raw: "2"})
	require.Equal(t, uint64(2), g.signals().Revision())
}

// countPanel is a minimal PanelI: it rejects until a result schema exists, then
// claims the field count. It proves the accept/reject contract end to end.
type countPanel struct{}

func (countPanel) ID() PanelID { return "count" }

func (countPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true}}
}

func (countPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "no result yet — run a query"
		return
	}
	claim = len(schema.Fields())
	return
}

func (countPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {}

func TestPanelAcceptRejectContract(t *testing.T) {
	exec := &mockExecutor{build: func(string) arrow.RecordBatch { return int64Rec("n", 1) }}
	g := newQueryGraph(exec, memory.NewGoAllocator())
	defer g.close()
	g.addNode(&Node{ID: "main", Compile: func(SignalEnvI) (string, error) { return "SELECT 1", nil }})

	var p PanelI = countPanel{}
	require.Equal(t, []ChannelSpec{{ID: chMain, Required: true}}, p.Channels())

	claim, reason := p.AcceptForChannel(chMain, nil, g.signals())
	require.Nil(t, claim)
	require.NotEmpty(t, reason, "no schema ⇒ reject with an empty-state reason")

	r, err := g.demand(context.Background(), "main")
	require.NoError(t, err)
	claim, reason = p.AcceptForChannel(chMain, r.schema, g.signals())
	require.Empty(t, reason, "a real schema ⇒ accept, no reason")
	require.Equal(t, 1, claim)
}
