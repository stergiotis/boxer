package adhocdata

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// PublishRequest publishes — or, with in.Handle set, republishes — a
// dataset via the adhoc.publish capability subject and returns the minted
// or reused handle (ADR-0134 SD2). It is how an in-process app (e.g. an
// embedder) drives the capability without holding a Service reference; the
// caller's bus client needs Pub on adhoc.publish. The publisher is
// attributed to the authenticated sender by the service, so in.Publisher
// is ignored on this path.
func PublishRequest(bus app.BusI, in PublishInput) (res PublishResult, err error) {
	payload, err := buscodec.Encode(wirePublishReq{
		V:              wireVersion,
		Alias:          in.Alias,
		Handle:         in.Handle,
		ArrowIPCStream: in.ArrowIPCStream,
	})
	if err != nil {
		return res, eh.Errorf("adhocdata: encode publish: %w", err)
	}
	replyBytes, err := bus.Request(SubjectPublish, payload)
	if err != nil {
		return res, eh.Errorf("adhocdata: publish request: %w", err)
	}
	rep, err := buscodec.Decode[wirePublishRep](replyBytes)
	if err != nil {
		return res, eh.Errorf("adhocdata: decode publish reply: %w", err)
	}
	if !rep.OK {
		return res, eh.Errorf("adhocdata: publish rejected: %s", rep.Error)
	}
	return PublishResult{Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows, Bytes: rep.Bytes}, nil
}

// RetractRequest retracts a dataset via the adhoc.retract subject.
func RetractRequest(bus app.BusI, handle string) (err error) {
	payload, err := buscodec.Encode(wireRetractReq{V: wireVersion, Handle: handle})
	if err != nil {
		return eh.Errorf("adhocdata: encode retract: %w", err)
	}
	replyBytes, err := bus.Request(SubjectRetract, payload)
	if err != nil {
		return eh.Errorf("adhocdata: retract request: %w", err)
	}
	rep, err := buscodec.Decode[wireRetractRep](replyBytes)
	if err != nil {
		return eh.Errorf("adhocdata: decode retract reply: %w", err)
	}
	if !rep.OK {
		return eh.Errorf("adhocdata: retract rejected: %s", rep.Error)
	}
	return nil
}
