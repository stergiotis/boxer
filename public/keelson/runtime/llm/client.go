package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
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
	// Local says the sensitivity wall (§SD3) admits confined content to the
	// endpoint: it is loopback, or a host the deployment trusts.
	Local bool
	// Trusted says Local holds only because BOXER_LLM_TRUSTED_HOSTS lists
	// the endpoint's host.
	Trusted bool
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
	// OnBehalfOf is set when the completion is work an agent's call
	// started: the service then refuses it unless the task's grant lists
	// the model service (ADR-0269 §SD6). Carry OperationCall.OnBehalfOf
	// here; a coordinator's own turns carry none.
	OnBehalfOf *app.OnBehalfOf
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

	// Retain sends the request on llm.retain.complete (ADR-0264): the host
	// keeps the conversation where its BOXER_LLM_RETAIN ceiling is durable.
	// Needs [RetainCaps] beside [ClientCaps]; the bus refuses it otherwise.
	Retain bool
	// Conversation is the app's id for the conversation, required with
	// Retain. ParentCallId is the CallId of the reply this turn continues,
	// empty on the first turn; the host then keeps only what is new.
	Conversation string
	ParentCallId string
	// OmitFrom and OmitTo declare that the request leaves out messages
	// [OmitFrom, OmitTo) of the conversation — positions in the whole
	// conversation, not in Messages — to fit the model's context. Messages
	// is then the conversation with that range removed, followed by what is
	// new, and the host still keeps only what is new; an omission that does
	// not match keeps the whole request. OmitTo 0 declares none.
	OmitFrom int
	OmitTo   int
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
	// CallId is the call's identity: the next turn's ParentCallId.
	CallId string
	// Retention says whether a retained request's messages were kept, and
	// RetentionReason why not (ADR-0264 §SD4).
	Retention       RetentionE
	RetentionReason string
}

// RetentionE is the verdict on a request's text.
type RetentionE uint8

const (
	// RetentionNotAsked is a request sent on llm.complete.
	RetentionNotAsked RetentionE = 0
	// RetentionKept means the messages were flushed to boxer.facts before
	// the reply was sent.
	RetentionKept RetentionE = 1
	// RetentionNotKept carries the reason: the ceiling, no durable backend,
	// or a failed write.
	RetentionNotKept RetentionE = 2
)

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
	d = Description{Configured: w.Configured, Model: w.Model, EndpointHost: w.EndpointHost, Local: w.Local, Trusted: w.Trusted,
		MaxTokens: w.MaxTokens, Reason: w.Reason}
	return
}

// Complete runs one completion. A refusal is a *RefusedError; a provider
// failure wraps the openaichat sentinel it maps to; a transport failure is
// neither. Cancelling ctx returns at once with its error and asks the
// service to stop the provider call (llm.cancel).
func (inst *Client) Complete(ctx context.Context, r Request) (res Response, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("llm: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("llm: before request: %w", err)
	}
	req := wireRequest{
		V: wireVersion, Purpose: r.Purpose, Sensitivity: uint8(r.Sensitivity),
		Messages: r.Messages, Temperature: r.Temperature, MaxTokens: r.MaxTokens, Seed: r.Seed, Stop: r.Stop,
		EnableThinking: r.EnableThinking, Tools: r.Tools, ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat,
		CancelKey: strconv.FormatUint(rand.Uint64(), 36),
	}
	if r.OnBehalfOf != nil {
		req.OnBehalfTask, req.OnBehalfEpoch = r.OnBehalfOf.Task, r.OnBehalfOf.Epoch
	}
	subject := SubjectComplete
	if r.Retain {
		subject = SubjectRetainComplete
		req.Conversation, req.ParentCallId = r.Conversation, r.ParentCallId
		req.OmitFrom, req.OmitTo = uint32(max(r.OmitFrom, 0)), uint32(max(r.OmitTo, 0))
	}
	if deadline, ok := ctx.Deadline(); ok {
		req.DeadlineUnixNanos = deadline.UnixNano()
	}
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.request(ctx, subject, payload, req.CancelKey)
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
		Elapsed: time.Duration(w.ElapsedNs), CallId: w.CallId,
		Retention: RetentionE(w.Retention), RetentionReason: w.RetentionReason,
	}
	return
}

// request sends payload and waits for the reply or for ctx, whichever
// comes first. On cancellation it publishes llm.cancel with key so the
// service stops the provider call rather than finishing it for no one;
// the abandoned request's goroutine ends when the reply or its wait does.
func (inst *Client) request(ctx context.Context, subject string, payload []byte, key string) (raw []byte, err error) {
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	wait := inst.wait(ctx, DefaultTimeout)
	go func() {
		r, e := inst.bus.RequestWithTimeout(subject, payload, wait)
		done <- result{raw: r, err: e}
	}()
	select {
	case r := <-done:
		return r.raw, r.err
	case <-ctx.Done():
		if cancelPayload, cerr := encode(wireCancel{V: wireVersion, Key: key}); cerr == nil {
			_ = inst.bus.Publish(SubjectCancel, cancelPayload)
		}
		return nil, eh.Errorf("llm: waiting for the reply: %w", ctx.Err())
	}
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
	case errKindCancelled:
		sentinel = context.Canceled
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
	case errors.Is(err, context.Canceled):
		return errKindCancelled
	default:
		return errKindOther
	}
}
