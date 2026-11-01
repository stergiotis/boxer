// Package verdict is the vocabulary of a writer run's evidence verdict as
// the trail viewer shows it (ADR-0350 §SD6, proposed): the state, the line
// of text that names it, the tone it is drawn in and the law it comes from.
// It holds no reads; a caller builds a [Line] from the recorder's seal
// verification ([FromSeals]) or from the rows of keelson('evidence_checks')
// ([FromChecks]), and renders [Line.Label], [Line.Short] and [Line.Tone]
// however it draws.
//
// Chat and the app center may adopt it, so it imports no widget package:
// [ToneE] is its own, and a renderer maps it to a badge tone.
package verdict

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// StateE is what the evidence says of one writer run.
type StateE uint8

const (
	// StateUnverified is a run nothing could verify: the trail is not
	// durable, the verification failed, or no line was read yet. Its rows
	// are marked, not hidden.
	StateUnverified StateE = iota
	// StateHolds is a seal stream that chains and names every row.
	StateHolds
	// StateBroken is a seal stream that breaks at a seal for a reason.
	StateBroken
	// StateGap is a stream with rows no seal names.
	StateGap
	// StateFork is two seals that follow one.
	StateFork
	// StateUnanchored is a stream whose anchored heads break from a seal on.
	StateUnanchored
)

// AllStates lists every state, in declaration order.
var AllStates = []StateE{StateUnverified, StateHolds, StateBroken, StateGap, StateFork, StateUnanchored}

var stateNames = [...]string{"unverified", "holds", "broken", "gap", "fork", "unanchored"}

func (inst StateE) String() (s string) {
	if int(inst) < len(stateNames) {
		return stateNames[inst]
	}
	return "unknown"
}

// ParseState reads a state name; ok is false for a name that is none.
func ParseState(s string) (st StateE, ok bool) {
	for i, n := range stateNames {
		if n == s {
			return StateE(i), true
		}
	}
	return
}

// ToneE is how a state is drawn, independent of any widget's palette.
type ToneE uint8

const (
	ToneNeutral ToneE = iota
	ToneOk
	ToneWarning
	ToneError
)

// The laws a line comes from (ADR-0327 §SD7): L34 the seal stream, L32
// its anchored heads, and the trail's durability row.
const (
	LawSeals     = "L34"
	LawAnchors   = "L32"
	LawDurable   = "durable"
	lawUnchecked = ""
)

// Line is one writer run's verdict.
type Line struct {
	Run   string
	State StateE
	// Ordinal is the seal a broken stream breaks at, or the first seal
	// whose anchored head breaks; 0 otherwise.
	Ordinal uint64
	// Count is the rows no seal names, for StateGap.
	Count int
	// Reason is why: the break's reason spelled out for StateBroken, the
	// failure for StateUnverified.
	Reason string
}

// Tone is the line's tone: ok for holds, warning for a gap or unanchored
// heads, error for a break or a fork, neutral when unverified.
func (inst Line) Tone() (t ToneE) {
	switch inst.State {
	case StateHolds:
		t = ToneOk
	case StateGap, StateUnanchored:
		t = ToneWarning
	case StateBroken, StateFork:
		t = ToneError
	default:
		t = ToneNeutral
	}
	return
}

// Law is the law the line's state comes from.
func (inst Line) Law() (law string) {
	switch inst.State {
	case StateHolds, StateBroken, StateGap, StateFork:
		law = LawSeals
	case StateUnanchored:
		law = LawAnchors
	default:
		law = lawUnchecked
	}
	return
}

// Label is the line's text, in the wording of ADR-0350 §SD6.
func (inst Line) Label() (s string) {
	switch inst.State {
	case StateHolds:
		s = "holds"
	case StateBroken:
		s = "broken at ordinal " + strconv.FormatUint(inst.Ordinal, 10) + ": " + inst.Reason
	case StateGap:
		s = "gap: " + strconv.Itoa(inst.Count) + " rows no seal names"
	case StateFork:
		s = "fork: two seals follow one"
	case StateUnanchored:
		s = "unanchored since ordinal " + strconv.FormatUint(inst.Ordinal, 10)
	default:
		s = "unverified"
		if inst.Reason != "" {
			s += ": " + inst.Reason
		}
	}
	return
}

// Short is the state in bounded words — holds; broken at ordinal N; gap: K
// rows no seal names; fork; unanchored since ordinal N; unverified — with no
// Reason in it. It is what a badge shows (ADR-0350 §SD6, §SD12): the Reason
// is an unbounded string from a read, which a wrapped row of the top panel
// cannot hold without moving the layout; Label carries it for the tooltip.
func (inst Line) Short() (s string) {
	switch inst.State {
	case StateHolds:
		s = "holds"
	case StateBroken:
		s = "broken at ordinal " + strconv.FormatUint(inst.Ordinal, 10)
	case StateGap:
		s = "gap: " + strconv.Itoa(inst.Count) + " rows no seal names"
	case StateFork:
		s = "fork"
	case StateUnanchored:
		s = "unanchored since ordinal " + strconv.FormatUint(inst.Ordinal, 10)
	default:
		s = "unverified"
	}
	return
}

