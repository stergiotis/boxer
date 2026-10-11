package headstore

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
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// ServiceAppId is the identity the host's anchor client holds on the bus.
const ServiceAppId app.AppIdT = "runtime.anchor"

// The family (ADR-0314, a dated Update on ADR-0026 §SD3): append a head,
// read a scope's heads. Both are scoped to the scope's owner: the app the
// journal store names as the run's owner when the service has one
// (Owners), else the sender that anchored the scope first; another sender
// is refused, so no app pre-anchors or reads another's run. The receipt
// names the sender. A head already held for an ordinal under its scheme
// is answered with its first receipt, and another digest there is
// refused: the earliest anchor counts.
const (
	SubjectPrefix = "runtime.anchor."
	SubjectAll    = "runtime.anchor.*"
	SubjectAppend = SubjectPrefix + "append"
	SubjectRead   = SubjectPrefix + "read"
)

// DefaultTimeout bounds one request.
const DefaultTimeout = 30 * time.Second

// wireVersion 2 carries the scope and the scheme (ADR-0327 §SD7).
const wireVersion = 2

type wireHead struct {
	V       uint8  `json:"v"`
	Cell    string `json:"cell,omitempty"`
	Scope   uint64 `json:"scope"`
	Ordinal uint64 `json:"ordinal"`
	Scheme  string `json:"scheme"`
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
	V     uint8  `json:"v"`
	Cell  string `json:"cell,omitempty"`
	Scope uint64 `json:"scope"`
}

type wireAnchored struct {
	Cell      string    `json:"cell,omitempty"`
	Scope     uint64    `json:"scope"`
	Ordinal   uint64    `json:"ordinal"`
	Scheme    string    `json:"scheme"`
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
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "headstore: serve runtime.anchor requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "headstore: reply to inboxes"},
	}
}

// ClientCaps are what a writer declares to ship and read heads.
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	return []app.SubjectFilter{{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true}}
}

// Service answers runtime.anchor.* over an anchor store: the host's client
// of the organisation's store.
type Service struct {
	store     anchor.StoreI
	busClient *inprocbus.Client
	unsub     func()
	closeOnce sync.Once
	log       zerolog.Logger
	Timeout   time.Duration
	// Owners resolves a scope to the app that owns it; set by the host
	// over the journal store (RunOwners), where a scope is a run. Without
	// it the first sender to anchor a scope is taken as its owner.
	Owners OwnerOfI
}

// OwnerOfI resolves a scope to the app that owns it.
type OwnerOfI interface {
	OwnerOf(ctx context.Context, scope uint64) (owner string, found bool, err error)
}

// RunOwners is OwnerOfI over the journal store: the run row's owner app.
func RunOwners(st agentrun.StoreI) OwnerOfI { return runOwners{st: st} }

type runOwners struct{ st agentrun.StoreI }

func (inst runOwners) OwnerOf(ctx context.Context, run uint64) (owner string, found bool, err error) {
	r, found, err := inst.st.GetRun(ctx, run)
	if err != nil || !found {
		return
	}
	return r.OwnerApp, true, nil
}

// allowed says the sender may touch the scope's heads: it is the scope's
// owner as the resolver names it, or, without a resolver, the sender the
// scope's first head came from (a scope no one anchored is anyone's to
// anchor first).
func (inst *Service) allowed(ctx context.Context, cell string, scope uint64, sender string) (ok bool, why string, err error) {
	if inst.Owners != nil {
		owner, found, oerr := inst.Owners.OwnerOf(ctx, scope)
		if oerr != nil {
			return false, "", eb.Build().Uint64("scope", scope).Errorf("resolve the scope's owner: %w", oerr)
		}
		if !found {
			return false, "no such run", nil
		}
		if owner != sender {
			return false, "another app's run", nil
		}
		return true, "", nil
	}
	heads, err := inst.store.Read(ctx, cell, scope)
	if err != nil {
		return false, "", eb.Build().Uint64("scope", scope).Errorf("read heads: %w", err)
	}
	for _, a := range heads {
		if a.Receipt.By != "" && a.Receipt.By != sender {
			return false, "another app's run", nil
		}
	}
	return true, "", nil
}

