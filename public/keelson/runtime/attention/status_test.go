package attention

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What opens on its own at each status (ADR-0328 §SD2): Here a critical
// item and an escalated one, Busy only the critical, Away neither. One held
// back does not open later on its own.
func TestStatusGatesOpens(t *testing.T) {
	for _, tc := range []struct {
		status StatusE
		want   []string
	}{
		{StatusHere, []string{"c", "q"}},
		{StatusBusy, []string{"c"}},
		{StatusAway, nil},
	} {
		t.Run(tc.status.String(), func(t *testing.T) {
			src := &fakeSource{items: []Item{
				{Key: "q", Kind: KindQuestion, Created: t0},
				{Key: "c", Kind: KindConfirmation, Created: t0},
			}}
			q := NewQueue(nil, 5*time.Minute)
			p := NewPresence(0, t0)
			p.Set(tc.status, t0)
			q.SetPresence(p)
			q.AddSource(src)
			open, _ := q.Snapshot(t0.Add(6 * time.Minute))
			assert.Equal(t, tc.want, q.TakeOpens(open))
			p.Set(StatusHere, t0.Add(7*time.Minute))
			open, _ = q.Snapshot(t0.Add(7 * time.Minute))
			assert.Empty(t, q.TakeOpens(open), "held back, not opened on return")
		})
	}
}

// An Away the host set for idle time ends at the next input, back to the
// status before it; an Away set by hand does not (§SD1).
func TestIdleAway(t *testing.T) {
	p := NewPresence(10*time.Minute, t0)
	var changes []StatusChange
	p.SetOnChange(func(ch StatusChange) { changes = append(changes, ch) })
	p.Set(StatusBusy, t0)
	p.Tick(t0.Add(9 * time.Minute))
	s, _ := p.Status()
	assert.Equal(t, StatusBusy, s)
	p.Tick(t0.Add(10 * time.Minute))
	s, byIdle := p.Status()
	assert.Equal(t, StatusAway, s)
	assert.True(t, byIdle)
	p.Input(t0.Add(12 * time.Minute))
	s, byIdle = p.Status()
	assert.Equal(t, StatusBusy, s, "back to the status before")
	assert.False(t, byIdle)
	require.Len(t, changes, 3)
	assert.True(t, changes[1].ByIdle)

	p.Set(StatusAway, t0.Add(13*time.Minute))
	p.Input(t0.Add(14 * time.Minute))
	s, _ = p.Status()
	assert.Equal(t, StatusAway, s, "an Away set by hand ends by hand")
	p.Tick(t0.Add(time.Hour))
	assert.Len(t, changes, 4, "Tick leaves Away alone")
}

func TestNoIdleAwayAtZero(t *testing.T) {
	p := NewPresence(0, t0)
	p.Tick(t0.Add(24 * time.Hour))
	s, _ := p.Status()
	assert.Equal(t, StatusHere, s)
}

// The away log (§SD3): what arrived, what left and why, refusals and
// highlights; not what the person decided in a card; shown once Away ends.
func TestAwayLog(t *testing.T) {
	svc := NewService()
	svc.Presence = NewPresence(0, t0)
	svc.Queue.SetPresence(svc.Presence)
	svc.Flood = NewFlood(1, 0)
	svc.Flood.SetOnRefuse(svc.Presence.recordRefusal)
	svc.Notices.SetOnKept(svc.Presence.recordNotice)
	src := &fakeSource{items: []Item{{Key: "before", Kind: KindQuestion, Created: t0}}}
	svc.Queue.AddSource(src)
	svc.Queue.Snapshot(t0)

	svc.Presence.Set(StatusAway, t0.Add(time.Minute))
	expiring := Item{Key: "exp", Kind: KindGrantRequest, Created: t0.Add(2 * time.Minute), Bound: t0.Add(5 * time.Minute)}
	decided := Item{Key: "dec", Kind: KindQuestion, Created: t0.Add(2 * time.Minute)}
	src.items = append(src.items, expiring, decided)
	svc.Queue.Snapshot(t0.Add(2 * time.Minute))
	svc.Queue.Decided("dec")
	src.items = src.items[:1]
	svc.Queue.Snapshot(t0.Add(6 * time.Minute))
	svc.Flood.Admit("conv-a", t0.Add(6*time.Minute))
	ok, _ := svc.Flood.Admit("conv-a", t0.Add(6*time.Minute))
	require.False(t, ok)
	svc.Levels.Set(AuthorPerson, "", "conv-b", Key{Kind: "turn"}, LevelHighlight)
	svc.Notices.Notify("app", 7, Notice{Conversation: "conv-b", Kind: "turn", Text: "the turn ended"}, t0.Add(7*time.Minute))

	_, shown := svc.Presence.Returned()
	assert.False(t, shown, "nothing to show while away")
	svc.Presence.Set(StatusHere, t0.Add(8*time.Minute))
	l, shown := svc.Presence.Returned()
	require.True(t, shown)
	var got []string
	for _, e := range l.Entries {
		got = append(got, e.What.String()+":"+e.Key+e.Conversation+":"+e.Why[:min(len(e.Why), 7)])
	}
	assert.Equal(t, []string{"arrived:exp:", "arrived:dec:", "left:exp:expired", "refused:conv-a:this co", "highlight:conv-b:"}, got,
		"the item waiting before Away is not logged; the decided one's leaving is not")
	svc.Presence.Dismiss()
	_, shown = svc.Presence.Returned()
	assert.False(t, shown)
}

func TestAwayLogBounded(t *testing.T) {
	p := NewPresence(0, t0)
	p.Set(StatusAway, t0)
	for range MaxAwayLog + 3 {
		p.Record(AwayEntry{What: AwayArrived})
	}
	p.Set(StatusHere, t0.Add(time.Minute))
	l, _ := p.Returned()
	assert.Len(t, l.Entries, MaxAwayLog)
	assert.Equal(t, 3, l.Dropped)
}
