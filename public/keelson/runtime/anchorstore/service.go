package anchorstore

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ServiceAppId is the identity the host's anchor client holds on the bus.
const ServiceAppId app.AppIdT = "runtime.anchor"

// The family (ADR-0314, a dated Update on ADR-0026 §SD3): append a head,
// read a run's heads. Both are scoped to the run's owner: the app the
// journal store names as the run's owner when the service has one
// (Owners), else the sender that anchored the run first; another sender
// is refused, so no app pre-anchors or reads another's run. The receipt
// names the sender. A head already held for an ordinal is answered with
// its first receipt, and another digest for that ordinal is refused: the
// earliest anchor counts.
const (
	SubjectPrefix = "runtime.anchor."
	SubjectAll    = "runtime.anchor.*"
	SubjectAppend = SubjectPrefix + "append"
	SubjectRead   = SubjectPrefix + "read"
)

// DefaultTimeout bounds one request.
const DefaultTimeout = 30 * time.Second

const wireVersion = 1

type wireHead struct {
	V       uint8  `json:"v"`
	Cell    string `json:"cell,omitempty"`
	Run     string `json:"run"`
	Ordinal uint32 `json:"ordinal"`
	Digest  string `json:"digest"`
}

type wireReceipt struct {
	V       uint8     `json:"v"`
	Ok      bool      `json:"ok"`
	Reason  string    `json:"reason,omitempty"`
	Id      string    `json:"id,omitempty"`
	At      time.Time `json:"at"`
	By      string    `json:"by,omitempty"`
	Durable bool      `json:"durable,omitempty"`
}

type wireRead struct {
	V    uint8  `json:"v"`
	Cell string `json:"cell,omitempty"`
	Run  string `json:"run"`
}

type wireAnchored struct {
	Cell      string    `json:"cell,omitempty"`
	Run       string    `json:"run"`
	Ordinal   uint32    `json:"ordinal"`
	Digest    string    `json:"digest"`
	ReceiptId string    `json:"receipt_id"`
	ReceiptAt time.Time `json:"receipt_at"`
	By        string    `json:"by,omitempty"`
	Durable   bool      `json:"durable,omitempty"`
}

type wireReadReply struct {
	V      uint8          `json:"v"`
	Ok     bool           `json:"ok"`
	Reason string         `json:"reason,omitempty"`
	Heads  []wireAnchored `json:"heads,omitempty"`
}

// ServiceCaps are the service's grants.
func ServiceCaps() (caps []app.SubjectFilter) {
	return []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "anchorstore: serve runtime.anchor requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "anchorstore: reply to inboxes"},
	}
}

// ClientCaps are what a writer declares to ship and read heads.
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	return []app.SubjectFilter{{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true}}
}

// Service answers runtime.anchor.* over a store: the host's client of the
// organisation's store.
type Service struct {
	store     StoreI
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger
	Timeout   time.Duration
	// Owners resolves a run to the app that owns it; set by the host
	// over the journal store (RunOwners). Without it the first sender to
	// anchor a run is taken as its owner.
	Owners OwnerOfI
}

// OwnerOfI resolves a run id to the app that owns it.
type OwnerOfI interface {
	OwnerOf(ctx context.Context, run string) (owner string, found bool, err error)
}

// RunOwners is OwnerOfI over the journal store: the run row's owner app.
func RunOwners(st agentrun.StoreI) OwnerOfI { return runOwners{st: st} }

type runOwners struct{ st agentrun.StoreI }

func (inst runOwners) OwnerOf(ctx context.Context, run string) (owner string, found bool, err error) {
	r, found, err := inst.st.GetRun(ctx, run)
	if err != nil || !found {
		return
	}
	return r.OwnerApp, true, nil
}

