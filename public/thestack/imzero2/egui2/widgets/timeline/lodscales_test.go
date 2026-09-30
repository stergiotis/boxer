package timeline

import (
	"testing"
	"time"
)

// The default LOD ladder starts below a coarse offset-axis unit; New must
// not panic on it.
func TestNew_OffsetAxisCoarseUnit_DefaultLadder(t *testing.T) {
	_ = newTestTimeline(t, nil, withOffsetAxis(time.Second))
}
