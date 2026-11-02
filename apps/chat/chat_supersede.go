package chat

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
)

// Superseded results: a read of current state is out of date once the same
// read answers again, and a window tree once the step that used it is past.
// Their bodies stay in the journal; an edit replaces each in the projection
// with a stub a recall brings back. Edits rewrite a request's middle, which
// costs a provider's cached prefix, so they are written in batches, when
// what they free reaches the profile's SupersedeBatchBytes.

const (
	// formSupersede names the journal's edit of a superseded result.
	formSupersede = "superseded"
	// treeKeptReplies is how many model replies a window tree stays whole
	// for: the one that reads it and the one that acts on it.
	treeKeptReplies = 2
)

// supersedeKey is what a later identical read replaces, empty for a call
// that nothing supersedes; tree says the call read a window tree.
func (inst *coordinator) supersedeKey(in agentrun.Entry, p ProfileT) (key string, tree bool) {
	if in.Name == "read_window_tree" {
		return "", true
	}
	name := in.Name
	if window, op, isOp := inst.windowOf(in); isOp {
		if in.Effect != agentrun.EffectNone {
			return "", false
		}
		name = "w" + strconv.FormatUint(window, 10) + "/" + op
		if !slices.Contains(p.SupersededReads, op) {
			return "", false
		}
	} else if !slices.Contains(p.SupersededReads, name) {
		return "", false
	}
	var args map[string]any
	if json.Unmarshal([]byte(in.Arguments), &args) != nil {
		return "", false
	}
	// The model's words for the call are not what it reads.
	delete(args, "title")
	delete(args, "reason")
	b, err := json.Marshal(args, json.Deterministic(true))
	if err != nil {
		return "", false
	}
	return name + "\x00" + string(b), false
}

// supersededEdits are the edits a request's history is due: nil until what
// they free reaches the batch size.
func (inst *coordinator) supersededEdits(entries []agentrun.Entry, p ProfileT, turn string) (edits []agentrun.Entry, freed int) {
	if p.SupersedeBatchBytes <= 0 {
		return
	}
	intents := make(map[string]agentrun.Entry, 32)
	edited := make(map[uint32]bool, 8)
	replies := 0
	for _, e := range entries {
		switch e.Kind {
		case agentrun.KindIntent:
			intents[e.Key] = e
		case agentrun.KindEdit:
			edited[e.Target] = true
		case agentrun.KindReply:
			replies++
		}
	}
	latest := make(map[string]uint32, 16)
	seen := 0
	type candidate struct {
		e   *agentrun.Entry
		key string
	}
	var candidates []candidate
	for i := range entries {
		e := &entries[i]
		if e.Kind == agentrun.KindReply {
			seen++
		}
		if e.Kind != agentrun.KindOutcome || e.Role != projection.RoleTool {
			continue
		}
		in, ok := intents[e.Key]
		if !ok {
			continue
		}
		key, tree := inst.supersedeKey(in, p)
		switch {
		case tree:
			if replies-seen >= treeKeptReplies {
				candidates = append(candidates, candidate{e: e})
			}
		case key != "":
			latest[key] = e.Ordinal
			candidates = append(candidates, candidate{e: e, key: key})
		}
	}
	for _, c := range candidates {
		e := c.e
		if edited[e.Ordinal] || len(e.Body) < p.Compact.ElideMinBytes || (c.key != "" && latest[c.key] == e.Ordinal) {
			continue
		}
		stub := projection.Stub(projection.Message{Key: e.Key, Ordinal: e.Ordinal, Content: e.Body})
		edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: e.Ordinal, Form: formSupersede, Body: stub, Turn: turn, Label: harnessLabel()})
		freed += len(e.Body) - len(stub)
	}
	if freed < p.SupersedeBatchBytes {
		return nil, 0
	}
	return
}

// configureSupersede is the "supersede" element of the configure seam: it
// writes the batch of edits due, if one is, and projects again.
func configureSupersede(s *requestState) {
	if s.err != nil {
		return
	}
	s.coord.mu.Lock()
	p := s.coord.profile
	s.coord.mu.Unlock()
	edits, freed := s.coord.supersededEdits(s.w.Entries(), p, s.req.Turn)
	if len(edits) == 0 {
		return
	}
	if s.err = s.coord.applySuperseded(s.jctx, s, edits, freed); s.err != nil {
		return
	}
	tools := s.coord.tools(s.ctx)
	if s.err = s.coord.syncTools(s.jctx, s.w, tools); s.err != nil {
		return
	}
	if s.batch, s.msgs, s.digest, s.err = s.coord.project(s.jctx, s.w); s.err != nil {
		return
	}
	s.req.Messages, s.req.Tools = s.msgs, tools
	s.req.Tier = uint8(s.coord.requestLabel(s.batch).Tier)
	s.coord.setProjected(projection.Tokens(s.batch))
}

// applySuperseded journals the edits, offers recall from now on and tells it
// as a step of the turn.
func (inst *coordinator) applySuperseded(ctx context.Context, s *requestState, edits []agentrun.Entry, freed int) (err error) {
	if _, err = inst.append(ctx, s.w, edits...); err != nil {
		return
	}
	inst.mu.Lock()
	inst.compacted = true
	inst.mu.Unlock()
	inst.compactStep(s.round, "superseded results", strconv.Itoa(len(edits))+" results a later read replaced, "+humanBytes(int64(freed))+" freed; recall brings one back", false, false)
	return
}