// allowed says the sender may touch the run's heads: it is the run's
// owner as the journal store names it, or, without a resolver, the
// sender the run's first head came from (a run no one anchored is
// anyone's to anchor first).
func (inst *Service) allowed(ctx context.Context, cell string, run string, sender string) (ok bool, why string, err error) {
	if inst.Owners != nil {
		owner, found, oerr := inst.Owners.OwnerOf(ctx, run)
		if oerr != nil {
			return false, "", eb.Build().Str("run", run).Errorf("resolve the run's owner: %w", oerr)
		}
		if !found {
			return false, "no such run", nil
		}
		if owner != sender {
			return false, "another app's run", nil
		}
		return true, "", nil
	}
	heads, err := inst.store.Read(ctx, cell, run)
	if err != nil {
		return false, "", eb.Build().Str("run", run).Errorf("read heads: %w", err)
	}
	for _, a := range heads {
		if a.Receipt.By != "" && a.Receipt.By != sender {
			return false, "another app's run", nil
		}
	}
	return true, "", nil
}

// NewService subscribes a service over store; the caller closes it.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, store StoreI) (s *Service, err error) {
	if bus == nil || store == nil {
		err = eh.Errorf("nil bus or store")
		return
	}
	s = &Service{store: store, log: log.With().Str("app", string(ServiceAppId)).Logger(), Timeout: DefaultTimeout}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	if s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handle); err != nil {
		_ = s.busClient.Close()
		err = eh.Errorf("subscribe: %w", err)
		return nil, err
	}
	return
}

// Close releases the subscription and the bus client; a handler still
// running finds the client closed, not gone.
func (inst *Service) Close() {
	inst.closeOnce.Do(func() {
		if inst.unsub != nil {
			inst.unsub()
		}
		_ = inst.busClient.Close()
	})
}

func (inst *Service) handle(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), inst.Timeout)
	defer cancel()
	switch msg.Subject {
	case SubjectAppend:
		req, err := buscodec.Decode[wireHead](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: err.Error()})
			return
		}
		if req.Run == "" || req.Digest == "" {
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: "a head names its run and digest"})
			return
		}
		h := Head{Cell: req.Cell, Run: req.Run, Ordinal: req.Ordinal, Digest: req.Digest}
		if ok, why, aerr := inst.allowed(ctx, h.Cell, h.Run, string(msg.Sender)); aerr != nil || !ok {
			if aerr != nil {
				why = aerr.Error()
			}
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: why})
			return
		}
		r, held, err := inst.held(ctx, h)
		if err != nil {
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: err.Error()})
			return
		}
		if !held {
			if m, ok := inst.store.(*MemStore); ok {
				r, err = m.appendBy(h, string(msg.Sender))
			} else {
				r, err = inst.store.Append(ctx, h)
				r.By = string(msg.Sender)
			}
		}
		if err != nil {
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: err.Error()})
			return
		}
		inst.reply(msg.Reply, wireReceipt{V: wireVersion, Ok: true, Id: r.Id, At: r.At, By: r.By, Durable: r.Durable})
	case SubjectRead:
		req, err := buscodec.Decode[wireRead](msg.Payload)
		if err != nil {
			inst.reply(msg.Reply, wireReadReply{V: wireVersion, Reason: err.Error()})
			return
		}
		if ok, why, aerr := inst.allowed(ctx, req.Cell, req.Run, string(msg.Sender)); aerr != nil || !ok {
			if aerr != nil {
				why = aerr.Error()
			}
			inst.reply(msg.Reply, wireReadReply{V: wireVersion, Reason: why})
			return
		}
		heads, err := inst.store.Read(ctx, req.Cell, req.Run)
		if err != nil {
			inst.reply(msg.Reply, wireReadReply{V: wireVersion, Reason: err.Error()})
			return
		}
		rep := wireReadReply{V: wireVersion, Ok: true}
		for _, a := range heads {
			rep.Heads = append(rep.Heads, wireAnchored{Cell: a.Cell, Run: a.Run, Ordinal: a.Ordinal, Digest: a.Digest, ReceiptId: a.Receipt.Id, ReceiptAt: a.Receipt.At, By: a.Receipt.By, Durable: a.Receipt.Durable})
		}
		inst.reply(msg.Reply, rep)
	default:
		inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: "no such verb"})
	}
}

