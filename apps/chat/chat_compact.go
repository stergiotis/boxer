package chat

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Compaction (ADR-0316): the plan is the projection's reading of the
// journal (projection.PlanCompaction); the act, here, writes the compact
// entry — derived from the span alone when an elision brings the request
// under the trigger, from the summariser's answer when it does not; the
// apply pass is the fold's. The summariser request extends the turn's on
// the turn's route (§SD3), so it reads the cached prefix; its answer is
// model output re-entering, labelled as the reply would be, and the
// person's messages ride beside it verbatim up to the pin budget (law
// L25). The constants are the profile's; the trigger is a share of the
// route's context, re-checked for thrash against the route's real context
// when it is below the floor the profile was validated at. What the plan
// cannot do is recorded in the journal rather than worked around: a
// summariser that failed leaves no elision the plan judged too small
// (law L24), and a request no cut brings under the trigger goes out as it
// is: a provider that refuses it surfaces as the turn's failure.

const (
	// compactPurpose names the summariser call in the call record, under
	// the prompt book's `chat/<slug>` convention.
	compactPurpose = promptBookId + "/summariser"
	// compactNoteName names the journal's record of a compaction the plan
	// could not make: an ignorable entry, kept out of the projection.
	compactNoteName = "compaction"
	// recallBudget bounds what a recall returns.
	recallBudget = 64 << 10
)

// compactConstants are the run's profile's constants under this route:
// the trigger is the route's context times the profile's share; a route
// whose context is unknown never compacts, as its context bar never
// warns, and so does a coordinator with no run open. A route whose
// context is below the floor the profile was validated at is checked for
// thrash again with the head as projected, and compaction is off for it
// when the check fails.
func (inst *coordinator) compactConstants(headTokens int64) (c projection.Constants) {
	inst.mu.Lock()
	p, size := inst.profile, inst.route.context
	inst.mu.Unlock()
	if size <= 0 || p.Id == "" {
		return
	}
	c = p.Compact
	c.TriggerTokens = int64(size) * int64(p.CompactTriggerPercent) / 100
	if int64(size) < p.CompactContextFloor {
		if err := c.Validate(headTokens); err != nil {
			if _, logged := compactOff.LoadOrStore(inst.conversation, true); !logged {
				log.Warn().Err(err).Str("conversation", inst.conversation).Int32("context", size).Int64("floor", p.CompactContextFloor).Msg("chat: the route's context is below the profile's floor and the constants would thrash under it; compaction is off for this conversation")
			}
			return projection.Constants{}
		}
	}
	return
}

// compactOff holds the conversations told once that compaction is off.
var compactOff sync.Map

// headTokens is the size of the batch's head: what the thrash check is
// run against.
func headTokens(b projection.Batch) int64 {
	var head projection.Batch
	for _, m := range b.Messages {
		if m.Part == projection.PartHead {
			head.Messages = append(head.Messages, m)
		}
	}
	return projection.Tokens(head)
}

// configureCompaction is the "compact" element of the configure seam: it
// runs the plan over the journal after the round's projection and, when
// the plan says so, the act, then projects again.
func configureCompaction(s *requestState) {
	if s.err != nil {
		return
	}
	c := s.coord.compactConstants(headTokens(s.batch))
	if c.TriggerTokens <= 0 {
		return
	}
	entries := s.w.Entries()
	reported, at := lastReported(entries)
	plan := projection.PlanCompaction(entries, sectionOrder(), c, reported, at)
	switch plan.Verdict {
	case projection.VerdictNone:
		return
	case projection.VerdictRefuse:
		// Nothing outside the kept turns brings the request under the
		// trigger: the request goes out as it is, and the journal says so
		// once a turn.
		if !compactNoted(entries, s.req.Turn, compactNoteRefused) {
			log.Warn().Str("conversation", s.coord.conversation).Int64("estimated", plan.Estimated).Int64("trigger", c.TriggerTokens).Msg("chat: the request is over the trigger and no cut outside the kept turns fits")
			s.err = s.coord.noteCompaction(s.jctx, s.w, s.req.Turn, compactNoteRefused, "the request is "+strconv.FormatInt(plan.Estimated, 10)+" tokens, over the trigger of "+strconv.FormatInt(c.TriggerTokens, 10)+", and no cut outside the kept turns brings it under")
		}
		return
	case projection.VerdictSummarise:
		if compactNoted(entries, s.req.Turn, compactNoteFailed) {
			// The summariser failed this turn already; it is not asked
			// again until the next.
			return
		}
	}
	if s.err = s.coord.compact(s, plan, c); s.err != nil {
		return
	}
	// The tool set grew by recall; declare it, then project again.
	tools := s.coord.tools(s.ctx)
	if s.err = s.coord.syncTools(s.jctx, s.w, tools); s.err != nil {
		return
	}
	if s.batch, s.msgs, s.digest, s.err = s.coord.project(s.jctx, s.w); s.err != nil {
		return
	}
	s.req.Messages, s.req.Tools = s.msgs, tools
	s.req.Tier = uint8(s.coord.requestLabel(s.batch).Tier)
}

