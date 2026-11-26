package agent

import (
	"context"
	"reflect"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Links a task mints and the person follows (ADR-0409 (proposed) §SD6): a
// task asks for a link through the dispatcher, which checks the call it
// describes as it would check the call and does not run it; the host's
// link service keeps the record and hands back a handle. A click runs the
// recorded call under the task's grant, checked again, or reports that the
// grant no longer authorises it, and the link service then follows it as
// an authorless link.

// SubjectLink is the service a coordinator mints a link on.
const SubjectLink = SubjectPrefix + "link"

// LinkMinterI keeps the records of the links tasks mint: the host's link
// service.
type LinkMinterI interface {
	MintTask(rec applink.Record) (link string, err error)
}

// SetLinks installs the service that keeps minted links; without one,
// make_link is refused.
func (inst *Service) SetLinks(m LinkMinterI) {
	inst.mu.Lock()
	inst.links = m
	inst.mu.Unlock()
}

// The targets a minted link names.
const (
	LinkTargetOpen   = "open"
	LinkTargetRaise  = "raise"
	LinkTargetWindow = "window"
)

type wireMakeLink struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
	Key    string `json:"key,omitempty"`
	// Target is open, raise or window; App names the app for open and
	// raise, Window the window for window.
	Target string `json:"target"`
	App    string `json:"app,omitempty"`
	Window uint64 `json:"window,omitempty"`
	// Operation and Args are the call, Args as the model's JSON; an empty
	// Operation is a link that only reaches the window.
	Operation string `json:"operation,omitempty"`
	Args      string `json:"args,omitempty"`
	Turn      string `json:"turn,omitempty"`
	ModelCall string `json:"model_call,omitempty"`
	ToolCall  string `json:"tool_call,omitempty"`
	ToolIndex uint32 `json:"tool_index,omitempty"`
	Tier      uint8  `json:"tier,omitempty"`
}

type wireMakeLinkReply struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	Link   string `json:"link,omitempty"`
}

// MakeLinkRequest is a coordinator's request for a link: the call it
// would make, and the target the link names.
type MakeLinkRequest struct {
	CallRequest
	Target string
	App    string
	Window uint64
}

// MakeLink asks the dispatcher to check a call without running it and the
// host to mint a link that runs it when the person follows it.
func (inst *Client) MakeLink(ctx context.Context, r MakeLinkRequest) (link string, err error) {
	rep, err := roundTrip[wireMakeLink, wireMakeLinkReply](ctx, inst, SubjectLink,
		wireMakeLink{V: wireVersion, Handle: r.Handle, Key: r.Key, Target: r.Target, App: r.App, Window: r.Window,
			Operation: r.Operation, Args: r.Args, Turn: r.Turn, ModelCall: r.ModelCall, ToolCall: r.ToolCall, ToolIndex: r.ToolIndex, Tier: r.Tier})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	link = rep.Link
	return
}

// mintedDecision is the decision a minting's action row carries
// (ADR-0409 §SD9).
const mintedDecision = "minted"

// makeLink checks a link's call against the task's grant and has the host
// mint it. Minting widens nothing: a call the grant does not cover now is
// refused, with the remedy of asking for it first.
func (inst *Service) makeLink(msg *app.Msg) (rep wireMakeLinkReply) {
	rep.V = wireVersion
	req, err := decode[wireMakeLink](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Reason = out.Reason
		return
	}
	minter := inst.links
	rec, spec, id, why := inst.linkRecord(t, req)
	inst.mu.Unlock()
	row := &callRec{key: req.Key, instance: req.Window, app: id, spec: spec, turn: req.Turn, tier: req.Tier,
		cause:      causeOf(wireCall{Turn: req.Turn, ModelCall: req.ModelCall, ToolCall: req.ToolCall, ToolIndex: req.ToolIndex}),
		argsDigest: digest(req.Args), created: time.Now(), title: "make a link"}
	if row.spec.Name == "" {
		row.spec.Name = req.Target
	}
	if why == "" && minter == nil {
		why = "this host keeps no links"
	}
	if why != "" {
		rep.Reason = why
		inst.record(t, row, mintedDecision, phaseOutcome(opwire.PhaseRefused, why))
		return
	}
	link, err := minter.MintTask(rec)
	if err != nil {
		rep.Reason = "the host did not mint the link: " + err.Error()
		inst.record(t, row, mintedDecision, phaseOutcome(opwire.PhaseFailed, rep.Reason))
		return
	}
	inst.record(t, row, mintedDecision, phaseOutcome(opwire.PhaseCompleted, link))
	rep.Ok, rep.Link = true, link
	return
}

