package adhocdata

import (
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocevent"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Capability subjects (request/reply, audited — ADR-0240 §SD2, the
// ADR-0026 taxonomy). The wire is the generated adhocrequest / adhocreply
// codec pair (§SD8); the resolve step is the audited hand-out moment;
// there is no grant.
const (
	SubjectPublish = "adhoc.publish"
	SubjectRetract = "adhoc.retract"
	SubjectResolve = "adhoc.resolve"
)

// Event subjects (fire-and-forget, the generated adhocevent codec —
// ADR-0188 §SD3). The service
// publishes one event per dataset transition so consumers react in a frame
// instead of polling: `published` on every publish and republish (the
// push notification ADR-0134 deferred), `retracted` at the LEAVE step of a
// two-phase withdrawal — the dataset has already stopped resolving when the
// event goes out, and its provider stays queryable for RetractGrace so a
// query that had already resolved the handle completes. Consumers declare
// `Sub adhoc.event.>` (SubjectEventAll) to receive them.
const (
	SubjectEventPublished = "adhoc.event.published"
	SubjectEventRetracted = "adhoc.event.retracted"
	SubjectEventAll       = "adhoc.event.>"
)

// Event is one dataset transition as consumers see it (decoded from the
// wire by DecodeEvent / SubscribeEvents).
type Event struct {
	// Op is EventPublished or EventRetracted.
	Op EventOpE
	// Handle is the dataset handle the transition concerns.
	Handle string
	// Alias is the stable alias the dataset was published under.
	Alias string
	// Publisher is the app that published it (bus sender or embedder stamp).
	Publisher string
	// Revision is the dataset revision after a publish, or the last live
	// revision at a retract.
	Revision uint64
}

// EventOpE names the dataset transition an Event carries.
type EventOpE uint8

const (
	EventOpUnspecified EventOpE = 0
	EventOpPublished   EventOpE = 1
	EventOpRetracted   EventOpE = 2
)

func (inst EventOpE) String() (s string) {
	switch inst {
	case EventOpPublished:
		s = "published"
	case EventOpRetracted:
		s = "retracted"
	default:
		s = "unspecified"
	}
	return
}

// DecodeEvent decodes an adhoc.event.* payload. Consumers that subscribe
// directly (rather than through SubscribeEvents) call it in their handler.
func DecodeEvent(subject string, payload []byte) (ev Event, err error) {
	w, err := buscodec.Decode[adhocevent.AdhocEvent](payload)
	if err != nil {
		err = eh.Errorf("adhocdata: decode event: %w", err)
		return
	}
	switch subject {
	case SubjectEventPublished:
		ev.Op = EventOpPublished
	case SubjectEventRetracted:
		ev.Op = EventOpRetracted
	default:
		err = eb.Build().Str("subject", subject).Errorf("adhocdata: unknown event subject")
		return
	}
	ev.Handle = w.Handle
	ev.Alias = w.Alias
	ev.Publisher = w.Publisher
	ev.Revision = w.Revision
	return
}

// publishEvent emits one adhoc.event.* message; a service without a bus
// (in-process Go callers only) emits nothing. Failures are logged, not
// returned: an event is best-effort notification, the state transition it
// reports has already happened.
func (inst *Service) publishEvent(subject string, ev Event) {
	if inst.busClient == nil {
		return
	}
	payload, err := buscodec.Encode(adhocevent.AdhocEvent{
		At: time.Now().UTC(), Op: ev.Op.String(), Handle: ev.Handle, Alias: ev.Alias,
		Publisher: ev.Publisher, Revision: ev.Revision,
	})
	if err != nil {
		inst.log.Warn().Err(err).Str("subject", subject).Msg("adhocdata: encode event")
		return
	}
	if pubErr := inst.busClient.Publish(subject, payload); pubErr != nil {
		inst.log.Warn().Err(pubErr).Str("subject", subject).Msg("adhocdata: publish event")
	}
}

// subscribe binds the three request subjects on the bus — each on its
// own, so the service's own events never echo back to it. A request/reply
// service needs the inbox-prefix Pub cap, or replies never reach the
// caller's inbox and requests time out.
func (inst *Service) subscribe(bus *inprocbus.Inst) (err error) {
	caps := []app.SubjectFilter{
		{Pattern: SubjectPublish, Direction: app.CapDirectionSub, Reason: "adhoc capability: publish"},
		{Pattern: SubjectRetract, Direction: app.CapDirectionSub, Reason: "adhoc capability: retract"},
		{Pattern: SubjectResolve, Direction: app.CapDirectionSub, Reason: "adhoc capability: resolve"},
		{Pattern: SubjectEventAll, Direction: app.CapDirectionPub, Reason: "adhoc: announce publish and retract"},
		{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionSub, Reason: "adhoc: retract what a closed instance published (ADR-0240 §SD5)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "adhoc: reply to caller inboxes"},
	}
	client := bus.NewClient(ServiceAppId, caps)
	subs := []struct {
		subject string
		handler app.MsgHandlerFunc
	}{
		{SubjectPublish, inst.handleRequest},
		{SubjectRetract, inst.handleRequest},
		{SubjectResolve, inst.handleRequest},
		{app.SubjectInstanceClosed, inst.handleInstanceClosed},
	}
	for _, sub := range subs {
		unsub, subErr := client.Subscribe(sub.subject, sub.handler)
		if subErr != nil {
			for _, u := range inst.unsubs {
				u()
			}
			inst.unsubs = nil
			return eb.Build().Str("subject", sub.subject).Errorf("adhocdata: subscribe: %w", subErr)
		}
		inst.unsubs = append(inst.unsubs, unsub)
	}
	inst.busClient = client
	return nil
}

// handleInstanceClosed is the runtime retract (ADR-0240 §SD5): the
// instance's client has closed, so everything it published and did not
// keep goes with it. A dataset lives as long as the window that published
// it, unless the publisher said otherwise.
func (inst *Service) handleInstanceClosed(msg *app.Msg) {
	ev, err := buscodec.Decode[instanceclosed.InstanceClosed](msg.Payload)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdata: decode instance-closed")
		return
	}
	if n := inst.retractOwnedBy(Identity{App: app.AppIdT(ev.AppId), Instance: ev.InstanceKey}); n > 0 {
		inst.log.Info().Str("app", ev.AppId).Uint64("instance", ev.InstanceKey).Int("retracted", n).
			Msg("adhocdata: instance closed; its datasets retracted")
	}
}

// sender is the identity the envelope carries: the authenticated sender
// app and the instance the host minted its client for (ADR-0240 §SD2).
func sender(msg *app.Msg) Identity { return Identity{App: msg.Sender, Instance: msg.SenderInstance} }

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("adhocdata: request without reply inbox")
		return
	}
	switch msg.Subject {
	case SubjectPublish:
		inst.handlePublish(msg)
	case SubjectRetract:
		inst.handleRetract(msg)
	case SubjectResolve:
		inst.handleResolve(msg)
	default:
		inst.refuse(msg, "unknown adhoc subject: "+msg.Subject)
	}
}

