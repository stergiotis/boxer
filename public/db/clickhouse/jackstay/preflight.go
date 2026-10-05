package jackstay

import (
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// PreflightDisk is the pre-flight verdict for one group of target disks: the
// disks of one storage policy, whose free space the tables on it share.
type PreflightDisk struct {
	Disks []string
	// Free is the free space of those disks, less what ClickHouse keeps free.
	Free uint64
	// Need is the estimated bytes the chosen tables add: the source's
	// on-disk bytes scaled by the share each sync copies, a row filter's
	// slice included.
	Need uint64
	// Held is the target bytes a sync clears but the disk keeps until the
	// parts merge: a lightweight DELETE only masks rows. It applies to
	// replace and repair.
	Held uint64
	// Headroom is Need plus Held, times the headroom factor: merges also
	// write a new part before they drop the old ones.
	Headroom uint64
	Tables   []datacatalog.TableRef
	// Overstated lists the tables with a row filter whose slice no diff has
	// counted: they are counted whole, so Need and Held overstate them.
	Overstated []datacatalog.TableRef
	// OK is false when Headroom exceeds Free, or when a disk of the set is
	// shared with other groups and their headrooms together exceed what that
	// disk has free.
	OK bool
}

// Preflight estimates what the chosen tables will need on the target and
// compares it with free space (ADR-0259 §SD6). It only warns: the estimate
// ignores codec differences between the servers and what merges reclaim. A
// group's headroom is set against the free space of its disks together. A
// policy does not say how it spreads parts over its disks, so a disk that
// several groups share is charged each group's whole headroom.
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
		f, fKnown := expectedCopy(pt)
		held, hKnown := heldBytes(pt, fp)
		g.Need += uint64(float64(pt.Bytes) * f)
		g.Held += held
		g.Tables = append(g.Tables, pt.Target)
		if !fKnown || !hKnown {
			g.Overstated = append(g.Overstated, pt.Target)
		}
	}
	demand := make(map[string]uint64, len(dst.Disks))
	groups := make(map[string]int, len(dst.Disks))
	for _, key := range order {
		g := byGroup[key]
		g.Headroom = uint64(float64(g.Need+g.Held) * max(headroomFactor, 1))
		for _, name := range g.Disks {
			demand[name] += g.Headroom
			groups[name]++
		}
	}
	for _, key := range order {
		g := byGroup[key]
		g.OK = g.Headroom <= g.Free
		for _, name := range g.Disks {
			if _, known := dst.Disk(name); known && groups[name] > 1 && demand[name] > usableFree(dst, name) {
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
// until merges: all of the target under replace, or its slice under a row
// filter; the cleared rows' share under repair. Bytes per row come from the
// target's own rows when the footprint has them. known is false when a row
// filter's share of the target could not be told and a larger share stands
// in for it.
func heldBytes(pt *PlanTable, fp TableFootprint) (held uint64, known bool) {
	if pt.Sync == nil {
		return 0, true
	}
	share := func(rows uint64) (held uint64, known bool) {
		switch {
		case rows == 0:
			return 0, true
		case fp.Rows > 0:
			return uint64(float64(fp.BytesOnDisk) * min(float64(rows)/float64(fp.Rows), 1)), true
		case pt.Diff != nil && pt.Diff.DstRows > 0:
			// The target's rows are not known; the diff's are the slice's
			// under a filter, which overstates the share.
			return uint64(float64(fp.BytesOnDisk) * min(float64(rows)/float64(pt.Diff.DstRows), 1)), pt.Filter == ""
		}
		return fp.BytesOnDisk, pt.Filter == ""
	}
	switch {
	case pt.Sync.Mode == SyncModeRepair:
		if pt.Diff == nil {
			return 0, true
		}
		return share(RepairClearRows(pt.Diff))
	case pt.Sync.Existing == ExistingPolicyReplace:
		if pt.Filter == "" {
			return fp.BytesOnDisk, true
		}
		if pt.Diff == nil {
			return fp.BytesOnDisk, false
		}
		return share(pt.Diff.DstRows)
	}
	return 0, true
}
