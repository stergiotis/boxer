package play

import (
	"fmt"

	"github.com/dustin/go-humanize"
)

// queryAnnouncement is what a screen reader hears when a run settles: the
// row count, or that it failed and why (ADR-0363 §M2, proposed). A failure
// interrupts. Nothing is said for a state that did not change, or for a move
// to or from a stale twin, which is the same result with different inputs.
// The text is also what a scene waits on: a `label` flagged `live` in
// `imzero2 drive`'s tree.
func queryAnnouncement(prev, cur queryStateE, numRows int64, err error) (text string, assertive bool) {
	if prev == cur {
		return
	}
	switch cur {
	case queryStateRows:
		if prev != queryStateRowsStale {
			text = fmt.Sprintf("%s rows", humanize.Comma(numRows))
		}
	case queryStateEmpty:
		if prev != queryStateEmptyStale {
			text = "0 rows"
		}
	case queryStateFailed:
		if prev != queryStateFailedStale {
			text, assertive = "Query failed", true
			if err != nil {
				text += ": " + truncateRunes(firstLine(err.Error()), 120)
			}
		}
	}
	return
}