// maxPublishPayload bounds the encoded publish request before it is
// decoded: the stream plus a generous allowance for the envelope fields.
const maxPublishPayload = PerDatasetMaxBytes + 4096

// refuse answers a request with a reason and nothing else.
func (inst *Service) refuse(msg *app.Msg, reason string) {
	inst.reply(msg.Reply, adhocreply.AdhocReply{At: time.Now().UTC(), Reason: reason})
}

func (inst *Service) handlePublish(msg *app.Msg) {
	if len(msg.Payload) > maxPublishPayload {
		inst.refuse(msg, "publish payload exceeds the per-dataset quota")
		return
	}
	req, err := buscodec.Decode[adhocrequest.AdhocRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "decode: "+err.Error())
		return
	}
	// The publisher is the envelope's sender, never a client-supplied field.
	res, pErr := inst.Publish(PublishInput{
		Alias: req.Alias, Handle: req.Handle, ArrowIPCStream: req.ArrowStream,
		KeepAfterClose: req.KeepAfterClose, By: sender(msg),
	})
	if pErr != nil {
		inst.refuse(msg, pErr.Error())
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{
		At: time.Now().UTC(), Ok: true, Handle: res.Handle, Revision: res.Revision, Rows: res.Rows, Bytes: res.Bytes,
	})
}

func (inst *Service) handleResolve(msg *app.Msg) {
	req, err := buscodec.Decode[adhocrequest.AdhocRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "decode: "+err.Error())
		return
	}
	live := req.Handle != "" && inst.IsLive(req.Handle)
	res, rErr := inst.Resolve(req.Alias)
	if rErr != nil {
		inst.reply(msg.Reply, adhocreply.AdhocReply{
			At: time.Now().UTC(), Reason: rErr.Error(), HandleLive: live, NoLive: errors.Is(rErr, ErrNoLiveDataset),
		})
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{
		At: time.Now().UTC(), Ok: true, Handle: res.Handle, Revision: res.Revision,
		Rows: res.Rows, Bytes: res.Bytes, CreatedAtUs: res.CreatedAtUnixUs, HandleLive: live,
	})
}

func (inst *Service) handleRetract(msg *app.Msg) {
	req, err := buscodec.Decode[adhocrequest.AdhocRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "decode: "+err.Error())
		return
	}
	if rErr := inst.Retract(req.Handle, sender(msg)); rErr != nil {
		inst.refuse(msg, rErr.Error())
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{At: time.Now().UTC(), Ok: true})
}

// reply encodes rep and publishes it to the caller's inbox. The parameter
// is the concrete DTO on purpose: buscodec.Encode picks the registered
// codec by the static type, and an `any` here would erase it and fall to
// the CBOR default the consumer's decoder does not speak.
func (inst *Service) reply(replySubject string, rep adhocreply.AdhocReply) {
	payload, err := buscodec.Encode(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdata: encode reply")
		return
	}
	if pubErr := inst.busClient.Publish(replySubject, payload); pubErr != nil {
		inst.log.Warn().Err(pubErr).Str("reply", replySubject).Msg("adhocdata: publish reply")
	}
}
