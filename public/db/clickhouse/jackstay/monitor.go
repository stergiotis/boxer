package jackstay

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DiskInfo is one ClickHouse disk as system.disks reports it.
type DiskInfo struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	FreeSpace     uint64 `json:"free_space"`
	TotalSpace    uint64 `json:"total_space"`
	KeepFreeSpace uint64 `json:"keep_free_space"`
}

// TableFootprint is where a table's data lives on a server and how much of
// it there is.
type TableFootprint struct {
	Ref datacatalog.TableRef
	// Policy is the storage policy; empty for engines outside the MergeTree
	// family, which store on the default disk.
	Policy string
	Disks  []string
	// BytesOnDisk sums the active parts. Merges move it after a copy lands,
	// so it is an observation, not a copy's cost (ADR-0259 §SD6).
	BytesOnDisk uint64
	Parts       uint64
}

// DiskReport is a server's disks and the footprint of some of its tables.
type DiskReport struct {
	Disks  []DiskInfo
	Tables []TableFootprint
}

func (inst *DiskReport) Disk(name string) (d DiskInfo, has bool) {
	for _, d := range inst.Disks {
		if d.Name == name {
			return d, true
		}
	}
	return
}

func (inst *DiskReport) Table(ref datacatalog.TableRef) (t TableFootprint, has bool) {
	for _, t := range inst.Tables {
		if t.Ref == ref {
			return t, true
		}
	}
	return
}

func refTuples(refs []datacatalog.TableRef) (sql string) {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, "("+QuoteString(r.Database)+", "+QuoteString(r.Name)+")")
	}
	return strings.Join(parts, ", ")
}

// ReadDisks reads a server's disks, and the storage policy and active-part
// footprint of the given tables, from system tables (ADR-0259 §SD6).
func ReadDisks(ctx context.Context, q QueryI, refs []datacatalog.TableRef) (rep DiskReport, err error) {
	rep.Disks, err = queryRows[DiskInfo](ctx, q, "SELECT name, path, free_space, total_space, keep_free_space FROM system.disks ORDER BY name"+jsonSettings)
	if err != nil {
		err = eh.Errorf("unable to read system.disks: %w", err)
		return
	}
	type policyRow struct {
		Policy string   `json:"policy_name"`
		Disks  []string `json:"disks"`
	}
	var policies []policyRow
	policies, err = queryRows[policyRow](ctx, q, "SELECT policy_name, groupUniqArrayArray(disks) AS disks FROM system.storage_policies GROUP BY policy_name"+jsonSettings)
	if err != nil {
		err = eh.Errorf("unable to read system.storage_policies: %w", err)
		return
	}
	if len(refs) == 0 {
		return
	}
	type tableRow struct {
		Database string `json:"database"`
		Name     string `json:"name"`
		Policy   string `json:"storage_policy"`
	}
	var tables []tableRow
	tables, err = queryRows[tableRow](ctx, q, "SELECT database, name, storage_policy FROM system.tables WHERE (database, name) IN ("+refTuples(refs)+")"+jsonSettings)
	if err != nil {
		err = eh.Errorf("unable to read table storage policies: %w", err)
		return
	}
	type partsRow struct {
		Database string `json:"database"`
		Table    string `json:"table"`
		Bytes    uint64 `json:"bytes"`
		Parts    uint64 `json:"parts"`
	}
	var parts []partsRow
	parts, err = queryRows[partsRow](ctx, q, "SELECT database, table, sum(bytes_on_disk) AS bytes, count() AS parts FROM system.parts WHERE active AND (database, table) IN ("+refTuples(refs)+") GROUP BY database, table"+jsonSettings)
	if err != nil {
		err = eh.Errorf("unable to read system.parts: %w", err)
		return
	}
	for _, t := range tables {
		fp := TableFootprint{Ref: datacatalog.TableRef{Database: t.Database, Name: t.Name}, Policy: t.Policy}
		for _, p := range policies {
			if p.Policy == t.Policy {
				fp.Disks = slices.Sorted(slices.Values(p.Disks))
			}
		}
		if len(fp.Disks) == 0 {
			fp.Disks = []string{"default"}
		}
		for _, p := range parts {
			if p.Database == t.Database && p.Table == t.Name {
				fp.BytesOnDisk, fp.Parts = p.Bytes, p.Parts
			}
		}
		rep.Tables = append(rep.Tables, fp)
	}
	return
}

