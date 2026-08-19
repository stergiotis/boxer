package sysmreplay

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Coverage — where stored history actually is (ADR-0197 §SD10).
//
// This is the cheap half of the two queries replay needs, and it is cheap for a
// structural reason worth stating: both columns it touches — the key and the
// order column — are `boxer.facts` *envelope* columns, not leeway sections. So
// it is an ordinary GROUP BY, with no `LW_GET`, no run decoding, and none of
// the array arithmetic the read-surface page warns against. The other query,
// the load preview, reads a metric value out of a section and is a different
// proposition entirely.

// CoverageBucket is one time bin that holds stored rows. Bins with no rows are
// absent rather than zero-valued: the gaps are the point, and materialising
// them would make an empty week cost as much as a busy one.
type CoverageBucket struct {
	// StartMS is the bin's inclusive lower bound, epoch milliseconds UTC.
	StartMS int64
	// Rows counts rows of every kind in the bin, across the host's series. It
	// answers "was the tee running", not "how much of anything happened".
	Rows int64
}

// DefaultCoverageBuckets is how many bins Coverage aims for when the caller
// does not choose a bucket size. It is a screen-width number: the strip that
// draws this is a few hundred pixels wide, and a finer grid would return rows
// nothing can distinguish.
const DefaultCoverageBuckets = 240

// Coverage reports which time bins hold rows for this reader's host.
//
// bucket is the bin width; zero picks one that divides the window into roughly
// [DefaultCoverageBuckets] bins, floored at a second because the order column
// is stamped per bundle and a finer bin cannot separate two of them.
//
// The window must be bounded: an unbounded one would scan the whole retention
// to answer a question about a screen.
//
// It is a blocking database read. Call it off the render thread.
func (inst *Reader) Coverage(ctx context.Context, w Window, bucket time.Duration) (buckets []CoverageBucket, err error) {
	if w.From.IsZero() || w.To.IsZero() {
		err = eh.Errorf("sysmreplay: coverage needs a bounded window")
		return
	}
	if !w.To.After(w.From) {
		err = eh.Errorf("sysmreplay: coverage window ends before it starts")
		return
	}
	if inst.exec == nil {
		err = eh.Errorf("sysmreplay: coverage needs Options.Exec — the reader was built without one")
		return
	}
	bucket = coverageBucket(w, bucket)

	sql := inst.coverageSQL(w, bucket)
	for rec, rerr := range inst.exec.QueryArrow(ctx, sql) {
		if rerr != nil {
			buckets = nil
			err = eh.Errorf("sysmreplay: coverage query: %w", rerr)
			return
		}
		starts, ok := rec.Column(0).(*array.Int64)
		if !ok {
			rec.Release()
			buckets = nil
			err = eh.Errorf("sysmreplay: coverage bucket column is %s, not int64", rec.Column(0).DataType())
			return
		}
		counts, ok := rec.Column(1).(*array.Uint64)
		if !ok {
			rec.Release()
			buckets = nil
			err = eh.Errorf("sysmreplay: coverage count column is %s, not uint64", rec.Column(1).DataType())
			return
		}
		for i := range int(rec.NumRows()) {
			buckets = append(buckets, CoverageBucket{StartMS: starts.Value(i), Rows: int64(counts.Value(i))})
		}
		rec.Release()
	}
	return
}

// coverageBucket resolves the bin width: the caller's, or one that divides the
// window into about DefaultCoverageBuckets bins.
func coverageBucket(w Window, bucket time.Duration) (out time.Duration) {
	out = bucket
	if out <= 0 {
		out = w.To.Sub(w.From) / DefaultCoverageBuckets
	}
	// A bundle stamps one order value, so bins finer than a second cannot
	// separate two bundles and only multiply rows nothing can draw.
	out = max(out, time.Second)
	return
}

