package trail

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The audit subject (ADR-0026 §SD3's `runtime.audit.{id}`, a write verb
// since ADR-0327 §SD8): an app publishes an audit event to the subject
// that names it, and the host records it with the envelope's sender as
// its origin, which the host vouches for (OriginBy envelope). The
// principal and the purpose stay the app's claims. A row sent to another
// app's subject, or in the recorder's own domain, is refused.
const (
	AuditSubjectPrefix = "runtime.audit."
	// AuditSubjectAll matches every app's subject: an app id carries dots
	// of its own, so the tail is several tokens.
	AuditSubjectAll = "runtime.audit.>"
)

// AuditServiceAppId is the identity the host's audit service holds on the
// bus.
const AuditServiceAppId app.AppIdT = "runtime.audit"

// AuditSubjectOf is the subject an app writes its audit events to.
func AuditSubjectOf(appId app.AppIdT) string { return AuditSubjectPrefix + string(appId) }

const auditWireVersion = 1

// wireAuditEvent is an audit event on the bus: the event's values as
// plain fields, and when it happened (zero: when the host took it).
type wireAuditEvent struct {
	V           uint8     `json:"v"`
	At          time.Time `json:"at"`
	Domain      string    `json:"domain"`
	Action      string    `json:"action"`
	Outcome     string    `json:"outcome"`
	Principal   string    `json:"principal,omitempty"`
	PrincipalBy string    `json:"principal_by,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	Node        string    `json:"node,omitempty"`
	Subject     uint64    `json:"subject,omitempty"`
	Retention   string    `json:"retention"`
	RefTypes    []string  `json:"ref_types,omitempty"`
	RefValues   []string  `json:"ref_values,omitempty"`
	AttrKeys    []string  `json:"attr_keys,omitempty"`
	AttrValues  []string  `json:"attr_values,omitempty"`
}

type wireAuditAck struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// AuditServiceCaps are the service's grants.
func AuditServiceCaps() (caps []app.SubjectFilter) {
	return []app.SubjectFilter{
		{Pattern: AuditSubjectAll, Direction: app.CapDirectionSub, Reason: "trail: serve runtime.audit requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "trail: reply to inboxes"},
	}
}

// AuditClientCaps are what an app declares to write its audit events.
func AuditClientCaps(reason string) (caps []app.SubjectFilter) {
	return []app.SubjectFilter{{Pattern: AuditSubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true}}
}

// AuditService answers runtime.audit.* over the host's recorder.
type AuditService struct {
	rec       *Recorder
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger
	// Timeout bounds one recorder call.
	Timeout time.Duration
}

// NewAuditService subscribes the service over rec; the caller closes it.
func NewAuditService(bus *inprocbus.Inst, log zerolog.Logger, rec *Recorder) (s *AuditService, err error) {
	if bus == nil || rec == nil {
		err = eh.Errorf("nil bus or recorder")
		return
	}
	s = &AuditService{rec: rec, log: log.With().Str("app", string(AuditServiceAppId)).Logger(), Timeout: 30 * time.Second}
	s.busClient = bus.NewClient(AuditServiceAppId, AuditServiceCaps())
	if s.unsub, err = s.busClient.Subscribe(AuditSubjectAll, s.handle); err != nil {
		_ = s.busClient.Close()
		err = eh.Errorf("subscribe: %w", err)
		return nil, err
	}
	return
}

// Close releases the subscription and the bus client.
func (inst *AuditService) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		_ = inst.busClient.Close()
	})
}

func (inst *AuditService) handle(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	if why := inst.record(msg); why != "" {
		inst.reply(msg.Reply, wireAuditAck{V: auditWireVersion, Reason: why})
		return
	}
	inst.reply(msg.Reply, wireAuditAck{V: auditWireVersion, Ok: true})
}

// record takes one message's event, or says why not.
func (inst *AuditService) record(msg *app.Msg) (why string) {
	sender := string(msg.Sender)
	if !strings.HasPrefix(msg.Subject, AuditSubjectPrefix) || strings.TrimPrefix(msg.Subject, AuditSubjectPrefix) != sender {
		return "an app writes audit events to its own subject"
	}
	w, err := buscodec.Decode[wireAuditEvent](msg.Payload)
	if err != nil {
		return err.Error()
	}
	if w.Domain == TrailDomain {
		return "the trail domain is the recorder's own"
	}
	row := AuditEvent{Domain: w.Domain, Action: w.Action, Outcome: w.Outcome, PrincipalBy: w.PrincipalBy, Subject: w.Subject, Retention: w.Retention,
		RefTypes: w.RefTypes, RefValues: w.RefValues, AttrKeys: w.AttrKeys, AttrValues: w.AttrValues}
	if w.Node != "" {
		row.Node = option.Some(w.Node)
	}
	if w.Principal != "" && row.PrincipalBy == "" {
		row.PrincipalBy = PrincipalByEnv
	}
	at := w.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(context.Background(), inst.Timeout)
	defer cancel()
	// The origin is the envelope's sender, which the host vouches for; the
	// principal and the purpose are the app's claims.
	o := inst.rec.Origin(msg)
	ctx = callident.WithCallIdentity(ctx, callident.CallIdentity{
		Origin: callident.Origin{Run: o.Run, App: o.App, Instance: o.Instance},
		Claims: callident.Claims{Principal: w.Principal, Purpose: w.Purpose},
	})
	ctx = evidence.WithAttestation(ctx, evidence.Attestation{OriginBy: evidence.OriginByEnvelope, PrincipalBy: row.PrincipalBy})
	if err = inst.rec.Event(ctx, at, Context{Origin: o}, row); err != nil {
		return err.Error()
	}
	return
}

func (inst *AuditService) reply(subject string, v any) {
	if err := buscodec.Reply(inst.busClient.Publish, subject, v); err != nil {
		inst.log.Warn().Err(err).Str("subject", subject).Msg("reply failed")
	}
}

// AuditClient writes an app's audit events over the bus.
type AuditClient struct {
	bus   app.BusI
	appId app.AppIdT
	// Timeout bounds one request.
	Timeout time.Duration
}

// NewAuditClient wraps bus for the app named appId; the app's manifest
// declares AuditClientCaps.
func NewAuditClient(bus app.BusI, appId app.AppIdT) *AuditClient {
	return &AuditClient{bus: bus, appId: appId, Timeout: 30 * time.Second}
}

// RefusedError is an event the service declined.
type RefusedError struct {
	Reason string
}

func (inst *RefusedError) Error() string { return "refused: " + inst.Reason }

// Event sends row, happening at at, with the principal and the purpose as
// the app's claims, and waits for the host's answer.
func (inst *AuditClient) Event(ctx context.Context, at time.Time, principal, purpose string, row AuditEvent) (err error) {
	if inst == nil || inst.bus == nil {
		return eh.Errorf("audit client without a bus")
	}
	w := wireAuditEvent{V: auditWireVersion, At: at, Domain: row.Domain, Action: row.Action, Outcome: row.Outcome,
		Principal: principal, PrincipalBy: row.PrincipalBy, Purpose: purpose, Node: row.Node.Val, Subject: row.Subject, Retention: row.Retention,
		RefTypes: row.RefTypes, RefValues: row.RefValues, AttrKeys: row.AttrKeys, AttrValues: row.AttrValues}
	payload, err := buscodec.Encode(w)
	if err != nil {
		return eh.Errorf("encode: %w", err)
	}
	d := inst.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if until := time.Until(deadline); until < d {
			d = until
		}
	}
	raw, err := inst.bus.RequestWithTimeout(AuditSubjectOf(inst.appId), payload, d)
	if err != nil {
		return eh.Errorf("request: %w", err)
	}
	ack, err := buscodec.Decode[wireAuditAck](raw)
	if err != nil {
		return eh.Errorf("decode: %w", err)
	}
	if !ack.Ok {
		return &RefusedError{Reason: ack.Reason}
	}
	return
}
