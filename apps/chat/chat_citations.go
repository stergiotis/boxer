package chat

// What the chat shows of a cited reply (ADR-0340 §SD6): each
// reference [a1] in a reply's text carries its verdict's mark, and a
// cited reply ends with its citations — the claim, the outcome and what
// was observed — in the transcript, the export and the artefact's
// hand-off. A copy or an export of text that cites a claim that did not
// hold, or that refers to no verdict, is held once and sent on a second
// gesture: the guard's Ask, in the chat's own form.

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
)

// citationMark is one verdict as the readers show it.
type citationMark struct {
	id, outcome, says, observed, statement, reason string
}

// marksOf are the turn's verdicts as marks, the latest per assertion.
func marksOf(entries []agentrun.Entry, turn string) (marks []citationMark) {
	for _, v := range projection.Verdicts(entries, turn) {
		marks = append(marks, markOf(v))
	}
	return
}

func markOf(v agentrun.Entry) (m citationMark) {
	m = citationMark{id: v.Name, outcome: v.Verdict.Outcome, observed: v.Body, reason: v.Verdict.Reason}
	rows := agentrun.VerdictRows([]agentrun.Entry{v})
	if len(rows) == 1 {
		m.says, m.statement = rows[0].Says, rows[0].Statement
	}
	return
}

// sign is the mark a verdict's outcome shows beside its reference.
func (inst citationMark) sign() string {
	switch inst.outcome {
	case projection.VerdictHeld:
		return "✓"
	case projection.VerdictFailed:
		return "✗"
	case projection.VerdictMoved:
		return "~"
	}
	return "?"
}

// markReferences is text with each reference outside code and links
// marked with its verdict, and each reference to none marked unknown.
func markReferences(text string, marks []citationMark) string {
	byId := make(map[string]citationMark, len(marks))
	for _, m := range marks {
		byId[m.id] = m
	}
	return projection.MarkReferences(text, func(id string) string {
		sign := "?"
		if m, ok := byId[id]; ok {
			sign = m.sign()
		}
		return "[" + id + " " + sign + "]"
	})
}

// citationLines are the citations a text's references name, one line
// each, for the foot of a reply or an artefact; empty without references.
func citationLines(text string, marks []citationMark) (lines []string) {
	refs := projection.References(text)
	if len(refs) == 0 {
		return
	}
	byId := make(map[string]citationMark, len(marks))
	for _, m := range marks {
		byId[m.id] = m
	}
	for _, id := range refs {
		m, ok := byId[id]
		if !ok {
			lines = append(lines, "["+id+"] ? no verdict of this turn names it")
			continue
		}
		line := "[" + id + "] " + m.sign() + " " + m.outcome
		if m.says != "" {
			line += " · " + oneLine(m.says)
		}
		if m.observed != "" {
			line += " · observed " + oneLine(m.observed)
		}
		if m.reason != "" && m.outcome != projection.VerdictHeld {
			line += " · " + oneLine(m.reason)
		}
		lines = append(lines, line)
	}
	return
}

// citationFoot is the citations as markdown under a text: a rule and one
// line each; empty without references.
func citationFoot(text string, marks []citationMark) string {
	lines := citationLines(text, marks)
	if len(lines) == 0 {
		return ""
	}
	return "\n\n---\n*Citations*\n- " + strings.Join(lines, "\n- ") + "\n"
}

// shown is the entry's text as the readers show it: a reply with its
// references marked and its citations at the foot. It is computed once
// per text and set of verdicts, since the transcript reads it every
// frame; setCites forgets it.
func (inst *entry) shown() string {
	if inst.shownText != "" {
		return inst.shownText
	}
	inst.shownText = inst.text
	if inst.speaker == speakerModel && len(projection.References(inst.text)) > 0 {
		inst.shownText = markReferences(inst.text, inst.cites) + citationFoot(inst.text, inst.cites)
	}
	return inst.shownText
}

// setCites sets a reply's verdicts and forgets what was shown.
func (inst *entry) setCites(marks []citationMark) {
	inst.cites, inst.shownText = marks, ""
}

// citationIndex is every verdict the conversation shows, by assertion id,
// the latest turn's standing where two turns reused an id: what the
// artefact and the copy guard resolve references against.
func (inst *conversation) citationIndex() (marks []citationMark) {
	seen := map[string]int{}
	for _, e := range inst.entries {
		for _, m := range e.cites {
			if i, ok := seen[m.id]; ok {
				marks[i] = m
				continue
			}
			seen[m.id] = len(marks)
			marks = append(marks, m)
		}
	}
	return
}

// citationHold is why text should not leave without a second look: it
// refers to a claim that did not hold, or to no verdict at all (ADR-0340
// §SD6's guard); empty when it may go.
func citationHold(text string, marks []citationMark) (why string) {
	byId := make(map[string]citationMark, len(marks))
	for _, m := range marks {
		byId[m.id] = m
	}
	var failed, unknown []string
	for _, id := range projection.References(text) {
		m, ok := byId[id]
		switch {
		case !ok:
			unknown = append(unknown, "["+id+"]")
		case m.outcome == projection.VerdictFailed:
			failed = append(failed, "["+id+"]")
		}
	}
	switch {
	case len(failed) > 0 && len(unknown) > 0:
		return "it cites a claim that did not hold (" + strings.Join(failed, ", ") + ") and refers to no verdict for " + strings.Join(unknown, ", ")
	case len(failed) > 0:
		return "it cites a claim that did not hold (" + strings.Join(failed, ", ") + ")"
	case len(unknown) > 0:
		return "it refers to no verdict for " + strings.Join(unknown, ", ")
	}
	return ""
}

// heldByCitations is the guard's Ask on text about to leave the chat: the
// first gesture is held with the reason shown, the next within the same
// conversation sends it. what names the gesture in the note.
func (inst *App) heldByCitations(what string, text string) (held bool) {
	why := citationHold(text, inst.conv.citationIndex())
	if why == "" {
		return false
	}
	key := what + "\x00" + agentrun.ContentDigest(text)
	if inst.citeConfirmed == nil {
		inst.citeConfirmed = map[string]bool{}
	}
	if inst.citeConfirmed[key] {
		delete(inst.citeConfirmed, key)
		return false
	}
	inst.citeConfirmed[key] = true
	inst.setNote("held: "+why+"; "+what+" again to send it anyway", true)
	return true
}
