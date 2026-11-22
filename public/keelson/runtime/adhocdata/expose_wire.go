package adhocdata

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Exposure subjects (ADR-0407 §SD4), request/reply. The window asking is
// the envelope's; an agent's call is named by task, epoch and call on the
// request and attested with the dispatcher. The wire is a versioned CBOR
// DTO pair.
const (
	// SubjectExpose exposes a dataset; the reply carries its token.
	SubjectExpose = "runtime.adhoc.expose"
	// SubjectExposeRevoke ends an exposure.
	SubjectExposeRevoke = "runtime.adhoc.expose.revoke"
	// SubjectExposeList lists the live exposures, never their tokens.
	SubjectExposeList = "runtime.adhoc.expose.list"
)

// exposeWireVersion is the version of the request and reply below.
const exposeWireVersion = 1

// exposeWireRequest is every exposure request's wire form.
type exposeWireRequest struct {
	V               uint8  `json:"v"`
	Alias           string `json:"alias,omitempty"`
	LifetimeSeconds int64  `json:"lifetimeSeconds,omitempty"`
	Id              string `json:"id,omitempty"`
	OboTask         string `json:"oboTask,omitempty"`
	OboEpoch        uint64 `json:"oboEpoch,omitempty"`
	OboCall         string `json:"oboCall,omitempty"`
}

// exposureWire is one exposure on the wire. Token, ClickHouse and Curl are
// set only in the reply to an exposure.
type exposureWire struct {
	Id          string `json:"id"`
	Alias       string `json:"alias"`
	Bundle      string `json:"bundle,omitempty"`
	Revision    uint64 `json:"revision,omitempty"`
	Structure   string `json:"structure,omitempty"`
	Rows        uint64 `json:"rows,omitempty"`
	Bytes       uint64 `json:"bytes,omitempty"`
	URL         string `json:"url,omitempty"`
	CSVURL      string `json:"csvUrl,omitempty"`
	CreatedUs   int64  `json:"createdUs,omitempty"`
	ExpiresUs   int64  `json:"expiresUs,omitempty"`
	Reads       uint64 `json:"reads,omitempty"`
	BytesServed uint64 `json:"bytesServed,omitempty"`
	ByApp       string `json:"byApp,omitempty"`
	ByInstance  uint64 `json:"byInstance,omitempty"`
	Task        string `json:"task,omitempty"`
	Yours       bool   `json:"yours,omitempty"`
	Token       string `json:"token,omitempty"`
	ClickHouse  string `json:"clickhouse,omitempty"`
	Curl        string `json:"curl,omitempty"`
}

// exposeWireReply is every exposure reply's wire form.
type exposeWireReply struct {
	V           uint8          `json:"v"`
	Ok          bool           `json:"ok"`
	Reason      string         `json:"reason,omitempty"`
	NoLive      bool           `json:"noLive,omitempty"`
	Destination string         `json:"destination,omitempty"`
	Exposure    exposureWire   `json:"exposure"`
	Exposures   []exposureWire `json:"exposures,omitempty"`
}

func toExposureWire(e ExposureInfo) (w exposureWire) {
	return exposureWire{
		Id: e.Id, Alias: e.Alias, Bundle: e.Bundle, Revision: e.Revision, Structure: e.Structure, Rows: e.Rows, Bytes: e.Bytes,
		URL: e.URL, CSVURL: e.CSVURL, CreatedUs: e.CreatedAt.UnixMicro(), ExpiresUs: e.ExpiresAt.UnixMicro(),
		Reads: e.Reads, BytesServed: e.BytesServed, ByApp: string(e.By.App), ByInstance: e.By.Instance, Task: e.Task, Yours: e.Yours,
	}
}

func fromExposureWire(w exposureWire) (e ExposureInfo) {
	return ExposureInfo{
		Id: w.Id, Alias: w.Alias, Bundle: w.Bundle, Revision: w.Revision, Structure: w.Structure, Rows: w.Rows, Bytes: w.Bytes,
		URL: w.URL, CSVURL: w.CSVURL, CreatedAt: time.UnixMicro(w.CreatedUs), ExpiresAt: time.UnixMicro(w.ExpiresUs),
		Reads: w.Reads, BytesServed: w.BytesServed, By: Identity{App: app.AppIdT(w.ByApp), Instance: w.ByInstance}, Task: w.Task, Yours: w.Yours,
	}
}

// exposeRefusal is the reply to a refused exposure request.
func exposeRefusal(err error) (rep exposeWireReply) {
	rep = exposeWireReply{V: exposeWireVersion, Reason: err.Error(), NoLive: errors.Is(err, ErrNoLiveDataset)}
	var ge *GrantError
	if errors.As(err, &ge) {
		rep.Destination = ge.Destination
	}
	return
}

// replyExpose encodes rep and publishes it to the caller's inbox.
func (inst *Service) replyExpose(replySubject string, rep exposeWireReply) {
	payload, err := buscodec.Encode(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("adhocdata: encode exposure reply")
		return
	}
	if pubErr := inst.busClient.Publish(replySubject, payload); pubErr != nil {
		inst.log.Warn().Err(pubErr).Str("reply", replySubject).Msg("adhocdata: publish exposure reply")
	}
}