// Mark is the short text a table cell carries for a row of the run: a
// check for holds, the state's name otherwise.
func (inst Line) Mark() (s string) {
	if inst.State == StateHolds {
		return "✓"
	}
	return inst.State.String()
}

// BreakReason spells a seal break's reason out, naming the row when the
// reason has one.
func BreakReason(b evidence.SealBreak) (s string) {
	switch b.Why {
	case evidence.SealBreakPrev:
		s = "a seal follows no seal of the stream"
	case evidence.SealBreakDigest:
		s = "a seal's digest is not over its rows and its predecessor"
	case evidence.SealBreakRowMissing:
		s = "a row the seal names is not on the store"
	case evidence.SealBreakRowDigest:
		s = "a row's digest on the store is not the one the seal names"
	case evidence.SealBreakRowTwice:
		s = "two seals name one row"
	case evidence.SealBreakOrder:
		s = "two seals follow one"
	default:
		s = b.Why.String()
	}
	if b.Row != 0 {
		s += " (row " + strconv.FormatUint(b.Row, 10) + ")"
	}
	return
}

// FromSeals is the line for run from the recorder's seal verification:
// a break wins, then rows no seal names, then unanchored heads.
func FromSeals(run string, broken evidence.SealBreak, unnamed []uint64, unanchored []uint64, err error) (l Line) {
	l = Line{Run: run}
	switch {
	case err != nil:
		l.State, l.Reason = StateUnverified, err.Error()
	case broken.Why == evidence.SealBreakOrder:
		l.State, l.Ordinal = StateFork, broken.Seal
	case broken.Why != evidence.SealBreakNone:
		l.State, l.Ordinal, l.Reason = StateBroken, broken.Seal, BreakReason(broken)
	case len(unnamed) > 0:
		l.State, l.Count = StateGap, len(unnamed)
	case len(unanchored) > 0:
		l.State, l.Ordinal = StateUnanchored, unanchored[0]
	default:
		l.State = StateHolds
	}
	return
}

// Check is one row of keelson('evidence_checks') as [FromChecks] reads it.
type Check struct {
	Store  string `ch:"store"`
	Scope  string `ch:"scope"`
	Law    string `ch:"law"`
	Holds  bool   `ch:"holds"`
	Broken uint64 `ch:"broken"`
	Why    string `ch:"why"`
}

// The why texts keelson('evidence_checks') writes for the trail's rows,
// which FromChecks reads back.
const (
	whyStreamBreaks   = "the stream breaks: "
	whyRowsUnnamed    = " rows no seal names"
	whyAnchoredBreaks = "anchored seals break at "
)

// FromChecks is the line for run from the trail's rows of
// keelson('evidence_checks'): its durability row, its L34 row and its L32
// row. ok is false when the rows name no L34 verdict for the run, so the
// caller can verify the seals itself.
func FromChecks(run string, checks []Check) (l Line, ok bool) {
	l = Line{Run: run}
	var seals, anchors *Check
	for i := range checks {
		ch := &checks[i]
		if ch.Store != "trail" || ch.Scope != run {
			continue
		}
		switch ch.Law {
		case LawDurable:
			if !ch.Holds {
				l.State, l.Reason = StateUnverified, ch.Why
				return l, true
			}
		case LawSeals:
			seals = ch
		case LawAnchors:
			anchors = ch
		}
	}
	if seals == nil {
		return l, false
	}
	ok = true
	switch {
	case !seals.Holds && strings.HasPrefix(seals.Why, whyStreamBreaks):
		reason := strings.TrimPrefix(seals.Why, whyStreamBreaks)
		if strings.HasPrefix(reason, evidence.SealBreakOrder.String()) {
			l.State, l.Ordinal = StateFork, seals.Broken
			return
		}
		l.State, l.Ordinal = StateBroken, seals.Broken
		l.Reason = reasonFromName(reason)
	case !seals.Holds && strings.HasSuffix(seals.Why, whyRowsUnnamed):
		n, err := strconv.Atoi(strings.TrimSuffix(seals.Why, whyRowsUnnamed))
		if err != nil {
			l.State, l.Reason = StateUnverified, seals.Why
			return
		}
		l.State, l.Count = StateGap, n
	case !seals.Holds:
		l.State, l.Reason = StateUnverified, seals.Why
	case anchors != nil && !anchors.Holds && strings.HasPrefix(anchors.Why, whyAnchoredBreaks):
		l.State, l.Ordinal = StateUnanchored, anchors.Broken
	default:
		l.State = StateHolds
	}
	return
}

// reasonFromName spells out a break reason the evidence_checks row names
// by its short name ("digest at row 12").
func reasonFromName(s string) (out string) {
	name, rest, _ := strings.Cut(s, " ")
	var row uint64
	if r, found := strings.CutPrefix(rest, "at row "); found {
		row, _ = strconv.ParseUint(r, 10, 64)
	}
	for w := evidence.SealBreakPrev; w <= evidence.SealBreakRowTwice; w++ {
		if w.String() == name {
			return BreakReason(evidence.SealBreak{Why: w, Row: row})
		}
	}
	return s
}
