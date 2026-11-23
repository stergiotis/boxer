package chat

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sectionhead"
)

// The conversation's label as the window states it (ADR-0311; ADR-0404
// §SD7). The journal holds a label on every entry and the run's is their
// join; the host routes model calls and admits exports by it. This file
// reads out what a person asks of it: the tier and what raised it, whether
// untrusted content was read and what brought it, and where the content
// was sent. Nothing here decides anything.

// labelFacts is the conversation's label for the state chip's inspector.
type labelFacts struct {
	// tier is the TLP tier, and tierBy what first carried it: empty at
	// CLEAR and when the journal names nothing.
	tier   agentrun.TierE
	tierBy string
	// untrusted says content from outside the person's own words reached
	// the model, and untrustedBy names a read that brought some.
	untrusted   bool
	untrustedBy string
	// confined says the tier reached the one the host confines.
	confined bool
	// sentTo are the remote providers the content was sent to, and from
	// the remote routes whose output it holds.
	sentTo []string
	from   []string
}

// labelCauses is what raised the label, as last read from the journal, and
// what it was read for: the scan runs again when any of these moved.
type labelCauses struct {
	writer      *agentrun.Writer
	n           uint32
	tier        agentrun.TierE
	untrusted   bool
	tierBy      string
	untrustedBy string
}

// labelFacts reads the conversation's label. A conversation without a
// journal has its flags and the tier of its last request, and no causes.
func (inst *coordinator) labelFacts() (f labelFacts) {
	inst.mu.Lock()
	f.untrusted, f.confined, f.tier = inst.tainted, inst.confined, inst.curTier
	w, seen := inst.journal, inst.labelSeen
	inst.mu.Unlock()
	if w != nil {
		l := w.Run().Label
		f.tier = max(f.tier, l.Tier)
		f.sentTo, f.from = l.Exposure, l.Origin
	}
	if f.tier >= inst.confinedTier {
		f.confined = true
	} else if f.confined {
		f.tier = inst.confinedTier
	}
	if w == nil {
		return
	}
	if n := w.Len(); seen.writer != w || seen.n != n || seen.tier != f.tier || seen.untrusted != f.untrusted {
		seen = labelCauses{writer: w, n: n, tier: f.tier, untrusted: f.untrusted}
		seen.tierBy, seen.untrustedBy = inst.labelRaisedBy(w.Entries(), f.tier, f.untrusted)
		inst.mu.Lock()
		inst.labelSeen = seen
		inst.mu.Unlock()
	}
	f.tierBy, f.untrustedBy = seen.tierBy, seen.untrustedBy
	return
}

// labelRaisedBy names the first read that carried the tier, and the first
// that carried external content alone. A result that also holds a model's
// earlier words carries the model's level rather than the external one, so
// untrustedBy names a read that brought untrusted content, not always the
// earliest.
func (inst *coordinator) labelRaisedBy(entries []agentrun.Entry, tier agentrun.TierE, untrusted bool) (tierBy string, untrustedBy string) {
	wantTier, wantUntrusted := tier > agentrun.TierClear, untrusted
	if !wantTier && !wantUntrusted {
		return
	}
	intents := make(map[string]agentrun.Entry, 8)
	for _, e := range entries {
		if e.Kind == agentrun.KindIntent {
			intents[e.Key] = e
		}
	}
	for i := range entries {
		e := &entries[i]
		atTier := wantTier && tierBy == "" && e.Label.Tier == tier
		external := wantUntrusted && untrustedBy == "" && e.Label.Level == agentrun.LevelExternal
		if !atTier && !external {
			continue
		}
		what := inst.readNamed(*e, intents)
		if what == "" {
			continue
		}
		if atTier {
			tierBy = what
		}
		if external {
			untrustedBy = what
		}
		if (!wantTier || tierBy != "") && (!wantUntrusted || untrustedBy != "") {
			return
		}
	}
	return
}