// NewService subscribes a service over store; the caller closes it.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, store anchor.StoreI) (s *Service, err error) {
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
		if req.Scope == 0 || req.Digest == "" || req.Scheme == "" {
			inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: "a head names its scope, scheme and digest"})
			return
		}
		h := anchor.Head{Cell: req.Cell, Scope: req.Scope, Ordinal: req.Ordinal, Scheme: req.Scheme, Digest: req.Digest}
		if ok, why, aerr := inst.allowed(ctx, h.Cell, h.Scope, string(msg.Sender)); aerr != nil || !ok {
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
			if m, ok := inst.store.(*anchor.MemStore); ok {
				r, err = m.AppendBy(h, string(msg.Sender))
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
		if ok, why, aerr := inst.allowed(ctx, req.Cell, req.Scope, string(msg.Sender)); aerr != nil || !ok {
			if aerr != nil {
				why = aerr.Error()
			}
			inst.reply(msg.Reply, wireReadReply{V: wireVersion, Reason: why})
			return
		}
		heads, err := inst.store.Read(ctx, req.Cell, req.Scope)
		if err != nil {
			inst.reply(msg.Reply, wireReadReply{V: wireVersion, Reason: err.Error()})
			return
		}
		rep := wireReadReply{V: wireVersion, Ok: true}
		for _, a := range heads {
			rep.Heads = append(rep.Heads, wireAnchored{Cell: a.Cell, Scope: a.Scope, Ordinal: a.Ordinal, Scheme: a.Scheme, Digest: a.Digest, ReceiptId: a.Receipt.Id, ReceiptAt: a.Receipt.At, By: a.Receipt.By, Durable: a.Receipt.Durable})
		}
		inst.reply(msg.Reply, rep)
	default:
		inst.reply(msg.Reply, wireReceipt{V: wireVersion, Reason: "no such verb"})
	}
}

// held is the earliest receipt the store holds for the head's ordinal
// under its scheme, when it holds one; a different digest there is an
// error.
func (inst *Service) held(ctx context.Context, h anchor.Head) (r Receipt, held bool, err error) {
	heads, err := inst.store.Read(ctx, h.Cell, h.Scope)
	if err != nil {
		err = eb.Build().Uint64("scope", h.Scope).Errorf("read heads: %w", err)
		return
	}
	for _, a := range heads {
		if a.Ordinal != h.Ordinal || a.Scheme != h.Scheme {
			continue
		}
		if held && a.Receipt.At.Compare(r.At) >= 0 {
			continue
		}
		if a.Digest != h.Digest {
			err = eb.Build().Uint64("scope", h.Scope).Uint64("ordinal", h.Ordinal).Str("scheme", h.Scheme).Errorf("another digest is anchored at the ordinal")
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

// Client is the anchor store over the bus: anchor.StoreI for any scope,
// and the journal's StoreI through Append and Read on Head.
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

// AppendHead anchors any scope's head.
func (inst *Client) AppendHead(ctx context.Context, h anchor.Head) (r Receipt, err error) {
	rep, err := roundTrip[wireHead, wireReceipt](ctx, inst, SubjectAppend, wireHead{V: wireVersion, Cell: h.Cell, Scope: h.Scope, Ordinal: h.Ordinal, Scheme: h.Scheme, Digest: h.Digest})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	return Receipt{Id: rep.Id, At: rep.At, By: rep.By, Durable: rep.Durable}, nil
}

// ReadScope reads a scope's heads under every scheme.
func (inst *Client) ReadScope(ctx context.Context, cell string, scope uint64) (heads []anchor.Anchored, err error) {
	rep, err := roundTrip[wireRead, wireReadReply](ctx, inst, SubjectRead, wireRead{V: wireVersion, Cell: cell, Scope: scope})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	for _, w := range rep.Heads {
		heads = append(heads, anchor.Anchored{Head: anchor.Head{Cell: w.Cell, Scope: w.Scope, Ordinal: w.Ordinal, Scheme: w.Scheme, Digest: w.Digest}, Receipt: Receipt{Id: w.ReceiptId, At: w.ReceiptAt, By: w.By, Durable: w.Durable}})
	}
	return
}

// Append anchors a journal head: the run's scope under the journal's
// scheme.
func (inst *Client) Append(ctx context.Context, h Head) (r Receipt, err error) {
	return inst.AppendHead(ctx, anchorHead(h))
}

// Read is the run's journal heads.
func (inst *Client) Read(ctx context.Context, cell string, run uint64) (heads []Anchored, err error) {
	rows, err := inst.ReadScope(ctx, cell, run)
	if err != nil {
		return
	}
	return journalHeads(rows), nil
}