// The forms of the journal's records of a compaction not made.
const (
	compactNoteRefused = "refused"
	compactNoteFailed  = "summariser-failed"
)

// compactNoted says the turn carries a record of the form already.
func compactNoted(entries []agentrun.Entry, turn string, form string) bool {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind == agentrun.KindIgnorable && e.Name == compactNoteName && e.Form == form && e.Turn == turn {
			return true
		}
	}
	return false
}

// noteCompaction records in the journal, outside the projection, that a
// compaction was not made and why: the request went out over the trigger
// (law L24 does not hold for it).
func (inst *coordinator) noteCompaction(ctx context.Context, w *agentrun.Writer, turn string, form string, why string) (err error) {
	_, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: compactNoteName, Form: form, Body: why, Turn: turn, Label: harnessLabel()})
	return
}

// lastReported is the provider's prompt tokens for the last answered
// request and the ordinal of its reply, zero where none reported any.
func lastReported(entries []agentrun.Entry) (tokens int64, at uint32) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != agentrun.KindReply {
			continue
		}
		var meta replyBody
		if json.Unmarshal([]byte(e.Name), &meta) == nil && meta.InputTokens > 0 {
			return int64(meta.InputTokens), e.Ordinal
		}
		return
	}
	return
}

// compactIntent is what the summariser's intent entry carries.
type compactIntent struct {
	Cut  uint32 `json:"cut"`
	From uint32 `json:"from"`
	Span int    `json:"span"`
}

