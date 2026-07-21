package windowhost

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// OpenSubject is the audited request/reply subject apps use to open
// another app's window (ADR-0135 §SD1). Callers declare it in their
// manifest Caps (Pub direction) and send a launchrequest.LaunchRequest;
// the reply is a launchreply.LaunchReply. Refusals are replies with a
// Reason, never silent drops.
const OpenSubject = "windowhost.open"

// OpenServiceAppId is the synthetic AppId the open service registers
// under on the bus. Apps inspecting Msg.Sender on launch replies see
// this string.
const OpenServiceAppId app.AppIdT = "runtime.windowhost"

// OpenService subscribes OpenSubject and forwards decoded requests to
// the host's OpenWithConfig, replying with the window key or the named
// refusal. On each accepted open it persists the request as a
// factsstore.LaunchRow beside the app-lifecycle "started" row the Open
// itself emitted (§SD6) — best-effort, like every audit write.
//
// Handlers run synchronously on the requester's goroutine (inprocbus
// dispatch), so the service leans on OpenWithConfig being safe off the
// render thread; the opened window is picked up by the next Frame.
type OpenService struct {
	host      *Inst
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()
}

// NewOpenService constructs the service bound to bus and host and
// immediately subscribes OpenSubject. Callers keep it alive for the
// bus's lifetime and invoke Close to release the subscription.
func NewOpenService(bus *inprocbus.Inst, host *Inst, log zerolog.Logger) (svc *OpenService, err error) {
	if bus == nil || host == nil {
		err = eh.Errorf("openservice: nil bus or host")
		return
	}
	svc = &OpenService{
		host: host,
		log:  log.With().Str("app", string(OpenServiceAppId)).Logger(),
	}
	svc.busClient = bus.NewClient(OpenServiceAppId, []app.SubjectFilter{
		{Pattern: OpenSubject, Direction: app.CapDirectionSub, Reason: "windowhost services open requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "windowhost replies to inboxes"},
	})
	svc.unsub, err = svc.busClient.Subscribe(OpenSubject, svc.handleRequest)
	if err != nil {
		err = eh.Errorf("openservice: subscribe %s: %w", OpenSubject, err)
		return
	}
	return
}

// Close unsubscribes the service from the open subject. Safe to call
// once; subsequent calls are no-ops.
func (inst *OpenService) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

func (inst *OpenService) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("openservice: request without reply subject, dropping")
		return
	}
	req, err := buscodec.Decode[launchrequest.LaunchRequest](msg.Payload)
	if err != nil {
		inst.reply(msg.Reply, 0, "openservice: decode request: "+err.Error())
		return
	}
	key, err := inst.host.OpenWithConfig(app.AppIdT(req.TargetAppId), req.ConfigKind, req.Config)
	if err != nil {
		inst.log.Info().Err(err).
			Str("caller", string(msg.Sender)).
			Str("target", req.TargetAppId).
			Str("kind", req.ConfigKind).
			Msg("openservice: open refused")
		inst.reply(msg.Reply, 0, err.Error())
		return
	}
	inst.emitLaunchFact(msg.Sender, req, key)
	inst.log.Info().
		Str("caller", string(msg.Sender)).
		Str("target", req.TargetAppId).
		Str("kind", req.ConfigKind).
		Uint64("windowKey", uint64(key)).
		Msg("openservice: opened window")
	inst.reply(msg.Reply, uint64(key), "")
}

// reply publishes a LaunchReply to the requester's inbox. A publish
// failure is logged — there is nothing else to do with it; the caller
// times out and retries or surfaces the error.
func (inst *OpenService) reply(subject string, windowKey uint64, reason string) {
	err := buscodec.Reply(inst.busClient.Publish, subject, launchreply.LaunchReply{
		At:        time.Now().UTC(),
		WindowKey: windowKey,
		Reason:    reason,
	})
	if err != nil {
		inst.log.Warn().Err(err).Str("inbox", subject).Msg("openservice: reply publish failed")
	}
}

// emitLaunchFact persists the accepted request as a runtime.facts row
// beside the app-lifecycle "started" row OpenWithConfig already wrote —
// same best-effort contract: a write failure is logged at warn and
// never bubbles. The caller identity is attributed from the bus
// envelope (msg.Sender), not from the payload.
func (inst *OpenService) emitLaunchFact(caller app.AppIdT, req launchrequest.LaunchRequest, key WindowKeyT) {
	inst.host.mu.Lock()
	runId := inst.host.runId
	facts := inst.host.facts
	inst.host.mu.Unlock()
	if facts == nil {
		return
	}
	cfg := req.Config
	if len(cfg) == 0 {
		cfg = nil // wire round-trips nil as empty; keep the row's plain-open shape
	}
	_, err := facts.WriteLaunch(factsstore.LaunchRow{
		RunId:       runId,
		CallerAppId: caller,
		TargetAppId: app.AppIdT(req.TargetAppId),
		TileKey:     uint64(key),
		ConfigKind:  req.ConfigKind,
		Config:      cfg,
	})
	if err != nil {
		inst.log.Warn().Err(err).
			Str("caller", string(caller)).
			Str("target", req.TargetAppId).
			Uint64("windowKey", uint64(key)).
			Msg("openservice: write launch fact failed")
	}
}
