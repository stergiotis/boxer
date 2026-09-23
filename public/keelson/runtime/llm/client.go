package llm

import (
	"context"
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is the family as an app sees it: two verbs over a bus client
// holding [ClientCaps], answered by the host's [Service]. The zero value is
// unusable; take one from [NewClient] with the bus the host minted for the
// app (its MountContextI.Bus()).
type Client struct {
	bus app.BusI
	// Timeout bounds one request; zero is [DefaultTimeout]. A completion
	// against a local model can take most of it.
	Timeout time.Duration
}

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// Description is what the host offers (ADR-0254 §SD1): whether a model is
// configured at all, which one, where it is, and whether that place is
// this box. An app renders its model surface only when Configured and
// shows EndpointHost beside the gesture.
type Description struct {
	Configured   bool
	Model        string
	EndpointHost string
	// Local says the endpoint is loopback: the sensitivity wall (§SD3)
	// admits confined content there and nowhere else.
	Local bool
	// MaxTokens is the host's ceiling when a request names none.
	MaxTokens int32
	// Reason says why nothing is configured.
	Reason string
}

// Request is one completion as a Go value: the caller's declaration of
// purpose and sensitivity, and the completion request minus the model
// (the host's) and the endpoint (the host's).
type Request struct {
	// Purpose names why, for the audit row and the call table — the
	// prompt slug, the pane, the transformation.
	Purpose string
	// Sensitivity is what the content was composed from (ADR-0145 §SD3):
	// SensitivityConfined when any of it derives from a sealed dataset.
	// The caller's declaration; the service cannot see provenance.
	Sensitivity queryengine.SensitivityE

	Messages       []openaichat.Message
	Temperature    *float32
	MaxTokens      int32
	Seed           *int64
	Stop           []string
	EnableThinking bool
	Tools          []openaichat.Tool
	ToolChoice     string
	ResponseFormat *openaichat.ResponseFormat
}

// Response is one completion's answer. Tool calls come back unexecuted
// (ADR-0254 §SD5): the caller runs them under its own grants and continues
// the conversation.
type Response struct {
	Content      string
	Reasoning    string
	FinishReason string
	ToolCalls    []openaichat.ToolCall
	InputTokens  int32
	OutputTokens int32
	// Incomplete marks a completion that hit the token ceiling with content
	// already produced — the client's own contract, kept: Content is what
	// there is, and err is nil.
	Incomplete bool
	Elapsed    time.Duration
}

// RefusedError is a reply the service declined: no model, a confined
// request against a remote endpoint, a malformed request. Provider
// failures are not refusals; they come back wrapped in the openaichat
// sentinel they correspond to so a caller's existing classification
// keeps working.
type RefusedError struct {
	Reason string
}

func (inst *RefusedError) Error() string { return "llm: refused: " + inst.Reason }

// Describe asks what the host offers.
func (inst *Client) Describe(ctx context.Context) (d Description, err error) {
	if inst == nil || inst.bus == nil {
		return d, eh.Errorf("llm: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return d, eh.Errorf("llm: before request: %w", err)
	}
	raw, err := inst.bus.RequestWithTimeout(SubjectDescribe, nil, inst.wait(ctx, 5*time.Second))
	if err != nil {
		return d, eh.Errorf("llm.describe request: %w", err)
	}
	w, err := decode[wireDescribe](raw)
	if err != nil {
		return
	}
	d = Description{Configured: w.Configured, Model: w.Model, EndpointHost: w.EndpointHost, Local: w.Local, MaxTokens: w.MaxTokens, Reason: w.Reason}
	return
}

// Complete runs one completion. A refusal is a *RefusedError; a provider
// failure wraps the openaichat sentinel it maps to; a transport failure is
// neither.
func (inst *Client) Complete(ctx context.Context, r Request) (res Response, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("llm: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("llm: before request: %w", err)
	}
	req := wireRequest{
		Purpose: r.Purpose, Sensitivity: uint8(r.Sensitivity),
		Messages: r.Messages, Temperature: r.Temperature, MaxTokens: r.MaxTokens, Seed: r.Seed, Stop: r.Stop,
		EnableThinking: r.EnableThinking, Tools: r.Tools, ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat,
	}
	if deadline, ok := ctx.Deadline(); ok {
		req.DeadlineUnixNanos = deadline.UnixNano()
	}
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.bus.RequestWithTimeout(SubjectComplete, payload, inst.wait(ctx, DefaultTimeout))
	if err != nil {
		return res, eb.Build().Str("purpose", r.Purpose).Errorf("llm.complete request: %w", err)
	}
	w, err := decode[wireReply](raw)
	if err != nil {
		return
	}
	if !w.Ok {
		return res, failureOf(w)
	}
	res = Response{
		Content: w.Content, Reasoning: w.Reasoning, FinishReason: w.FinishReason, ToolCalls: w.ToolCalls,
		InputTokens: w.InputTokens, OutputTokens: w.OutputTokens, Incomplete: w.Incomplete,
		Elapsed: time.Duration(w.ElapsedNs),
	}
	return
}

// wait is the request wait: Timeout, else fallback, shortened to the
// context's deadline.
func (inst *Client) wait(ctx context.Context, fallback time.Duration) (d time.Duration) {
	d = inst.Timeout
	if d <= 0 {
		d = fallback
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
	var sentinel error
	switch w.ErrorKind {
	case errKindRefused, "":
		return &RefusedError{Reason: w.Reason}
	case errKindAuth:
		sentinel = openaichat.ErrAuth
	case errKindModelNotFound:
		sentinel = openaichat.ErrModelNotFound
	case errKindRateLimited:
		sentinel = openaichat.ErrRateLimited
	case errKindBadRequest:
		sentinel = openaichat.ErrBadRequest
	case errKindServer:
		sentinel = openaichat.ErrServer
	case errKindTimeout:
		sentinel = context.DeadlineExceeded
	default:
		return errors.New("llm: " + w.Reason)
	}
	return eb.Build().Str("reason", w.Reason).Errorf("llm: %w", sentinel)
}

// kindOf is failureOf's inverse on the service side.
func kindOf(err error) (kind string) {
	switch {
	case errors.Is(err, openaichat.ErrAuth):
		return errKindAuth
	case errors.Is(err, openaichat.ErrModelNotFound):
		return errKindModelNotFound
	case errors.Is(err, openaichat.ErrRateLimited):
		return errKindRateLimited
	case errors.Is(err, openaichat.ErrBadRequest):
		return errKindBadRequest
	case errors.Is(err, openaichat.ErrServer):
		return errKindServer
	case errors.Is(err, context.DeadlineExceeded):
		return errKindTimeout
	default:
		return errKindOther
	}
}
