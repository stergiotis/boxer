package imztop

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// The replay session's phases as a machine (keelson-app T1). The session is
// process-wide and already keeps its state (imztop_replay_session.go); the
// machine does not drive it. Each frame the window observes the session's
// status, mirrors it, and draws the chip; the rules below are the graph the
// chip opens and the edges an operator or an agent causes, so a mirror the
// rules cannot reach is logged rather than hidden. Failure returns to off
// through the person's Dismiss (or leave_replay), and a failed open may be
// retried, so the failed phase is a resting point, not a terminal one.

var allReplayStates = []ReplayStateE{ReplayOff, ReplayOpening, ReplayOn, ReplayFailed}

// observeReplay derives the phase from the session's status, with no memory
// of its own: the session is the fact, the machine mirrors it. Empty and
// the error are the phase's details, not phases.
func observeReplay(st ReplayStatus) (ph ReplayStateE) {
	return st.State
}

// newReplayMachine declares the session's lifecycle. Opening leaves in one
// of three ways: it completes, it fails, or a leave (the person's Cancel,
// leave_replay, or the opening window going away) discards it.
func newReplayMachine() (m *fsmview.Machine[ReplayStateE]) {
	m = fsmview.NewMachine(ReplayOff, 32, fsmview.MachineOptions[ReplayStateE]{
		Label:      func(ph ReplayStateE) string { return ph.String() },
		StateOrder: allReplayStates,
		StateColor: replayColor,
	})
	m.AddRule(ReplayOff, ReplayOpening).
		AddRule(ReplayOpening, ReplayOn, ReplayFailed, ReplayOff).
		AddRule(ReplayOn, ReplayOff).
		AddRule(ReplayFailed, ReplayOff, ReplayOpening).
		EdgeLabel(ReplayOff, ReplayOpening, "Replay / enter_replay").
		EdgeLabel(ReplayOpening, ReplayOn, "the open completes").
		EdgeLabel(ReplayOpening, ReplayFailed, "the open fails").
		EdgeLabel(ReplayOpening, ReplayOff, "Cancel / leave_replay").
		EdgeLabel(ReplayOn, ReplayOff, "Go live / leave_replay").
		EdgeLabel(ReplayFailed, ReplayOff, "Dismiss / leave_replay").
		EdgeLabel(ReplayFailed, ReplayOpening, "Replay / enter_replay")
	return
}

// replayColor tints the graph's nodes: opening in the accent, on in the
// warning tone replay shares with the frozen banner (ADR-0197 §SD8), failed
// as an error, off muted.
func replayColor(ph ReplayStateE, _ bool) styletokens.RGBA8 {
	switch ph {
	case ReplayOpening:
		return styletokens.AccentDefault
	case ReplayOn:
		return styletokens.WarningDefault
	case ReplayFailed:
		return styletokens.ErrorDefault
	}
	return styletokens.NeutralSubtle
}

// replayTone is replayColor for the chip's badge.
func replayTone(ph ReplayStateE) badge.ToneE {
	switch ph {
	case ReplayOpening:
		return badge.TonePrimary
	case ReplayOn:
		return badge.ToneWarning
	case ReplayFailed:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// mirrorReplay moves the machine to the phase this frame observed. A frame
// samples the session, so an open that completes and is left between two
// frames shows as an edge that skips a phase; that is a declared path taken
// unwatched and logs at debug. An observation no declared path reaches
// contradicts the model and logs a warning.
func (inst *App) mirrorReplay(st ReplayStatus) {
	m := inst.replayMachine
	cur, obs := m.Current(), observeReplay(st)
	if cur == obs || m.Mirror(obs) {
		return
	}
	ev := log.Warn()
	msg := "imztop: the replay phase moved along an edge the declared graph cannot reach (mirrored)"
	if m.CanReach(cur, obs) {
		ev = log.Debug()
		msg = "imztop: the replay phase skipped states no frame sampled (mirrored)"
	}
	ev.Stringer("from", cur).Stringer("to", obs).Msg(msg)
}

// ensureReplayChip builds the chip on the window's id stack. Mount calls it
// once the host's stack is in place; the tour and tests, which never mount,
// get it on the first frame that draws the top bar.
func (inst *App) ensureReplayChip() {
	if inst.replayChip != nil {
		return
	}
	inst.replayChip = fsmview.New(inst.ids, "replay-phase", inst.replayMachine, fsmview.Options[ReplayStateE]{
		Title:      "Replay phase",
		Tethered:   true,
		BadgeTone:  replayTone,
		AutoAnchor: true,
	})
}

// renderReplayChip draws the phase chip in the top bar (T2) with a one-line
// summary: live, the host replayed, or why the open failed. The chip opens
// the inspector: the graph, the history of phases and where they come from.
func (inst *App) renderReplayChip(st ReplayStatus) {
	inst.ensureReplayChip()
	inst.replayChip.Opts.Provenance = inspector.Provenance{
		Subject:   "app.imztop.replay.phase",
		SourceApp: string(manifest.Id),
	}
	inst.replayChip.Opts.Summary = func() {
		s := replayPhaseLine(st)
		if s == "" {
			return
		}
		muted := color.Hex(styletokens.NeutralTextSecondary.AsHex())
		c.LabelAtoms(c.Atoms().BeginRichTextColored(muted, color.Transparent, s).Small().End().Keep()).Send()
	}
	inst.replayChip.Render()
}

// replayPhaseLine is the chip's summary.
func replayPhaseLine(st ReplayStatus) (s string) {
	switch st.State {
	case ReplayOff:
		return "live"
	case ReplayOpening:
		return "opening a session…"
	case ReplayOn:
		if st.Empty {
			return "nothing recorded for " + st.Host
		}
		return "stored history for " + st.Host
	case ReplayFailed:
		if st.Err != nil {
			return trimTo(st.Err.Error(), 80)
		}
		return "the open failed"
	}
	return ""
}
