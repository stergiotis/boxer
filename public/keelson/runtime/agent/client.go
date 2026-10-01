package agent

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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

func request[Req any, Rep any](ctx context.Context, inst *Client, subject string, req Req) (rep Rep, err error) {
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
	rep, err := request[wireDescribeRequest, wireDescribeReply](ctx, inst, SubjectDescribe,
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
