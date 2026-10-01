package agent

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultTimeout bounds one request when the context names no earlier
// deadline.
const DefaultTimeout = 10 * time.Second

// Client is the family as a caller sees it, over a bus client holding
// [ClientCaps]. Take one from [NewClient] with the bus the host minted for
// the caller.
type Client struct {
	bus app.BusI
	// Timeout bounds one request when the context has no earlier deadline;
	// zero is DefaultTimeout.
	Timeout time.Duration
}

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// DescribeRequest asks for the operations agents may call. App names one
// app by id or subject alias; Search matches app and operation names and
// summaries; Operation names one operation of App and returns its schemas.
type DescribeRequest struct {
	App       string
	Search    string
	Operation string
}

// Resource is a resource an operation reads or writes.
type Resource struct {
	Name    string
	Summary string
}

// Operation is one operation as a caller sees it. The schemas are set only
// when the request named the operation.
type Operation struct {
	Name         string
	Version      uint16
	Summary      string
	Class        string
	Effect       string
	Reads        []string
	Writes       []string
	Refs         []string
	Follows      []string
	Untrusted    bool
	Gesture      string
	ArgsSchema   string
	ResultSchema string
}

// AppOperations is one app's operations agents may call.
type AppOperations struct {
	App        string
	Display    string
	Summary    string
	Resources  []Resource
	Operations []Operation
}

// RefusedError is a request the service declined, with the reason.
type RefusedError struct {
	Reason string
}

func (inst *RefusedError) Error() string { return "agent: refused: " + inst.Reason }

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
		err = eh.Errorf("agent: client without a bus")
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.bus.RequestWithTimeout(subject, payload, inst.wait(ctx))
	if err != nil {
		err = eb.Build().Str("subject", subject).Errorf("agent: request: %w", err)
		return
	}
	rep, err = decode[Rep](raw)
	return
}

// Describe lists operations agents may call; it needs no grant.
func (inst *Client) Describe(ctx context.Context, r DescribeRequest) (apps []AppOperations, err error) {
	rep, err := roundTrip[wireDescribeRequest, wireDescribeReply](ctx, inst, SubjectDescribe,
		wireDescribeRequest{V: wireVersion, App: r.App, Search: r.Search, Operation: r.Operation})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	for _, a := range rep.Apps {
		out := AppOperations{App: a.App, Display: a.Display, Summary: a.Summary}
		for _, res := range a.Resources {
			out.Resources = append(out.Resources, Resource(res))
		}
		for _, o := range a.Operations {
			out.Operations = append(out.Operations, Operation(o))
		}
		apps = append(apps, out)
	}
	return
}

// GrantEntry names one instance a task may work in, its mode, and the
// operations it may call there; none means every operation the app exposes
// to agents.
type GrantEntry struct {
	Instance   uint64
	Mode       ModeE
	Operations []string
}

// GrantRequest asks for a task grant, or with Handle for the widening of
// one.
type GrantRequest struct {
	Handle       string
	Plan         string
	Entries      []GrantEntry
	Destinations []string
	// Calls is the call budget; zero is DefaultCallBudget.
	Calls uint32
	// Deadline is how long the task may run; zero is DefaultDeadline.
	Deadline time.Duration
}

// Grant is what a caller holds: the task id for the record and the handle
// it presents. The handle is honoured only from the requesting instance.
type Grant struct {
	Task   string
	Handle string
}

// Request asks for a grant and waits until the person decides or ctx ends.
// Test grants, where the host enables them, are issued at once.
func (inst *Client) Request(ctx context.Context, r GrantRequest) (g Grant, err error) {
	key, g, err := inst.RequestKey(ctx, r)
	if err != nil || g.Handle != "" {
		return
	}
	g, err = inst.AwaitGrant(ctx, key)
	if err == nil && g.Handle == "" && r.Handle != "" {
		// A widening keeps the task's handle.
		g = Grant{Handle: r.Handle}
	}
	return
}

// RequestKey sends a request and returns its key without waiting; a test
// grant comes back at once as g.
func (inst *Client) RequestKey(ctx context.Context, r GrantRequest) (key string, g Grant, err error) {
	req := wireGrantRequest{V: wireVersion, Handle: r.Handle, Plan: r.Plan, Destinations: r.Destinations, Calls: r.Calls,
		DeadlineSecs: uint32(r.Deadline / time.Second)}
	for _, e := range r.Entries {
		req.Entries = append(req.Entries, wireGrantEntry{Instance: e.Instance, Mode: e.Mode.String(), Operations: e.Operations})
	}
	rep, err := roundTrip[wireGrantRequest, wireGrantReply](ctx, inst, SubjectRequest, req)
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	key, g = rep.Key, Grant{Task: rep.Task, Handle: rep.Handle}
	return
}

