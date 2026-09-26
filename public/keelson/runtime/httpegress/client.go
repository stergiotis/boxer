package httpegress

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is the family as an app sees it: a fetch per destination over a
// bus client holding [ClientCaps] for each, answered by the host's
// [Service]. Take one from [NewClient] with the bus the host minted for
// the app (its MountContextI.Bus()).
type Client struct {
	bus app.BusI
	// Timeout bounds one request when the context has no earlier
	// deadline; zero is DefaultTimeout.
	Timeout time.Duration
}

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// Request is one fetch.
type Request struct {
	// Method is GET or HEAD; empty is GET.
	Method string
	// URL is absolute and must lie under the destination's prefixes.
	URL string
	// Purpose names why, for the call table.
	Purpose string
	// Sensitivity is what the request was composed from (ADR-0145 §SD3):
	// SensitivityConfined when the URL derives from a sealed dataset. The
	// caller's declaration; the service cannot see provenance.
	Sensitivity queryengine.SensitivityE
}

// Response is a completed exchange, whatever its status.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
	Elapsed     time.Duration
}

// RefusedError is a request the service declined: an unregistered or
// unresolved destination, a URL outside it, a confined request to a
// remote destination, an unoffered method.
type RefusedError struct {
	Reason string
}

func (inst *RefusedError) Error() string { return "httpegress: refused: " + inst.Reason }

// ErrTooLarge is a body past the destination's cap.
var ErrTooLarge = errors.New("httpegress: body exceeds the destination's cap")

// Fetch runs one request against destination. A refusal is a
// *RefusedError; a body past the cap wraps ErrTooLarge; a timeout wraps
// context.DeadlineExceeded; a denied publish (no cap for the destination)
// is the bus's permission error.
func (inst *Client) Fetch(ctx context.Context, destination string, r Request) (res Response, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("httpegress: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("httpegress: before request: %w", err)
	}
	if !ValidDestinationName(destination) {
		return res, eb.Build().Str("destination", destination).Errorf("httpegress: invalid destination name")
	}
	req := wireRequest{V: wireVersion, Method: r.Method, URL: r.URL, Purpose: r.Purpose, Sensitivity: uint8(r.Sensitivity)}
	if deadline, ok := ctx.Deadline(); ok {
		req.DeadlineUnixNanos = deadline.UnixNano()
	}
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.bus.RequestWithTimeout(Subject(destination), payload, inst.wait(ctx))
	if err != nil {
		return res, eb.Build().Str("destination", destination).Errorf("net.http.fetch request: %w", err)
	}
	w, err := decode[wireReply](raw)
	if err != nil {
		return
	}
	if !w.Ok {
		return res, failureOf(w)
	}
	res = Response{Status: int(w.Status), ContentType: w.ContentType, Body: w.Body, Elapsed: time.Duration(w.ElapsedNs)}
	return
}

// wait is the request wait: Timeout, else DefaultTimeout, shortened to the
// context's deadline.
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

// failureOf maps a failed reply back onto an error a caller can classify.
func failureOf(w wireReply) (err error) {
	switch w.ErrorKind {
	case errKindRefused, "":
		return &RefusedError{Reason: w.Reason}
	case errKindTooLarge:
		return eb.Build().Str("reason", w.Reason).Errorf("%w", ErrTooLarge)
	case errKindTimeout:
		return eb.Build().Str("reason", w.Reason).Errorf("httpegress: %w", context.DeadlineExceeded)
	default:
		return errors.New("httpegress: " + w.Reason)
	}
}

// Getter is a GET against one destination as a byte fetch: a status other
// than 200 is an error. The shape a loader that wants bytes, not HTTP,
// is handed.
type Getter struct {
	Client      *Client
	Destination string
	// Purpose and Sensitivity ride on every request (see [Request]).
	Purpose     string
	Sensitivity queryengine.SensitivityE
}

// Get fetches url's body.
func (inst Getter) Get(ctx context.Context, url string) (data []byte, err error) {
	res, err := inst.Client.Fetch(ctx, inst.Destination, Request{URL: url, Purpose: inst.Purpose, Sensitivity: inst.Sensitivity})
	if err != nil {
		return
	}
	if res.Status != http.StatusOK {
		return nil, eb.Build().Int("status", res.Status).Str("url", url).Errorf("httpegress: the server answered an error status")
	}
	data = res.Body
	return
}
