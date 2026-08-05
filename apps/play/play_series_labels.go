package play

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_series_labels.go is ADR-0163 M3: adjudication, and the readout it
// exists to make possible.
//
// The loop it closes is the one ADR-0150's load study named and could not
// finish. Events are not labels — an incident ticket says something happened
// somewhere near a time, which is not the same as a labelled extent — so
// "this detector beats the one-liners" stayed an argument rather than a
// measurement. A person marking a flagged span confirmed or false-alarm is
// the missing input, and once there are labels the VUS readout turns the
// claim into a number that can come out the wrong way.
//
// That last part is the point. The readout scores the DETECTOR and the
// BASELINE side by side on the same adjudicated spans, so the panel is
// equally capable of reporting that the moving-average residual won.
//
// Labels are append-only and read latest-wins per (input, span), the
// ADR-0148 pattern: a changed verdict is a new row, so the record keeps that
// someone changed their mind and when. Modelling adjudications as FACTS is
// deferred to its own dialogue — the identity question (a pinned QueryRun)
// is not settled, and gating M3 on it would have kept the labels bootstrap
// from ever starting.

const (
	// tsLabelsTable is the qualified labels table.
	tsLabelsTable = "boxer.tslabels"
	// tsLabelsTimeout bounds one write round (DDL plus the row).
	tsLabelsTimeout = 30 * time.Second
	// tsLabelsReadTimeout bounds the read lane's round trip. Chrome: a slow
	// answer must delay a readout, never a result.
	tsLabelsReadTimeout = 10 * time.Second
)

// tsLabelsDDL creates the table. Plain readable columns, MergeTree, ordered
// so the per-input read is a prefix scan — the browser for this is meant to
// be ordinary SQL over an ordinary table.
const tsLabelsDDL = `CREATE TABLE IF NOT EXISTS ` + tsLabelsTable + ` (
  created_at DateTime64(3,'UTC') DEFAULT now64(3),
  input_hash String,
  span_from  DateTime64(3,'UTC'),
  span_to    DateTime64(3,'UTC'),
  verdict    String,
  detector   String,
  window     UInt32,
  note       String
) ENGINE MergeTree() ORDER BY (input_hash, span_from, span_to, created_at)`

// tsVerdictE is one adjudication.
type tsVerdictE uint8

const (
	// tsVerdictNone is "not adjudicated" — the absence of a row, never a
	// stored value.
	tsVerdictNone tsVerdictE = iota
	// tsVerdictConfirmed: the flagged extent really was anomalous.
	tsVerdictConfirmed
	// tsVerdictFalseAlarm: it was not.
	tsVerdictFalseAlarm
)

func (inst tsVerdictE) String() (name string) {
	switch inst {
	case tsVerdictConfirmed:
		return "confirmed"
	case tsVerdictFalseAlarm:
		return "false_alarm"
	}
	return ""
}

func tsVerdictFromString(s string) tsVerdictE {
	switch s {
	case "confirmed":
		return tsVerdictConfirmed
	case "false_alarm":
		return tsVerdictFalseAlarm
	}
	return tsVerdictNone
}

// tsLabelRow is one written adjudication. The JSON tags are the insert's
// column names — the write goes in as JSONEachRow, like the pin metadata.
type tsLabelRow struct {
	InputHash string `json:"input_hash"`
	SpanFrom  string `json:"span_from"`
	SpanTo    string `json:"span_to"`
	Verdict   string `json:"verdict"`
	Detector  string `json:"detector"`
	Window    uint32 `json:"window"`
	Note      string `json:"note"`
}

// tsLabelKey identifies an adjudicated extent within one input. Milliseconds
// rather than the float seconds the plot uses: a key must not depend on
// floating-point equality.
type tsLabelKey struct {
	fromMS, toMS int64
}

// tsInputHash identifies WHAT was adjudicated: the compiled input — its SQL
// and its resolved params — rather than the query text a user is editing.
// Two buffers that fuse to the same input over the same data are the same
// series, and a label taken on one is honest about the other; a buffer whose
// input changed is a different series, and its old labels correctly stop
// applying.
func tsInputHash(c compiledNode) (hash string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(c.key()))
	return fmt.Sprintf("%016x", h.Sum64())
}

