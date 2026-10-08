package llm

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config is the host's one provider (ADR-0254 §SD2).
type Config struct {
	// Endpoint is the OpenAI-compatible base URL; empty means no model.
	Endpoint string
	// Model is the model id; empty means no model.
	Model  string
	ApiKey string
	// MaxTokens is the ceiling when a request names none.
	MaxTokens int32
	// ContextTokens is the model's context size as the deployment states
	// it; zero asks the endpoint's model list once (ProbeContextTokens),
	// unless Client replaces the endpoint.
	ContextTokens int32
	// Timeout bounds one completion on the service side.
	Timeout time.Duration
	// TrustedHosts are endpoint hosts treated like loopback by the
	// sensitivity wall; matched case-insensitively against the endpoint's
	// host name, without port.
	TrustedHosts []string
	// Retain is the ceiling on keeping text (ADR-0264 §SD1): ring keeps
	// prompt and completion on the in-process records, durable also keeps
	// the messages of retained requests on boxer.facts. Empty is off.
	Retain RetainE
	// KeepCalls bounds the in-process call record; zero is
	// DefaultKeepCalls.
	KeepCalls int
	// Client, when set, replaces the client the service would build from
	// Endpoint and ApiKey — a test's fake. Endpoint and Model still say
	// whether a model is configured.
	Client openaichat.ClientI
	// Trail is the host's audit trail (ADR-0277): every call and each of its
	// new messages lands there as a row, beside the in-process record. Nil,
	// or a recorder without a backend, keeps the record alone — the
	// in-memory host's case.
	Trail *trail.Recorder
}

// ConfigFromEnv resolves the config from the ADR-0009 registry.
func ConfigFromEnv() (cfg Config) {
	cfg = Config{
		Endpoint: Endpoint.Get(), Model: Model.Get(), ApiKey: ApiKey.Get(),
		MaxTokens: int32(MaxTokens.Get()), ContextTokens: int32(ContextTokens.Get()), Timeout: Timeout.Get(), Retain: RetainE(Retain.Get()),
		TrustedHosts: ParseTrustedHosts(TrustedHosts.Get()),
	}
	return
}

// ParseTrustedHosts splits TrustedHosts' value.
func ParseTrustedHosts(s string) (hosts []string) {
	for _, h := range strings.Split(s, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return
}

// Configured says a model is offered: endpoint AND model, neither with a
// default.
func (inst Config) Configured() (yes bool) { return inst.Endpoint != "" && inst.Model != "" }

// DefaultKeepCalls bounds the in-process call record.
const DefaultKeepCalls = 1000

// Service answers `llm.<verb>`. Without a configured model it still
// subscribes and answers describe with the reason and complete with a
// refusal, so a consumer is told rather than left to a timeout.
type Service struct {
	// delegation checks agent-caused completions (ADR-0269 §SD6); set once
	// the host's dispatcher runs.
	delegation atomic.Pointer[delegationRef]
	cfg        Config
	client     openaichat.ClientI
	host       string
	local      bool
	// trusted says local holds by TrustedHosts, not loopback.
	trusted bool
	// context is the model's context size and where it came from, set
	// once from the config or by the probe.
	context   atomic.Pointer[contextSize]
	busClient *inprocbus.Client
	unsubs    []func()
	log       zerolog.Logger
	// base parents every completion's context; Close cancels it, then
	// waits on inflight before it releases the clients the calls use.
	base       context.Context
	cancelBase context.CancelFunc
	inflight   sync.WaitGroup

	mu    sync.Mutex
	calls []CallRecord
	next  uint64
	// minted salts the call ids this process mints.
	minted uint64
	// closed refuses completions that arrive once Close has begun, so
	// inflight is never added to while Close waits on it.
	closed bool
	// running holds the cancel of each completion in flight, by its
	// sender's cancel key (llm.cancel).
	running map[cancelKey]context.CancelFunc
	// seen remembers the calls whose rows landed, by call id, so a
	// continuing call writes only what is new (ADR-0264 §SD3); seenOrder
	// bounds it to KeepCalls. In-process: after a restart the first turn
	// of a conversation keeps its whole history again.
	seen      map[string]seen
	seenOrder []string
}

// NewService constructs and subscribes a Service. The caller MUST invoke
// Close.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, cfg Config) (s *Service, err error) {
	if bus == nil {
		err = eh.Errorf("llm: nil bus")
		return
	}
	if cfg.KeepCalls <= 0 {
		cfg.KeepCalls = DefaultKeepCalls
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	s = &Service{cfg: cfg, log: log.With().Str("app", string(ServiceAppId)).Logger(), seen: map[string]seen{}, running: map[cancelKey]context.CancelFunc{}}
	s.base, s.cancelBase = context.WithCancel(context.Background())
	if cfg.Configured() {
		s.host = EndpointHost(cfg.Endpoint)
		s.local = isLocalEndpoint(cfg.Endpoint)
		if !s.local && isTrustedEndpoint(cfg.Endpoint, cfg.TrustedHosts) {
			s.local, s.trusted = true, true
			s.log.Warn().Str("host", s.host).
				Msg("llm: the endpoint is not loopback but BOXER_LLM_TRUSTED_HOSTS lists it; confined content may be sent to it")
		}
		s.client = cfg.Client
		if s.client == nil {
			// No retry policy on purpose: this backs interactive gestures,
			// and a failed attempt should surface as a line the reader can
			// act on rather than as half a minute of silent backoff.
			s.client, err = openaichat.NewClient(cfg.Endpoint, cfg.ApiKey)
			if err != nil {
				return nil, err
			}
		}
	}
	if cfg.Configured() {
		switch {
		case cfg.ContextTokens > 0:
			s.context.Store(&contextSize{tokens: cfg.ContextTokens, source: "BOXER_LLM_CONTEXT_TOKENS"})
		case cfg.Client == nil:
			go s.probeContext()
		}
	}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps())
	for _, pattern := range []string{SubjectAll, SubjectRetainAll} {
		var unsub func()
		unsub, err = s.busClient.Subscribe(pattern, s.handleRequest)
		if err != nil {
			s.Close()
			err = eb.Build().Str("pattern", pattern).Errorf("llm: subscribe: %w", err)
			return nil, err
		}
		s.unsubs = append(s.unsubs, unsub)
	}
	return
}

