package capinspector

import (
	"sync"
	"time"
)

// sparkBuckets is the number of bins each cap's recent-activity strip
// renders. With sparkWindow = 60s, each bucket covers 2s.
const (
	sparkBuckets     = 30
	sparkWindow      = 60 * time.Second
	sparkBucketWidth = sparkWindow / sparkBuckets
)

// SparkSnapshot is the oldest-to-newest sequence of per-bucket counts
// the renderer consumes. Fixed length so the caller can iterate
// without bounds checks; zero entries render as gaps in the strip.
type SparkSnapshot = [sparkBuckets]uint64

// auditHistogram is a fixed-width, sliding-window counter. Buckets
// accumulate counts for one period each; the head advances with wall-
// clock time so old data ages out without a sweeper goroutine. All
// writes (record) and reads (snapshot) take the same mutex; contention
// is negligible at audit rates (<1k/s) versus UI read rate (~360/s
// across six caps at 60 Hz).
type auditHistogram struct {
	mu      sync.Mutex
	buckets SparkSnapshot
	// headIdx is the bucket new records currently accumulate into;
	// wraps modulo sparkBuckets. The oldest bucket sits at headIdx+1.
	headIdx int
	// headStart is the wall-clock instant the current head bucket
	// began. advance() consults this to roll the head forward and
	// zero the buckets it crosses.
	headStart time.Time
}

func (inst *auditHistogram) record(now time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.advance(now)
	inst.buckets[inst.headIdx]++
}

// advance moves headIdx forward by however many bucket-widths have
// elapsed since headStart, zeroing each bucket it crosses so stale
// counts age out. Must be called with inst.mu held. A first call
// (headStart zero) just stamps headStart without rolling.
func (inst *auditHistogram) advance(now time.Time) {
	if inst.headStart.IsZero() {
		inst.headStart = now
		return
	}
	elapsed := now.Sub(inst.headStart)
	if elapsed < sparkBucketWidth {
		return
	}
	nBuckets := int(elapsed / sparkBucketWidth)
	if nBuckets >= sparkBuckets {
		// The entire window has expired; one wipe is cheaper than
		// rolling N>=sparkBuckets times.
		for i := range inst.buckets {
			inst.buckets[i] = 0
		}
		inst.headIdx = 0
		inst.headStart = now
		return
	}
	for range nBuckets {
		inst.headIdx = (inst.headIdx + 1) % sparkBuckets
		inst.buckets[inst.headIdx] = 0
	}
	inst.headStart = inst.headStart.Add(time.Duration(nBuckets) * sparkBucketWidth)
}

// snapshot returns the buckets oldest-to-newest. The renderer paints
// left-to-right, so the last element is the most recent bucket.
func (inst *auditHistogram) snapshot(now time.Time) (out SparkSnapshot) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.advance(now)
	// headIdx holds the newest bucket; (headIdx + 1) mod N is the
	// oldest. Walk forward sparkBuckets-1 positions to land on
	// headIdx as the final entry.
	for i := range sparkBuckets {
		idx := (inst.headIdx + 1 + i) % sparkBuckets
		out[i] = inst.buckets[idx]
	}
	return
}

// reset zeros every bucket and clears headStart so the next record()
// re-anchors. Test-only helper.
func (inst *auditHistogram) reset() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for i := range inst.buckets {
		inst.buckets[i] = 0
	}
	inst.headIdx = 0
	inst.headStart = time.Time{}
}
