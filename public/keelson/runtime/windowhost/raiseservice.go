package windowhost

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func (inst *OpenService) handleRaise(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("openservice: raise without reply subject, dropping")
		return
	}
	req, err := buscodec.Decode[windowopen.RaiseRequest](msg.Payload)
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
