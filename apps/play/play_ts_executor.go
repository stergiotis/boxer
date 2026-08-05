package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_ts_executor.go makes a recognised call executable (ADR-0163 §SD4): the
// input CTE runs on ClickHouse through the ordinary wire executor, the
// transform runs here in Go, and the result comes back as Arrow. Everything
// the lane gives an ordinary node — memoisation, supersession, staleness,
// last-good, off-render-thread execution — the client node gets unchanged,
// because it IS an ordinary lane whose compiled node happens to carry a
// transform.
//
// The transform rides on the COMPILED NODE rather than on the executor. The
// ADR sketched it the other way round, as an executor wrapping the wire
// executor, and that does not survive contact with the lanes: one intermediate
// lane serves whichever node is currently observed, so an executor pinned to a
// single node's call could not follow the observation. The compiled node is
// per demand, which is the granularity the transform actually varies at — and
// it is also what carries the call into the memo key.

// Key is the call as written, which is exactly what must distinguish two
// otherwise identical nodes: the fused SQL is the INPUT CTE and carries
// neither the function name nor its arguments.
func (inst *tsCall) Key() string { return inst.Text }

// Apply runs the transform over the input CTE's result.
func (inst *tsCall) Apply(in arrow.RecordBatch, params map[string]string, alloc memory.Allocator) (out arrow.RecordBatch, err error) {
	return applyTsCall(inst, in, params, alloc)
}

var _ clientTransformI = (*tsCall)(nil)

// clientNodeCaption is the honesty line a client node carries wherever its
// SQL is shown (ADR-0163 §SD5). It says three things a reader cannot get from
// the SQL: which engine ran it, what was actually sent, and — for a two-sided
// function — that the numbers are not what an alert would have known.
func clientNodeCaption(call *tsCall) (line string) {
	var b strings.Builder
	b.WriteString("computed client-side by ")
	b.WriteString(call.Spec.Name)
	b.WriteString(" — not sent to ClickHouse; what is sent is the `")
	b.WriteString(string(call.Input))
	b.WriteString("` CTE below.")
	if !call.Spec.Causal {
		b.WriteString(" Two-sided: every value sees the whole series, so this is not what an alert would have known at the time.")
	}
	return b.String()
}

// tsCompiledNode builds the executable form of a client node: the fused INPUT
// CTE as the SQL, the node's own resolved signals as the params, and the call
// itself as the transform. Every lane that can show a client node compiles it
// through here, so the input-not-the-body substitution is stated once.
func tsCompiledNode(split splitResult, node splitNode, bound map[string]bool, sig SignalEnvI) (c compiledNode) {
	return compiledNode{
		SQL:    fuseNode(split, node.Client.Input),
		Params: resolveSignalNamesWithDefaults(node.Reads, bound, sig),
		Client: node.Client,
	}
}

// compileNodeFor is tsCompiledNode for a client node and the ordinary fusion
// for everything else — the one call site a lane needs.
func compileNodeFor(split splitResult, node splitNode, bound map[string]bool, sig SignalEnvI) (c compiledNode) {
	if node.Client != nil {
		return tsCompiledNode(split, node, bound, sig)
	}
	return compiledNode{
		SQL:    fuseNode(split, node.ID),
		Params: resolveSignalNamesWithDefaults(node.Reads, bound, sig),
	}
}

// applyTsCall reads the input's declared columns and dispatches to the
// transform. Pure over (call, record): no context, no clock, no I/O — which is
// what lets the transforms be tested against the substrate packages directly.
func applyTsCall(call *tsCall, in arrow.RecordBatch, params map[string]string, alloc memory.Allocator) (rec arrow.RecordBatch, err error) {
	ts, vals, err := readTsInput(call, in)
	if err != nil {
		return
	}
	n := int32(len(ts))
	if call.Spec.MaxLen > 0 && n > call.Spec.MaxLen {
		err = eh.Errorf("%s: the input has %d rows, past this function's %d-row ceiling — "+
			"the algorithm is superlinear and the wait would stop being a wait. "+
			"Aggregate to a coarser grid, or narrow the range, before the call",
			call.Spec.Name, n, call.Spec.MaxLen)
		return
	}
	switch call.Spec.Name {
	case "tsSmooth":
		return tsApplySmooth(call, ts, vals, params, alloc)
	case "tsProfile":
		return tsApplyProfile(call, ts, vals, params, alloc)
	case "tsAnomalyScores":
		return tsApplyScores(call, ts, vals, params, alloc)
	case "tsAnomalySpans":
		return tsApplySpans(call, ts, vals, params, alloc)
	}
	err = eh.Errorf("%s: recognised but not implemented", call.Spec.Name)
	return
}

