package adhocdata

import (
	"errors"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Capability subjects (request/reply, CBOR, audited — ADR-0240 §SD2, the
// ADR-0026 taxonomy). The resolve step is the audited hand-out moment;
// there is no grant.
const (
	SubjectPublish = "adhoc.publish"
	SubjectRetract = "adhoc.retract"
	SubjectResolve = "adhoc.resolve"
)

// Event subjects (fire-and-forget, CBOR — ADR-0188 §SD3). The service
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

type wireEvent struct {
	V         uint8  `json:"v"`
	Op        string `json:"op"`
	Handle    string `json:"handle"`
	Alias     string `json:"alias,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Revision  uint64 `json:"revision,omitempty"`
}

// DecodeEvent decodes an adhoc.event.* payload. Consumers that subscribe
// directly (rather than through SubscribeEvents) call it in their handler.
func DecodeEvent(subject string, payload []byte) (ev Event, err error) {
	w, err := buscodec.Decode[wireEvent](payload)
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
	payload, err := buscodec.Encode(wireEvent{
		V: wireVersion, Op: ev.Op.String(), Handle: ev.Handle, Alias: ev.Alias,
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

const wireVersion uint8 = 1

type wirePublishReq struct {
	V              uint8  `json:"v"`
	Alias          string `json:"alias"`
	Handle         string `json:"handle,omitempty"`
	KeepAfterClose bool   `json:"keep_after_close,omitempty"`
	ArrowIPCStream []byte `json:"arrow_ipc_stream"`
}

type wirePublishRep struct {
	V        uint8  `json:"v"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Handle   string `json:"handle,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
	Rows     uint64 `json:"rows,omitempty"`
	Bytes    uint64 `json:"bytes,omitempty"`
}

type wireResolveReq struct {
	V     uint8  `json:"v"`
	Alias string `json:"alias"`
	// Handle, when set, asks the service to report in the reply whether
	// this handle is still live (ADR-0188 §SD3 reconcile): a consumer bound
	// to it verifies its binding and learns the alias's newest dataset in
	// one round trip, without a grant.
	Handle string `json:"handle,omitempty"`
}

type wireResolveRep struct {
	V               uint8  `json:"v"`
	OK              bool   `json:"ok"`
	Error           string `json:"error,omitempty"`
	Handle          string `json:"handle,omitempty"`
	Revision        uint64 `json:"revision,omitempty"`
	Rows            uint64 `json:"rows,omitempty"`
	Bytes           uint64 `json:"bytes,omitempty"`
	CreatedAtUnixUs int64  `json:"created_at_unix_us,omitempty"`
	// HandleLive answers wireResolveReq.Handle: true when that handle is
	// still in the live set (not left, not unloading). Meaningful whether
	// or not the alias itself resolved.
	HandleLive bool `json:"handle_live,omitempty"`
	// NoLive marks a failed resolve as ErrNoLiveDataset rather than a
	// malformed request, so the caller can wait instead of retrying.
	NoLive bool `json:"no_live,omitempty"`
}

type wireRetractReq struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
}

type wireRetractRep struct {
	V     uint8  `json:"v"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "adhoc: reply to caller inboxes"},
	}
	client := bus.NewClient(ServiceAppId, caps)
	for _, subject := range []string{SubjectPublish, SubjectRetract, SubjectResolve} {
		unsub, subErr := client.Subscribe(subject, inst.handleRequest)
		if subErr != nil {
			for _, u := range inst.unsubs {
				u()
			}
			inst.unsubs = nil
			return eb.Build().Str("subject", subject).Errorf("adhocdata: subscribe: %w", subErr)
		}
		inst.unsubs = append(inst.unsubs, unsub)
	}
	inst.busClient = client
	return nil
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
		inst.reply(msg.Reply, wireRetractRep{V: wireVersion, OK: false, Error: "unknown adhoc subject: " + msg.Subject})
	}
}

// maxPublishPayload bounds the encoded publish request before it is
// decoded: the stream plus a generous allowance for the envelope fields.
const maxPublishPayload = PerDatasetMaxBytes + 4096

func (inst *Service) handlePublish(msg *app.Msg) {
	if len(msg.Payload) > maxPublishPayload {
		inst.reply(msg.Reply, wirePublishRep{V: wireVersion, Error: "publish payload exceeds the per-dataset quota"})
		return
	}
	req, err := buscodec.Decode[wirePublishReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wirePublishRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	// The publisher is the envelope's sender, never a client-supplied field.
	res, pErr := inst.Publish(PublishInput{
		Alias: req.Alias, Handle: req.Handle, ArrowIPCStream: req.ArrowIPCStream,
		KeepAfterClose: req.KeepAfterClose, By: sender(msg),
	})
	if pErr != nil {
		inst.reply(msg.Reply, wirePublishRep{V: wireVersion, Error: pErr.Error()})
		return
	}
	inst.reply(msg.Reply, wirePublishRep{
		V: wireVersion, OK: true, Handle: res.Handle, Revision: res.Revision, Rows: res.Rows, Bytes: res.Bytes,
	})
}

func (inst *Service) handleResolve(msg *app.Msg) {
	req, err := buscodec.Decode[wireResolveReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wireResolveRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	live := req.Handle != "" && inst.IsLive(req.Handle)
	res, rErr := inst.Resolve(req.Alias)
	if rErr != nil {
		inst.reply(msg.Reply, wireResolveRep{
			V: wireVersion, Error: rErr.Error(), HandleLive: live, NoLive: errors.Is(rErr, ErrNoLiveDataset),
		})
		return
	}
	inst.reply(msg.Reply, wireResolveRep{
		V: wireVersion, OK: true, Handle: res.Handle, Revision: res.Revision,
		Rows: res.Rows, Bytes: res.Bytes, CreatedAtUnixUs: res.CreatedAtUnixUs, HandleLive: live,
	})
}

func (inst *Service) handleRetract(msg *app.Msg) {
	req, err := buscodec.Decode[wireRetractReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wireRetractRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	if rErr := inst.Retract(req.Handle, sender(msg)); rErr != nil {
		inst.reply(msg.Reply, wireRetractRep{V: wireVersion, Error: rErr.Error()})
		return
	}
	inst.reply(msg.Reply, wireRetractRep{V: wireVersion, OK: true})
}

// reply encodes v and publishes it to the caller's inbox.
func (inst *Service) reply(replySubject string, v any) {
	payload, err := buscodec.Encode(v)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdata: encode reply")
		return
	}
	if pubErr := inst.busClient.Publish(replySubject, payload); pubErr != nil {
		inst.log.Warn().Err(pubErr).Str("reply", replySubject).Msg("adhocdata: publish reply")
	}
}
