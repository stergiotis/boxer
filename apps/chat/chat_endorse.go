package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Labels at the chat's seams (ADR-0311): a body a model wrote carries its
// writer run and turn as metadata beside it, so the projection marks it
// in band when it re-enters (§SD4, laws L16 and L17); the artefact's text
// and a recalled result are where that happens here. An endorsement is
// the only lowering (§SD3, law L15), and it is a gesture of the person's:
// their decision in the dispatcher's dialog when a consequential call
// waited on them, recorded as the endorse entry before the call's outcome
// is; the Endorse button on the artefact's head. A call the task's grant
// admits without asking carries no endorse entry: the grant, journaled as
// the person's answer when the task was granted, is the consent in force.
// Never a pane closing. A grant the host gave in the person's place
// (ADR-0298) is journaled as the host's and consents to nothing under law
// L15.

// writerRef is who wrote a body that re-enters, and the label it carried
// in the journal when it is a recalled entry's.
type writerRef struct {
	run     uint64
	turn    string
	ordinal uint32
	label   agentrun.Label
}

// noteWriter marks the running call's result as a model's earlier output.
func (inst *coordinator) noteWriter(w writerRef) {
	inst.mu.Lock()
	inst.lastWriter = w
	inst.mu.Unlock()
}

// noteArtefactWriter marks the running call's result as the artefact's
// head when a model wrote it; a revision the person made or reverted to
// is theirs (level 0 by source).
func (inst *coordinator) noteArtefactWriter(art *artefact) {
	revs := art.revisions()
	if len(revs) == 0 {
		return
	}
	head := revs[len(revs)-1]
	if head.source != revSourceModel {
		return
	}
	// The model that wrote it is this conversation's own run.
	var run uint64
	if w := inst.journalNow(); w != nil {
		run = w.Run().Id
	}
	inst.noteWriter(writerRef{run: run, turn: head.turn})
}

// takeWriter is the running call's writer and who decided it, read once
// when the outcome is journaled.
func (inst *coordinator) takeWriter() (w writerRef, decided string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.lastWriter, inst.lastDecided
}

// noteDecided records who decided the running call.
func (inst *coordinator) noteDecided(who string) {
	inst.mu.Lock()
	inst.lastDecided = who
	inst.mu.Unlock()
}

// The deciders an endorsement or a grant names.
const (
	endorsedByPerson = "person"
	// grantedByHost is a task the host approved in the person's place
	// (ADR-0298): nobody was there to decide.
	grantedByHost = "host"
	// endorseKeyArtefact keys the artefact's endorsements.
	endorseKeyArtefact = "artefact"
	// grantForm is the form of the answered entry that records a task's
	// grant: the person's standing consent to the calls it admits
	// (ADR-0288 §SD4).
	grantForm = "grant"
)

// callCompleted says a call_operation result reports the command done.
func callCompleted(content string) bool {
	var co callOutcome
	return json.Unmarshal([]byte(content), &co) == nil && co.Phase == "completed"
}

// endorseEntry is the person's endorsement of a consequential call they
// decided in the dispatcher's dialog: the digest of the arguments as the
// call showed them — over their shredded attributes, so their spelling
// does not move it (ADR-0325 §SD4) — under the turn as the epoch.
func endorseEntry(key string, turn string, arguments string, effect string) (e agentrun.Entry, err error) {
	digest, err := agentrun.ArgumentsDigest(arguments)
	if err != nil {
		return
	}
	return agentrun.Entry{Kind: agentrun.KindEndorse, Key: key, Name: endorsedByPerson, Turn: turn, Label: harnessLabel(),
		Endorse: &agentrun.Endorsement{Digest: digest, Epoch: turn, Classes: []string{effect}, Previous: "none"}}, nil
}

// journalGrant records the task's grant as the person's answer once
// request_access succeeded: the consent the calls it admits proceed
// under (law L15). A widening is recorded again under the same handle.
func (inst *coordinator) journalGrant(ctx context.Context, turn string) (err error) {
	inst.mu.Lock()
	g := inst.grant
	inst.mu.Unlock()
	w := inst.journalNow()
	if w == nil || g.Handle == "" {
		return
	}
	rec := &agentrun.GrantRecord{Task: g.Task, Handle: g.Handle, Unattended: g.Unattended}
	if t := g.Terms; t != nil {
		rec.HasTerms, rec.CallsBudget, rec.CallsUsed = true, uint32(max(t.CallsBudget, 0)), uint32(max(t.CallsUsed, 0))
		rec.Deadline, rec.Destinations = t.Deadline, t.Destinations
	}
	by := endorsedByPerson
	if g.Unattended {
		by = grantedByHost
	}
	_, err = inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindAnswered, Key: g.Handle, Name: by, Form: grantForm, Grant: rec, Turn: turn, Label: harnessLabel()})
	return
}

