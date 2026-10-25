package chat

import (
	"context"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// The levels (ADR-0323 §SD7): how loud each step of a turn is,
// from the host's one table — its defaults, the person's overrides and the
// conversation's. The window keeps the table its conversation sees and reads
// it again when the host announces a change or the conversation changes;
// without a host the defaults apply. A step's mark and whether the trail
// folds it follow from its level.

// levelsState is the window's copy of the table.
type levelsState struct {
	snap attention.Snapshot
	conv string
	// fetched says snap is the host's for conv, not the defaults.
	fetched bool
	// announced is the latest version the host announced, from the bus's
	// goroutine.
	announced atomic.Uint64
	job       bgjob.Runner[fetchedLevels]
	unsub     func()
}

type fetchedLevels struct {
	snap attention.Snapshot
	conv string
}

// startLevels subscribes to the host's announcements of a change.
func (inst *App) startLevels() {
	if inst.bus == nil {
		return
	}
	unsub, err := attention.SubscribeChanges(inst.bus, func(v uint64) { inst.levels.announced.Store(v) })
	if err != nil {
		inst.log.Debug().Err(err).Msg("chat: no announcements of the levels; the defaults and the first read stand")
		return
	}
	inst.levels.unsub = unsub
}

// stopLevels ends the subscription and the read in flight.
func (inst *App) stopLevels() {
	if inst.levels.unsub != nil {
		inst.levels.unsub()
		inst.levels.unsub = nil
	}
	inst.levels.job.Cancel()
}

// syncLevels takes a finished read, and starts one when the copy is stale:
// never read for this conversation, or older than the host's announcement.
// The render goroutine calls it once per frame.
func (inst *App) syncLevels() {
	l := &inst.levels
	if got, _, ok := l.job.TakeResult(); ok && got != nil {
		l.snap, l.conv, l.fetched = got.snap, got.conv, true
	}
	if inst.bus == nil || l.job.Snapshot().State == bgjob.StateRunning {
		return
	}
	conv := inst.conv.id
	if l.fetched && l.conv == conv && l.announced.Load() <= l.snap.Version {
		return
	}
	bus := inst.bus
	l.job.Start(nil, bgjob.Spec{Kind: "chat-levels", Title: "read the levels"}, func(context.Context) (out *fetchedLevels, err error) {
		s, err := attention.FetchLevels(bus, conv)
		if err != nil {
			return
		}
		return &fetchedLevels{snap: s, conv: conv}, nil
	})
}

// stepEvent is a step as the table classifies it (ADR-0323 §SD3): a model
// call is its reply, a tool call waiting on the person is asked, a refused
// one a refused outcome, any other its intent with the operation's effect.
func stepEvent(s *turnStep, conversation string) (ev attention.Event) {
	ev.Conversation = conversation
	switch {
	case s.kind == stepCompact && (s.failed || s.refused):
		// A compaction not made is a note to the person: the request went
		// out over the trigger (ADR-0316).
		ev.Kind = "note"
	case s.kind == stepCompact:
		ev.Kind = "compact"
	case s.kind == stepModel && s.failed:
		ev.Kind = "turn"
	case s.kind == stepModel:
		ev.Kind = "reply"
	case s.waitingOnPerson():
		ev.Kind, ev.Effect = "asked", effectOf(s.effect)
	case s.refused:
		ev.Kind, ev.Refused = "outcome", true
	default:
		ev.Kind, ev.Effect = "intent", effectOf(s.effect)
	}
	return
}

// stepLevel is a step's level under the window's copy of the table.
func (inst *App) stepLevel(s *turnStep) attention.LevelE {
	return inst.levels.snap.Classify(stepEvent(s, inst.conv.id))
}

// levelMark is a level's mark at the start of a step's line.
func levelMark(l attention.LevelE) string {
	switch l {
	case attention.LevelMessage:
		return icons.PhDot
	case attention.LevelHighlight:
		return icons.PhStar
	case attention.LevelYourTurn:
		return icons.PhHandPalm
	case attention.LevelCritical:
		return icons.PhWarning
	default:
		return icons.PhDotOutline
	}
}

// levelTip is what the mark means, for its hover.
func levelTip(l attention.LevelE) string {
	return "Level: " + l.String() + ". How loud a step is comes from the host's table of levels (ADR-0323): routine steps fold, a message stands out, your turn waits on you."
}