// Close releases the subscription, cancels the completions in flight and
// waits for them, then releases the bus client and the provider client.
// Safe to call more than once.
func (inst *Service) Close() {
	for _, unsub := range inst.unsubs {
		unsub()
	}
	inst.unsubs = nil
	inst.mu.Lock()
	inst.closed = true
	inst.mu.Unlock()
	inst.cancelBase()
	inst.inflight.Wait()
	if inst.busClient != nil {
		if err := inst.busClient.Close(); err != nil {
			inst.log.Warn().Err(err).Msg("llm: closing the bus client")
		}
		inst.busClient = nil
	}
	if inst.client != nil {
		_ = inst.client.Close()
		inst.client = nil
	}
}

// Durable says the calls land on boxer.facts as well as in the record.
func (inst *Service) Durable() (yes bool) {
	return inst.cfg.Trail.Durable()
}

// Describe is the describe reply as a Go value, for the host's own use.
func (inst *Service) Describe() (d Description) {
	if !inst.cfg.Configured() {
		d.Reason = "no model is configured on this host (BOXER_LLM_ENDPOINT and BOXER_LLM_MODEL)"
		d.Timeout = inst.cfg.Timeout
		return
	}
	d = Description{Configured: true, Model: inst.cfg.Model, EndpointHost: inst.host, Local: inst.local, Trusted: inst.trusted,
		MaxTokens: inst.cfg.MaxTokens, Timeout: inst.cfg.Timeout}
	if cs := inst.context.Load(); cs != nil {
		d.ContextTokens, d.ContextSource = cs.tokens, cs.source
	}
	return
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Subject == SubjectCancel {
		inst.handleCancel(msg)
		return
	}
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("llm: request without reply, dropping")
		return
	}
	switch msg.Subject {
	case SubjectDescribe:
		d := inst.Describe()
		inst.reply(msg.Reply, wireDescribe{Configured: d.Configured, Model: d.Model, EndpointHost: d.EndpointHost, Local: d.Local,
			Trusted: d.Trusted, MaxTokens: d.MaxTokens, ContextTokens: d.ContextTokens, ContextSource: d.ContextSource, Reason: d.Reason,
			TimeoutNs: int64(d.Timeout)})
	case SubjectComplete, SubjectRetainComplete:
		inst.startComplete(msg, msg.Subject == SubjectRetainComplete)
	default:
		inst.reply(msg.Reply, wireReply{ErrorKind: errKindRefused, Reason: "unknown verb " + strings.TrimPrefix(msg.Subject, SubjectPrefix)})
	}
}

