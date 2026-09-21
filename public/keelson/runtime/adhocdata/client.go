package adhocdata

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// PublishRequest publishes — or, with in.Handle set, republishes — a
// dataset via the adhoc.publish capability subject and returns the minted
// or reused handle (ADR-0134 SD2). It is how an in-process app (e.g. an
// embedder) drives the capability without holding a Service reference; the
// caller's bus client needs Pub on adhoc.publish. The publisher is the
// envelope's sender and instance; in.By is ignored on this path.
func PublishRequest(bus app.BusI, in PublishInput) (res PublishResult, err error) {
	rep, err := request(bus, SubjectPublish, adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpPublish, Alias: in.Alias, Handle: in.Handle,
		KeepAfterClose: in.KeepAfterClose, ArrowStream: in.ArrowIPCStream,
	}, "publish")
	if err != nil {
		return
	}
	return PublishResult{Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows, Bytes: rep.Bytes}, nil
}

// request encodes req, sends it on subject and decodes the reply; a reply
// with Ok false comes back as an error carrying its reason.
func request(bus app.BusI, subject string, req adhocrequest.AdhocRequest, verb string) (rep adhocreply.AdhocReply, err error) {
	payload, err := buscodec.Encode(req)
	if err != nil {
		return rep, eb.Build().Str("verb", verb).Errorf("adhocdata: encode request: %w", err)
	}
	replyBytes, err := bus.Request(subject, payload)
	if err != nil {
		return rep, eb.Build().Str("verb", verb).Errorf("adhocdata: request: %w", err)
	}
	rep, err = buscodec.Decode[adhocreply.AdhocReply](replyBytes)
	if err != nil {
		return rep, eb.Build().Str("verb", verb).Errorf("adhocdata: decode reply: %w", err)
	}
	if !rep.Ok {
		return rep, eb.Build().Str("verb", verb).Str("reason", rep.Reason).Errorf("adhocdata: request rejected")
	}
	return rep, nil
}

// ResolveRequest maps a stable alias to the newest live dataset published
// under it via the adhoc.resolve subject (ADR-0134 §SD4, update
// 2026-08-01). It is how a standalone applet binds its declared
// `datasets:` aliases at open; the caller's bus client needs Pub on
// adhoc.resolve.
func ResolveRequest(bus app.BusI, alias string) (res ResolveResult, err error) {
	res, _, err = resolve(bus, alias, "")
	return
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
	return resolve(bus, alias, boundHandle)
}

// resolve is the shared body of the two resolve helpers: a typed
// ErrNoLiveDataset for "nothing under the alias", a transport or refusal
// error otherwise, and the bound handle's liveness whenever the service
// answered.
func resolve(bus app.BusI, alias string, boundHandle string) (res ResolveResult, boundLive bool, err error) {
	payload, err := buscodec.Encode(adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpResolve, Alias: alias, Handle: boundHandle,
	})
	if err != nil {
		return res, false, eh.Errorf("adhocdata: encode resolve: %w", err)
	}
	replyBytes, err := bus.Request(SubjectResolve, payload)
	if err != nil {
		return res, false, eh.Errorf("adhocdata: resolve request: %w", err)
	}
	rep, err := buscodec.Decode[adhocreply.AdhocReply](replyBytes)
	if err != nil {
		return res, false, eh.Errorf("adhocdata: decode resolve reply: %w", err)
	}
	boundLive = rep.HandleLive
	if !rep.Ok {
		if rep.NoLive {
			return res, boundLive, eb.Build().Str("alias", alias).Errorf("adhocdata: resolve: %w", ErrNoLiveDataset)
		}
		return res, boundLive, eb.Build().Str("reason", rep.Reason).Errorf("adhocdata: resolve rejected")
	}
	res = ResolveResult{Handle: rep.Handle, Revision: rep.Revision, Rows: rep.Rows, Bytes: rep.Bytes, CreatedAtUnixUs: rep.CreatedAtUs}
	return res, boundLive, nil
}

// RetractRequest retracts a dataset via the adhoc.retract subject.
func RetractRequest(bus app.BusI, handle string) (err error) {
	_, err = request(bus, SubjectRetract, adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpRetract, Handle: handle,
	}, "retract")
	return
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
