package jackstay

// sliceShare is the share of a source table's rows its row filter selects
// (ADR-0271 §SD1), as the latest diff counted the slice. With no filter it is
// the whole table. known is false when a filter is set and no diff has
// counted the slice; share is then 1, an upper bound.
func sliceShare(pt *PlanTable) (share float64, known bool) {
	switch {
	case pt.Filter == "" || pt.Rows == 0:
		return 1, true
	case pt.Diff == nil:
		return 1, false
	}
	return min(float64(pt.Diff.SrcRows)/float64(pt.Rows), 1), true
}

// ExpectedCopyFraction is the share of a source table a sync is expected to
// copy: its slice under a row filter, the sample fraction of that, or, for
// repair, the source rows of the leaves the diff listed. A filtered table no
// diff has counted is taken whole.
func ExpectedCopyFraction(pt *PlanTable) (f float64) {
	f, _ = expectedCopy(pt)
	return
}

// expectedCopy is [ExpectedCopyFraction]; known is false when the fraction
// stands in for a slice share no diff has counted.
func expectedCopy(pt *PlanTable) (f float64, known bool) {
	share, known := sliceShare(pt)
	if pt.Sync == nil {
		return share, known
	}
	switch pt.Sync.Mode {
	case SyncModeSample:
		if pt.Sync.SampleDen == 0 {
			return share, known
		}
		return share * float64(pt.Sync.SampleNum) / float64(pt.Sync.SampleDen), known
	case SyncModeRepair:
		if pt.Diff == nil || pt.Rows == 0 {
			return 1, pt.Filter == ""
		}
		return float64(RepairCopyRows(pt.Diff)) / float64(pt.Rows), true
	}
	return share, known
}

// RepairCopyRows is the source row count repair would copy, as of the diff.
func RepairCopyRows(d *TableDiff) (rows uint64) {
	for i := range d.Differing {
		rows += d.Differing[i].repairCopyRows()
	}
	return
}

// repairCopyRows is the source row count repair copies for one chunk.
func (inst *ChunkDiff) repairCopyRows() (rows uint64) {
	switch {
	case inst.AbsentOnTarget:
		return inst.SrcRows
	case inst.AbsentOnSource:
		return 0
	}
	for _, ld := range inst.Leaves {
		rows += ld.SrcRows
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