// startComplete answers off the requester's goroutine. The in-process bus
// runs a handler on the publisher's goroutine, so a completion answered
// inline would hold the requester for the whole provider call, and the
// requester's own wait (Client.Timeout, its context) would never take
// effect. msg is the bus's and is not retained, so it is copied first.
func (inst *Service) startComplete(msg *app.Msg, retained bool) {
	m := *msg
	m.Payload = bytes.Clone(msg.Payload)
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.inflight.Add(1)
	inst.mu.Unlock()
	go func() {
		defer inst.inflight.Done()
		inst.handleComplete(&m, retained)
	}()
}

// cancelKey is a completion's cancel handle: the key the requester minted,
// scoped to the requester, so one app cannot stop another's call.
type cancelKey struct {
	sender   app.AppIdT
	instance uint64
	key      string
}

// handleCancel stops the completion the sender's key names. Unknown keys
// are ignored: the call may already have been answered.
func (inst *Service) handleCancel(msg *app.Msg) {
	c, err := decode[wireCancel](msg.Payload)
	if err != nil || c.Key == "" {
		return
	}
	inst.mu.Lock()
	cancel := inst.running[cancelKey{sender: msg.Sender, instance: msg.SenderInstance, key: c.Key}]
	inst.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (inst *Service) handleComplete(msg *app.Msg, retained bool) {
	req, err := decode[wireRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "malformed request: "+err.Error(), CallRecord{}, nil)
		return
	}
	var extra map[string]any
	if len(req.Extra) > 0 {
		if err = json.Unmarshal(req.Extra, &extra); err != nil {
			inst.refuse(msg, "malformed request: extra: "+err.Error(), CallRecord{}, nil)
			return
		}
	}
	rec := CallRecord{
		CallId: inst.mintCallId(), At: time.Now().UTC(), Sender: msg.Sender, SenderInstance: msg.SenderInstance,
		Purpose: req.Purpose, Sensitivity: queryengine.SensitivityE(req.Sensitivity),
		Model: inst.cfg.Model, EndpointHost: inst.host, Messages: len(req.Messages), Tools: len(req.Tools),
		ToolsDigest:  toolsDigest(req.Tools),
		Conversation: req.Conversation, Turn: req.Turn, Round: int(req.Round), ParentCallId: req.ParentCallId,
		OmitFrom: int(req.OmitFrom), OmitTo: int(req.OmitTo), RetainAsked: retained,
	}
	// The call's messages reach the trail whether it is answered, refused or
	// fails (ADR-0277 §SD3); a retained request adds their text (ADR-0264).
	t := &turn{conversation: req.Conversation, parent: req.ParentCallId, messages: req.Messages,
		omitFrom: int(req.OmitFrom), omitTo: int(req.OmitTo)}
	if retained && req.Conversation == "" {
		inst.refuse(msg, "a retained request names its conversation", rec, t)
		return
	}
	for _, m := range req.Messages {
		rec.PromptBytes += len(m.Content)
		// Attached images are prompt too, and usually most of its bytes.
		for _, img := range m.Images {
			rec.PromptBytes += len(img.Data)
		}
	}
	if inst.cfg.Retain.atLeast(RetainRing) {
		rec.Prompt = joinMessages(req.Messages)
	}
	if !inst.cfg.Configured() {
		inst.refuse(msg, inst.Describe().Reason, rec, t)
		return
	}
	if len(req.Messages) == 0 {
		inst.refuse(msg, "the request carries no messages", rec, t)
		return
	}
	if req.OnBehalfTask != "" {
		ref := inst.delegation.Load()
		if ref == nil {
			inst.refuse(msg, "agent-caused work, and no dispatcher to check its grant", rec, t)
			return
		}
		// The task goes on the row only as the dispatcher attests it
		// (ADR-0277 §SD1): a context it does not confirm is refused and
		// recorded as the sender's own call.
		cc, ok, why := ref.d.CallContext(req.OnBehalfTask, req.OnBehalfEpoch, req.OnBehalfCall, msg.Sender, msg.SenderInstance)
		if !ok {
			inst.refuse(msg, "agent-caused work the dispatcher does not attest: "+why, rec, t)
			return
		}
		rec.Task, rec.TaskEpoch, rec.TaskCall = cc.Task, cc.Epoch, cc.Call
		if ok, why := ref.d.AllowDestination(cc.Task, cc.Epoch, DelegationDestination); !ok {
			inst.refuse(msg, "agent-caused work: "+why, rec, t)
			return
		}
	}
	// The sensitivity wall (ADR-0254 §SD3, the ADR-0145 rule): confined
	// content leaves for a loopback provider and nowhere else.
	if rec.Sensitivity == queryengine.SensitivityConfined && !inst.local {
		inst.refuse(msg, "the content derives from sealed data that must not leave this box, and "+inst.host+" is neither loopback nor a trusted host", rec, t)
		return
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = inst.cfg.MaxTokens
	}
	rec.MaxTokens = maxTokens
	// Written ahead: what the request adds to the conversation is on the
	// trail before the request leaves the machine, so a process that dies
	// mid-call leaves "sent, outcome unknown" rather than nothing.
	inst.writeRequest(&rec, t)
	ahead, refuse := inst.cfg.Trail.WriteAhead(inst.base)
	if refuse != nil {
		inst.refuse(msg, refuse.Error(), rec, t)
		return
	}
	t.notAhead = !ahead
	ctx, cancel := context.WithTimeout(inst.base, inst.cfg.Timeout)
	defer cancel()
	if req.CancelKey != "" {
		k := cancelKey{sender: msg.Sender, instance: msg.SenderInstance, key: req.CancelKey}
		inst.mu.Lock()
		inst.running[k] = cancel
		inst.mu.Unlock()
		defer func() {
			inst.mu.Lock()
			delete(inst.running, k)
			inst.mu.Unlock()
		}()
	}
	if req.DeadlineUnixNanos > 0 {
		if d := time.Unix(0, req.DeadlineUnixNanos); d.Before(time.Now().Add(inst.cfg.Timeout)) {
			var c2 context.CancelFunc
			ctx, c2 = context.WithDeadline(ctx, d)
			defer c2()
		}
	}
	started := time.Now()
	resp, cerr := inst.client.Complete(ctx, openaichat.CompletionRequest{
		ModelId: inst.cfg.Model, Messages: req.Messages, Temperature: req.Temperature, MaxTokens: maxTokens,
		Seed: req.Seed, Stop: req.Stop, EnableThinking: req.EnableThinking, Tools: req.Tools,
		ToolChoice: req.ToolChoice, ResponseFormat: req.ResponseFormat, Extra: extra,
	})
	rec.Elapsed = time.Since(started)
	rec.InputTokens, rec.OutputTokens = resp.InputTokens, resp.OutputTokens
	rec.CompletionBytes = len(resp.Content)
	rec.ToolCalls = len(resp.ToolCalls)
	rec.FinishReason = resp.FinishReason
	rec.ProviderId, rec.ReportedModel = resp.Id, resp.Model
	if inst.cfg.Retain.atLeast(RetainRing) {
		rec.Completion = resp.Content
	}
	rep := wireReply{
		Ok: true, Content: resp.Content, Reasoning: resp.Reasoning, FinishReason: resp.FinishReason, ToolCalls: resp.ToolCalls,
		InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, ElapsedNs: int64(rec.Elapsed), CallId: rec.CallId,
	}
	if cerr != nil {
		if errors.Is(cerr, openaichat.ErrIncompleteCompletion) && resp.Content != "" {
			// A truncated completion still carries its content (the
			// client's documented contract): hand it over marked rather
			// than losing work the provider already did.
			rep.Incomplete, rec.Incomplete = true, true
		} else {
			rep.Ok, rep.Reason, rep.ErrorKind = false, cerr.Error(), kindOf(cerr)
			if errors.Is(cerr, openaichat.ErrIncompleteCompletion) {
				// Truncated with nothing to hand over: incomplete, and
				// failed — the row's Error says so.
				rep.Reason, rec.Incomplete = incompleteReason(resp, maxTokens), true
			}
			rec.Error = rep.Reason
		}
	}
	if rep.Ok {
		// The reply as the app will echo it back: what the next turn's
		// prefix is hashed against.
		t.reply = option.Some(openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls})
		t.reasoning = resp.Reasoning
	}
	rep.Retention, rep.RetentionReason = inst.record(rec, t)
	lg := inst.log.Debug()
	if !rep.Ok {
		lg = inst.log.Warn()
	}
	lg.Str("sender", string(msg.Sender)).Str("purpose", req.Purpose).Bool("ok", rep.Ok).Str("reason", rep.Reason).
		Int32("in", resp.InputTokens).Int32("out", resp.OutputTokens).Dur("elapsed", rec.Elapsed).Msg("llm: complete")
	inst.reply(msg.Reply, rep)
}