// readNamed says what an entry brought into the conversation, as the steps
// name it: the operation and its window, the tool, or what was sent with a
// message; with the time it was written. Empty for an entry that is not a
// read.
func (inst *coordinator) readNamed(e agentrun.Entry, intents map[string]agentrun.Entry) (what string) {
	switch {
	case e.Kind == agentrun.KindUser && e.Form == formSent:
		what = "an image you sent"
	case e.Kind == agentrun.KindUser && e.Form == formPixels:
		what = "a window capture the model looked at"
	case e.Kind == agentrun.KindOutcome && e.Role == projection.RoleTool:
		in, ok := intents[e.Key]
		if !ok {
			what = "a tool call"
		} else if window, op, isOp := inst.windowOf(in); isOp {
			what = op + " in window " + strconv.FormatUint(window, 10)
		} else {
			what = in.Name
		}
	default:
		return ""
	}
	if !e.At.IsZero() {
		what += " at " + e.At.Local().Format("15:04")
	}
	return
}

// tierReach says who a tier lets read what carries it, as the predicate of
// a sentence about the label.
func tierReach(t agentrun.TierE) (reach string, known bool) {
	switch t {
	case agentrun.TierClear:
		return "lets it be shared with anyone", true
	case agentrun.TierGreen:
		return "lets it be shared within your community, not published", true
	case agentrun.TierAmber:
		return "keeps it to your organisation and those it works with, as they need it", true
	case agentrun.TierAmberStrict:
		return "keeps it to your organisation", true
	case agentrun.TierRed:
		return "keeps it to the people it is given to", true
	}
	return "", false
}

const (
	tipLabel         = "The conversation's traffic-light label (TLP): the highest of what it has read. It only rises. The host checks it before a model call goes out and before an export is written, and the artefact carries it when it is exported."
	labelConfined    = "Confined: later model calls go only to a local or trusted endpoint, and the artefact cannot be exported to the shared file store."
	labelNoUntrusted = "No untrusted content read."
	labelUntrusted   = "what the model writes since may have been steered by it."
	labelNotSent     = "Not sent to a remote provider."
)

// labelTierLine is the tier with who it lets read the conversation.
func labelTierLine(t agentrun.TierE) string {
	reach, known := tierReach(t)
	if !known {
		return t.String() + ": not a tier the chat knows."
	}
	return t.String() + ": " + reach + "."
}

// labelUntrustedLine says whether untrusted content was read, and by what.
func labelUntrustedLine(f labelFacts) string {
	switch {
	case !f.untrusted:
		return labelNoUntrusted
	case f.untrustedBy == "":
		return "Read untrusted content: " + labelUntrusted
	}
	return "Read untrusted content, from " + f.untrustedBy + ": " + labelUntrusted
}

// labelSentLines say where the conversation's content went and whose
// output it holds; the second only where it names another host.
func labelSentLines(f labelFacts) (lines []string) {
	if len(f.sentTo) == 0 {
		lines = append(lines, labelNotSent)
	} else {
		lines = append(lines, "Sent to: "+strings.Join(f.sentTo, ", ")+".")
	}
	var others []string
	for _, h := range f.from {
		if !containsStr(f.sentTo, h) {
			others = append(others, h)
		}
	}
	if len(others) > 0 {
		lines = append(lines, "Holds model output from: "+strings.Join(others, ", ")+".")
	}
	return
}

func containsStr(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// renderLabelFacts is the label's section of the inspector.
func (inst *App) renderLabelFacts() {
	f := inst.coord.labelFacts()
	sectionhead.New("Label").Tip(tipLabel).Send()
	c.Label(labelTierLine(f.tier)).Wrap().Send()
	if f.tierBy != "" {
		c.Label("Raised by " + f.tierBy + ".").Wrap().Send()
	}
	if f.confined {
		for range c.HoverText(tipConfined).KeepIter() {
			c.Label(labelConfined).Wrap().Send()
		}
	}
	for range c.HoverText(tipTainted).KeepIter() {
		c.Label(labelUntrustedLine(f)).Wrap().Send()
	}
	for _, l := range labelSentLines(f) {
		c.Label(l).Wrap().Send()
	}
}