// readTsInput resolves the call's column arguments against the input schema
// and reads them as a time axis (epoch ms) plus values.
//
// A null in either column ends the read with a reason rather than being
// skipped: skipping would silently close a gap, which is the one thing this
// whole feature refuses to do. The fix — WITH FILL, or a filter — is the
// user's, and it belongs in the input CTE where it is recorded.
func readTsInput(call *tsCall, in arrow.RecordBatch) (ts []int64, vals []float64, err error) {
	schema := in.Schema()
	tName, vName := call.Args[0], call.Args[1]
	tIdx := schema.FieldIndices(tName)
	vIdx := schema.FieldIndices(vName)
	if len(tIdx) == 0 || len(vIdx) == 0 {
		missing := tName
		if len(tIdx) != 0 {
			missing = vName
		}
		err = eh.Errorf("%s: the input CTE %q has no column %q; it has %v",
			call.Spec.Name, string(call.Input), missing, fieldNames(schema))
		return
	}
	tArr, vArr := in.Column(tIdx[0]), in.Column(vIdx[0])
	if !isSeriesTemporalType(tArr.DataType()) {
		err = eh.Errorf("%s: column %q is %s, which is not a time. A ClickHouse DateTime arrives as a "+
			"bare UInt32 and cannot be told from a count — wrap it: toDateTime64(%s, 3)",
			call.Spec.Name, tName, tArr.DataType(), tName)
		return
	}
	n := int(in.NumRows())
	ts = make([]int64, 0, n)
	vals = make([]float64, 0, n)
	for row := range n {
		ms, ok := temporalCellMS(tArr, row, false)
		if !ok || tArr.IsNull(row) {
			err = eh.Errorf("%s: row %d has no time in %q. The transform reads a series, and skipping a "+
				"row would close a gap that is really there", call.Spec.Name, row, tName)
			return
		}
		v, got := numericCellValue(vArr, int64(row))
		if !got {
			err = eh.Errorf("%s: row %d has a NULL %q. Decide what the gap means in the input CTE — "+
				"drop it, or fill it explicitly — rather than letting the analysis guess",
				call.Spec.Name, row, vName)
			return
		}
		ts = append(ts, ms)
		vals = append(vals, v)
	}
	if len(ts) == 0 {
		err = eh.Errorf("%s: the input CTE %q returned no rows", call.Spec.Name, string(call.Input))
	}
	return
}

// tsIntArg reads a positional integer argument. Slots are resolved by the
// time the executor runs — the lane substitutes them into the input SQL — but
// the ARGUMENT text still holds the slot spelling, so the value comes from
// the params the compiled node carries.
func tsIntArg(call *tsCall, pos int, params map[string]string) (v int32, err error) {
	text := call.Args[pos]
	if isTsIntLiteral(text) {
		return tsParseInt32(call, pos, text)
	}
	name := tsSlotName(text)
	raw, bound := params["param_"+name]
	if !bound || raw == "" {
		err = eh.Errorf("%s: %s reads {%s}, which has no value. Bind it — a SET line, or a control that "+
			"writes the signal — before the analysis can run", call.Spec.Name, call.Spec.Args[pos].Name, name)
		return
	}
	if !isTsIntLiteral(raw) {
		err = eh.Errorf("%s: %s reads {%s}, whose value %q is not a whole number",
			call.Spec.Name, call.Spec.Args[pos].Name, name, raw)
		return
	}
	return tsParseInt32(call, pos, raw)
}

// tsParseInt32 converts already-validated digits, guarding the width.
func tsParseInt32(call *tsCall, pos int, digits string) (v int32, err error) {
	n, cErr := strconv.ParseInt(digits, 10, 32)
	if cErr != nil {
		err = eh.Errorf("%s: %s is out of range (%s)", call.Spec.Name, call.Spec.Args[pos].Name, digits)
		return
	}
	return int32(n), nil
}

// tsSlotName extracts `x` from `{x:UInt32}`.
func tsSlotName(text string) (name string) {
	if len(text) < 2 || text[0] != '{' {
		return ""
	}
	body := text[1 : len(text)-1]
	for i := 0; i < len(body); i++ {
		if body[i] == ':' {
			return strings.TrimSpace(body[:i])
		}
	}
	return strings.TrimSpace(body)
}