// refuse answers with the reason and records the refusal: a refused call
// is a call the table should show, and a retained turn's messages are kept
// with it (ADR-0264 §SD4).
func (inst *Service) refuse(msg *app.Msg, reason string, rec CallRecord, t *turn) {
	rec.Refused, rec.Error = true, reason
	if rec.At.IsZero() {
		rec.At, rec.Sender, rec.SenderInstance = time.Now().UTC(), msg.Sender, msg.SenderInstance
	}
	if rec.CallId == "" {
		rec.CallId = inst.mintCallId()
	}
	inst.record(rec, t)
	inst.log.Warn().Str("sender", string(msg.Sender)).Str("purpose", rec.Purpose).Str("reason", reason).Msg("llm: refused")
	inst.reply(msg.Reply, wireReply{ErrorKind: errKindRefused, Reason: reason, CallId: rec.CallId})
}

func (inst *Service) reply(inbox string, v any) {
	payload, err := encode(v)
	if err != nil {
		inst.log.Error().Err(err).Msg("llm: encode reply")
		return
	}
	if err = inst.busClient.Publish(inbox, payload); err != nil {
		inst.log.Warn().Err(err).Str("inbox", inbox).Msg("llm: publish reply")
	}
}

// EndpointHost is the endpoint reduced to its host, for the visibility
// label an app shows beside the gesture: where text goes should be
// readable where it is sent from.
func EndpointHost(endpoint string) (host string) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	return u.Host
}

