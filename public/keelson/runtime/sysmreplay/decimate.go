package sysmreplay

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Decimation — replaying a range longer than the history window can hold
// (ADR-0197 §SD6, closed here).
//
// # It samples whole bundles; it does not aggregate them
//
// ADR-0197 §SD11 predicted that the load preview and this would be the same
// machinery pointed at different consumers. They share the bucketing idea and
// nothing else, because a bundle is not a metric. A CPU percent has a mean; a
// process table does not, a topology tree does not, and an interface list whose
// members come and go does not. Averaging those would invent a machine that
// never existed, which is a worse answer than a sparser true one.
//
// So decimation picks one *recorded* bundle per bin and replays it unchanged.
// Every field stays exactly as it was written, and what the user loses is
// resolution rather than fidelity.
//
// # Which makes it the cheap query, not the expensive one
//
// Choosing a representative timestamp per bin touches only the key and the
// order column — envelope columns — so it is the same class of query as
// coverage, not the section read the preview needs. The rows themselves then
// come back through the store's own Scan verbs, filtered to those timestamps.
// §SD11 expected to pay for `LW_GET_LIST` here and does not.

// DecimationPlan is the set of recorded instants a decimated replay will read:
// one per occupied bin, in ascending order.
type DecimationPlan struct {
	// Bucket is the bin width the plan was built at.
	Bucket time.Duration
	// TimesMS are the chosen bundle stamps, epoch milliseconds UTC. Empty when
	// the range holds nothing.
	TimesMS []int64
}

// NeedsDecimation reports whether a window holds more bundles than the history
// window can show, and therefore wants a plan.
//
// slots is the fold's capacity. The comparison is against the count of stored
// bundles rather than against wall-clock span, because a range over an outage
// may be hours long and hold nothing at all.
func NeedsDecimation(bundles int64, slots int) (yes bool) {
	yes = slots > 0 && bundles > int64(slots)
	return
}

// CountBundles reports how many bundles a window holds for this host.
//
// It counts the CPU series rather than every kind: one bundle writes one cpu
// row, so that count is the bundle count, where a count over all thirteen kinds
// would be a multiple of it that varies with which collectors were wired.
func (inst *Reader) CountBundles(ctx context.Context, w Window) (n int64, err error) {
	if inst.exec == nil {
		err = eh.Errorf("sysmreplay: counting needs Options.Exec")
		return
	}
	sql := "SELECT toInt64(count()) AS c FROM " + sysmfacts.SysmetricsTableName +
		" WHERE " + inst.windowPredicate(w, DomainCPU)
	for rec, rerr := range inst.exec.QueryArrow(ctx, sql) {
		if rerr != nil {
			err = eh.Errorf("sysmreplay: counting bundles: %w", rerr)
			return
		}
		counts, ok := rec.Column(0).(*array.Int64)
		if !ok {
			rec.Release()
			err = eb.Build().Stringer("dataType", rec.Column(0).DataType()).Errorf("sysmreplay: count column is not int64")
			return
		}
		if rec.NumRows() > 0 {
			n = counts.Value(0)
		}
		rec.Release()
	}
	return
}

// PlanDecimation chooses one stored bundle per bin over the window.
//
// slots is how many bundles the consumer can hold; the plan aims to fill it. A
// window already inside that budget returns an empty plan, which the caller
// reads as "replay it whole".
func (inst *Reader) PlanDecimation(ctx context.Context, w Window, slots int) (plan DecimationPlan, err error) {
	if w.From.IsZero() || w.To.IsZero() {
		err = eh.Errorf("sysmreplay: decimation needs a bounded window")
		return
	}
	// Arguments before dependencies: a caller who passed a bad slot budget
	// should hear about the budget whether or not an executor happens to be
	// attached, rather than being told to fix the other thing first.
	if slots <= 0 {
		err = eh.Errorf("sysmreplay: decimation needs a positive slot budget")
		return
	}
	if inst.exec == nil {
		err = eh.Errorf("sysmreplay: decimation needs Options.Exec")
		return
	}
	bucket := max(w.To.Sub(w.From)/time.Duration(slots), time.Second)
	plan.Bucket = bucket

	secs := int64(bucket / time.Second)
	col := sysmfacts.SysmetricsColOrder
	// min() per bin, so the chosen instant is a real recorded one and the
	// choice is stable across runs — a plan the user re-enters must show them
	// the same frames.
	sql := "SELECT toInt64(toUnixTimestamp64Milli(min(" + col + "))) AS t FROM " +
		sysmfacts.SysmetricsTableName +
		" WHERE " + inst.windowPredicate(w, DomainCPU) +
		" GROUP BY toStartOfInterval(" + col + ", INTERVAL " + strconv.FormatInt(secs, 10) + " SECOND)" +
		" ORDER BY t"
	for rec, rerr := range inst.exec.QueryArrow(ctx, sql) {
		if rerr != nil {
			plan.TimesMS = nil
			err = eh.Errorf("sysmreplay: planning decimation: %w", rerr)
			return
		}
		ts, ok := rec.Column(0).(*array.Int64)
		if !ok {
			rec.Release()
			plan.TimesMS = nil
			err = eb.Build().Stringer("dataType", rec.Column(0).DataType()).Errorf("sysmreplay: decimation stamp column is not int64")
			return
		}
		for i := range int(rec.NumRows()) {
			plan.TimesMS = append(plan.TimesMS, ts.Value(i))
		}
		rec.Release()
	}
	return
}

// windowPredicate is the key-and-range restriction both envelope queries share.
func (inst *Reader) windowPredicate(w Window, domain string) (pred string) {
	col := sysmfacts.SysmetricsColOrder
	var b strings.Builder
	b.WriteString(sysmfacts.SysmetricsColKey)
	b.WriteString(" = ")
	b.WriteString(formatKey(EntityKey(inst.host, domain)))
	b.WriteString(" AND ")
	b.WriteString(col)
	b.WriteString(" >= fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.From.UnixNano(), 10))
	b.WriteString(") AND ")
	b.WriteString(col)
	b.WriteString(" < fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.To.UnixNano(), 10))
	b.WriteString(")")
	pred = b.String()
	return
}

// decimatedPredicate restricts a scan to the plan's chosen instants for one
// domain's key.
//
// The stamps go in as an IN list rather than as a join: a plan is bounded by
// the consumer's slot budget — a few hundred values — and an IN list keeps the
// read on the store's own generated verbs instead of inventing a second read
// path beside them.
func (inst *Reader) decimatedPredicate(domain string, timesMS []int64) (pred string) {
	col := sysmfacts.SysmetricsColOrder
	var b strings.Builder
	b.WriteString(sysmfacts.SysmetricsColKey)
	b.WriteString(" = ")
	b.WriteString(formatKey(EntityKey(inst.host, domain)))
	b.WriteString(" AND ")
	b.WriteString(col)
	b.WriteString(" IN (")
	for i, ms := range timesMS {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("fromUnixTimestamp64Milli(")
		b.WriteString(strconv.FormatInt(ms, 10))
		b.WriteString(")")
	}
	b.WriteString(")")
	pred = b.String()
	return
}
