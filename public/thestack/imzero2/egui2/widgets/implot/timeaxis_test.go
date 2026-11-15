package implot

import (
	"testing"
	"time"
)

// Ticks in a zone fall on that zone's calendar boundaries, across a DST
// change: Europe/Zurich leaves summer time on 2026-10-25, a 25-hour day.
func TestTimeTicksInFollowsTheZone(t *testing.T) {
	zrh, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	from := time.Date(2026, 10, 22, 12, 0, 0, 0, zrh)
	to := time.Date(2026, 10, 28, 12, 0, 0, 0, zrh)
	values, labels := TimeTicksIn(float64(from.Unix()), float64(to.Unix()), 600, zrh)
	if len(values) < 3 || len(values) != len(labels) {
		t.Fatalf("ticks %v labels %v", values, labels)
	}
	var sawLongDay bool
	for i, v := range values {
		at := time.Unix(int64(v), 0).In(zrh)
		if at.Hour() != 0 || at.Minute() != 0 {
			t.Errorf("tick %d (%q) at %v, want a Zurich midnight", i, labels[i], at)
		}
		if i > 0 && v-values[i-1] == 25*3600 {
			sawLongDay = true
		}
	}
	if !sawLongDay {
		t.Errorf("no 25-hour step across 2026-10-25 in %v", values)
	}
	utc, _ := TimeTicksIn(float64(from.Unix()), float64(to.Unix()), 600, nil)
	if len(utc) == 0 || time.Unix(int64(utc[0]), 0).UTC().Hour() != 0 {
		t.Errorf("a nil zone does not tick on UTC midnights: %v", utc)
	}
}