// coverageSQL builds the GROUP BY.
//
// The bucket expression goes through toUnixTimestamp rather than
// toUnixTimestamp64Milli: `toStartOfInterval` on a DateTime64 returns a plain
// DateTime — it drops the sub-second scale along with the sub-second part —
// and the 64-bit converters reject that type outright. Seconds are the right
// resolution for the question anyway, so the millisecond form is reconstructed
// by multiplication rather than asked for.
//
// The counts are cast so the Arrow column types are decided here rather than by
// whatever ClickHouse would infer, which is the same reason the timestamp is
// returned as an integer: a DateTime would arrive as a 32-bit Arrow column and
// silently lose the range.
func (inst *Reader) coverageSQL(w Window, bucket time.Duration) (sql string) {
	secs := int64(bucket / time.Second)
	col := sysmfacts.SysmetricsColOrder
	var b strings.Builder
	b.WriteString("SELECT toInt64(toUnixTimestamp(toStartOfInterval(")
	b.WriteString(col)
	b.WriteString(", INTERVAL ")
	b.WriteString(strconv.FormatInt(secs, 10))
	b.WriteString(" SECOND))) * 1000 AS b, toUInt64(count()) AS c FROM ")
	b.WriteString(sysmfacts.SysmetricsTableName)
	b.WriteString(" WHERE ")
	b.WriteString(sysmfacts.SysmetricsColKey)
	b.WriteString(" IN (")
	for i, domain := range allDomains {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(formatKey(EntityKey(inst.host, domain)))
	}
	b.WriteString(") AND ")
	b.WriteString(col)
	b.WriteString(" >= fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.From.UnixNano(), 10))
	b.WriteString(") AND ")
	b.WriteString(col)
	b.WriteString(" < fromUnixTimestamp64Nano(")
	b.WriteString(strconv.FormatInt(w.To.UnixNano(), 10))
	b.WriteString(") GROUP BY b ORDER BY b")
	sql = b.String()
	return
}

// allDomains is every series one host writes — the per-tick kinds plus the
// three carried ones. Coverage asks about the host, not about a kind, so it
// spans all of them: a window where only the topology was written is not one
// with metrics in it, but it is one the tee was alive for.
var allDomains = append(append([]string{}, perTickDomains...),
	DomainCPUInfo, DomainSockets, DomainTopology)

// CoverageRun is a contiguous stretch of covered bins, [StartMS, EndMS).
type CoverageRun struct {
	StartMS int64
	EndMS   int64
	Rows    int64
}

// CoverageRuns queries coverage and merges it into runs in one step.
//
// It exists so a caller never has to know the bin width the query settled on:
// merging needs the same step the GROUP BY used, and a caller mirroring that
// choice would drift from it silently — the runs would simply stop merging.
func (inst *Reader) CoverageRuns(ctx context.Context, w Window, bucket time.Duration) (runs []CoverageRun, err error) {
	buckets, err := inst.Coverage(ctx, w, bucket)
	if err != nil {
		return
	}
	runs = MergeCoverage(buckets, coverageBucket(w, bucket))
	return
}

// MergeCoverage merges adjacent covered bins into runs, so an unbroken hour is
// one run rather than 240 bins.
//
// Merging matters for more than tidiness. The band producer that draws these is
// called per frame with the visible range, and a band per bin would put a paint
// op per bin on the wire every frame; run-merged, it puts a handful.
func MergeCoverage(buckets []CoverageBucket, bucket time.Duration) (runs []CoverageRun) {
	if len(buckets) == 0 {
		return
	}
	stepMS := bucket.Milliseconds()
	if stepMS <= 0 {
		stepMS = time.Second.Milliseconds()
	}
	cur := CoverageRun{StartMS: buckets[0].StartMS, EndMS: buckets[0].StartMS + stepMS, Rows: buckets[0].Rows}
	for _, b := range buckets[1:] {
		if b.StartMS == cur.EndMS {
			cur.EndMS = b.StartMS + stepMS
			cur.Rows += b.Rows
			continue
		}
		runs = append(runs, cur)
		cur = CoverageRun{StartMS: b.StartMS, EndMS: b.StartMS + stepMS, Rows: b.Rows}
	}
	runs = append(runs, cur)
	return
}

// formatKey renders an entity key as the SQL literal the filter uses.
func formatKey(key uint64) string {
	return strconv.FormatUint(key, 10)
}