// ExpectedCopyFraction is the share of a source table a sync is expected to
// copy: all of it, the sample fraction, or, for repair, the source rows of
// the leaves the diff listed.
func ExpectedCopyFraction(pt *PlanTable) (f float64) {
	if pt.Sync == nil {
		return 1
	}
	switch pt.Sync.Mode {
	case SyncModeSample:
		if pt.Sync.SampleDen == 0 {
			return 1
		}
		return float64(pt.Sync.SampleNum) / float64(pt.Sync.SampleDen)
	case SyncModeRepair:
		if pt.Diff == nil || pt.Rows == 0 {
			return 1
		}
		return float64(RepairCopyRows(pt.Diff)) / float64(pt.Rows)
	}
	return 1
}

// RepairCopyRows is the source row count repair would copy, as of the diff.
func RepairCopyRows(d *TableDiff) (rows uint64) {
	for _, cd := range d.Differing {
		switch {
		case cd.AbsentOnTarget:
			rows += cd.SrcRows
		case cd.AbsentOnSource:
		default:
			for _, ld := range cd.Leaves {
				rows += ld.SrcRows
			}
		}
	}
	return
}

// RepairClearRows is the target row count repair would clear, as of the diff.
func RepairClearRows(d *TableDiff) (rows uint64) {
	for _, cd := range d.Differing {
		switch {
		case cd.AbsentOnTarget:
		case cd.AbsentOnSource:
			rows += cd.DstRows
		default:
			for _, ld := range cd.Leaves {
				rows += ld.DstRows
			}
		}
	}
	return
}

// PreflightDisk is the pre-flight verdict for one group of target disks: the
// disks of one storage policy, whose free space the tables on it share.
type PreflightDisk struct {
	Disks []string
	// Free is the free space of those disks, less what ClickHouse keeps free.
	Free uint64
	// Need is the estimated bytes the chosen tables add: the source's
	// on-disk bytes scaled by the share each sync copies.
	Need uint64
	// Held is the target bytes a sync clears but the disk keeps until the
	// parts merge: a lightweight DELETE only masks rows. It applies to
	// replace and repair.
	Held uint64
	// Headroom is Need plus Held, times the headroom factor: merges also
	// write a new part before they drop the old ones.
	Headroom uint64
	Tables   []datacatalog.TableRef
	// OK is false when Headroom exceeds Free, or when a disk of the set is
	// shared with other groups and their headrooms together exceed what that
	// disk has free.
	OK bool
}

// Preflight estimates what the chosen tables will need on the target and
// compares it with free space (ADR-0259 §SD6). It only warns: the estimate
// ignores codec differences between the servers and what merges reclaim. A
// policy does not say how it spreads parts over its disks, so a group's whole
// headroom is charged to each of its disks when disks are shared between
// groups.
func Preflight(tables []*PlanTable, dst *DiskReport, headroomFactor float64) (out []PreflightDisk) {
	byGroup := make(map[string]*PreflightDisk, 4)
	order := make([]string, 0, 4)
	for _, pt := range tables {
		fp, has := dst.Table(pt.Target)
		disks := []string{"default"}
		if has {
			disks = fp.Disks
		}
		key := strings.Join(disks, ",")
		g := byGroup[key]
		if g == nil {
			g = &PreflightDisk{Disks: disks}
			for _, name := range disks {
				g.Free += usableFree(dst, name)
			}
			byGroup[key] = g
			order = append(order, key)
		}
		g.Need += uint64(float64(pt.Bytes) * ExpectedCopyFraction(pt))
		g.Held += heldBytes(pt, fp)
		g.Tables = append(g.Tables, pt.Target)
	}
	demand := make(map[string]uint64, len(dst.Disks))
	for _, key := range order {
		g := byGroup[key]
		g.Headroom = uint64(float64(g.Need+g.Held) * max(headroomFactor, 1))
		for _, name := range g.Disks {
			demand[name] += g.Headroom
		}
	}
	for _, key := range order {
		g := byGroup[key]
		g.OK = g.Headroom <= g.Free
		for _, name := range g.Disks {
			if _, known := dst.Disk(name); known && demand[name] > usableFree(dst, name) {
				g.OK = false
			}
		}
		out = append(out, *g)
	}
	return
}