// endorseArtefact journals the person's endorsement of the artefact's head
// for publishing: the digest of the text as shown, the revision as the
// epoch, and the change since the last endorsement.
func (inst *coordinator) endorseArtefact(ctx context.Context, n int, text string) (err error) {
	w := inst.journalNow()
	if w == nil {
		return eh.Errorf("no conversation yet: nothing to endorse in")
	}
	previous, prevText := "none", ""
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindEndorse && e.Key == endorseKeyArtefact {
			if e.Endorse != nil {
				previous = e.Endorse.Digest
			}
			if t, ok := inst.artefactOf().text(revisionOf(e)); ok {
				prevText = t
			}
		}
	}
	diff := "first endorsement"
	if previous != "none" {
		if c := changedLines(prevText, text); c.First > 0 {
			diff = "lines " + strconv.Itoa(c.First) + "-" + strconv.Itoa(c.Last) + " changed"
		} else {
			diff = "unchanged"
		}
	}
	rec := &agentrun.Endorsement{Digest: agentrun.ContentDigest(text), Epoch: "r" + strconv.Itoa(n), Classes: []string{"publish"}, Previous: previous, Diff: diff}
	_, err = inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindEndorse, Key: endorseKeyArtefact, Name: endorsedByPerson, Endorse: rec, Label: harnessLabel()})
	return
}

// revisionOf reads the revision an artefact endorsement named.
func revisionOf(e agentrun.Entry) int {
	if e.Endorse == nil || !strings.HasPrefix(e.Endorse.Epoch, "r") {
		return -1
	}
	n, _ := strconv.Atoi(e.Endorse.Epoch[1:])
	return n
}

// endorsedHead says whether the artefact's head carries a standing
// endorsement: the latest artefact endorsement names a revision the
// artefact holds with the text it endorsed, and no model has written a
// revision since. A person's own edit, or a revert they chose, is theirs
// (level 0 by source) and leaves the endorsement standing (ADR-0311 §SD3).
// The text is compared, not only the number, since a resumed artefact
// holds only what its export brought back.
func (inst *coordinator) endorsedHead() (endorsed bool) {
	w := inst.journalNow()
	art := inst.artefactOf()
	if w == nil || art == nil {
		return false
	}
	endorsedRev, digest := -1, ""
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindEndorse && e.Key == endorseKeyArtefact {
			endorsedRev = revisionOf(e)
			if e.Endorse != nil {
				digest = e.Endorse.Digest
			}
		}
	}
	if endorsedRev < 0 {
		return false
	}
	if t, ok := art.text(endorsedRev); !ok || agentrun.ContentDigest(t) != digest {
		return false
	}
	for _, r := range art.revisions() {
		if r.n > endorsedRev && r.source == revSourceModel {
			return false
		}
	}
	return true
}

// checkEndorsed is law L15 over a journal: every consequential intent
// whose outcome reports the command completed was consented to before
// the outcome was recorded — by an endorse entry under its key, written
// before the outcome, whose digest is the intent's arguments; or by the
// grant in force, the answered entry the person gave (never the host in
// their place) for the task the intent names, written before it. It
// returns a line per breach.
func checkEndorsed(entries []agentrun.Entry) (broken []string) {
	endorsements := make(map[string]agentrun.Entry)
	outcomes := make(map[string]agentrun.Entry)
	grants := make(map[string]uint32)
	for _, e := range entries {
		switch e.Kind {
		case agentrun.KindEndorse:
			if first, seen := endorsements[e.Key]; !seen || e.Ordinal < first.Ordinal {
				endorsements[e.Key] = e
			}
		case agentrun.KindOutcome:
			outcomes[e.Key] = e
		case agentrun.KindAnswered:
			if e.Form == grantForm && e.Name == endorsedByPerson {
				if at, seen := grants[e.Key]; !seen || e.Ordinal < at {
					grants[e.Key] = e.Ordinal
				}
			}
		}
	}
	for _, e := range entries {
		if e.Kind != agentrun.KindIntent || e.Effect != agentrun.EffectConsequential {
			continue
		}
		out, ok := outcomes[e.Key]
		if !ok || !callCompleted(out.Body) {
			continue
		}
		if en, endorsed := endorsements[e.Key]; endorsed && en.Ordinal < out.Ordinal {
			if digest, derr := agentrun.ArgumentsDigest(e.Arguments); derr == nil && en.Endorse != nil && en.Endorse.Digest == digest {
				continue
			}
			broken = append(broken, "intent "+e.Key+" ("+e.Name+"): L15: the endorsement before its outcome is not over the arguments the act ran with")
			continue
		}
		if at, granted := grants[e.Form]; granted && e.Form != "" && at < e.Ordinal {
			continue
		}
		broken = append(broken, "intent "+e.Key+" ("+e.Name+"): L15: a consequential act completed without an endorsement before its outcome or the person's grant for its task in force")
	}
	return
}
