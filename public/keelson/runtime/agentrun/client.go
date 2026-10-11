package agentrun

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is StoreI over the bus: an app's view of the host's journal
// service, scoped to the app by the service. A Writer over it is the
// run's single writer in the app, with the record on the host.
type Client struct {
	bus app.BusI
	// Timeout bounds one request; zero is DefaultTimeout.
	Timeout time.Duration
}

var _ StoreI = (*Client)(nil)

// NewClient wraps bus; the app's manifest declares ClientCaps.
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
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.bus.RequestWithTimeout(subject, payload, inst.wait(ctx))
	if err != nil {
		err = eb.Build().Str("subject", subject).Errorf("request: %w", err)
		return
	}
	return decode[Rep](raw)
}

func ackOf(a wireAck, err error) error {
	if err != nil {
		return err
	}
	if !a.Ok {
		return &RefusedError{Reason: a.Reason}
	}
	return nil
}

func (inst *Client) CreateRun(ctx context.Context, r Run) (err error) {
	a, err := roundTrip[wireRunRequest, wireAck](ctx, inst, SubjectCreate, wireRunRequest{V: wireVersion, Run: wireOfRun(r)})
	return ackOf(a, err)
}

func (inst *Client) UpdateRun(ctx context.Context, r Run) (err error) {
	a, err := roundTrip[wireRunRequest, wireAck](ctx, inst, SubjectUpdate, wireRunRequest{V: wireVersion, Run: wireOfRun(r)})
	return ackOf(a, err)
}

func (inst *Client) GetRun(ctx context.Context, id string) (r Run, found bool, err error) {
	rep, err := roundTrip[wireIdRequest, wireRunReply](ctx, inst, SubjectGet, wireIdRequest{V: wireVersion, Id: id})
	if err = ackOf(rep.wireAck, err); err != nil {
		return
	}
	return runOfWire(rep.Run), rep.Found, nil
}

func (inst *Client) ListRuns(ctx context.Context, _ string, limit int) (runs []Run, err error) {
	rep, err := roundTrip[wireIdRequest, wireRunReply](ctx, inst, SubjectList, wireIdRequest{V: wireVersion, Limit: limit})
	if err = ackOf(rep.wireAck, err); err != nil {
		return
	}
	for _, w := range rep.Runs {
		runs = append(runs, runOfWire(w))
	}
	return
}

func (inst *Client) Append(ctx context.Context, entries []Entry) (err error) {
	req := wireAppendRequest{V: wireVersion, Entries: make([]wireEntry, 0, len(entries))}
	for _, e := range entries {
		req.Entries = append(req.Entries, wireOfEntry(e))
	}
	a, err := roundTrip[wireAppendRequest, wireAck](ctx, inst, SubjectAppend, req)
	return ackOf(a, err)
}

func (inst *Client) Entries(ctx context.Context, run string, from uint32) (entries []Entry, err error) {
	rep, err := roundTrip[wireIdRequest, wireEntriesReply](ctx, inst, SubjectEntries, wireIdRequest{V: wireVersion, Id: run, From: from})
	if err = ackOf(rep.wireAck, err); err != nil {
		return
	}
	for _, w := range rep.Entries {
		entries = append(entries, entryOfWire(w))
	}
	return
}

func (inst *Client) Tombstone(ctx context.Context, run string) (err error) {
	a, err := roundTrip[wireIdRequest, wireAck](ctx, inst, SubjectTombstone, wireIdRequest{V: wireVersion, Id: run})
	return ackOf(a, err)
}

// Erase destroys the content of the named entries after their erased
// entries are in the chain (ADR-0320 §SD4).
func (inst *Client) Erase(ctx context.Context, run string, ordinals []uint32) (err error) {
	a, err := roundTrip[wireEraseRequest, wireAck](ctx, inst, SubjectErase, wireEraseRequest{V: wireVersion, Id: run, Ordinals: ordinals})
	return ackOf(a, err)
}
