package ration

import "time"

const (
	minuteSlots = int64(MinuteHorizon / time.Minute)
	hourSlots   = int64(HourHorizon / time.Hour)
)

// series is one quantity of one account over time: per-minute counts for
// MinuteHorizon and per-hour counts for HourHorizon, each a ring whose
// slots remember which minute or hour they hold, so a stale slot reads as
// empty without a sweep.
type series struct {
	minute    [minuteSlots]int64
	minuteIdx [minuteSlots]int64
	hour      [hourSlots]int64
	hourIdx   [hourSlots]int64
}

func minuteOf(t time.Time) int64 { return t.UnixNano() / int64(time.Minute) }
func hourOf(t time.Time) int64   { return t.UnixNano() / int64(time.Hour) }

func (inst *series) add(at time.Time, v int64) {
	m := minuteOf(at)
	s := m % minuteSlots
	if inst.minuteIdx[s] != m {
		inst.minuteIdx[s], inst.minute[s] = m, 0
	}
	inst.minute[s] += v
	h := hourOf(at)
	s = h % hourSlots
	if inst.hourIdx[s] != h {
		inst.hourIdx[s], inst.hour[s] = h, 0
	}
	inst.hour[s] += v
}

// sum is the total in [from, now]. A span inside the minute horizon is
// summed by the minute, from's minute included; a longer one by the hour,
// from's hour included, so it may count up to an hour early.
func (inst *series) sum(from time.Time, now time.Time) (total int64) {
	if now.Sub(from) < MinuteHorizon {
		for m := minuteOf(from); m <= minuteOf(now); m++ {
			if s := m % minuteSlots; inst.minuteIdx[s] == m {
				total += inst.minute[s]
			}
		}
		return
	}
	first := max(hourOf(from), hourOf(now)-hourSlots+1)
	for h := first; h <= hourOf(now); h++ {
		if s := h % hourSlots; inst.hourIdx[s] == h {
			total += inst.hour[s]
		}
	}
	return
}

// freedBy is how long until a rolling window of length window, now at
// total, has dropped at least excess: the oldest buckets leave first. Zero
// when it cannot say.
func (inst *series) freedBy(window time.Duration, now time.Time, excess int64) (d time.Duration) {
	from := now.Add(-window)
	var dropped int64
	if window < MinuteHorizon {
		for m := minuteOf(from); m <= minuteOf(now); m++ {
			if s := m % minuteSlots; inst.minuteIdx[s] == m {
				dropped += inst.minute[s]
			}
			if dropped >= excess {
				leaves := time.Unix(0, (m+1)*int64(time.Minute)).Add(window)
				return max(leaves.Sub(now), time.Second)
			}
		}
		return
	}
	for h := max(hourOf(from), hourOf(now)-hourSlots+1); h <= hourOf(now); h++ {
		if s := h % hourSlots; inst.hourIdx[s] == h {
			dropped += inst.hour[s]
		}
		if dropped >= excess {
			leaves := time.Unix(0, (h+1)*int64(time.Hour)).Add(window)
			return max(leaves.Sub(now), time.Second)
		}
	}
	return
}
