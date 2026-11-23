package adhocdata

import (
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// standingE is how a published revision stands to a task: whether it is the
// task's own work at the lowest tier. One rule decides what a task may
// release by an exposure and what it reads back without a label.
type standingE uint8

const (
	// standingUnspecified is the zero value, never a revision's standing.
	standingUnspecified standingE = iota
	// standingOthers — no attested call of the task published the
	// revision: a person did, an app on its own account, or another task.
	standingOthers
	// standingLabelled — the task's call published it under a label above
	// TLP:CLEAR.
	standingLabelled
	// standingDerived — the task's call published it at TLP:CLEAR, and it
	// derives from sealed data.
	standingDerived
	// standingOwnClear — the task's call published it at TLP:CLEAR, from
	// nothing sealed.
	standingOwnClear
)

// allStandings lists every standingE value.
var allStandings = []standingE{standingUnspecified, standingOthers, standingLabelled, standingDerived, standingOwnClear}

func (inst standingE) String() (s string) {
	switch inst {
	case standingOthers:
		s = "others"
	case standingLabelled:
		s = "labelled"
	case standingDerived:
		s = "derived"
	case standingOwnClear:
		s = "own-clear"
	default:
		s = "unspecified"
	}
	return
}

// standingOf is the standing, to task, of a revision published by the
// attested call pub, derived saying whether it derives from sealed data.
// The task is the one the dispatcher confirmed for the asking call, never
// one a sender stated. An empty task owns nothing.
func standingOf(pub option.Option[app.CallContext], derived bool, task string) (standing standingE) {
	switch {
	case task == "" || !pub.Has || pub.Val.Task != task:
		return standingOthers
	case pub.Val.Tier > uint8(agentrun.TierClear):
		return standingLabelled
	case derived:
		return standingDerived
	}
	return standingOwnClear
}