// isLocalEndpoint says the endpoint's host is loopback: a literal loopback
// address or "localhost". Not resolved — a name that resolves to loopback
// through a hosts file is not a fact this process can vouch for.
func isLocalEndpoint(endpoint string) (yes bool) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// isTrustedEndpoint says the endpoint's host is one the deployment lists in
// TrustedHosts.
func isTrustedEndpoint(endpoint string, trusted []string) (yes bool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return false
	}
	h := u.Hostname()
	for _, t := range trusted {
		if strings.EqualFold(strings.Trim(t, "[]"), h) {
			return true
		}
	}
	return false
}

func joinMessages(ms []openaichat.Message) (s string) {
	var b strings.Builder
	for i, m := range ms {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(m.Role.String())
		b.WriteString(": ")
		b.WriteString(m.Content)
	}
	return b.String()
}

// mintCallId is the call's identity: this process's run-scoped counter
// behind the answer instant, unique on the box the way a run id is, and
// the natural key of the facts row.
func (inst *Service) mintCallId() (id string) {
	inst.mu.Lock()
	inst.minted++
	n := inst.minted
	inst.mu.Unlock()
	return "llm-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "-" + strconv.FormatUint(n, 36)
}

type delegationRef struct{ d app.DelegationI }

// DelegationDestination is how a grant names the model service.
const DelegationDestination = "llm"

// SetDelegation installs the check for agent-caused completions: the host's
// dispatcher (ADR-0269 §SD6). Until it is set, such completions are
// refused.
func (inst *Service) SetDelegation(d app.DelegationI) { inst.delegation.Store(&delegationRef{d: d}) }

// incompleteReason says why an answer the provider ended early has no text:
// how it ended, what it spent against the ceiling the call had, and whether
// the model spent it reasoning — what a person needs to tell a reasoning
// model's budget from a filter.
func incompleteReason(resp openaichat.CompletionResponse, maxTokens int32) (s string) {
	s = "the provider ended the answer early (finish_reason " + strconv.Quote(resp.FinishReason) + ") after " +
		strconv.FormatInt(int64(resp.OutputTokens), 10) + " output tokens"
	if maxTokens > 0 {
		s += " of the call's ceiling of " + strconv.FormatInt(int64(maxTokens), 10) + " (BOXER_LLM_MAXTOKENS when the request names none)"
	}
	s += ", with no answer text"
	if resp.Reasoning != "" {
		s += "; the model spent them reasoning (" + strconv.Itoa(len(resp.Reasoning)) + " bytes of reasoning)"
	}
	return
}