// handleExposure answers the exposure subjects.
func (inst *Service) handleExposure(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("adhocdata: request without reply inbox")
		return
	}
	req, err := buscodec.Decode[exposeWireRequest](msg.Payload)
	if err == nil && req.V != exposeWireVersion {
		err = eb.Build().Uint64("version", uint64(req.V)).Errorf("unknown exposure wire version")
	}
	if err != nil {
		inst.replyExpose(msg.Reply, exposeRefusal(eh.Errorf("decode: %w", err)))
		return
	}
	ctx := context.WithoutCancel(msg.Context())
	obo := callContextFields(req.OboTask, req.OboEpoch, req.OboCall)
	rep := exposeWireReply{V: exposeWireVersion, Ok: true}
	switch msg.Subject {
	case SubjectExpose:
		res, xErr := inst.Expose(ctx, ExposeInput{Alias: req.Alias, Lifetime: time.Duration(req.LifetimeSeconds) * time.Second, By: sender(msg), OnBehalfOf: obo})
		if xErr != nil {
			rep = exposeRefusal(xErr)
			break
		}
		rep.Exposure = toExposureWire(res.ExposureInfo)
		rep.Exposure.Token, rep.Exposure.ClickHouse, rep.Exposure.Curl = res.Token, res.ClickHouse, res.Curl
	case SubjectExposeRevoke:
		if rErr := inst.RevokeExposure(ctx, req.Id, sender(msg), obo); rErr != nil {
			rep = exposeRefusal(rErr)
		}
	case SubjectExposeList:
		es, lErr := inst.Exposures(sender(msg), obo)
		if lErr != nil {
			rep = exposeRefusal(lErr)
			break
		}
		for _, e := range es {
			rep.Exposures = append(rep.Exposures, toExposureWire(e))
		}
	default:
		rep = exposeRefusal(eb.Build().Str("subject", msg.Subject).Errorf("unknown exposure subject"))
	}
	inst.replyExpose(msg.Reply, rep)
}

// exposeRequest sends req on subject and decodes the reply; a refusal comes
// back as an error: a GrantError when it names a destination, and
// ErrNoLiveDataset when nothing is live under the alias.
func exposeRequest(ctx context.Context, bus app.BusI, subject string, req exposeWireRequest, obo *app.OnBehalfOf) (rep exposeWireReply, err error) {
	req.V = exposeWireVersion
	req.OboTask, req.OboEpoch, req.OboCall = oboTask(obo), oboEpoch(obo), oboCall(obo)
	payload, err := buscodec.Encode(req)
	if err != nil {
		return rep, eh.Errorf("encode exposure request: %w", err)
	}
	raw, err := bus.Request(ctx, subject, payload)
	if err != nil {
		return rep, eb.Build().Str("subject", subject).Errorf("exposure request: %w", err)
	}
	rep, err = buscodec.Decode[exposeWireReply](raw)
	if err != nil {
		return rep, eh.Errorf("decode exposure reply: %w", err)
	}
	if !rep.Ok {
		switch {
		case rep.NoLive:
			err = eb.Build().Str("alias", req.Alias).Errorf("expose: %w", ErrNoLiveDataset)
		case rep.Destination != "":
			err = &GrantError{Destination: rep.Destination, Reason: strings.TrimPrefix(rep.Reason, "agent limit: ")}
		default:
			err = eb.Build().Str("subject", subject).Errorf("refused: %s", rep.Reason) //boxer:lint disable=CS013 reason="the service's refusal crosses the bus as text and is what a window shows"
		}
	}
	return
}

// ExposeRequest exposes the newest live dataset under in.Alias via
// runtime.adhoc.expose (ADR-0407). The answer is the only place the token
// is ever given. in.By is ignored: the window asking is the envelope's
// sender. The caller's bus client needs Pub on runtime.adhoc.expose.
func ExposeRequest(ctx context.Context, bus app.BusI, in ExposeInput) (res ExposeResult, err error) {
	rep, err := exposeRequest(ctx, bus, SubjectExpose, exposeWireRequest{Alias: in.Alias, LifetimeSeconds: int64(in.Lifetime / time.Second)}, in.OnBehalfOf)
	if err != nil {
		return
	}
	res = ExposeResult{ExposureInfo: fromExposureWire(rep.Exposure), Token: rep.Exposure.Token, ClickHouse: rep.Exposure.ClickHouse, Curl: rep.Exposure.Curl}
	return
}

// RevokeExposureRequest ends the exposure id via runtime.adhoc.expose.revoke.
func RevokeExposureRequest(ctx context.Context, bus app.BusI, id string, obo *app.OnBehalfOf) (err error) {
	_, err = exposeRequest(ctx, bus, SubjectExposeRevoke, exposeWireRequest{Id: id}, obo)
	return
}

// ExposuresRequest lists the live exposures via runtime.adhoc.expose.list.
func ExposuresRequest(ctx context.Context, bus app.BusI, obo *app.OnBehalfOf) (out []ExposureInfo, err error) {
	rep, err := exposeRequest(ctx, bus, SubjectExposeList, exposeWireRequest{}, obo)
	if err != nil {
		return
	}
	for _, w := range rep.Exposures {
		out = append(out, fromExposureWire(w))
	}
	return
}
