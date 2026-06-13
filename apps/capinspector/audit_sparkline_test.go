package capinspector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
)

// TestAuditHistogram_Record_LandsInCurrentBucket records once and
// snapshots immediately; the lone hit must appear in the NEWEST
// (last) bucket because the head anchors on the first record.
func TestAuditHistogram_Record_LandsInCurrentBucket(t *testing.T) {
	var h auditHistogram
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h.record(t0)
	snap := h.snapshot(t0)
	assert.EqualValues(t, 1, snap[sparkBuckets-1], "newest bucket")
	for i := range sparkBuckets - 1 {
		assert.EqualValues(t, 0, snap[i], "older bucket %d", i)
	}
}

// TestAuditHistogram_Snapshot_OldestToNewest spaces three records
// across three different buckets and asserts the spatial order.
func TestAuditHistogram_Snapshot_OldestToNewest(t *testing.T) {
	var h auditHistogram
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h.record(t0)
	h.record(t0.Add(2 * sparkBucketWidth))
	h.record(t0.Add(5 * sparkBucketWidth))

	snap := h.snapshot(t0.Add(5 * sparkBucketWidth))
	// Newest (last) entry is the t0+5w record.
	assert.EqualValues(t, 1, snap[sparkBuckets-1])
	// t0+2w lands three buckets behind the newest.
	assert.EqualValues(t, 1, snap[sparkBuckets-1-3])
	// t0 lands five buckets behind the newest.
	assert.EqualValues(t, 1, snap[sparkBuckets-1-5])
	// Sum across the snapshot equals the three recorded hits.
	var total uint64
	for _, v := range snap {
		total += v
	}
	assert.EqualValues(t, 3, total)
}

// TestAuditHistogram_Advance_AgesOutFullWindow records one hit, then
// snapshots after the entire window has expired — every bucket must
// be zeroed regardless of where the record originally landed.
func TestAuditHistogram_Advance_AgesOutFullWindow(t *testing.T) {
	var h auditHistogram
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h.record(t0)
	// One full window plus a margin.
	snap := h.snapshot(t0.Add(sparkWindow + sparkBucketWidth))
	for i, v := range snap {
		assert.EqualValues(t, 0, v, "bucket %d should age out", i)
	}
}

// TestAuditHistogram_Advance_RollsPartialWindow checks the partial-
// roll path: a few bucket-widths between record and snapshot must
// shift the older entries to earlier slots and zero the buckets
// crossed by the head pointer.
func TestAuditHistogram_Advance_RollsPartialWindow(t *testing.T) {
	var h auditHistogram
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h.record(t0)
	// Six buckets later — the original record now sits six positions
	// behind the newest bucket.
	snap := h.snapshot(t0.Add(6 * sparkBucketWidth))
	assert.EqualValues(t, 1, snap[sparkBuckets-1-6])
	for i, v := range snap {
		if i == sparkBuckets-1-6 {
			continue
		}
		assert.EqualValues(t, 0, v, "bucket %d", i)
	}
}

// TestAuditHistogram_Record_AccumulatesWithinBucket verifies that
// multiple records inside one bucket-width sum, rather than spilling
// into adjacent buckets.
func TestAuditHistogram_Record_AccumulatesWithinBucket(t *testing.T) {
	var h auditHistogram
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Three records spaced under one bucket-width apart.
	h.record(t0)
	h.record(t0.Add(sparkBucketWidth / 4))
	h.record(t0.Add(sparkBucketWidth / 2))
	snap := h.snapshot(t0.Add(sparkBucketWidth / 2))
	assert.EqualValues(t, 3, snap[sparkBuckets-1])
}

// TestCounters_Snapshot_PerCap exercises the recordAt/snapshotAt
// time-injected path on the Counters wrapper. A single fs subject
// must show up in both the CapFs sparkline (classifier match) and
// the CapBus sparkline (universal substrate).
func TestCounters_Snapshot_PerCap(t *testing.T) {
	c := &Counters{}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.recordAt(audit.AuditRecord{Subject: "fs.dialog.read"}, t0)

	fs := c.snapshotAt(CapFs, t0)
	assert.EqualValues(t, 1, fs[sparkBuckets-1], "fs newest bucket")
	bus := c.snapshotAt(CapBus, t0)
	assert.EqualValues(t, 1, bus[sparkBuckets-1], "bus newest bucket")
	// Persist saw no traffic — empty sparkline.
	persist := c.snapshotAt(CapPersist, t0)
	for i, v := range persist {
		assert.EqualValues(t, 0, v, "persist bucket %d", i)
	}
}

// TestCounters_Snapshot_UnknownCapIsZero exercises the default arm
// of the switch — an unknown capId must return a zero array, not
// panic.
func TestCounters_Snapshot_UnknownCapIsZero(t *testing.T) {
	c := &Counters{}
	snap := c.snapshotAt(CapId("unknown"), time.Now())
	for i, v := range snap {
		assert.EqualValues(t, 0, v, "bucket %d", i)
	}
}

// TestCounters_Reset_ZerosHistograms verifies Reset clears both the
// monotonic counters and the sliding-window histograms.
func TestCounters_Reset_ZerosHistograms(t *testing.T) {
	c := &Counters{}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.recordAt(audit.AuditRecord{Subject: "fs.dialog.read"}, t0)
	c.Reset()
	fs := c.snapshotAt(CapFs, t0)
	for i, v := range fs {
		assert.EqualValues(t, 0, v, "fs bucket %d", i)
	}
	bus := c.snapshotAt(CapBus, t0)
	for i, v := range bus {
		assert.EqualValues(t, 0, v, "bus bucket %d", i)
	}
}
