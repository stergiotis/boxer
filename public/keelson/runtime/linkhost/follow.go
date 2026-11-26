package linkhost

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// authorless decides a link followed with the person as the trigger
// (ADR-0409 §SD5): which operation it calls — the stage, for a run — and
// what the follow comes to, by the operation's effect and whether the
// window is new. A refusal says why in reason.
func authorless(spec app.OperationSpec, hasCall bool, fresh bool) (op string, decision string, reason string) {
	if !hasCall {
		return "", applink.DecisionReached, ""
	}
	if spec.Class != app.OperationClassCommand {
		return "", applink.DecisionRefused, "a link carries a command: a query's answer would have nowhere to go"
	}
	switch spec.Effect {
	case app.OperationEffectView:
		return spec.Name, applink.DecisionApplied, ""
	case app.OperationEffectDocument:
		if !fresh {
			return "", applink.DecisionRefused, "a link written by someone else changes only the view of an open window; this one would change what it holds"
		}
		return spec.Name, applink.DecisionApplied, ""
	case app.OperationEffectRun:
		if spec.LinkStage == "" {
			return "", applink.DecisionRefused, "a link written by someone else does not run work, and this operation names no stage that puts it in place"
		}
		if !fresh {
			return "", applink.DecisionRefused, "a link written by someone else stages work only in a new window"
		}
		return spec.LinkStage, applink.DecisionStaged, ""
	}
	return "", applink.DecisionRefused, "a link never carries a consequential command"
}

func (inst *Service) handleFollow(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	rep := applink.FollowReply{V: 1}
	req, err := buscodec.Decode[applink.FollowRequest](msg.Payload)
	if err != nil {
		rep.Decision, rep.Reason = applink.DecisionRefused, err.Error()
	} else {
		rep = inst.follow(context.WithoutCancel(msg.Context()), req, msg.Sender)
	}
	if err = buscodec.Reply(inst.busClient.Publish, msg.Reply, rep); err != nil {
		inst.log.Warn().Err(err).Msg("linkhost: reply")
	}
}

// follow follows one clicked link.
func (inst *Service) follow(ctx context.Context, req applink.FollowRequest, surface app.AppIdT) (rep applink.FollowReply) {
	rep.V = 1
	r, err := inst.resolve(req.Link, req.Author)
	f := Followed{Link: req.Link, Surface: surface, At: time.Now()}
	defer func() {
		f.Reply = rep
		if inst.cfg.Recorder != nil {
			inst.cfg.Recorder.Followed(ctx, f)
		}
	}()
	if err != nil {
		rep.Decision, rep.Reason = applink.DecisionRefused, err.Error()
		return
	}
	f.Record, f.Target = r.rec, r.appId
	inst.mu.Lock()
	inst.clicks++
	click := inst.clicks
	follower := inst.follower
	inst.mu.Unlock()
	var opened uint64
	if r.minted {
		if follower != nil {
			fl := follower.FollowLink(ctx, agent.LinkClick{Record: r.rec, Candidates: inst.topmost(r.appId), Surface: surface, Click: click})
			if fl.Authorised {
				if fl.Window != 0 {
					_ = inst.cfg.Host.OpsRaise(fl.Window)
				}
				rep.Window = fl.Window
				rep.Decision, rep.Reason = decisionOf(fl.Outcome, r.rec.Operation != "")
				rep.Note = noteOf(r, rep, false)
				return
			}
			rep.Reason = fl.Reason
			if fl.Opened {
				// The click opened a window for the task before the call
				// was refused: the link is followed there, as in the new
				// window it would otherwise open.
				opened = fl.Window
			}
		} else {
			rep.Reason = "this host runs no dispatcher"
		}
		rep.Authorless = true
	}
	inst.followAuthorless(ctx, r, surface, click, opened, &rep)
	rep.Note = noteOf(r, rep, rep.Authorless)
	return
}

// followAuthorless reaches the link's window and makes its call with the
// person as the trigger. opened, when set, is a window the click already
// opened for the link, which counts as the new window.
func (inst *Service) followAuthorless(ctx context.Context, r resolved, surface app.AppIdT, click uint64, opened uint64, rep *applink.FollowReply) {
	why := rep.Reason
	var key uint64
	switch {
	case opened != 0:
	case r.rec.Window != 0:
		if _, open := inst.instance(r.rec.Window); !open {
			rep.Decision, rep.Reason = applink.DecisionRefused, "the window the link names has closed"
			return
		}
		key = r.rec.Window
	case r.rec.Verb == applink.VerbRaise:
		if keys := inst.topmost(r.appId); len(keys) > 0 {
			key = keys[0]
		}
	}
	fresh := key == 0 || opened != 0
	op, decision, reason := authorless(r.spec, r.rec.Operation != "", fresh)
	if decision == applink.DecisionRefused {
		rep.Decision, rep.Reason = decision, joinReason(why, reason)
		return
	}
	if opened != 0 {
		key = opened
		if err := inst.cfg.Host.OpsRaise(key); err != nil {
			rep.Decision, rep.Reason = applink.DecisionRefused, joinReason(why, err.Error())
			return
		}
	} else if fresh {
		var err error
		if key, err = inst.cfg.Host.OpsOpen(r.appId, r.rec.Kind, r.rec.Config); err != nil {
			rep.Decision, rep.Reason = applink.DecisionRefused, joinReason(why, "the host did not open the window: "+err.Error())
			return
		}
		if !inst.waitLoaded(key) {
			rep.Window = key
			rep.Decision, rep.Reason = applink.DecisionRefused, joinReason(why, "the window did not load")
			return
		}
	} else if err := inst.cfg.Host.OpsRaise(key); err != nil {
		rep.Decision, rep.Reason = applink.DecisionRefused, joinReason(why, err.Error())
		return
	}
	rep.Window = key
	if op == "" {
		rep.Decision, rep.Reason = decision, why
		return
	}
	// The call expects no revision: it is the person's click and read
	// nothing (§SD5); the engine skips the check for a call with a link
	// origin and no task, as it does for a gesture.
	callId := "link-" + strconv.FormatUint(click, 10)
	creq := opwire.CallRequest{V: opwire.WireVersion, CallId: callId, Args: r.rec.Args,
		Writer: opwire.WriterPerson, Key: callId, Reason: "followed a link",
		Link: &app.LinkOrigin{Level: r.author.Level, Tier: r.author.Tier, Surface: surface, Digest: r.rec.Digest()}}
	out, ok := inst.cfg.Host.OpsSubmit(ctx, key, op, creq)
	if !ok {
		rep.Decision, rep.Reason = applink.DecisionRefused, joinReason(why, "the window serves no operations")
		return
	}
	deadline := time.Now().Add(applySettle)
	for !out.Phase.Final() && out.Phase != opwire.PhaseApplied && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if st, sok := inst.cfg.Host.OpsStatus(key, callId); sok {
			out = st
		}
	}
	d, reason := decisionOf(out, true)
	if d == applink.DecisionApplied {
		d = decision
	}
	rep.Decision, rep.Reason = d, joinReason(why, reason)
}

