package timescrubber

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Without indices the readout names a step by its time and value, not its
// number; a range is worded by its times, and only while its controls are
// shown (ADR-0251 §SD10). A series without times keeps its numbers.
func TestAReadoutWithoutIndices(t *testing.T) {
	steps := stepsAt(0, 1, 2, 3)
	for i := range steps {
		steps[i].Peak = float32(math.NaN())
	}
	steps[2].Value = 3
	now := steps[3].At
	sc := &Scrubber{Opts: Options{ValueName: "rows"}, now: func() time.Time { return now }}
	sc.Transport.Pos = 2
	sc.Transport.SetRange(1, 3)
	assert.Equal(t, "Sun 2026-03-01 02:00 UTC · step 3 of 4 · −60 min · 1 h step · range 2–4", sc.readout(steps))

	sc.Opts.NoIndices = true
	assert.Equal(t, "Sun 2026-03-01 02:00 UTC · rows 3 · −60 min · 1 h step · range Sun 01:00 UTC to Sun 03:00 UTC", sc.readout(steps))

	sc.Opts.Hide = PartRange
	assert.Equal(t, "Sun 2026-03-01 02:00 UTC · rows 3 · −60 min · 1 h step", sc.readout(steps), "a hidden range is the host's to word")

	sc.drag = dragPlayhead
	assert.Equal(t, "Sun 2026-03-01 02:00 UTC · lets go on Sun 02:00 UTC · rows 3 · −60 min · 1 h step", sc.readout(steps))
	sc.drag = dragNone

	same := stepsAt(0, 0, 0, 0)
	sc.Transport.Pos = 1
	assert.Equal(t, "Step 2 of 4", sc.readout(same), "steps without times have only their numbers")
}