// tsLabelsWriter performs the adjudication writes. Single-flight and
// off-thread, mirroring the pin driver: a click while a write is in flight is
// dropped rather than queued, because the affordance is one button per span
// and a person cannot meaningfully mean two things at once.
type tsLabelsWriter struct {
	client *Client

	mu      sync.Mutex
	writing bool
	err     error
	// wrote counts completed writes; the read side watches it so a fresh
	// verdict shows up without the user having to re-run anything.
	wrote uint64
}

func newTsLabelsWriter(client *Client) (inst *tsLabelsWriter) {
	return &tsLabelsWriter{client: client}
}

func (inst *tsLabelsWriter) status() (writing bool, wrote uint64, err error) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.writing, inst.wrote, inst.err
}

// write appends one verdict.
func (inst *tsLabelsWriter) write(row tsLabelRow) {
	if inst == nil || inst.client == nil {
		return
	}
	inst.mu.Lock()
	if inst.writing {
		inst.mu.Unlock()
		return
	}
	inst.writing = true
	inst.err = nil
	inst.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), tsLabelsTimeout)
		defer cancel()
		err := inst.doWrite(ctx, row)
		inst.mu.Lock()
		inst.writing = false
		inst.err = err
		if err == nil {
			inst.wrote++
		}
		inst.mu.Unlock()
	}()
}

func (inst *tsLabelsWriter) doWrite(ctx context.Context, row tsLabelRow) (err error) {
	if _, err = inst.client.rawTsvQuery(ctx, tsLabelsDDL); err != nil {
		return eh.Errorf("play: tslabels: ddl: %w", err)
	}
	payload, mErr := json.Marshal(row)
	if mErr != nil {
		return eh.Errorf("play: tslabels: marshal: %w", mErr)
	}
	err = inst.client.rawInsertBody(ctx,
		"INSERT INTO "+tsLabelsTable+
			" (input_hash, span_from, span_to, verdict, detector, window, note) FORMAT JSONEachRow",
		bytes.NewReader(payload))
	if err != nil {
		return eh.Errorf("play: tslabels: insert: %w", err)
	}
	return
}

// tsLabelsQuery reads the LATEST verdict per span for one input. argMax over
// created_at is what makes the append-only table read as current state (the
// ADR-0148 pattern); the table itself keeps every revision.
const tsLabelsQuery = "SELECT toUnixTimestamp64Milli(span_from) AS from_ms, " +
	"toUnixTimestamp64Milli(span_to) AS to_ms, " +
	"argMax(verdict, created_at) AS verdict " +
	"FROM " + tsLabelsTable + " WHERE input_hash = {ts_input_hash:String} " +
	"GROUP BY span_from, span_to"

// readSeriesLabels demands the current input's verdicts on their own lane. It
// returns nothing at all until the table exists — an unadjudicated session
// must not show an error, since not having labelled anything yet is the
// ordinary state.
func (inst *PlayApp) readSeriesLabels(hash string) (out map[tsLabelKey]tsVerdictE) {
	if inst.client == nil || hash == "" {
		return
	}
	if inst.seriesLabelsLane == nil {
		inst.seriesLabelsLane = newNodeLane(
			clientExecutor{client: inst.client, opts: newExecOptions("series-labels")},
			memory.NewGoAllocator(), tsLabelsReadTimeout)
	}
	v := inst.seriesLabelsLane.demand(compiledNode{
		SQL:    tsLabelsQuery,
		Params: map[string]string{"param_ts_input_hash": hash},
	})
	if v.rec == nil {
		return
	}
	return decodeSeriesLabels(v.rec)
}

// decodeSeriesLabels maps the read-back rows to verdicts.
func decodeSeriesLabels(rec arrow.RecordBatch) (out map[tsLabelKey]tsVerdictE) {
	schema := rec.Schema()
	fromIdx := schema.FieldIndices("from_ms")
	toIdx := schema.FieldIndices("to_ms")
	vIdx := schema.FieldIndices("verdict")
	if len(fromIdx) == 0 || len(toIdx) == 0 || len(vIdx) == 0 {
		return
	}
	out = make(map[tsLabelKey]tsVerdictE, rec.NumRows())
	fromArr, toArr, vArr := rec.Column(fromIdx[0]), rec.Column(toIdx[0]), rec.Column(vIdx[0])
	for row := range int(rec.NumRows()) {
		from, okF := numericCellValue(fromArr, int64(row))
		to, okT := numericCellValue(toArr, int64(row))
		if !okF || !okT {
			continue
		}
		verdict := tsVerdictFromString(readStringCell(vArr, row))
		if verdict == tsVerdictNone {
			continue
		}
		out[tsLabelKey{fromMS: int64(from), toMS: int64(to)}] = verdict
	}
	return
}

