package jackstay

import (
	"context"
	"slices"
	"strings"
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
	// Rows sums the active parts' rows, rows a lightweight DELETE masked
	// included, which is what BytesOnDisk holds.
	Rows  uint64
	Parts uint64
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
		Rows     uint64 `json:"rows"`
		Parts    uint64 `json:"parts"`
	}
	var parts []partsRow
	parts, err = queryRows[partsRow](ctx, q, "SELECT database, table, sum(bytes_on_disk) AS bytes, sum(rows) AS rows, count() AS parts FROM system.parts WHERE active AND (database, table) IN ("+refTuples(refs)+") GROUP BY database, table"+jsonSettings)
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
				fp.BytesOnDisk, fp.Rows, fp.Parts = p.Bytes, p.Rows, p.Parts
			}
		}
		rep.Tables = append(rep.Tables, fp)
	}
	return
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
