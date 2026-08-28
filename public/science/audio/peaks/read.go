package peaks

// PickLevel returns the highest level whose bin spans at most
// framesPerColumn frames — the coarsest summary that still has a bin per
// drawn column. It returns 0 when framesPerColumn is below the base bin (or
// is not a number), which is the case ADR-0208 §SD3 hands to the raw-frame
// path instead.
func (inst *Pyramid) PickLevel(framesPerColumn float64) (level int32) {
	for level+1 < inst.levels && float64(inst.framesPerBin[level+1]) <= framesPerColumn {
		level++
	}
	return level
}

// readableBins returns how many leading bins of a level a reader may touch.
// It is the whole level once the build is complete, and otherwise the bins
// whose frames lie entirely inside the published prefix.
func (inst *Pyramid) readableBins(level int32) (bins int64) {
	if inst.complete.Load() {
		// Safe without further synchronisation: the store that set
		// complete happened after every array and bookkeeping write, and
		// no write follows it.
		return inst.storedBins[level]
	}
	bins = inst.built.Load() / inst.framesPerBin[level]
	if total := inst.binCounts[level]; bins > total {
		bins = total
	}
	return bins
}

// Query copies up to min(len(dstMin), len(dstMax)) bins of one channel,
// starting at firstBin, and returns how many it copied. Only bins fully
// covered by [Pyramid.Built] are copied, so a short return means the build
// has not reached that far — not that the level ends there. An out-of-range
// level, channel or firstBin copies nothing.
//
// Safe from any goroutine, concurrently with the builder.
func (inst *Pyramid) Query(level int32, firstBin int64, ch int, dstMin []int8, dstMax []int8) (n int) {
	if level < 0 || level >= inst.levels || ch < 0 || ch >= int(inst.format.Channels) || firstBin < 0 {
		return 0
	}
	want := int64(min(len(dstMin), len(dstMax)))
	if want == 0 {
		return 0
	}
	available := inst.readableBins(level) - firstBin
	if available <= 0 {
		return 0
	}
	if want > available {
		want = available
	}
	row := int(level)*int(inst.format.Channels) + ch
	copy(dstMin[:want], inst.mins[row][firstBin:firstBin+want])
	copy(dstMax[:want], inst.maxs[row][firstBin:firstBin+want])
	return int(want)
}

// Columns reduces [fromFrame, toFrame) onto min(len(dstMin), len(dstMax))
// equal-width columns of one channel and returns how many leading columns
// were written. It picks the level from the frames per column
// ([Pyramid.PickLevel]) and takes the min/max over the bins of that level
// overlapping each column, so a column is never narrower than the signal it
// stands for but may be up to one bin of the picked level wider at each
// edge; bin-aligned columns are exact. Columns whose frames are not built
// yet are left untouched, and the scan stops at the first of them — that
// index is the return value, which is what lets a caller draw the built
// prefix and a placeholder beyond it.
//
// A column reaching past the end of the signal is reduced over the frames
// that exist; one that starts at or past the end is not built at all, so a
// view wider than the signal reports the columns that hold audio.
// fromFrame must not be negative; the caller clamps its view to
// [0, Frames()]. Nothing is allocated.
//
// Safe from any goroutine, concurrently with the builder.
func (inst *Pyramid) Columns(fromFrame int64, toFrame int64, ch int, dstMin []int8, dstMax []int8) (builtColumns int) {
	columns := min(len(dstMin), len(dstMax))
	if columns == 0 || fromFrame < 0 || toFrame <= fromFrame || ch < 0 || ch >= int(inst.format.Channels) {
		return 0
	}
	span := toFrame - fromFrame
	level := inst.PickLevel(float64(span) / float64(columns))
	fpb := inst.framesPerBin[level]
	total := inst.binCounts[level]
	readable := inst.readableBins(level)
	row := int(level)*int(inst.format.Channels) + ch
	rowMin := inst.mins[row]
	rowMax := inst.maxs[row]

	cols := int64(columns)
	for c := range columns {
		colFrom := fromFrame + span*int64(c)/cols
		colTo := fromFrame + span*int64(c+1)/cols
		if colTo <= colFrom {
			// More columns than frames: the column is the bin its frame
			// falls into.
			colTo = colFrom + 1
		}
		if colFrom >= inst.frames {
			// The column is entirely past the signal; the caller draws
			// nothing there rather than a copy of the last bin.
			break
		}
		firstBin := colFrom / fpb
		lastBin := (colTo + fpb - 1) / fpb
		if lastBin > total {
			lastBin = total
		}
		if firstBin >= lastBin || lastBin > readable {
			break
		}
		lo := rowMin[firstBin]
		hi := rowMax[firstBin]
		for b := firstBin + 1; b < lastBin; b++ {
			if v := rowMin[b]; v < lo {
				lo = v
			}
			if v := rowMax[b]; v > hi {
				hi = v
			}
		}
		dstMin[c] = lo
		dstMax[c] = hi
		builtColumns = c + 1
	}
	return builtColumns
}