// tsScoreReadout is the measured comparison (§SD6): the detector and the
// baseline on the SAME adjudicated spans.
type tsScoreReadout struct {
	detector adscore.Measures
	baseline adscore.Measures
	// labelled counts the positions the adjudicated spans cover, and spans
	// how many extents were confirmed. Both belong on screen: a VUS over one
	// confirmed span is a number, but it is not evidence.
	labelled int
	spans    int
	// haveBaseline is false when the panel could not compute one, in which
	// case the readout shows the detector alone and says why elsewhere.
	haveBaseline bool
	err          string
}

// buildSeriesReadout scores both curves against the adjudicated labels.
//
// Only CONFIRMED spans become positives. A false alarm is not a negative
// label over its extent — it is the absence of an event there, which is what
// the unlabelled majority already says — so treating it as anything else
// would put a thumb on the scale.
func buildSeriesReadout(scores []float64, baseline []float64, t []float64,
	labels map[tsLabelKey]tsVerdictE) (out tsScoreReadout, ok bool) {
	if len(scores) == 0 || len(t) != len(scores) || len(labels) == 0 {
		return
	}
	truth := make([]bool, len(t))
	for key, verdict := range labels {
		if verdict != tsVerdictConfirmed {
			continue
		}
		out.spans++
		from, to := float64(key.fromMS)/1000, float64(key.toMS)/1000
		for i, ts := range t {
			if ts >= from && ts <= to {
				truth[i] = true
				out.labelled++
			}
		}
	}
	if out.labelled == 0 {
		return
	}
	ranges := adscore.RangesFromLabels(truth)
	maxBuffer := adscore.DefaultMaxBuffer(ranges)
	m, err := adscore.EvaluateE(scores, truth, maxBuffer)
	if err != nil {
		out.err = err.Error()
		return out, true
	}
	out.detector = m
	if len(baseline) == len(scores) {
		if bm, bErr := adscore.EvaluateE(baseline, truth, maxBuffer); bErr == nil {
			out.baseline = bm
			out.haveBaseline = true
		}
	}
	ok = true
	return
}

// seriesReadoutLine renders the comparison. VUS-PR leads because it is the
// measure the literature settled on and the one least distorted by the buffer
// (adscore's package doc); VUS-ROC follows WITH its usable band, because a
// reader who takes 0.5 for chance and 1.0 for perfect will misread it — under
// VUS a perfectly-located detector lands near 0.92 and a random one near 0.55.
func seriesReadoutLine(r tsScoreReadout) (line string) {
	var b strings.Builder
	if r.err != "" {
		return "The adjudicated spans could not be scored: " + r.err
	}
	fmt.Fprintf(&b, "On %d confirmed span(s) covering %d position(s): VUS-PR %.3f",
		r.spans, r.labelled, r.detector.VUSPR)
	if r.haveBaseline {
		fmt.Fprintf(&b, " vs the baseline's %.3f", r.baseline.VUSPR)
		switch {
		case r.detector.VUSPR > r.baseline.VUSPR:
			b.WriteString(" — the detector is ahead here")
		case r.detector.VUSPR < r.baseline.VUSPR:
			b.WriteString(" — THE ONE-LINER IS AHEAD HERE, on this input")
		default:
			b.WriteString(" — level")
		}
	}
	fmt.Fprintf(&b, ". VUS-ROC %.3f", r.detector.VUSROC)
	if r.haveBaseline {
		fmt.Fprintf(&b, " vs %.3f", r.baseline.VUSROC)
	}
	b.WriteString(" (read against a usable band of roughly 0.55–0.92, not 0–1: " +
		"the buffer gives a random scorer positive mass, and costs a perfect one)")
	if r.spans < 3 {
		b.WriteString(". Too few spans to conclude anything — this is a reading, not evidence")
	}
	return b.String()
}
