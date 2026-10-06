package agent

// The disclosure record (ADR-0287 §SD6): a coordinator reports each view of
// a screenshot's pixels its model asked for — shown, declined or refused —
// and the host writes it on the trail. The coordinator enforces the Pixels
// setting, as it does the artefact's policy; what it states here is its own
// account, stamped with the coordinator window as origin, the way the
// conversation and turn of every other row are stated by the app.
//
// A shown view is reported before its pixels are attached, so a host that
// cannot keep the record refuses the disclosure rather than lose it.

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// The decisions and deciders a disclosure names.
const (
	DisclosureShown    = "shown"
	DisclosureDeclined = "declined"
	DisclosureRefused  = "refused"

	// DecidedByPerson is the person allowing or declining this view.
	DecidedByPerson = "person"
	// DecidedByConsent is a consent the person gave this content earlier.
	DecidedByConsent = "consent"
	// DecidedBySetting is a level that does not ask.
	DecidedBySetting = "setting"
	// DecidedByChat is the coordinator refusing under the setting or the
	// host's cap.
	DecidedByChat = "chat"
)

// Disclosure is one view as the coordinator reports it.
type Disclosure struct {
	// Handle is the task holding the coordinator, when one still does; the
	// row then carries it as its delegation.
	Handle       string
	Conversation string
	Asked        Asked
	Image        string
	// Digest is the BLAKE3-256 hex of the PNG; RootDigest that of the
	// capture it descends from; Source "capture", "copy" or "crop".
	Digest     string
	RootDigest string
	Source     string
	Width      uint32
	Height     uint32
	Bytes      uint64
	Level      PixelsE
	LocalOnly  bool
	Decision   string
	DecidedBy  string
	Endpoint   string
	Reason     string
}

type wireDisclose struct {
	V            uint8  `json:"v"`
	Handle       string `json:"handle,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	wireCause
	Image      string `json:"image"`
	Digest     string `json:"digest,omitempty"`
	RootDigest string `json:"root_digest,omitempty"`
	Source     string `json:"source,omitempty"`
	Width      uint32 `json:"width,omitempty"`
	Height     uint32 `json:"height,omitempty"`
	Bytes      uint64 `json:"bytes,omitempty"`
	Level      uint8  `json:"level"`
	LocalOnly  bool   `json:"local_only,omitempty"`
	Decision   string `json:"decision"`
	DecidedBy  string `json:"decided_by"`
	Endpoint   string `json:"endpoint,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Disclose reports one view; a refusal means the host did not record it,
// and a shown view must then not be attached.
func (inst *Client) Disclose(ctx context.Context, d Disclosure) (err error) {
	w := wireDisclose{V: wireVersion, Handle: d.Handle, Conversation: d.Conversation, wireCause: d.Asked.wire(),
		Image: d.Image, Digest: d.Digest, RootDigest: d.RootDigest, Source: d.Source, Width: d.Width, Height: d.Height,
		Bytes: d.Bytes, Level: uint8(d.Level), LocalOnly: d.LocalOnly, Decision: d.Decision, DecidedBy: d.DecidedBy,
		Endpoint: d.Endpoint, Reason: d.Reason}
	return ack(roundTrip[wireDisclose, wireAck](ctx, inst, SubjectDisclose, w))
}

// disclose answers SubjectDisclose: only a registered coordinator reports,
// and the row is written or the report refused. A refused report is a row of
// the actions file, so an app that keeps reporting what it may not is seen.
func (inst *Service) disclose(msg *app.Msg) (rep wireAck) {
	rep.V = wireVersion
	req, err := decode[wireDisclose](msg.Payload)
	defer func() {
		if !rep.Ok {
			inst.record(nil, &callRec{key: "disclose|" + strconv.FormatInt(time.Now().UnixNano(), 36), turn: req.Turn,
				cause: req.wireCause.cause(), conversation: req.Conversation, actor: msg.Sender, actorInstance: msg.SenderInstance,
				spec: app.OperationSpec{Name: "disclose", Effect: app.OperationEffectNone}}, "final", phaseOutcome(opwire.PhaseRefused, rep.Reason))
		}
	}()
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if !inst.isCoordinator(msg.Sender) {
		rep.Reason = "only an app registered as a coordinator reports disclosures"
		return
	}
	if why := disclosureMismatch(req.Decision, req.DecidedBy, req.Digest); why != "" {
		rep.Reason = why
		return
	}
	if err = inst.cfg.Trail.Admit(); err != nil {
		rep.Reason = err.Error()
		return
	}
	r := ActionRecord{At: time.Now(), Actor: msg.Sender, ActorInstance: msg.SenderInstance, Conversation: req.Conversation,
		Turn: req.Turn, ModelCall: req.ModelCall, ToolCallId: req.ToolCall, ToolIndex: req.ToolIndex}
	if req.Handle != "" {
		inst.mu.Lock()
		// A task that ended still names the work the image came from; one of
		// another coordinator does not.
		if t := inst.tasks[req.Handle]; t != nil && t.actor == msg.Sender && t.actorInstance == msg.SenderInstance {
			r.Task, r.Epoch = t.id, t.epoch
		}
		inst.mu.Unlock()
	}
	c, cause, _ := TrailRowOf(inst.cfg.Trail, r)
	row := trail.AgentDisclosure{
		Image: req.Image, Digest: req.Digest, RootDigest: req.RootDigest, Source: req.Source,
		Width: req.Width, Height: req.Height, Bytes: req.Bytes,
		Level: min(PixelsE(req.Level), PixelsCaptures).String(), LocalOnly: req.LocalOnly,
		Decision: req.Decision, DecidedBy: req.DecidedBy,
	}
	if req.Endpoint != "" {
		row.Endpoint = []string{req.Endpoint}
	}
	if req.Reason != "" {
		row.Reason = []string{req.Reason}
	}
	if err = inst.cfg.Trail.AgentDisclosure(r.At, c, cause, row); err != nil {
		rep.Reason = "the trail could not record the view: " + err.Error()
		return
	}
	inst.cfg.Trail.FlushSoon()
	rep.Ok = true
	return
}

// disclosureMismatch is why a decision and its decider do not belong
// together, "" when they do: the person declines; the chat refuses; a shown
// view names its digest and was let through by the person, a consent they
// gave or a level that does not ask.
func disclosureMismatch(decision string, decidedBy string, digest string) (why string) {
	switch {
	case decision == DisclosureShown && decidedBy != DecidedByPerson && decidedBy != DecidedByConsent && decidedBy != DecidedBySetting:
		return "a shown view is let through by the person, a consent or the setting"
	case decision == DisclosureShown && digest == "":
		return "a shown view names its digest"
	case decision == DisclosureDeclined && decidedBy != DecidedByPerson:
		return "only the person declines a view"
	case decision == DisclosureRefused && decidedBy != DecidedByChat:
		return "only the chat refuses a view"
	case decision != DisclosureShown && decision != DisclosureDeclined && decision != DisclosureRefused:
		return "a disclosure's decision is shown, declined or refused"
	}
	return ""
}
