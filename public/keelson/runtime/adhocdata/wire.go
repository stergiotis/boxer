package adhocdata

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Capability subjects (request/reply, CBOR, audited — ADR-0134 SD2, the
// ADR-0026 taxonomy).
const (
	SubjectPublish = "adhoc.publish"
	SubjectGrant   = "adhoc.grant"
	SubjectRetract = "adhoc.retract"
	subjectAll     = "adhoc.>"
)

const wireVersion uint8 = 1

type wirePublishReq struct {
	V              uint8  `json:"v"`
	Alias          string `json:"alias"`
	Handle         string `json:"handle,omitempty"`
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

type wireGrantReq struct {
	V      uint8  `json:"v"`
	Handle string `json:"handle"`
}

type wireGrantRep struct {
	V             uint8  `json:"v"`
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	Structure     string `json:"structure,omitempty"`
	SchemaSummary string `json:"schema_summary,omitempty"`
	Revision      uint64 `json:"revision,omitempty"`
	Alias         string `json:"alias,omitempty"`
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

// subscribe binds the capability subjects on the bus. A request/reply
// service needs the inbox-prefix Pub cap, or replies never reach the
// caller's inbox and requests time out.
func (inst *Service) subscribe(bus *inprocbus.Inst) (err error) {
	caps := []app.SubjectFilter{
		{Pattern: subjectAll, Direction: app.CapDirectionBoth, Reason: "adhoc capability: publish/grant/retract"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "adhoc: reply to caller inboxes"},
	}
	client := bus.NewClient(ServiceAppId, caps)
	unsub, subErr := client.Subscribe(subjectAll, inst.handleRequest)
	if subErr != nil {
		return eh.Errorf("adhocdata: subscribe: %w", subErr)
	}
	inst.busClient = client
	inst.unsub = unsub
	return nil
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("adhocdata: request without reply inbox")
		return
	}
	switch msg.Subject {
	case SubjectPublish:
		inst.handlePublish(msg)
	case SubjectGrant:
		inst.handleGrant(msg)
	case SubjectRetract:
		inst.handleRetract(msg)
	default:
		inst.reply(msg.Reply, wireRetractRep{V: wireVersion, OK: false, Error: "unknown adhoc subject: " + msg.Subject})
	}
}

func (inst *Service) handlePublish(msg *app.Msg) {
	req, err := buscodec.Decode[wirePublishReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wirePublishRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	res, pErr := inst.Publish(PublishInput{Alias: req.Alias, Handle: req.Handle, ArrowIPCStream: req.ArrowIPCStream})
	if pErr != nil {
		inst.reply(msg.Reply, wirePublishRep{V: wireVersion, Error: pErr.Error()})
		return
	}
	inst.reply(msg.Reply, wirePublishRep{
		V: wireVersion, OK: true, Handle: res.Handle, Revision: res.Revision, Rows: res.Rows, Bytes: res.Bytes,
	})
}

func (inst *Service) handleGrant(msg *app.Msg) {
	req, err := buscodec.Decode[wireGrantReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wireGrantRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	res, gErr := inst.Grant(req.Handle)
	if gErr != nil {
		inst.reply(msg.Reply, wireGrantRep{V: wireVersion, Error: gErr.Error()})
		return
	}
	inst.reply(msg.Reply, wireGrantRep{
		V: wireVersion, OK: true, Structure: res.Structure, SchemaSummary: res.SchemaSummary,
		Revision: res.Revision, Alias: res.Alias,
	})
}

func (inst *Service) handleRetract(msg *app.Msg) {
	req, err := buscodec.Decode[wireRetractReq](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, wireRetractRep{V: wireVersion, Error: "decode: " + err.Error()})
		return
	}
	if rErr := inst.Retract(req.Handle); rErr != nil {
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