// held is the earliest receipt the store holds for the head's ordinal,
// when it holds one; a different digest at that ordinal is an error.
func (inst *Service) held(ctx context.Context, h Head) (r Receipt, held bool, err error) {
	heads, err := inst.store.Read(ctx, h.Cell, h.Run)
	if err != nil {
		err = eb.Build().Str("run", h.Run).Errorf("read heads: %w", err)
		return
	}
	for _, a := range heads {
		if a.Ordinal != h.Ordinal {
			continue
		}
		if held && a.Receipt.At.Compare(r.At) >= 0 {
			continue
		}
		if a.Digest != h.Digest {
			err = eb.Build().Str("run", h.Run).Uint32("ordinal", h.Ordinal).Errorf("another digest is anchored at the ordinal")
			return
		}
		r, held = a.Receipt, true
	}
	return
}

func (inst *Service) reply(subject string, v any) {
	if err := buscodec.Reply(inst.busClient.Publish, subject, v); err != nil {
		inst.log.Warn().Err(err).Str("subject", subject).Msg("reply failed")
	}
}

// Client is StoreI over the bus.
type Client struct {
	bus     app.BusI
	Timeout time.Duration
}

var _ StoreI = (*Client)(nil)

// NewClient wraps bus; the caller's manifest declares ClientCaps.
func NewClient(bus app.BusI) (inst *Client) { return &Client{bus: bus} }

// RefusedError is a request the service declined.
type RefusedError struct {
	Reason string
}

func (inst *RefusedError) Error() string { return "refused: " + inst.Reason }

func (inst *Client) wait(ctx context.Context) (d time.Duration) {
	d = inst.Timeout
	if d <= 0 {
		d = DefaultTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		if until := time.Until(deadline); until < d {
			d = until
		}
	}
	return
}

func roundTrip[Req any, Rep any](ctx context.Context, inst *Client, subject string, req Req) (rep Rep, err error) {
	if inst == nil || inst.bus == nil {
		err = eh.Errorf("client without a bus")
		return
	}
	if err = ctx.Err(); err != nil {
		err = eb.Build().Str("subject", subject).Errorf("before the request: %w", err)
		return
	}
	payload, err := buscodec.Encode(req)
	if err != nil {
		err = eh.Errorf("encode: %w", err)
		return
	}
	raw, err := inst.bus.RequestWithTimeout(subject, payload, inst.wait(ctx))
	if err != nil {
		err = eb.Build().Str("subject", subject).Errorf("request: %w", err)
		return
	}
	if rep, err = buscodec.Decode[Rep](raw); err != nil {
		err = eh.Errorf("decode: %w", err)
	}
	return
}

func (inst *Client) Append(ctx context.Context, h Head) (r Receipt, err error) {
	rep, err := roundTrip[wireHead, wireReceipt](ctx, inst, SubjectAppend, wireHead{V: wireVersion, Cell: h.Cell, Run: h.Run, Ordinal: h.Ordinal, Digest: h.Digest})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	return Receipt{Id: rep.Id, At: rep.At, By: rep.By, Durable: rep.Durable}, nil
}

func (inst *Client) Read(ctx context.Context, cell string, run string) (heads []Anchored, err error) {
	rep, err := roundTrip[wireRead, wireReadReply](ctx, inst, SubjectRead, wireRead{V: wireVersion, Cell: cell, Run: run})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	for _, w := range rep.Heads {
		heads = append(heads, Anchored{Head: Head{Cell: w.Cell, Run: w.Run, Ordinal: w.Ordinal, Digest: w.Digest}, Receipt: Receipt{Id: w.ReceiptId, At: w.ReceiptAt, By: w.By, Durable: w.Durable}})
	}
	return
}
