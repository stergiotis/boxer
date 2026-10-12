package windowhost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RaiseSubject is the request/reply subject an app uses to bring one of its
// own windows to the front: the chat's roster raises the chat window that
// holds a conversation. The window must run the caller's app, as the bus
// attributes it (Msg.Sender); another app's window is refused, so an app
// cannot pull the person away to someone else's. The service is OpenService's
// and the reply is a launchreply.LaunchReply, its Reason set on refusal.
const RaiseSubject = "windowhost.raise"

type wireRaise struct {
	V      uint8  `json:"v"`
	Window uint64 `json:"window"`
}

func (inst *OpenService) handleRaise(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("openservice: raise without reply subject, dropping")
		return
	}
	req, err := buscodec.Decode[wireRaise](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, 0, "openservice: decode raise: "+err.Error())
		return
	}
	if err = inst.host.raiseOwn(msg.Sender, WindowKeyT(req.Window)); err != nil {
		inst.reply(msg.Reply, 0, err.Error())
		return
	}
	inst.reply(msg.Reply, req.Window, "")
}

// raiseOwn is Raise for a window that runs caller's app.
func (inst *Inst) raiseOwn(caller app.AppIdT, key WindowKeyT) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	w := inst.openWindowLocked(key)
	switch {
	case w == nil:
		return eb.Build().Uint64("key", uint64(key)).Errorf("windowhost: no open window with this key")
	case w.manifest.Id != caller:
		return eb.Build().Uint64("key", uint64(key)).Str("caller", string(caller)).Errorf("windowhost: an app raises only its own windows")
	}
	inst.pendingRaise = key
	return
}

// RequestRaise asks the window host to raise windowKey, a window of the
// caller's own app, over RaiseSubject. The caller's bus client needs Pub on
// it. It blocks on the round-trip, so call it off the frame loop.
func RequestRaise(bus app.BusI, windowKey uint64) (err error) {
	if bus == nil {
		return eh.Errorf("windowhost: RequestRaise: no bus wired")
	}
	reqBytes, err := buscodec.Encode(wireRaise{V: 1, Window: windowKey})
	if err != nil {
		return eh.Errorf("windowhost: encode raise: %w", err)
	}
	replyBytes, err := bus.Request(RaiseSubject, reqBytes)
	if err != nil {
		return eh.Errorf("windowhost: raise request: %w", err)
	}
	rep, err := buscodec.Decode[launchreply.LaunchReply](replyBytes)
	if err != nil {
		return eh.Errorf("windowhost: decode raise reply: %w", err)
	}
	if rep.Reason != "" {
		return eb.Build().Str("reason", rep.Reason).Errorf("windowhost: raise refused")
	}
	return
}