// linkRecord checks a requested link and builds its record. The caller
// holds mu.
func (inst *Service) linkRecord(t *task, req wireMakeLink) (rec applink.Record, spec app.OperationSpec, id app.AppIdT, why string) {
	rec = applink.Record{V: applink.RecordVersion, AuthorLevel: applink.LevelOwnModel, AuthorTier: req.Tier,
		Task: t.id, Epoch: t.epoch, Conversation: t.conversation}
	var mode ModeE
	coversOp := func(string) bool { return true }
	switch req.Target {
	case LinkTargetWindow:
		e := t.entries[req.Window]
		if e == nil {
			why = "the grant does not cover that window"
			return
		}
		id, mode, coversOp = e.app, e.mode, e.covers
		rec.Verb, rec.Window = applink.VerbRaise, req.Window
	case LinkTargetOpen, LinkTargetRaise:
		var known bool
		if id, known = inst.resolveApp(req.App); !known {
			why = inst.unknownAppReason(req.App)
			return
		}
		le := t.launches[id]
		launchable := decideLaunch(t.ceiling, le != nil, launchCount(le), launchUsed(le)) == decisionGranted
		if le != nil {
			mode = le.mode
		}
		rec.Verb = applink.VerbOpen
		if req.Target == LinkTargetRaise {
			rec.Verb = applink.VerbRaise
			for _, e := range t.entries {
				if e.app == id {
					mode, coversOp, launchable = e.mode, e.covers, true
					break
				}
			}
		}
		if !launchable {
			why = "the grant lets the task neither open nor reach a window of " + string(id) + "; ask for it with request_access first"
			return
		}
	default:
		why = "the target is open, raise or window"
		return
	}
	rec.App = app.AliasOf(id)
	if req.Operation == "" {
		return
	}
	m, ok := inst.cfg.Registry.LookupManifest(id)
	if !ok || m.Operations == nil {
		why = "the app serves no catalog"
		return
	}
	if spec, ok = m.Operations.Lookup(req.Operation); !ok {
		why = "no such operation"
		return
	}
	if !spec.Agents {
		why = "the operation is not exposed to agents"
		return
	}
	if spec.Class != app.OperationClassCommand {
		why = "a link carries a command: a query's answer would have nowhere to go"
		return
	}
	if spec.Effect == app.OperationEffectConsequential {
		why = "a link never carries a consequential command"
		return
	}
	switch decideOperation(t.ceiling, operationFacts{covered: true, coversOp: coversOp(spec.Name), mode: mode, effect: spec.Effect}) {
	case decisionAboveCeiling:
		why = t.ceiling.refuseEffect(spec.Effect)
		return
	case decisionGranted:
	default:
		why = "the grant does not cover this call in a mode that runs it; ask for it with request_access first"
		return
	}
	js := req.Args
	if js == "" {
		js = "{}"
	}
	args, err := encodeArgs(spec, js)
	if err != nil {
		why = schemaRefusal(spec, err).Reason
		return
	}
	rec.Operation, rec.Version, rec.Args = spec.Name, spec.Version, args
	if rec.Window != 0 {
		read := t.lastRead[rec.Window]
		for _, r := range spec.Writes {
			if v, ok := read[r]; ok {
				if rec.Expects == nil {
					rec.Expects = make(map[string]uint64, len(spec.Writes))
				}
				rec.Expects[r] = v
			}
		}
	}
	return
}

// LinkClick is a person's click on a minted link, as the host's link
// service hands it to the dispatcher.
type LinkClick struct {
	Record applink.Record
	// Candidates are the open windows of the record's app, topmost first:
	// where a raise looks for a window the grant covers.
	Candidates []uint64
	// Surface is the app the link was clicked in; Click numbers the click,
	// so each is a call of its own.
	Surface app.AppIdT
	Click   uint64
}

// LinkFollowed is what a click on a minted link came to. Authorised false
// means the grant does not authorise the call now; the link service then
// follows the link as an authorless one (ADR-0409 §SD6), and Reason says
// why.
type LinkFollowed struct {
	Authorised bool
	Window     uint64
	Outcome    opwire.Outcome
	Reason     string
}

// linkSettle bounds how long a click waits for its call to apply.
const linkSettle = 3 * time.Second

