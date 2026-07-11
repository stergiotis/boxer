package play

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diagMockExecutor is a nodeExecutorI for the probe lane: records the demanded
// nodes and returns a canned outcome per SQL.
type diagMockExecutor struct {
	calls atomic.Int64
	sqls  []compiledNode
	fail  func(sql string) error // nil result → success with a minimal record
}

func (inst *diagMockExecutor) execute(ctx context.Context, cn compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	inst.calls.Add(1)
	inst.sqls = append(inst.sqls, cn)
	if inst.fail != nil {
		if fErr := inst.fail(cn.SQL); fErr != nil {
			err = fErr
			return
		}
	}
	rec = int64Rec("explain", 1)
	schema = rec.Schema()
	return
}

func newTestDiagDriver(exec nodeExecutorI) *DiagnosticsDriver {
	return &DiagnosticsDriver{
		lane: newNodeLane(exec, memory.NewGoAllocator(), 0),
		// A pass-through residual builder: prelude harvesting is BuildStatement
		// territory, covered by the client tests.
		buildResidual: func(s string) (string, map[string]string) { return s, nil },
	}
}

func waitVerdict(t *testing.T, d *DiagnosticsDriver, want probeVerdictE) (detail string) {
	t.Helper()
	var v probeVerdictE
	require.Eventually(t, func() bool {
		v, detail = d.probeView()
		return v == want
	}, 2*time.Second, 5*time.Millisecond, "verdict never became %d (last %d)", want, v)
	return
}

func TestDiagnosticsProbeAccepted(t *testing.T) {
	exec := &diagMockExecutor{}
	d := newTestDiagDriver(exec)
	defer d.close()

	// A grammar-parseable buffer never arms the probe.
	d.noteParse("SELECT 1", nil)
	v, _ := d.probeView()
	assert.Equal(t, probeNone, v)
	assert.Zero(t, exec.calls.Load(), "parse success must not probe the server")

	// A grammar failure arms it; the server accepting yields the verdict.
	d.noteParse("SHOW TABLES", errors.New("grammar1: no viable alternative"))
	waitVerdict(t, d, probeAccepted)
	require.NotEmpty(t, exec.sqls)
	assert.Equal(t, diagProbePrefix+"SHOW TABLES", exec.sqls[0].SQL,
		"probe must wrap the residual in EXPLAIN AST with the newline prefix")

	// Disarming (buffer became parseable) drops the verdict without a probe.
	d.noteParse("SELECT 1", nil)
	v, _ = d.probeView()
	assert.Equal(t, probeNone, v)
}

func TestDiagnosticsProbeRejectedAndUnavailable(t *testing.T) {
	serverErr := errors.New(`clientExecutor.execute: clickhouse http 400: Code: 62. DB::Exception: Syntax error: failed at position 19 (1) (line 2, col 7): 1. (SYNTAX_ERROR)`)
	exec := &diagMockExecutor{fail: func(sql string) error { return serverErr }}
	d := newTestDiagDriver(exec)
	defer d.close()

	d.noteParse("SELEC 1", errors.New("grammar1: mismatched input"))
	detail := waitVerdict(t, d, probeRejected)
	assert.Contains(t, detail, "Code: 62", "detail slices from the ClickHouse diagnostic")
	assert.NotContains(t, detail, "clientExecutor.execute", "transport prefix stripped")
	assert.Contains(t, detail, "(line 1, col 7)", "probe-prefix line offset corrected")

	// A non-400 failure is not a statement verdict.
	exec2 := &diagMockExecutor{fail: func(sql string) error {
		return errors.New("clientExecutor.execute: clickhouse request failed: dial tcp: connection refused")
	}}
	d2 := newTestDiagDriver(exec2)
	defer d2.close()
	d2.noteParse("SELEC 1", errors.New("grammar1: mismatched input"))
	detail = waitVerdict(t, d2, probeUnavailable)
	assert.Contains(t, detail, "connection refused")
}

func TestDiagnosticsProbeSupersession(t *testing.T) {
	exec := &diagMockExecutor{}
	d := newTestDiagDriver(exec)
	defer d.close()

	gErr := errors.New("grammar1: nope")
	d.noteParse("SHOW TABLES", gErr)
	waitVerdict(t, d, probeAccepted)

	// A new buffer re-arms: the stale verdict must not leak while the new
	// probe is in flight — probeView reports pending until the served key
	// matches, then the fresh verdict.
	d.noteParse("SHOW DATABASES", gErr)
	waitVerdict(t, d, probeAccepted)
	last := exec.sqls[len(exec.sqls)-1]
	assert.Equal(t, diagProbePrefix+"SHOW DATABASES", last.SQL)
}

func TestDiagnosticsDriverNilSafety(t *testing.T) {
	// No client (tests, legacy CLI): the driver stays inert.
	d := NewDiagnosticsDriver(nil)
	defer d.close()
	d.noteParse("SHOW TABLES", errors.New("x"))
	v, detail := d.probeView()
	assert.Equal(t, probeNone, v)
	assert.Empty(t, detail)
}

func TestClassifyProbeError(t *testing.T) {
	v, detail := classifyProbeError(errors.New("clickhouse http 400: Code: 62. DB::Exception: boom"))
	assert.Equal(t, probeRejected, v)
	assert.Equal(t, "Code: 62. DB::Exception: boom", detail)

	v, _ = classifyProbeError(errors.New("clickhouse http 403: Code: 516. DB::Exception: auth"))
	assert.Equal(t, probeUnavailable, v)

	v, _ = classifyProbeError(errors.New("clickhouse request failed: timeout"))
	assert.Equal(t, probeUnavailable, v)
}

func TestAdjustProbeLineNumbers(t *testing.T) {
	assert.Equal(t, "err (line 1, col 7): x", adjustProbeLineNumbers("err (line 2, col 7): x"))
	assert.Equal(t, "err (line 41, col 3)", adjustProbeLineNumbers("err (line 42, col 3)"))
	// Line 1 (inside the probe prefix itself) is left alone rather than
	// becoming line 0.
	assert.Equal(t, "err (line 1, col 2)", adjustProbeLineNumbers("err (line 1, col 2)"))
	assert.Equal(t, "no positions here", adjustProbeLineNumbers("no positions here"))
}