// usableFree is a disk's free space less what ClickHouse keeps free; zero for
// a disk the report does not list.
func usableFree(rep *DiskReport, name string) (free uint64) {
	if d, ok := rep.Disk(name); ok && d.FreeSpace > d.KeepFreeSpace {
		return d.FreeSpace - d.KeepFreeSpace
	}
	return 0
}

// heldBytes estimates the target bytes a sync of pt clears that stay on disk
// until merges: all of the target under replace, the cleared share under
// repair.
func heldBytes(pt *PlanTable, fp TableFootprint) (held uint64) {
	if pt.Sync == nil {
		return 0
	}
	switch {
	case pt.Sync.Mode == SyncModeRepair && pt.Diff != nil && pt.Diff.DstRows > 0:
		return uint64(float64(fp.BytesOnDisk) * float64(RepairClearRows(pt.Diff)) / float64(pt.Diff.DstRows))
	case pt.Sync.Mode != SyncModeRepair && pt.Sync.Existing == ExistingPolicyReplace:
		return fp.BytesOnDisk
	}
	return 0
}

// FreeFloor is the free space below which a sync waits between chunks.
type FreeFloor struct {
	// MinFreeBytes and MinFreeFraction are both floors; the larger applies.
	MinFreeBytes    uint64
	MinFreeFraction float64
	// Poll is how often a waiting sync looks again.
	Poll time.Duration
}

func DefaultFreeFloor() (f FreeFloor) {
	return FreeFloor{MinFreeBytes: 1 << 30, MinFreeFraction: 0.05, Poll: 10 * time.Second}
}

func (inst FreeFloor) floor(d DiskInfo) (floor uint64) {
	return max(inst.MinFreeBytes, uint64(float64(d.TotalSpace)*inst.MinFreeFraction))
}

// Low returns the disks among names below the floor.
func (inst FreeFloor) Low(rep *DiskReport, names []string) (low []DiskInfo) {
	for _, n := range names {
		d, has := rep.Disk(n)
		if has && d.FreeSpace < inst.floor(d)+d.KeepFreeSpace {
			low = append(low, d)
		}
	}
	return
}

// WaitForFree returns once none of the table's target disks is below the
// floor, polling every Poll. notify is called each time it finds them low,
// so an operator sees why the sync stands still. A failed disk readout does
// not end the wait: the disks are then unknown, notify is called with no
// disks, and the next poll reads again. Only cancelling ctx stops the wait,
// and the run resumes from its journal later.
func (inst FreeFloor) WaitForFree(ctx context.Context, q QueryI, target datacatalog.TableRef, notify func(low []DiskInfo)) (err error) {
	for {
		rep, rerr := ReadDisks(ctx, q, []datacatalog.TableRef{target})
		if rerr == nil {
			disks := []string{"default"}
			if fp, has := rep.Table(target); has {
				disks = fp.Disks
			}
			low := inst.Low(&rep, disks)
			if len(low) == 0 {
				return
			}
			if notify != nil {
				notify(low)
			}
		} else if notify != nil {
			notify(nil)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(max(inst.Poll, time.Second)):
		}
	}
}

// pollWrittenRows advances rows by the INSERT's written_rows, read from the
// target's system.processes every period until stop is closed, and returns
// the last value it saw. It is best effort: a failed poll is skipped, and a
// query no longer in system.processes has finished.
func pollWrittenRows(ctx context.Context, q QueryI, queryId string, period time.Duration, rows *atomic.Int64, stop <-chan struct{}) (seen int64) {
	if rows == nil {
		<-stop
		return
	}
	sql := "SELECT toUInt64(written_rows) AS n FROM system.processes WHERE query_id = " + QuoteString(queryId) + jsonSettings
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
		}
		got, err := queryRows[countRow](ctx, q, sql)
		if err != nil || len(got) != 1 {
			continue
		}
		n := int64(got[0].N)
		if n > seen {
			rows.Add(n - seen)
			seen = n
		}
	}
}