// Outcome is where a call stands; Phase is one of the phase names of
// ADR-0269 §SD4.
type Outcome struct {
	Phase     string
	Reason    string
	AsOf      uint64
	Revisions map[string]uint64
	// ResultRef names the call's result, to read with Read.
	ResultRef string
	// Job names a capture, to read with Read once completed.
	Job string
	// Held marks an input_required call that waits on the person.
	Held bool
	// Task and Handle answer an approved request's key.
	Task   string
	Handle string
}

// Final reports whether no later phase can follow.
func (inst Outcome) Final() (final bool) {
	if inst.Held {
		return false
	}
	for _, p := range opwire.AllPhases {
		if p.String() == inst.Phase {
			return p.Final()
		}
	}
	return
}

// CallRequest is one command or query. Args is JSON in the operation's
// argument schema; Expects are the revisions a command expects of what it
// writes, defaulting to those the task last read.
type CallRequest struct {
	Handle    string
	Instance  uint64
	Operation string
	Args      string
	Expects   map[string]uint64
	Key       string
	Reason    string
}

func outcomeOfWire(w wireOutcome) (out Outcome) {
	return Outcome{Phase: w.Phase, Reason: w.Reason, AsOf: w.AsOf, Revisions: w.Revisions, ResultRef: w.ResultRef, Job: w.Job,
		Held: w.Held, Task: w.Task, Handle: w.Handle}
}

func callReply(rep wireCallReply, err error) (out Outcome, rerr error) {
	if err != nil {
		rerr = err
		return
	}
	if !rep.Ok {
		rerr = &RefusedError{Reason: rep.Reason}
		return
	}
	out = outcomeOfWire(rep.Outcome)
	return
}

// Call calls one command or query.
func (inst *Client) Call(ctx context.Context, r CallRequest) (out Outcome, err error) {
	return callReply(roundTrip[wireCall, wireCallReply](ctx, inst, SubjectCall, wireCall{V: wireVersion, Handle: r.Handle,
		Instance: r.Instance, Operation: r.Operation, Args: r.Args, Expects: r.Expects, Key: r.Key, Reason: r.Reason}))
}

// Status reads a call's or a capture's phase by key, waiting up to wait
// (at most MaxStatusWait) for a final one.
func (inst *Client) Status(ctx context.Context, handle string, key string, wait time.Duration) (out Outcome, err error) {
	sub := *inst
	sub.Timeout = inst.wait(ctx) + wait
	return callReply(roundTrip[wireStatus, wireCallReply](ctx, &sub, SubjectStatus,
		wireStatus{V: wireVersion, Handle: handle, Key: key, WaitMs: uint32(wait / time.Millisecond)}))
}

// Cancel withdraws a queued call by key; a call past the queue keeps its
// phase, which is returned.
func (inst *Client) Cancel(ctx context.Context, handle string, key string) (out Outcome, err error) {
	return callReply(roundTrip[wireCancel, wireCallReply](ctx, inst, SubjectCancel, wireCancel{V: wireVersion, Handle: handle, Key: key}))
}

// Capture captures an instance's window; the outcome's Job names it.
func (inst *Client) Capture(ctx context.Context, handle string, instance uint64, key string) (out Outcome, err error) {
	return callReply(roundTrip[wireCapture, wireCallReply](ctx, inst, SubjectCapture,
		wireCapture{V: wireVersion, Handle: handle, Instance: instance, Key: key}))
}

// ReadResult is a result as JSON, or an artifact by media type and path.
type ReadResult struct {
	MediaType string
	Text      string
	Path      string
}

// Read reads a result reference or a completed capture.
func (inst *Client) Read(ctx context.Context, handle string, ref string) (res ReadResult, err error) {
	rep, err := roundTrip[wireRead, wireReadReply](ctx, inst, SubjectRead, wireRead{V: wireVersion, Handle: handle, Ref: ref})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	res = ReadResult{MediaType: rep.MediaType, Text: rep.Text, Path: rep.Path}
	return
}

// Instance is one window of the task.
type Instance struct {
	Instance uint64
	App      string
	Title    string
	Mode     string
	Ops      bool
}

// List lists the task's open instances.
func (inst *Client) List(ctx context.Context, handle string) (out []Instance, err error) {
	rep, err := roundTrip[wireHandle, wireListReply](ctx, inst, SubjectList, wireHandle{V: wireVersion, Handle: handle})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	for _, i := range rep.Instances {
		out = append(out, Instance(i))
	}
	return
}

func ack(rep wireAck, err error) (rerr error) {
	if err != nil {
		return err
	}
	if !rep.Ok {
		return &RefusedError{Reason: rep.Reason}
	}
	return
}

// Detach removes one instance from the task; its queued calls expire.
func (inst *Client) Detach(ctx context.Context, handle string, instance uint64) (err error) {
	return ack(roundTrip[wireHandle, wireAck](ctx, inst, SubjectDetach, wireHandle{V: wireVersion, Handle: handle, Instance: instance}))
}

// Stop ends the task.
func (inst *Client) Stop(ctx context.Context, handle string) (err error) {
	return ack(roundTrip[wireHandle, wireAck](ctx, inst, SubjectStop, wireHandle{V: wireVersion, Handle: handle}))
}
