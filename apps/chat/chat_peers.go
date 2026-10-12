package chat

import (
	"strconv"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// What each chat window's turn is doing, for the roster of the others
// (ADR-0324 background §3.1). The chat windows of a host share its process,
// so a window keeps its line here, by its key, and drops it at Unmount; a
// roster reads the others' beside the runs. Nothing is kept: a window of
// another process, as in a browser tab, has no line here, and its row says
// only which window holds it.

// peerTurn is a chat window's turn as its roster row shows it.
type peerTurn struct {
	conv  string
	line  string
	level attention.LevelE
}

// peerTurns are the chat windows' turns, by window key.
var peerTurns sync.Map

// publishTurn keeps this window's line for the other rosters. The render
// goroutine calls it once per frame; it writes only on a change.
func (inst *App) publishTurn() {
	if inst.window == 0 {
		return
	}
	line, level := inst.turnLine()
	p := peerTurn{conv: inst.conv.id, line: line, level: level}
	if p == inst.published {
		return
	}
	inst.published = p
	peerTurns.Store(strconv.FormatUint(inst.window, 10), p)
}

// unpublishTurn drops this window's line.
func (inst *App) unpublishTurn() {
	if inst.window != 0 {
		peerTurns.Delete(strconv.FormatUint(inst.window, 10))
	}
}

// peerTurnOf is the line of the chat window key while it holds
// conversation.
func peerTurnOf(key string, conversation string) (p peerTurn, ok bool) {
	v, found := peerTurns.Load(key)
	if !found {
		return
	}
	p = v.(peerTurn)
	return p, p.conv == conversation
}

// turnLine is what this window's turn is doing now, or what landed while
// the person was away, and its level: the highest since they last looked
// (ADR-0323 §SD7).
func (inst *App) turnLine() (line string, level attention.LevelE) {
	level = max(attention.LevelData, inst.looked.level)
	steps := inst.stepsNow()
	if inst.pending == nil || len(steps) == 0 {
		line = inst.observeTurn().String()
		if inst.looked.mark >= 0 || inst.looked.level > attention.LevelData {
			line += " · new since you looked"
		}
		return
	}
	for i := range steps {
		level = max(level, inst.stepLevel(&steps[i]))
	}
	return stepLine(&steps[len(steps)-1]), level
}

const tipRosterRaise = "Bring the chat window that holds this conversation to the front."

// raiseWindow brings the chat window key to the front, off the frame.
func (inst *App) raiseWindow(key string) {
	bus := inst.bus
	k, err := strconv.ParseUint(key, 10, 64)
	if bus == nil || err != nil {
		return
	}
	go func() {
		if rerr := windowhost.RequestRaise(bus, k); rerr != nil {
			inst.log.Debug().Err(rerr).Str("window", key).Msg("chat: raise the conversation's window")
		}
	}()
}
