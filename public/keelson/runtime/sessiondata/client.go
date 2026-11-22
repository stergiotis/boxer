package sessiondata

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// The bus client: one function per subject, each taking the caller's bus.
// The window a request comes from is the envelope's, so an input's
// Caller.By is not sent; its OnBehalfOf names the agent's call, which the
// service attests.

// Open starts the person's data session for the calling window and
// returns its ticket.
func Open(ctx context.Context, bus app.BusI) (ticket string, err error) {
	rep, err := request(ctx, bus, SubjectOpen, wireRequest{}, 0)
	return rep.Ticket, err
}

// Attach makes the calling window a holder of the data session ticket
// names.
func Attach(ctx context.Context, bus app.BusI, ticket string) (err error) {
	_, err = request(ctx, bus, SubjectAttach, wireRequest{Ticket: ticket}, 0)
	return
}

// CloseSession ends the person's data session ticket names.
func CloseSession(ctx context.Context, bus app.BusI, ticket string) (err error) {
	_, err = request(ctx, bus, SubjectClose, wireRequest{Ticket: ticket}, 0)
	return
}

// Define defines or redefines a data set; see [Service.Define].
func Define(ctx context.Context, bus app.BusI, in DefineInput) (res DefineResult, err error) {
	req := withCaller(wireRequest{Ticket: in.Ticket, Name: in.Name, SQL: in.SQL, OrderBy: in.OrderBy}, in.Caller)
	req = withEndpoint(req, in.Endpoint)
	rep, err := request(ctx, bus, SubjectDefine, req, RequestTimeout)
	if err != nil {
		return
	}
	if rep.Dataset != nil {
		res.Dataset = *rep.Dataset
	}
	res.Replayed, res.Ticket = rep.Names, rep.Ticket
	return
}

// Drop drops a data set; see [Service.Drop].
func Drop(ctx context.Context, bus app.BusI, in DropInput) (dropped []string, err error) {
	rep, err := request(ctx, bus, SubjectDrop, withCaller(wireRequest{Ticket: in.Ticket, Name: in.Name, Cascade: in.Cascade}, in.Caller), RequestTimeout)
	return rep.Names, err
}

// Prepare readies a statement to run in a data session; see
// [Service.Prepare]. A binding with a lease is released with Release once
// the statement's response is drained or abandoned.
func Prepare(ctx context.Context, bus app.BusI, in PrepareInput) (b Binding, err error) {
	req := withEndpoint(withCaller(wireRequest{Ticket: in.Ticket, SQL: in.SQL}, in.Caller), in.Endpoint)
	rep, err := request(ctx, bus, SubjectPrepare, req, RequestTimeout)
	if err != nil {
		return
	}
	if rep.Binding == nil {
		return b, eh.Errorf("the service answered without a binding")
	}
	return *rep.Binding, nil
}

// Release ends the lease a binding carried. An empty lease is a no-op.
func Release(ctx context.Context, bus app.BusI, lease string) (err error) {
	if lease == "" {
		return
	}
	_, err = request(ctx, bus, SubjectRelease, wireRequest{Lease: lease}, 0)
	return
}

// List lists the data sets of the caller's data session and returns its
// ticket: the task's for an agent's call, the one ticket names for the
// person's.
func List(ctx context.Context, bus app.BusI, ticket string, obo *app.OnBehalfOf) (infos []DatasetInfo, sessionTicket string, err error) {
	rep, err := request(ctx, bus, SubjectList, withCaller(wireRequest{Ticket: ticket}, Caller{OnBehalfOf: obo}), 0)
	return rep.Datasets, rep.Ticket, err
}

func withCaller(req wireRequest, c Caller) (out wireRequest) {
	out = req
	if c.OnBehalfOf != nil {
		out.OboTask, out.OboEpoch, out.OboCall = c.OnBehalfOf.Task, c.OnBehalfOf.Epoch, c.OnBehalfOf.Call
	}
	return
}

func withEndpoint(req wireRequest, e Endpoint) (out wireRequest) {
	out = req
	out.Endpoint, out.User, out.Password, out.ServesConfined = e.URL, e.User, e.Password, e.ServesConfined
	return
}

// request sends req and decodes the reply; a refusal comes back as an
// error, a *GrantError when it names the destination a grant lacks.
func request(ctx context.Context, bus app.BusI, subject string, req wireRequest, within time.Duration) (rep wireReply, err error) {
	req.V = wireVersion
	payload, err := buscodec.Encode(req)
	if err != nil {
		return rep, eb.Build().Str("subject", subject).Errorf("encode the request: %w", err)
	}
	raw, err := app.RequestWithin(ctx, bus, within, subject, payload)
	if err != nil {
		return rep, eb.Build().Str("subject", subject).Errorf("request: %w", err)
	}
	rep, err = buscodec.Decode[wireReply](raw)
	if err != nil {
		return rep, eb.Build().Str("subject", subject).Errorf("decode the reply: %w", err)
	}
	if !rep.Ok {
		if rep.Destination != "" {
			return rep, &GrantError{Destination: rep.Destination, Reason: trimAgentLimit(rep.Reason)}
		}
		return rep, eb.Build().Str("subject", subject).Errorf("refused: %s", rep.Reason) //boxer:lint disable=CS013 reason="the service's refusal crosses the bus as text and is what a window or an agent reads"
	}
	return rep, nil
}

func trimAgentLimit(s string) (out string) {
	const p = "agent limit: "
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}