// compact is the act: the compact entry, after the summariser's intent,
// request and outcome when the plan says to summarise. The summariser
// reads the request the round projected, so its call carries that
// request's label (laws L3 and L6): the intent and the request entry
// carry it, the outcome and the compact entry the reply's over it. A
// summariser that fails leaves the journal as it was but for the intent,
// the request, the outcome recording the failure, and the record that the
// turn went out over the trigger: the elision the plan judged too small is
// not written in its place, and the summariser is not asked again this
// turn. After a cut the snapshots the model was told before it — the
// settings' ceiling, the artefact's state — are told again, since the cut
// took them; the notices — the changes others made, the pixels — are not
// composed again, since composing them is what takes them (ADR-0269
// §SD8).
func (inst *coordinator) compact(s *requestState, plan projection.Plan, c projection.Constants) (err error) {
	ctx, jctx, cli, w, req, msgs := s.ctx, s.jctx, s.cli, s.w, s.req, s.msgs
	entry := agentrun.Entry{Kind: agentrun.KindCompact, Run: w.Run().Id, Target: plan.Cut, From: plan.From, Turn: req.Turn, Form: projection.FormElide, Body: plan.Elided, Label: plan.Label}
	if plan.Verdict == projection.VerdictSummarise {
		if cli == nil {
			return inst.noteCompaction(jctx, w, req.Turn, compactNoteFailed, "no model to summarise with")
		}
		key := "compact#" + strconv.FormatUint(uint64(plan.Cut), 10)
		rl := inst.requestLabel(s.batch)
		intent, _ := jsonMarshal(compactIntent{Cut: plan.Cut, From: plan.From, Span: len(plan.Span)})
		sreq := req
		sreq.Purpose, sreq.Tools, sreq.ToolChoice, sreq.MaxTokens = compactPurpose, nil, "", int32(c.SummaryTokens)
		sreq.Messages = append(slices.Clip(msgs), openaichat.Message{Role: openaichat.ChatRoleUser, Content: summariserNote + "\n\nSummarise everything before the person's message that begins: " + strconv.Quote(clip(plan.CutText, 120))})
		request, _ := jsonMarshal(requestBody{Digest: s.digest, Messages: len(sreq.Messages), Round: s.round, Label: rl.String()})
		if _, err = inst.append(jctx, w,
			agentrun.Entry{Kind: agentrun.KindIntent, Key: key, Name: "summarise", Effect: agentrun.EffectNone, Body: intent, Turn: req.Turn, Label: rl},
			agentrun.Entry{Kind: agentrun.KindRequest, Key: key, Body: request, Turn: req.Turn, Label: rl}); err != nil {
			return
		}
		res, cerr := cli.Complete(ctx, sreq)
		outcome := agentrun.Entry{Kind: agentrun.KindOutcome, Key: key, Name: res.CallId, Turn: req.Turn, Label: inst.replyLabel(rl), WriterRun: w.Run().Id, WriterTurn: req.Turn}
		if cerr != nil {
			outcome.Body = "error: " + cerr.Error()
			log.Warn().Err(cerr).Str("conversation", inst.conversation).Msg("chat: the summariser failed; the request goes out over the trigger this turn")
			if _, err = inst.append(jctx, w, outcome); err != nil {
				return
			}
			return inst.noteCompaction(jctx, w, req.Turn, compactNoteFailed, "the summariser failed: "+cerr.Error()+"; the request stays over the trigger")
		}
		outcome.Body, _ = jsonMarshal(replyBody{CallId: res.CallId, FinishReason: res.FinishReason, InputTokens: res.InputTokens, OutputTokens: res.OutputTokens})
		entry.Form, entry.Key = projection.FormSummary, res.CallId
		entry.Body = res.Content + projection.PersonVerbatim(plan.Person, c.PinBytes)
		entry.Label = plan.Label.Join(inst.replyLabel(rl))
		entry.WriterRun, entry.WriterTurn = w.Run().Id, req.Turn
		if _, err = inst.append(jctx, w, outcome); err != nil {
			return
		}
	}
	if _, err = inst.append(jctx, w, entry); err != nil {
		return
	}
	inst.mu.Lock()
	inst.compacted = true
	// The snapshots are news again: the cut took the notes that carried them.
	inst.toldOnce, inst.artToldN = false, false
	inst.mu.Unlock()
	for _, src := range contextSources {
		if src.form != contextFormSnapshot {
			continue
		}
		text := src.compose(ctx, inst, req.Turn)
		if text == "" {
			continue
		}
		if _, err = inst.append(jctx, w, agentrun.Entry{Kind: agentrun.KindNote, Name: src.name, Form: src.form.String(), Body: text, Turn: req.Turn, Label: harnessLabel()}); err != nil {
			return
		}
	}
	return
}

// recall is the read-only act that brings an elided tool result back from
// the journal by its ordinal (ADR-0316 §SD2); the result re-enters under
// the label the journal holds it at, joined with the coordinator's, and
// marked with its writer when a model wrote it.
func (inst *coordinator) recall(spec *toolSpec, args map[string]any) (content string, activity string) {
	w := inst.journalNow()
	n, _ := args["ordinal"].(float64)
	ordinal := uint32(n)
	if w == nil || ordinal == 0 {
		return "error: ordinal names a stub's ordinal", "recall: no ordinal"
	}
	for _, e := range w.Entries() {
		if e.Ordinal != ordinal {
			continue
		}
		if e.Kind != agentrun.KindOutcome {
			return "error: ordinal " + strconv.FormatUint(uint64(ordinal), 10) + " is not a tool result", "recall: not a result"
		}
		content = e.Body
		if spec.budget > 0 && len(content) > spec.budget {
			content = cutRunes(content, spec.budget) + "\n… cut at " + strconv.Itoa(spec.budget) + " bytes"
		}
		if e.Label.Level >= agentrun.LevelExternal {
			inst.markTainted()
		}
		inst.noteWriter(writerRef{run: e.WriterRun, turn: e.WriterTurn, ordinal: e.WriterOrdinal, label: e.Label})
		return content, "recalled the result at ordinal " + strconv.FormatUint(uint64(ordinal), 10)
	}
	return "error: no entry at ordinal " + strconv.FormatUint(uint64(ordinal), 10), "recall: no such ordinal"
}

// cutRunes is s cut to at most n bytes on a rune boundary.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
