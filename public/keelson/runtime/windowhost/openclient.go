package windowhost

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RequestOpen asks the window host to open targetAppId's window over the
// audited OpenSubject (ADR-0135 §SD1), optionally delivering a launch
// config: configKind is the vocabulary kind the config bytes claim and
// config is its facts-CBOR (encode with the config's generated codec, e.g.
// buscodec.Encode(playground.Launch{…})); pass "" and nil for a plain
// open. It is the client half of OpenService — the mirror of
// adhocdata.PublishRequest — so an app drives windowhost.open without
// re-deriving the request/reply codec dance.
//
// The caller's bus client needs Pub on OpenSubject; the caller identity is
// attributed by the bus (Msg.Sender), not the payload. RequestOpen blocks
// on the bus round-trip, so call it off the frame loop. A refusal reply
// (unknown app, kind mismatch, oversize, malformed envelope) returns an
// error carrying the host's reason; on success it returns the opened
// window's key.
func RequestOpen(bus app.BusI, targetAppId app.AppIdT, configKind string, config []byte) (windowKey uint64, err error) {
	return requestOpen(bus, launchrequest.LaunchRequest{
		At:          time.Now().UTC(),
		TargetAppId: string(targetAppId),
		ConfigKind:  configKind,
		Config:      config,
	})
}

// RequestOpenKind asks the window host to open whichever app takes
// configKind, naming no target (ADR-0361 §SD2). The host resolves the kind
// at the open: the launchable apps declaring it as their LaunchKind, the
// one designated for it at the wiring site if several do; several with no
// designation, a designation that is not among them, or none at all are
// refusals carrying the reason. Everything after resolution is RequestOpen:
// same subject, same caller caps, same size cap, same probe, same reply.
// config must be non-empty: the host refuses a claimed kind with no bytes.
func RequestOpenKind(bus app.BusI, configKind string, config []byte) (windowKey uint64, err error) {
	if configKind == "" {
		err = eh.Errorf("windowhost: RequestOpenKind: no config kind")
		return
	}
	return requestOpen(bus, launchrequest.LaunchRequest{
		At:         time.Now().UTC(),
		ConfigKind: configKind,
		Config:     config,
	})
}

// requestOpen runs the launch request/reply over the open subject and turns
// a refusal reply into an error.
func requestOpen(bus app.BusI, req launchrequest.LaunchRequest) (windowKey uint64, err error) {
	if bus == nil {
		err = eh.Errorf("windowhost: RequestOpen: no bus wired")
		return
	}
	reqBytes, err := buscodec.Encode(req)
	if err != nil {
		err = eh.Errorf("windowhost: encode launch request: %w", err)
		return
	}
	replyBytes, err := bus.Request(OpenSubject, reqBytes)
	if err != nil {
		err = eh.Errorf("windowhost: open request: %w", err)
		return
	}
	rep, err := buscodec.Decode[launchreply.LaunchReply](replyBytes)
	if err != nil {
		err = eh.Errorf("windowhost: decode launch reply: %w", err)
		return
	}
	if rep.Reason != "" {
		err = eb.Build().Str("reason", rep.Reason).Errorf("windowhost: open refused")
		return
	}
	windowKey = rep.WindowKey
	return
}
