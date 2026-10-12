package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

func run(conv string, n uint32) agentrun.Run {
	return agentrun.Run{Conversation: conv, State: agentrun.StateOpen, LastOrdinal: n}
}

func TestThePaceCountsStepsPerMinute(t *testing.T) {
	var p paceTracker
	t0 := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	p.observe([]agentrun.Run{run("c", 10)}, t0)
	assert.Empty(t, p.paceGauge("c", t0), "one sample: no pace yet")

	p.observe([]agentrun.Run{run("c", 14)}, t0.Add(30*time.Second))
	p.observe([]agentrun.Run{run("c", 20)}, t0.Add(90*time.Second))
	now := t0.Add(100 * time.Second)
	perMin, last, _, ok := p.pace("c", now)
	assert.True(t, ok)
	assert.Equal(t, [paceBuckets]int{0, 0, 0, 4, 6}, perMin, "four in the minute before, six in the last")
	assert.Equal(t, t0.Add(90*time.Second), last)
	assert.Equal(t, "▁▁▁▃▄ 6/min", p.paceGauge("c", now))
}

func TestAnOpenConversationThatWritesNothingIsQuiet(t *testing.T) {
	var p paceTracker
	t0 := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	p.observe([]agentrun.Run{run("c", 3)}, t0)
	p.observe([]agentrun.Run{run("c", 5)}, t0.Add(10*time.Second))
	for i := 1; i <= 20; i++ {
		p.observe([]agentrun.Run{run("c", 5)}, t0.Add(10*time.Second+time.Duration(i)*10*time.Second))
	}
	now := t0.Add(210 * time.Second)
	assert.Equal(t, "▁▂▁▁▁ quiet 3 min", p.paceGauge("c", now), "the two steps three minutes back, then nothing")

	var fresh paceTracker
	fresh.observe([]agentrun.Run{run("d", 7)}, t0)
	fresh.observe([]agentrun.Run{run("d", 7)}, t0.Add(150*time.Second))
	assert.Equal(t, "▁▁▁▁▁ quiet ≥ 2 min", fresh.paceGauge("d", t0.Add(150*time.Second)), "quiet since first seen, at least")
}

func TestThePaceForgetsWhatClosedAndWhatIsOld(t *testing.T) {
	var p paceTracker
	t0 := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	for i := range 40 {
		p.observe([]agentrun.Run{run("c", uint32(i))}, t0.Add(time.Duration(i)*15*time.Second))
	}
	s := p.by["c"]
	assert.LessOrEqual(t, s[len(s)-1].at.Sub(s[1].at), paceWindow, "kept within the window, and one before it")
	closed := run("c", 40)
	closed.State = agentrun.StateClosed
	p.observe([]agentrun.Run{closed}, t0.Add(11*time.Minute))
	assert.Empty(t, p.by, "a closed conversation has no pace")
}
