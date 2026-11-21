package appstate

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstatereply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/appstaterequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Client is the delete seam as a manager app sees it: two verbs over a bus
// client holding [ClientCaps], answered by the host's [Service]. The zero
// value is unusable; take one from [NewClient] with the bus the host minted
// for the app (its MountContextI.Bus()).
type Client struct {
	bus app.BusI
	// Timeout bounds one request; zero is the bus's default.
	Timeout time.Duration
}

// deferred: ADR-0396 §9 makes a request/reply client free functions taking
// ctx and app.BusI first, with one capability helper per capability; this
// client type stays until the package is tiered Stable.

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// Outcome is one kind's result within a reply.
type Outcome struct {
	Kind    string
	Cleared uint64
	Failed  uint64
}

// Result is a reply as a Go value. Ok is true when every attempted delete
// landed; Reason says why not, whether the service refused before
// attempting anything or some deletes failed.
type Result struct {
	Ok       bool
	Reason   string
	Outcomes []Outcome
}

// Delete clears one entry, named as keelson('app_state') shows it: kind and
// key, or — for kind `unknown` — entityId. A refusal is a Result with Ok
// false, not an error; err is a transport or codec failure.
func (inst *Client) Delete(ctx context.Context, appId app.AppIdT, kind string, key string, entityId string) (res Result, err error) {
	return inst.call(ctx, appstaterequest.AppStateRequest{
		Op: appstaterequest.OpDelete, AppId: string(appId), Kind: kind, Key: key, EntityId: entityId,
	})
}

// Forget clears every entry appId keeps, of every kind, and reports per
// kind what was cleared and what failed.
func (inst *Client) Forget(ctx context.Context, appId app.AppIdT) (res Result, err error) {
	return inst.call(ctx, appstaterequest.AppStateRequest{Op: appstaterequest.OpForget, AppId: string(appId)})
}

func (inst *Client) call(ctx context.Context, req appstaterequest.AppStateRequest) (res Result, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("appstate: client without a bus")
	}
	req.At = time.Now().UTC()
	payload, err := buscodec.Encode(req)
	if err != nil {
		return res, eh.Errorf("encode request: %w", err)
	}
	raw, err := app.RequestWithin(ctx, inst.bus, inst.Timeout, Subject(req.Op), payload)
	if err != nil {
		return res, eb.Build().Str("op", req.Op).Errorf("appstate request: %w", err)
	}
	r, err := buscodec.Decode[appstatereply.AppStateReply](raw)
	if err != nil {
		return res, eh.Errorf("decode reply: %w", err)
	}
	res = ResultOf(r)
	return
}

// ResultOf zips a reply's outcome columns. A reply whose columns disagree
// in length is truncated to the shortest rather than read out of range.
func ResultOf(r appstatereply.AppStateReply) (res Result) {
	res = Result{Ok: r.Ok, Reason: r.Reason}
	n := min(len(r.Kind), len(r.Cleared), len(r.Failed))
	for i := 0; i < n; i++ {
		res.Outcomes = append(res.Outcomes, Outcome{Kind: r.Kind[i], Cleared: r.Cleared[i], Failed: r.Failed[i]})
	}
	return
}
