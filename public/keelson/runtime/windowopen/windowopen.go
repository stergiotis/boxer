// Package windowopen is the client half of the window host's open and raise
// services: the subjects and the request helpers an app calls to open
// another app's window or raise one of its own. It links none of the window
// host, so an app that only opens windows does not carry the host
// (ADR-0135, ADR-0394).
package windowopen

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// OpenSubject is the audited request/reply subject apps use to open
// another app's window (ADR-0135 §SD1). Callers declare it in their
// manifest Caps (Pub direction, OpenCap) and send a
// launchrequest.LaunchRequest; the reply is a launchreply.LaunchReply.
// Refusals are replies with a Reason, never silent drops.
const OpenSubject = "windowhost.open"

// RaiseSubject is the request/reply subject an app uses to bring one of its
// own windows to the front: the chat's roster raises the chat window that
// holds a conversation. The window must run the caller's app, as the bus
// attributes it (Msg.Sender); another app's window is refused, so an app
// cannot pull the person away to someone else's. The reply is a
// launchreply.LaunchReply, its Reason set on refusal.
const RaiseSubject = "windowhost.raise"

// RaiseRequest is the payload sent on RaiseSubject.
type RaiseRequest struct {
	V      uint8  `json:"v"`
	Window uint64 `json:"window"`
}

// OpenCap is the manifest capability RequestOpen and RequestOpenKind need,
// with the caller's reason.
func OpenCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: OpenSubject, Direction: app.CapDirectionPub, Reason: reason}
}

// RaiseCap is the manifest capability RequestRaise needs, with the caller's
// reason.
func RaiseCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: RaiseSubject, Direction: app.CapDirectionPub, Reason: reason}
}

// RequestOpen asks the window host to open targetAppId's window over the
// audited OpenSubject (ADR-0135 §SD1), optionally delivering a launch
// config: configKind is the vocabulary kind the config bytes claim and
// config is its facts-CBOR (encode with the config's generated codec, e.g.
// buscodec.Encode(playground.Launch{…})); pass "" and nil for a plain
// open. It is the client half of the window host's open service — the
// mirror of adhocdata.PublishRequest — so an app drives windowhost.open
// without re-deriving the request/reply codec dance.
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

// RefusedError is a request the window host refused. Its message carries
// the host's reason, so a caller that shows err.Error() shows why: which app
// was unknown, which kind no app takes, which candidates an open by kind
// found and none designated (ADR-0361 §SD2). errors.As reaches the reason
// for a caller that words the outcome itself.
type RefusedError struct {
	// Act is the request refused: "open" or "raise".
	Act string
	// Reason is the host's text, as its reply carried it.
	Reason string
}

func (inst *RefusedError) Error() string {
	return "windowhost: " + inst.Act + " refused: " + inst.Reason
}

// RequestRaise asks the window host to raise windowKey, a window of the
// caller's own app, over RaiseSubject. The caller's bus client needs Pub on
// it (RaiseCap). It blocks on the round-trip, so call it off the frame loop.
func RequestRaise(bus app.BusI, windowKey uint64) (err error) {
	if bus == nil {
		return eh.Errorf("windowhost: RequestRaise: no bus wired")
	}
	reqBytes, err := buscodec.Encode(RaiseRequest{V: 1, Window: windowKey})
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
		return &RefusedError{Act: "raise", Reason: rep.Reason}
	}
	return
}

// requestOpen runs the launch request/reply over the open subject and turns
// a refusal reply into a RefusedError.
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
		err = &RefusedError{Act: "open", Reason: rep.Reason}
		return
	}
	windowKey = rep.WindowKey
	return
}
