package windowhost

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// RequestOpen asks the window host to open targetAppId's window over the
// audited OpenSubject (ADR-0135 §SD1), optionally delivering a launch
// config: configKind is the vocabulary kind the config bytes claim and
// config is its facts-CBOR (encode with the config's generated codec, e.g.
// buscodec.Encode(launchcfg.PlayLaunch{…})); pass "" and nil for a plain
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
	if bus == nil {
		err = eh.Errorf("windowhost: RequestOpen: no bus wired")
		return
	}
	reqBytes, err := buscodec.Encode(launchrequest.LaunchRequest{
		At:          time.Now().UTC(),
		TargetAppId: string(targetAppId),
		ConfigKind:  configKind,
		Config:      config,
	})
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
		err = eh.Errorf("windowhost: open refused: %s", rep.Reason)
		return
	}
	windowKey = rep.WindowKey
	return
}
