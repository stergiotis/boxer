package adhocdata

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// ResolveRequest maps a stable alias to the newest live dataset published
// under it via the adhoc.resolve subject (ADR-0134 §SD4, update
// 2026-08-01). It is how a standalone applet binds its declared
// `datasets:` aliases at open; the caller's bus client needs Pub on
// adhoc.resolve.
func ResolveRequest(bus app.BusI, alias string) (res ResolveResult, err error) {
	payload, err := buscodec.Encode(wireResolveReq{V: wireVersion, Alias: alias})
	if err != nil {
		return res, eh.Errorf("adhocdata: encode resolve: %w", err)
	}
	replyBytes, err := bus.Request(SubjectResolve, payload)
	if err != nil {
		return res, eh.Errorf("adhocdata: resolve request: %w", err)
	}
	rep, err := buscodec.Decode[wireResolveRep](replyBytes)
	if err != nil {
		return res, eh.Errorf("adhocdata: decode resolve reply: %w", err)
	}
	if !rep.OK {
		return res, eh.Errorf("adhocdata: resolve rejected: %s", rep.Error)
	}
	return ResolveResult{
		Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows,
		Bytes: rep.Bytes, CreatedAtUnixUs: rep.CreatedAtUnixUs,
	}, nil
}

// ResolveVerifyRequest is ResolveRequest with a second question in the same
// round trip: is boundHandle still live? A consumer bound to boundHandle
// reconciles its binding with it (ADR-0188 §SD3): boundLive true means keep
// the binding whatever the alias's newest dataset is (an open applet does
// not re-resolve to a newer sibling, ADR-0134); false means the handle has
// left, and res — when err is nil — is the successor to bind, or when err
// is set there is nothing live under the alias yet. err is set only for
// transport failures and for "no live dataset under alias"; boundLive is
// meaningful in both cases.
func ResolveVerifyRequest(bus app.BusI, alias string, boundHandle string) (res ResolveResult, boundLive bool, err error) {
	payload, err := buscodec.Encode(wireResolveReq{V: wireVersion, Alias: alias, Handle: boundHandle})
	if err != nil {
		err = eh.Errorf("adhocdata: encode resolve: %w", err)
		return
	}
	replyBytes, err := bus.Request(SubjectResolve, payload)
	if err != nil {
		err = eh.Errorf("adhocdata: resolve request: %w", err)
		return
	}
	rep, err := buscodec.Decode[wireResolveRep](replyBytes)
	if err != nil {
		err = eh.Errorf("adhocdata: decode resolve reply: %w", err)
		return
	}
	boundLive = rep.HandleLive
	if !rep.OK {
		if rep.NoLive {
			err = eb.Build().Str("alias", alias).Errorf("adhocdata: resolve: %w", ErrNoLiveDataset)
			return
		}
		err = eh.Errorf("adhocdata: resolve rejected: %s", rep.Error)
		return
	}
	res = ResolveResult{
		Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows,
		Bytes: rep.Bytes, CreatedAtUnixUs: rep.CreatedAtUnixUs,
	}
	return
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

// SubscribeEvents delivers every dataset transition the service publishes
// (ADR-0188 §SD3) to handler, decoded. The caller's bus must carry
// `Sub adhoc.event.>`; the returned unsubscribe releases the subscription
// (the host releases it at the closing edge as well). Payloads that fail
// to decode are dropped with a log line by the caller's own choosing —
// handler is only ever invoked with a well-formed Event.
func SubscribeEvents(bus app.BusI, handler func(ev Event)) (unsubscribe func(), err error) {
	if bus == nil {
		err = eh.Errorf("adhocdata: subscribe events: nil bus")
		return
	}
	unsubscribe, err = bus.Subscribe(SubjectEventAll, func(msg *app.Msg) {
		ev, dErr := DecodeEvent(msg.Subject, msg.Payload)
		if dErr != nil {
			return
		}
		handler(ev)
	})
	if err != nil {
		err = eh.Errorf("adhocdata: subscribe events: %w", err)
	}
	return
}