// FollowLink runs a minted link's call under the grant it was minted
// under, checked again at the click: the task live and its epoch unmoved,
// the target covered in a mode that allows the effect, the budgets, the
// ceiling. The work is agent-caused. A click is not a confirmation: in
// suggest mode the call becomes a proposal.
func (inst *Service) FollowLink(ctx context.Context, c LinkClick) (out LinkFollowed) {
	r := c.Record
	if inst.cfg.Host == nil {
		out.Reason = "no window host"
		return
	}
	inst.mu.Lock()
	var t *task
	for _, cand := range inst.tasks {
		if cand.id == r.Task {
			t = cand
			break
		}
	}
	switch {
	case t == nil, t.revoked != "", t.epoch != r.Epoch:
		inst.mu.Unlock()
		out.Reason = "the task that made the link has ended"
		return
	case time.Now().After(t.deadline):
		inst.mu.Unlock()
		out.Reason = reasonDeadline
		return
	}
	id, known := inst.resolveApp(r.App)
	if !known {
		inst.mu.Unlock()
		out.Reason = inst.unknownAppReason(r.App)
		return
	}
	var key uint64
	switch {
	case r.Window != 0:
		if e := t.entries[r.Window]; e != nil {
			key = r.Window
		} else {
			inst.mu.Unlock()
			out.Reason = "the grant no longer covers that window"
			return
		}
	case r.Verb == applink.VerbRaise:
		for _, k := range c.Candidates {
			if e := t.entries[k]; e != nil && e.app == id {
				key = k
				break
			}
		}
	}
	if key == 0 {
		le := t.launches[id]
		if decideLaunch(t.ceiling, le != nil, launchCount(le), launchUsed(le)) != decisionGranted {
			inst.mu.Unlock()
			out.Reason = "the grant no longer lets the task open a window of " + string(id)
			return
		}
		var err error
		key, _, _, err = inst.openFor(t, id, le, r.Kind, r.Config)
		if err != nil {
			out.Reason = "the host did not open it: " + err.Error()
			return
		}
		inst.mu.Lock()
	}
	out.Window = key
	if r.Operation == "" {
		inst.mu.Unlock()
		out.Authorised = true
		out.Outcome = phaseOutcome(opwire.PhaseCompleted, "")
		return
	}
	req := wireCall{Instance: key, Operation: r.Operation, Key: "link-" + r.Digest()[:16] + "-" + strconv.FormatUint(c.Click, 10),
		Reason: "followed a link", Title: "followed a link from " + string(c.Surface), Tier: r.AuthorTier}
	if key == r.Window {
		req.Expects = r.Expects
	}
	m, _ := inst.cfg.Registry.LookupManifest(id)
	if m.Operations != nil {
		if spec, ok := m.Operations.Lookup(r.Operation); ok {
			if spec.Version != r.Version {
				inst.mu.Unlock()
				out.Reason = "the link names version " + strconv.Itoa(int(r.Version)) + " of " + r.Operation + "; the app serves version " + strconv.Itoa(int(spec.Version))
				return
			}
			js, err := argsJSON(spec, r.Args)
			if err != nil {
				inst.mu.Unlock()
				out.Reason = "the link's arguments do not read: " + err.Error()
				return
			}
			req.Args = js
		}
	}
	rec := &callRec{key: req.Key, instance: key, argsDigest: digest(req.Args), created: time.Now(), tier: req.Tier,
		title: req.Title, why: req.Reason}
	t.keys[req.Key] = rec
	o, spec, e, _, _, consent := inst.check(t, req)
	rec.spec, rec.consent = spec, consent
	if e != nil {
		rec.app = e.app
	}
	switch o.Phase {
	case opwire.PhaseUnspecified:
	case opwire.PhaseProposed:
		t.callsUsed++
		rec.req, rec.entry = req, e
		rec.proposal = &proposal{expects: inst.expectsFor(t, req, spec)}
		if why := inst.admitConfirmation(t, rec); why != "" {
			t.callsUsed--
			o = phaseOutcome(opwire.PhaseRefused, why)
			rec.outcome = o
			inst.mu.Unlock()
			inst.record(t, rec, "dispatch", o)
			out.Reason = why
			return
		}
		rec.outcome = o
		inst.mu.Unlock()
		inst.record(t, rec, "dispatch", o)
		out.Authorised, out.Outcome = true, o
		return
	default:
		// Not authorised now — not covered, observe mode, a spent budget,
		// busy, paused, above the ceiling: the link is followed as an
		// authorless one. A click never asks the person to widen.
		rec.outcome = o
		inst.mu.Unlock()
		inst.record(t, rec, "dispatch", o)
		out.Reason = o.Reason
		return
	}
	t.callsUsed++
	inst.mu.Unlock()
	if spec.Effect != app.OperationEffectNone {
		inst.pace(t)
	}
	inst.route(ctx, t, rec, req, spec, e)
	inst.mu.Lock()
	_ = inst.outcomeOf(t, rec, linkSettle)
	out.Outcome = rec.outcome
	inst.mu.Unlock()
	out.Authorised = true
	return
}

// argsJSON is a call's CBOR arguments as the model's JSON, the form the
// dispatcher checks and routes.
func argsJSON(spec app.OperationSpec, args []byte) (js string, err error) {
	if spec.Args == nil || len(args) == 0 {
		return "{}", nil
	}
	v := reflect.New(spec.Args)
	if err = buscodec.Default().Decode(args, v.Interface()); err != nil {
		return
	}
	b, err := opjson.Encode(v.Elem().Interface())
	js = string(b)
	return
}
