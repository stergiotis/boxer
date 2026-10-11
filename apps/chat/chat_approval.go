package chat

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// An exit above the route's clearance (ADR-0312 §SD3): the model route
// refuses the request and says the person may approve it. The chat asks
// them, as ask_user does, with the tier and the route named, and journals
// the question and their answer; a yes with their justification is
// journaled as an approval entry naming the refused call, before the
// request is sent again under the entry's key (cell.ApprovalKey: the run
// and the ordinal), which the route verifies against the entry and
// records on the call's row, so law L33 joins the two by (run, ordinal).
// A no leaves the refusal as the turn's failure. The person is asked once
// per turn for one sink and tier: a later round's exit of the same kind
// is approved under their standing answer, with an approval entry of its
// own naming that round's refused call, since the route binds every
// approval to one exit.

// The person's choices.
const (
	approveSend = "Send it"
	approveKeep = "Do not send"
	// approvalAsks is how many times the person is asked before a yes
	// without a justification counts as a no.
	approvalAsks = 3
)

// exitQuestion is what the asked entry of an exit carries.
type exitQuestion struct {
	Question string `json:"question"`
	Sink     string `json:"sink"`
	Tier     string `json:"tier"`
	Path     string `json:"path"`
}

// exitAnswer is what the answered entry of an exit carries: the person's
// standing answer for the sink and tier in the turn.
type exitAnswer struct {
	Sink          string `json:"sink"`
	Tier          string `json:"tier"`
	Approved      bool   `json:"approved"`
	Justification string `json:"justification,omitempty"`
}

// complete runs one model call, asking the person once when the route
// wants their approval of the exit.
func (inst *coordinator) complete(ctx context.Context, cli completerI, w *agentrun.Writer, req llm.Request) (res llm.Response, err error) {
	res, err = cli.Complete(ctx, req)
	var refused *llm.RefusedError
	if err == nil || !errors.As(err, &refused) || !refused.NeedsApproval() || w == nil {
		return
	}
	key, approved, aerr := inst.approveExit(ctx, w, req.Turn, refused)
	if aerr != nil {
		return res, aerr
	}
	if !approved {
		return
	}
	req.Approval = key
	return cli.Complete(ctx, req)
}

// approveExit journals the person's decision on the exit — asking them
// unless they answered for this sink and tier in this turn — and, when
// they approved, the approval entry; key is that entry's.
func (inst *coordinator) approveExit(ctx context.Context, w *agentrun.Writer, turn string, refused *llm.RefusedError) (key string, approved bool, err error) {
	answer, answered := standingAnswer(w.Entries(), turn, refused.Sink, refused.Tier)
	if !answered {
		if answer, err = inst.askExit(ctx, w, turn, refused); err != nil {
			return
		}
	}
	if !answer.Approved {
		return
	}
	channel, edge, _ := strings.Cut(refused.Sink, " ")
	// The digest of the bytes the route refused goes on the entry, so a
	// reader sees which request the person approved; the route binds the
	// resend to those bytes by its own digest.
	body, err := cell.Approval{Channel: channel, Edge: edge, Tier: refused.Tier, Path: refused.ApprovalPath, Approver: endorsedByPerson, Justification: answer.Justification, At: time.Now().UTC(), Digest: refused.Digest}.Body()
	if err != nil {
		return "", false, eh.Errorf("approval: %w", err)
	}
	written, err := inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindApproval, Key: refused.CallId, Name: refused.Sink, Body: body, Turn: turn, Label: harnessLabel()})
	if err != nil {
		return "", false, err
	}
	return cell.ApprovalKey(w.Run().Id, written[0].Ordinal), true, nil
}

// standingAnswer is the person's answer in the turn for the sink and
// tier, read from the journal.
func standingAnswer(entries []agentrun.Entry, turn string, sink string, tier string) (a exitAnswer, found bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != agentrun.KindAnswered || e.Form != answerFormExit || e.Turn != turn {
			continue
		}
		var candidate exitAnswer
		if json.Unmarshal([]byte(e.Body), &candidate) == nil && candidate.Sink == sink && candidate.Tier == tier {
			return candidate, true
		}
	}
	return
}

// The forms of the asked and answered entries of an exit.
const (
	askFormExit    = "exit"
	answerFormExit = "exit"
)

// askExit asks the person about the exit and journals the question and
// their answer. A yes needs a justification: without one they are asked
// again, up to approvalAsks times, after which it is a no.
func (inst *coordinator) askExit(ctx context.Context, w *agentrun.Writer, turn string, refused *llm.RefusedError) (a exitAnswer, err error) {
	inst.setStage(stagePerson)
	defer inst.setStage(stageModel)
	a = exitAnswer{Sink: refused.Sink, Tier: refused.Tier}
	question := "The model route refused this request: " + refused.Reason + ". Send " + refused.Tier + " content to " + refused.Sink + " anyway? Say why in the note."
	asked, err := jsonMarshal(exitQuestion{Question: question, Sink: refused.Sink, Tier: refused.Tier, Path: refused.ApprovalPath})
	if err != nil {
		return a, eh.Errorf("exit question: %w", err)
	}
	if _, err = inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindAsked, Key: refused.CallId, Name: "exit", Form: askFormExit, Body: asked, Turn: turn, Label: harnessLabel()}); err != nil {
		return
	}
	for attempt := range approvalAsks {
		q := askQuestion{Header: "Exit", Question: question,
			Options: []askOption{{Label: approveSend, Description: "the request leaves the cell under your recorded justification"}, {Label: approveKeep, Description: "the turn fails with the refusal"}}}
		if attempt > 0 {
			q.Question = "A justification is required to send it. " + question
		}
		r, aerr := inst.ask.ask(ctx, []askQuestion{q})
		if aerr != nil {
			return a, aerr
		}
		a.Approved, a.Justification = false, ""
		if r.Skipped || len(r.Answers) == 0 {
			break
		}
		ans := r.Answers[0]
		a.Justification = strings.TrimSpace(ans.Other)
		for _, c := range ans.Chosen {
			if c.Label == approveSend {
				a.Approved = true
			}
			if c.Note != "" {
				a.Justification = strings.TrimSpace(c.Note)
			}
		}
		if !a.Approved || a.Justification != "" {
			break
		}
		a.Approved = false
	}
	answered, err := jsonMarshal(a)
	if err != nil {
		return a, eh.Errorf("exit answer: %w", err)
	}
	_, err = inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindAnswered, Key: refused.CallId, Name: endorsedByPerson, Form: answerFormExit, Body: answered, Turn: turn, Label: harnessLabel()})
	return
}
