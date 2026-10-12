package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// The roster's pace (ADR-0324 background §3.4): how many steps a
// conversation wrote in each of the last minutes, and how long it has been
// quiet. It is read off the runs the roster lists anyway — a run's last
// ordinal grows by one per journal entry — so it covers every conversation,
// those of other windows too, with no read of its own. Sampled while the
// roster is open; a conversation seen once has no pace yet.

// paceBuckets is how many minutes the gauge shows; paceWindow how long
// samples are kept.
const (
	paceBuckets = 5
	paceWindow  = paceBuckets * time.Minute
)

// quietAfter is how long an open conversation writes nothing before the
// roster says it is quiet.
const quietAfter = 2 * time.Minute

type paceSample struct {
	at time.Time
	n  uint32
}

// paceTracker holds each open conversation's recent samples.
type paceTracker struct {
	by map[string][]paceSample
}

// observe takes a listing of runs at now: one sample per open run, and the
// samples older than the window dropped, but for the newest of them, which
// anchors the first delta inside it.
func (inst *paceTracker) observe(runs []agentrun.Run, now time.Time) {
	if inst.by == nil {
		inst.by = make(map[string][]paceSample, len(runs))
	}
	open := make(map[string]bool, len(runs))
	for _, r := range runs {
		if r.State != agentrun.StateOpen {
			continue
		}
		open[r.Conversation] = true
		s := append(inst.by[r.Conversation], paceSample{at: now, n: r.LastOrdinal})
		cut := 0
		for cut+1 < len(s) && now.Sub(s[cut+1].at) > paceWindow {
			cut++
		}
		inst.by[r.Conversation] = s[cut:]
	}
	for conv := range inst.by {
		if !open[conv] {
			delete(inst.by, conv)
		}
	}
}

// pace is a conversation's steps per minute, newest minute last, and when
// it last wrote; ok is false until two samples are in.
func (inst *paceTracker) pace(conversation string, now time.Time) (perMin [paceBuckets]int, lastMove time.Time, since time.Time, ok bool) {
	s := inst.by[conversation]
	if len(s) < 2 {
		return
	}
	since = s[0].at
	for i := 1; i < len(s); i++ {
		d := int(s[i].n) - int(s[i-1].n)
		if d <= 0 {
			continue
		}
		lastMove = s[i].at
		if age := int(now.Sub(s[i].at) / time.Minute); age < paceBuckets {
			perMin[paceBuckets-1-age] += d
		}
	}
	return perMin, lastMove, since, true
}

// sparks are the gauge's bars, none to full.
var sparks = []rune("▁▂▃▄▅▆▇█")

// paceGauge is the roster's gauge for a conversation: a bar per minute and
// the last minute's count, or how long it has been quiet. Empty while the
// pace is unknown.
func (inst *paceTracker) paceGauge(conversation string, now time.Time) string {
	perMin, lastMove, since, ok := inst.pace(conversation, now)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, v := range perMin {
		// One bar per two steps a minute, full from thirteen up.
		b.WriteRune(sparks[min(len(sparks)-1, (v+1)/2)])
	}
	from := since
	if lastMove.After(from) {
		from = lastMove
	}
	switch quietFor := now.Sub(from); {
	case quietFor >= quietAfter && lastMove.IsZero():
		b.WriteString(" quiet ≥ " + strconv.Itoa(int(quietFor/time.Minute)) + " min")
	case quietFor >= quietAfter:
		b.WriteString(" quiet " + strconv.Itoa(int(quietFor/time.Minute)) + " min")
	default:
		b.WriteString(" " + strconv.Itoa(perMin[paceBuckets-1]) + "/min")
	}
	return b.String()
}