// decisionOf is what a call's outcome comes to as a follow's decision.
func decisionOf(out opwire.Outcome, hasCall bool) (decision string, reason string) {
	if !hasCall {
		return applink.DecisionReached, ""
	}
	switch out.Phase {
	case opwire.PhaseApplied, opwire.PhaseRendered, opwire.PhaseAccepted, opwire.PhaseRunning, opwire.PhaseCompleted:
		return applink.DecisionApplied, ""
	case opwire.PhaseProposed:
		return applink.DecisionProposed, out.Reason
	case opwire.PhaseConflict:
		return applink.DecisionConflict, out.Reason
	}
	reason = out.Reason
	if reason == "" {
		reason = "the call ended " + out.Phase.String()
	}
	return applink.DecisionRefused, reason
}

func joinReason(a string, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}

// noteOf is the line a surface shows on its action line.
func noteOf(r resolved, rep applink.FollowReply, authorless bool) (s string) {
	name := r.m.Display
	if name == "" {
		name = r.rec.App
	}
	switch rep.Decision {
	case applink.DecisionReached:
		s = "opened " + name
	case applink.DecisionApplied:
		s = name + ": " + r.rec.Operation + " applied"
	case applink.DecisionStaged:
		s = name + ": " + r.rec.Operation + " put in place; run it yourself"
	case applink.DecisionProposed:
		s = name + ": " + r.rec.Operation + " waits for you to accept it"
	case applink.DecisionConflict:
		s = name + ": " + r.rec.Operation + " not applied, the window changed since the link was made"
	default:
		s = "the link was not followed: " + rep.Reason
	}
	if authorless && rep.Decision != applink.DecisionRefused {
		s += " (the task's grant no longer covers it: followed as your own click)"
	}
	return
}

func (inst *Service) handleDescribe(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	var rep applink.DescribeReply
	req, err := buscodec.Decode[applink.DescribeRequest](msg.Payload)
	if err != nil {
		rep.V, rep.Refused = 1, err.Error()
	} else {
		rep = inst.Describe(req.Link, req.Author)
	}
	if err = buscodec.Reply(inst.busClient.Publish, msg.Reply, rep); err != nil {
		inst.log.Warn().Err(err).Msg("linkhost: reply")
	}
}

// Describe is the host's reading of a link: what its hover shows, and what
// a click would do now. It acts on nothing.
func (inst *Service) Describe(link string, author applink.Author) (rep applink.DescribeReply) {
	rep.V = 1
	r, err := inst.resolve(link, author)
	if err != nil {
		rep.Refused = err.Error()
		return
	}
	rep.App, rep.Display, rep.Operation = r.rec.App, r.m.Display, r.rec.Operation
	if rep.Display == "" {
		rep.Display = r.rec.App
	}
	if r.rec.Operation != "" {
		rep.Summary, rep.Effect = r.spec.Summary, r.spec.Effect.String()
		rep.Args = argsText(r.spec, r.rec.Args)
	}
	rep.Author = levelName(r.author.Level)
	switch {
	case r.rec.Window != 0:
		rep.Target = "window " + strconv.FormatUint(r.rec.Window, 10)
	case r.rec.Verb == applink.VerbRaise:
		rep.Target = "the open " + rep.Display + " window in front, or a new one"
	default:
		rep.Target = "a new " + rep.Display + " window"
	}
	if r.minted {
		rep.Task, rep.Conversation = r.rec.Task, r.rec.Conversation
		inst.mu.Lock()
		follower := inst.follower
		inst.mu.Unlock()
		if follower != nil && follower.LinkLive(r.rec.Task, r.rec.Epoch) {
			rep.Would = "runs under the task's grant, checked again at the click"
			return
		}
		rep.Would = "the task has ended: followed as your own click — "
	}
	fresh := r.rec.Window == 0 && (r.rec.Verb == applink.VerbOpen || len(inst.topmost(r.appId)) == 0)
	_, decision, reason := authorless(r.spec, r.rec.Operation != "", fresh)
	switch decision {
	case applink.DecisionRefused:
		rep.Refused = reason
	case applink.DecisionStaged:
		rep.Would += "puts the work in place for you to run"
	case applink.DecisionReached:
		rep.Would += "opens the window"
	default:
		rep.Would += "applies it"
	}
	return
}
