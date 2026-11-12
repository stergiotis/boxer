package windowhost

import (
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
// refusal. A request with a kind and no target is first resolved to the
// app that takes the kind (ADR-0361 §SD2, see ResolveLaunchKind); from
// there it runs as a by-id open. On each accepted open it persists the
// request as a factsstore.LaunchRow beside the app-lifecycle "started"
// row the Open itself emitted (§SD6) — best-effort, like every audit
// write; the row names the app that opened.
//
// Handlers run synchronously on the requester's goroutine (inprocbus
// dispatch), so the service leans on OpenWithConfig being safe off the
// render thread; the opened window is picked up by the next Frame.
type OpenService struct {
	host      *Inst
	log       zerolog.Logger
	busClient *inprocbus.Client
	unsub     func()
	unsubR    func()
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
		{Pattern: RaiseSubject, Direction: app.CapDirectionSub, Reason: "windowhost services raise requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "windowhost replies to inboxes"},
	})
	svc.unsub, err = svc.busClient.Subscribe(OpenSubject, svc.handleRequest)
	if err != nil {
		err = eb.Build().Str("openSubject", OpenSubject).Errorf("openservice: subscribe: %w", err)
		return
	}
	svc.unsubR, err = svc.busClient.Subscribe(RaiseSubject, svc.handleRaise)
	if err != nil {
		svc.unsub()
		err = eb.Build().Str("raiseSubject", RaiseSubject).Errorf("openservice: subscribe: %w", err)
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
	if inst.unsubR != nil {
		inst.unsubR()
		inst.unsubR = nil
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
	target := app.AppIdT(req.TargetAppId)
	resolved := false
	if target == "" && req.ConfigKind != "" {
		target, err = inst.host.ResolveLaunchKind(req.ConfigKind)
		if err != nil {
			inst.log.Info().Err(err).
				Str("caller", string(msg.Sender)).
				Str("kind", req.ConfigKind).
				Msg("openservice: open by kind refused")
			inst.reply(msg.Reply, 0, refusalText(err))
			return
		}
		resolved = true
	}
	key, err := inst.host.OpenWithConfig(target, req.ConfigKind, req.Config)
	if err != nil {
		inst.log.Info().Err(err).
			Str("caller", string(msg.Sender)).
			Str("target", string(target)).
			Bool("resolvedFromKind", resolved).
			Str("kind", req.ConfigKind).
			Msg("openservice: open refused")
		inst.reply(msg.Reply, 0, refusalText(err))
		return
	}
	inst.emitLaunchFact(msg.Sender, target, resolved, req, key)
	inst.log.Info().
		Str("caller", string(msg.Sender)).
		Str("target", string(target)).
		Bool("resolvedFromKind", resolved).
		Str("kind", req.ConfigKind).
		Uint64("windowKey", uint64(key)).
		Msg("openservice: opened window")
	inst.reply(msg.Reply, uint64(key), "")
}

// reply publishes a LaunchReply to the requester's inbox. A publish
// failure is logged — there is nothing else to do with it; the caller
// times out and retries or surfaces the error.
// refusalText is the reply's reason for a refused open: the error's message
// without the package prefix the host's errors carry for its own log. The
// caller's RefusedError names the subject already, so the text a person
// reads is "windowhost: open refused: app not registered", not the prefix
// twice.
func refusalText(err error) (reason string) {
	reason = err.Error()
	reason = strings.TrimPrefix(reason, "windowhost: ")
	reason = strings.TrimPrefix(reason, "open: ")
	return
}

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

// emitLaunchFact persists the accepted request as a boxer.facts row
// beside the app-lifecycle "started" row OpenWithConfig already wrote —
// same best-effort contract: a write failure is logged at warn and
// never bubbles. The caller identity is attributed from the bus
// envelope (msg.Sender), not from the payload. target is the app that
// opened: the request's target, or the app resolved from its kind.
func (inst *OpenService) emitLaunchFact(caller app.AppIdT, target app.AppIdT, resolved bool, req launchrequest.LaunchRequest, key WindowKeyT) {
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
		RunId:            runId,
		CallerAppId:      caller,
		TargetAppId:      target,
		TileKey:          uint64(key),
		ConfigKind:       req.ConfigKind,
		Config:           cfg,
		ResolvedFromKind: resolved,
	})
	if err != nil {
		inst.log.Warn().Err(err).
			Str("caller", string(caller)).
			Str("target", string(target)).
			Uint64("windowKey", uint64(key)).
			Msg("openservice: write launch fact failed")
	}
}
