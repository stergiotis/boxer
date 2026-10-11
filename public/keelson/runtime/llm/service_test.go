package llm

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

const appId app.AppIdT = "test.llm.app"

// fakeProvider scripts one answer and records the request it saw.
type fakeProvider struct {
	resp openaichat.CompletionResponse
	err  error
	seen openaichat.CompletionRequest
	// during, when set, runs while the provider "answers".
	during func()
	// calls counts what reached the provider, for the tests that race.
	calls atomic.Int32
}

func (f *fakeProvider) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.calls.Add(1)
	f.seen = req
	if f.during != nil {
		f.during()
	}
	return f.resp, f.err
}
func (f *fakeProvider) Close() (err error) { return }

// serve is the host's arrangement: the service on one bus, an app client
// holding ClientCaps, the bus's audit sink.
func serve(t *testing.T, cfg Config) (cli *Client, svc *Service, sink *audit.InMemoryAuditSink) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	sink = audit.NewInMemoryAuditSink()
	bus.SetAuditSink(sink)
	var err error
	svc, err = NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli = NewClient(bus.NewClient(appId, ClientCaps("test: ask")))
	cli.Timeout = 5 * time.Second
	return
}

func localCfg(p *fakeProvider) Config {
	return Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", MaxTokens: 64, Client: p}
}

// An unconfigured host says so on describe and refuses complete with the
// same reason; nothing times out.
func TestUnconfiguredHostSaysSo(t *testing.T) {
	cli, _, _ := serve(t, Config{})
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.False(t, d.Configured)
	assert.Contains(t, d.Reason, "BOXER_LLM_ENDPOINT")
	_, err = cli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
}

// An answer the provider ended early with no text says how it ended, what
// it spent against the ceiling, and that the model was reasoning.
func TestAnAnswerCutOffWithoutTextSaysWhy(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
	p := &fakeProvider{resp: openaichat.CompletionResponse{FinishReason: "length", OutputTokens: 64, Reasoning: "thinking"},
		err: openaichat.ErrIncompleteCompletion}
	cfg := localCfg(p)
	cfg.MaxTokens = 64
	cli, svc, _ := serve(t, cfg)
	_, err := cli.Complete(context.Background(), Request{Messages: msg})
	var failed *CallError
	require.True(t, errors.As(err, &failed), "%v", err)
	assert.Equal(t, errKindIncomplete, failed.Kind)
	assert.True(t, errors.Is(err, openaichat.ErrIncompleteCompletion))
	assert.Contains(t, failed.Reason, `finish_reason "length"`)
	assert.Contains(t, failed.Reason, "64 output tokens of the call's ceiling of 64")
	assert.Contains(t, failed.Reason, "spent them reasoning")
	assert.Equal(t, "thinking", failed.Reasoning, "the reasoning comes back for inspection")
	require.Len(t, svc.Calls(), 1)
	assert.Equal(t, failed.Reason, svc.Calls()[0].Error, "the call record says the same")
}

// A failure names its row in the call record, so a caller can show where
// to read it; a refusal does as well.
func TestAFailureNamesItsCallRecord(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
	p := &fakeProvider{err: openaichat.ErrServer}
	cli, svc, _ := serve(t, localCfg(p))
	_, err := cli.Complete(context.Background(), Request{Messages: msg})
	var failed *CallError
	require.True(t, errors.As(err, &failed), "%v", err)
	assert.Equal(t, errKindServer, failed.Kind)
	require.Len(t, svc.Calls(), 1)
	assert.Equal(t, svc.Calls()[0].CallId, CallIdOf(err))
	assert.True(t, errors.Is(err, openaichat.ErrServer))

	cli, svc, _ = serve(t, Config{})
	_, err = cli.Complete(context.Background(), Request{Messages: msg})
	require.Len(t, svc.Calls(), 1)
	assert.NotEmpty(t, CallIdOf(err))
	assert.Equal(t, svc.Calls()[0].CallId, CallIdOf(err))
	assert.Empty(t, CallIdOf(context.Canceled))
}

// A completion goes through with the host's model and ceiling, the reply
// carries the answer and the counts, the bus audits the app as sender, and
// the call table shows the row.
func TestCompleteThroughTheHostsModel(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop", InputTokens: 10, OutputTokens: 20}}
	cli, svc, sink := serve(t, localCfg(p))
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.True(t, d.Configured && d.Local)
	assert.Equal(t, "127.0.0.1:1234", d.EndpointHost)

	res, err := cli.Complete(context.Background(), Request{Purpose: "test-ask",
		Messages: []openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: "sys"}, {Role: openaichat.ChatRoleUser, Content: "hi"}}})
	require.NoError(t, err)
	assert.Equal(t, "out", res.Content)
	assert.EqualValues(t, 10, res.InputTokens)
	assert.Equal(t, "m", p.seen.ModelId)
	assert.EqualValues(t, 64, p.seen.MaxTokens, "the host's ceiling when the request names none")
	assert.Len(t, p.seen.Messages, 2)

	calls := svc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, appId, calls[0].Sender)
	assert.Equal(t, "test-ask", calls[0].Purpose)
	assert.EqualValues(t, 20, calls[0].OutputTokens)
	assert.Empty(t, calls[0].Prompt, "bodies are not kept unless asked")

	var seen bool
	for _, rec := range sink.Records() {
		if rec.Subject == SubjectComplete && rec.AppId == appId {
			seen = true
		}
	}
	assert.True(t, seen, "the call is a request the bus audits with the app as sender")
}

// A request's provider-specific members reach the provider as the app sent
// them, nested members included; a request without any sends none.
func TestExtraReachesTheProvider(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop"}}
	cli, _, _ := serve(t, localCfg(p))
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}

	_, err := cli.Complete(context.Background(), Request{Purpose: "test-extra", Messages: msg,
		Extra: map[string]any{"dry_multiplier": 0.0, "chat_template_kwargs": map[string]any{"enable_thinking": false}}})
	require.NoError(t, err)
	assert.EqualValues(t, 0, p.seen.Extra["dry_multiplier"])
	kwargs, ok := p.seen.Extra["chat_template_kwargs"].(map[string]any)
	require.True(t, ok, "a nested member stays an object: %T", p.seen.Extra["chat_template_kwargs"])
	assert.Equal(t, false, kwargs["enable_thinking"])

	_, err = cli.Complete(context.Background(), Request{Purpose: "test-extra", Messages: msg})
	require.NoError(t, err)
	assert.Empty(t, p.seen.Extra)
}

// The sensitivity wall: confined content is refused against a remote
// endpoint and served against a loopback one.
func TestConfinedContentStaysOnTheBox(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "secret"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote})
	_, err := cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "sealed data")
	assert.Empty(t, remote.seen.Messages, "nothing reached the provider")
	require.Len(t, svc.Calls(), 1)
	assert.True(t, svc.Calls()[0].Refused)

	local := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ = serve(t, localCfg(local))
	_, err = cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	require.NoError(t, err)
	assert.Len(t, local.seen.Messages, 1)
}

// Provider failures come back as the sentinel they were; a truncated
// completion keeps its content; tool calls come back unexecuted.
func TestFailuresKeepTheirKind(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
	p := &fakeProvider{err: openaichat.ErrAuth}
	cli, _, _ := serve(t, localCfg(p))
	_, err := cli.Complete(context.Background(), Request{Messages: msg})
	assert.True(t, errors.Is(err, openaichat.ErrAuth), "%v", err)

	p = &fakeProvider{resp: openaichat.CompletionResponse{Content: "partial"}, err: openaichat.ErrIncompleteCompletion}
	cli, _, _ = serve(t, localCfg(p))
	res, err := cli.Complete(context.Background(), Request{Messages: msg})
	require.NoError(t, err)
	assert.True(t, res.Incomplete)
	assert.Equal(t, "partial", res.Content)

	p = &fakeProvider{resp: openaichat.CompletionResponse{ToolCalls: []openaichat.ToolCall{{Id: "c1", Name: "keelson_query", Arguments: `{"sql":"SELECT 1"}`}}}}
	cli, _, _ = serve(t, localCfg(p))
	res, err = cli.Complete(context.Background(), Request{Messages: msg, Tools: []openaichat.Tool{{Name: "keelson_query", Description: "d"}}})
	require.NoError(t, err)
	require.Len(t, res.ToolCalls, 1)
	assert.Equal(t, "keelson_query", res.ToolCalls[0].Name)
	assert.Len(t, p.seen.Tools, 1, "the definitions reached the provider; the call did not run here")
}

// Without the grant the bus refuses the publish.
func TestNoGrantIsAPermissionError(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), Config{})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient(appId, nil))
	cli.Timeout = time.Second
	_, err = cli.Describe(context.Background())
	require.Error(t, err)
}

// A host the deployment trusts is treated as loopback by the wall, and
// says so in describe; any other remote host is still refused.
func TestATrustedHostReceivesConfinedContent(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "secret"}}
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ := serve(t, Config{Endpoint: "http://AmpereOne.lan:8080/v1", Model: "m", Client: p,
		TrustedHosts: []string{"ampereone.lan"}})
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.True(t, d.Local)
	assert.True(t, d.Trusted, "describe tells a trusted host from loopback")
	_, err = cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	require.NoError(t, err)
	assert.Len(t, p.seen.Messages, 1)

	other := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ = serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: other,
		TrustedHosts: []string{"ampereone.lan"}})
	d, err = cli.Describe(context.Background())
	require.NoError(t, err)
	assert.False(t, d.Local)
	assert.False(t, d.Trusted)
	_, err = cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "neither loopback nor a trusted host")
	assert.Empty(t, other.seen.Messages)
}

func TestTrustedHostsParse(t *testing.T) {
	assert.Equal(t, []string{"a.lan", "10.0.0.5", "[fd00::1]"}, ParseTrustedHosts(" a.lan,,10.0.0.5 , [fd00::1]"))
	assert.Empty(t, ParseTrustedHosts(""))
	assert.True(t, isTrustedEndpoint("http://[fd00::1]:8080/v1", []string{"[fd00::1]"}))
	assert.True(t, isTrustedEndpoint("http://10.0.0.5:8080/v1", []string{"10.0.0.5"}))
	assert.False(t, isTrustedEndpoint("http://10.0.0.6:8080/v1", []string{"10.0.0.5"}))
	assert.False(t, isTrustedEndpoint("http://evil.a.lan/v1", []string{"a.lan"}), "a name, not a suffix")
}

func TestLocalEndpoint(t *testing.T) {
	assert.True(t, isLocalEndpoint("http://localhost:1234/v1"))
	assert.True(t, isLocalEndpoint("http://127.0.0.1:1234/v1"))
	assert.True(t, isLocalEndpoint("http://[::1]:8080/v1"))
	assert.False(t, isLocalEndpoint("https://api.example.net/v1"))
	assert.False(t, isLocalEndpoint("http://192.168.1.5:1234/v1"))
	assert.Equal(t, "api.example.net", EndpointHost("https://api.example.net/v1"))
}

// The env gate: endpoint AND model, neither with a default.
func TestConfigFromEnvGatesOnEndpointAndModel(t *testing.T) {
	Endpoint.SetForTest(t, "")
	Model.SetForTest(t, "")
	assert.False(t, ConfigFromEnv().Configured(), "no endpoint, no model")
	Endpoint.SetForTest(t, "http://localhost:1234/v1")
	assert.False(t, ConfigFromEnv().Configured(), "an endpoint without a model is still no model — a wrong default is worse than a refusal")
	Model.SetForTest(t, "qwen3")
	cfg := ConfigFromEnv()
	assert.True(t, cfg.Configured())
	assert.Equal(t, int32(4096), cfg.MaxTokens)
	assert.Equal(t, 120*time.Second, cfg.Timeout)
	assert.Equal(t, RetainOff, cfg.Retain, "text is not kept unless the deployment says so")
}

// The durable row carries the counts and the verdict and never the text,
// and reads back as the record it came from.
func TestRowRoundTrip(t *testing.T) {
	rec := CallRecord{
		CallId: "llm-x-1", At: time.Unix(1700000000, 0).UTC(), Sender: appId, SenderInstance: 7,
		Purpose: "play/ask", Sensitivity: queryengine.SensitivityConfined, Model: "m", EndpointHost: "127.0.0.1:1234",
		Messages: 3, Tools: 2, PromptBytes: 400, CompletionBytes: 120, InputTokens: 100, OutputTokens: 30, ToolCalls: 1,
		FinishReason: "stop", Elapsed: 1500 * time.Millisecond, Incomplete: true, Error: "boom",
		Prompt: "secret prompt", Completion: "secret answer",
	}
	rec.Durable = true
	ent := entityOf(rec, false)
	row := ent.LlmCall.Val
	assert.Equal(t, "confined", row.Sensitivity)
	assert.Equal(t, []string{"boom"}, row.Error)
	assert.Equal(t, string(appId), ent.Origin.Val.App, "who asked is the row's origin")

	back := RecordOf(ent)
	rec.Id, rec.Prompt, rec.Completion = 0, "", ""
	assert.Equal(t, rec, back, "everything but the id and the bodies survives the row")
	assert.Empty(t, RowOf(CallRecord{CallId: "c"}, false).Error, "no error, no element")
}

// entityOf is the trail entity a record lands as: its call row and the
// context components the record names.
func entityOf(rec CallRecord, kept bool) (ent *trail.TrailEntity) {
	c := (&Service{}).contextOf(rec)
	return &trail.TrailEntity{Ts: rec.At, Origin: option.Some(c.Origin), Conversation: c.Conversation, Delegation: c.Delegation,
		LlmCall: option.Some(RowOf(rec, kept))}
}

// A service without an executor is not durable and scans nothing; with
// one it says so — the wiring hostboot relies on.
func TestDurableOnlyWithAnExecutor(t *testing.T) {
	_, svc, _ := serve(t, Config{})
	assert.False(t, svc.Durable())
	recs, err := svc.ScanCalls(context.Background(), time.Time{}, 10)
	require.NoError(t, err)
	assert.Nil(t, recs)
}

// Over clickhouse-local with the facts table provisioned the way chstore
// provisions it: a completion lands as one llmCall row that scans back as
// the record, and a refusal lands too.
func TestCallsLandOnTheFactsTable(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	setup, err := chstore.ComposeSetupSQL(chstore.Config{Database: factsschema.DatabaseName, Table: factsschema.TableName}, "")
	require.NoError(t, err)
	for stmt := range strings.SplitSeq(setup, ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			require.NoError(t, exec.Exec(ctx, stmt))
		}
	}

	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop", InputTokens: 10, OutputTokens: 20}}
	cfg := localCfg(p)
	cfg.Trail = trail.NewRecorder(exec, "run-test", zerolog.Nop())
	t.Cleanup(cfg.Trail.Close)
	cli, svc, _ := serve(t, cfg)
	require.True(t, svc.Durable())
	_, err = cli.Complete(ctx, Request{Purpose: "test/ask", Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}})
	require.NoError(t, err)
	_, err = cli.Complete(ctx, Request{Purpose: "test/ask"})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)

	rows, err := svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ring := svc.Calls()
	assert.Equal(t, ring[0].CallId, rows[0].CallId, "the row is the record, by id")
	assert.Equal(t, appId, rows[0].Sender)
	assert.Equal(t, "test/ask", rows[0].Purpose)
	assert.EqualValues(t, 20, rows[0].OutputTokens)
	assert.Equal(t, "stop", rows[0].FinishReason)
	assert.True(t, rows[1].Refused)
	assert.Contains(t, rows[1].Error, "no messages")
}

// An image attached to a message crosses the bus intact and counts toward
// the call's prompt size (ADR-0266, proposed, §SD7).
func TestImagesCrossTheBus(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "a", FinishReason: "stop"}}
	cli, svc, _ := serve(t, localCfg(p))
	img := openaichat.Image{MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G', 0, 1, 2}}
	_, err := cli.Complete(context.Background(), Request{Purpose: "test-look",
		Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "q", Images: []openaichat.Image{img}}}})
	require.NoError(t, err)
	require.Len(t, p.seen.Messages, 1)
	assert.Equal(t, []openaichat.Image{img}, p.seen.Messages[0].Images)
	require.Len(t, svc.Calls(), 1)
	body, err := svc.cfg.Route.Encode(p.seen)
	require.NoError(t, err)
	assert.Equal(t, len(body), svc.Calls()[0].PromptBytes, "the prompt's size is the body as sent, the image inside it")
	assert.Greater(t, svc.Calls()[0].PromptBytes, len(img.Data))
}

// blockingProvider holds a completion until its context ends, and says
// how it ended.
type blockingProvider struct {
	started chan struct{}
	ended   chan error
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}, 1), ended: make(chan error, 1)}
}

func (f *blockingProvider) Complete(ctx context.Context, _ openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.started <- struct{}{}
	<-ctx.Done()
	f.ended <- ctx.Err()
	return openaichat.CompletionResponse{}, ctx.Err()
}
func (f *blockingProvider) Close() (err error) { return }

func userMessage() []openaichat.Message {
	return []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
}

// Cancelling the requester's context returns at once and stops the
// provider call on the service side; the call table records it.
func TestCancelStopsTheProviderCall(t *testing.T) {
	p := newBlockingProvider()
	cli, svc, _ := serve(t, Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := cli.Complete(ctx, Request{Purpose: "cancel", Messages: userMessage()})
		errc <- err
	}()
	<-p.started
	cancel()
	select {
	case err := <-errc:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Complete did not return on cancellation")
	}
	select {
	case err := <-p.ended:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the provider call was not cancelled")
	}
	require.Eventually(t, func() bool {
		calls := svc.Calls()
		return len(calls) == 1 && calls[0].Error != ""
	}, 5*time.Second, 10*time.Millisecond)
}

// Client.Timeout bounds the wait in-process too: the service answers off
// the requester's goroutine, so a slow provider no longer holds it.
func TestClientTimeoutHoldsInProcess(t *testing.T) {
	p := newBlockingProvider()
	cli, _, _ := serve(t, Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p})
	cli.Timeout = 50 * time.Millisecond
	t0 := time.Now()
	_, err := cli.Complete(context.Background(), Request{Messages: userMessage()})
	require.Error(t, err)
	assert.ErrorIs(t, err, inprocbus.ErrTimeout)
	assert.Less(t, time.Since(t0), 5*time.Second)
}

// Without a Timeout of its own the client waits as long as the service
// bounds a completion, plus the reply margin: the caller sees the
// service's timeout, with its call id, not a bus timeout of its own.
func TestClientWaitFollowsTheServiceTimeout(t *testing.T) {
	p := newBlockingProvider()
	cli, _, _ := serve(t, Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p, Timeout: 50 * time.Millisecond})
	cli.Timeout = 0
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 50*time.Millisecond, d.Timeout)
	_, err = cli.Complete(context.Background(), Request{Messages: userMessage()})
	require.Error(t, err)
	assert.NotErrorIs(t, err, inprocbus.ErrTimeout)
	assert.NotEmpty(t, CallIdOf(err), "the service answered the timeout")
	assert.Equal(t, 50*time.Millisecond+ReplyMargin, cli.completionWait(context.Background()))
}

// A client whose wait runs out asks the service to stop the call, so the
// provider does not run on for an answer no one will read.
func TestAnExpiredWaitCancelsTheCall(t *testing.T) {
	p := newBlockingProvider()
	cli, _, _ := serve(t, Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p, Timeout: time.Minute})
	cli.Timeout = 50 * time.Millisecond
	_, err := cli.Complete(context.Background(), Request{Messages: userMessage()})
	require.ErrorIs(t, err, inprocbus.ErrTimeout)
	select {
	case err = <-p.ended:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the provider call ran on after the client stopped waiting")
	}
}

// Another app cannot stop a call by guessing its key: cancel keys are
// scoped to the sender.
func TestCancelIsScopedToTheSender(t *testing.T) {
	p := newBlockingProvider()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	owner := bus.NewClient(appId, ClientCaps("test: ask"))
	other := bus.NewClient("test.llm.other", ClientCaps("test: other"))
	payload, err := encode(wireRequest{V: wireVersion, Messages: userMessage(), CancelKey: "k"})
	require.NoError(t, err)
	go func() { _, _ = owner.RequestWithTimeout(SubjectComplete, payload, 5*time.Second) }()
	<-p.started
	stop, err := encode(wireCancel{V: wireVersion, Key: "k"})
	require.NoError(t, err)
	require.NoError(t, other.Publish(SubjectCancel, stop))
	select {
	case <-p.ended:
		t.Fatal("another sender stopped the call")
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, owner.Publish(SubjectCancel, stop))
	select {
	case <-p.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the owner could not stop its call")
	}
}

// A provider failure crosses the bus with its kind and its reason: the
// caller can branch on the sentinel and show the provider's message.
func TestAProviderFailureKeepsItsKindAndItsReason(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
	p := &fakeProvider{err: &openaichat.HTTPError{Status: 402, Message: "This request requires more credits"}}
	cli, _, _ := serve(t, localCfg(p))
	_, err := cli.Complete(context.Background(), Request{Messages: msg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 402: This request requires more credits")
	assert.Equal(t, errKindPayment, kindOf(fmt.Errorf("x: %w", openaichat.ErrPaymentRequired)))
	assert.True(t, errors.Is(failureOf(wireReply{ErrorKind: errKindPayment, Reason: "HTTP 402: no credit"}), openaichat.ErrPaymentRequired))
	assert.Contains(t, failureOf(wireReply{ErrorKind: errKindPayment, Reason: "HTTP 402: no credit"}).Error(), "no credit")
}

// The wall in tiers (ADR-0312 §SD2): a remote route clears GREEN by
// default, so AMBER content declared as such is refused with the sink
// named, GREEN passes, and a loopback route takes RED; confined content
// carries the confined tier whatever it declares; a declared route
// clearance of AMBER lets AMBER out. What leaves a remote route is charged
// as bytes sent.
func TestTheWallReadsATierAgainstTheRoutesClearance(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote})
	_, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "AMBER, above the clearance (GREEN) of the model route api.example.net")
	assert.Empty(t, remote.seen.Messages)
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierGreen), Messages: msg})
	require.NoError(t, err)
	assert.Len(t, remote.seen.Messages, 1)
	sinks := svc.Cell().Sinks()
	require.NotEmpty(t, sinks)
	assert.Equal(t, cell.SinkKindModelRoute, sinks[0].Kind)
	assert.Equal(t, "api.example.net", sinks[0].Edge)
	calls := svc.Calls()
	require.Len(t, calls, 2)
	assert.True(t, calls[0].Refused)

	local := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ = serve(t, localCfg(local))
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierRed), Messages: msg})
	require.NoError(t, err, "inside the cell nothing is withheld")
	for _, s := range svc.Cell().Sinks() {
		assert.NotEqual(t, cell.SinkKindModelRoute, s.Kind, "a loopback route is no sink")
	}

	open := cell.Defaults()
	open.Clearances.ModelRoute = agentrun.TierAmber
	remote = &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ = serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Cell: &open})
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg})
	require.NoError(t, err, "the deployment cleared AMBER for the route")
	_, err = cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	require.True(t, errors.As(err, &refused), "confined content keeps the compile-date refusal on a remote route")
}

// Bytes sent to a remote route are the exit budget's quantity (law L19):
// the exact size of the request body is reserved at admission, so the
// call that would cross the budget is refused and the account never
// exceeds the limit; nothing is charged on a local route.
func TestBytesSentAreTheExitBudgetsQuantity(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "0123456789"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote})
	_, err := cli.Complete(context.Background(), Request{Messages: msg})
	require.NoError(t, err)
	n := int64(svc.Calls()[0].PromptBytes)
	require.Greater(t, n, int64(len(msg[0].Content)), "the body is larger than its text")
	limit := 3*n - 1
	require.NoError(t, svc.ledger.Set(ration.Rule{Id: "exit", Select: ration.Selector{Kind: ration.AccountKindRun}, Kind: ration.RuleKindBudget,
		Quantity: ration.QuantityBytesSent, Limit: limit, Window: time.Hour, Author: "person"}))
	_, err = cli.Complete(context.Background(), Request{Messages: msg})
	require.NoError(t, err, "the second call fits")
	_, err = cli.Complete(context.Background(), Request{Messages: msg})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "exit")
	assert.Equal(t, ration.RefusalStop, refused.Refusal)
	for _, a := range svc.ledger.Accounts() {
		if a.Account.Kind == ration.AccountKindRun {
			assert.Equal(t, 2*n, a.Totals[ration.QuantityBytesSent], "what left is what was reserved")
			assert.LessOrEqual(t, a.Totals[ration.QuantityBytesSent], limit, "never over the limit")
			assert.Zero(t, a.Reserved[ration.QuantityBytesSent], "the refusal reserved nothing")
		}
	}

	local := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ = serve(t, localCfg(local))
	require.NoError(t, svc.ledger.Set(ration.Rule{Id: "exit", Select: ration.Selector{Kind: ration.AccountKindRun}, Kind: ration.RuleKindBudget,
		Quantity: ration.QuantityBytesSent, Limit: 1, Window: time.Hour, Author: "person"}))
	for range 4 {
		_, err = cli.Complete(context.Background(), Request{Messages: msg})
		require.NoError(t, err, "a local route sends nothing out of the cell")
	}
}

// fakeApprovals is a checker over the entries a test planted; every run
// is the test app's unless owners says otherwise.
type fakeApprovals struct {
	entries map[string]agentrun.Entry
	owners  map[string]string
}

func (f *fakeApprovals) ApprovalEntry(_ context.Context, run string, ordinal uint32) (e agentrun.Entry, owner string, found bool, err error) {
	e, found = f.entries[cell.ApprovalKey(run, ordinal)]
	owner = string(appId)
	if o, ok := f.owners[run]; ok {
		owner = o
	}
	return
}

// plant journals an approval of the refused call for the test.
func (f *fakeApprovals) plant(t *testing.T, run string, ordinal uint32, callId string, sink string, path string) (key string) {
	t.Helper()
	body, err := cell.Approval{Channel: "model-route", Tier: "AMBER", Path: path, Approver: "person", Justification: "for the report"}.Body()
	require.NoError(t, err)
	key = cell.ApprovalKey(run, ordinal)
	f.entries[key] = agentrun.Entry{Run: run, Ordinal: ordinal, Kind: agentrun.KindApproval, Key: callId, Name: sink, Body: body}
	return
}

// An exit above the clearance that the person may approve comes back as a
// typed refusal naming the path, the sink and the tier (ADR-0312 §SD3);
// resent with the key of an approval entry the route verifies — of that
// call, for that sink, on the person's path, the same bytes and tier — it
// goes through, once, and the key is on the call's row with the tier the
// wall saw. A tier the policy never lets out stays refused, and so does
// one on the organisation path, since no organisation approver is wired.
func TestAnApprovedExitGoesThroughAndIsRecorded(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Approvals: journal})
	_, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Profile: "default@abc", Conversation: "chat-r"})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.True(t, refused.NeedsApproval())
	assert.Equal(t, "model-route api.example.net", refused.Sink)
	assert.Equal(t, "AMBER", refused.Tier)
	assert.Contains(t, refused.Reason, "person's recorded justification")
	assert.Empty(t, remote.seen.Messages)
	key := journal.plant(t, "chat-r", 17, refused.CallId, refused.Sink, "person")
	res, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: key, Profile: "default@abc", Route: "r1", Conversation: "chat-r"})
	require.NoError(t, err)
	assert.Equal(t, "x", res.Content)
	calls := svc.Calls()
	require.Len(t, calls, 2)
	assert.True(t, calls[0].Refused)
	assert.Equal(t, key, calls[1].Approval)
	assert.Equal(t, "default@abc", calls[1].Profile)
	assert.Equal(t, "r1", calls[1].Route)
	assert.EqualValues(t, agentrun.TierAmber, calls[1].Tier)
	row := RowOf(calls[1], false)
	assert.Equal(t, key, row.Approval.Val, "the key is on the durable row")
	assert.Equal(t, "default@abc", row.Profile.Val)
	assert.EqualValues(t, agentrun.TierAmber, row.Tier)

	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: key, Conversation: "chat-r"})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "names no call this route refused", "one approval covers one exit")

	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierRed), Messages: msg, Approval: key})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.False(t, refused.NeedsApproval(), "RED never leaves")
	assert.Contains(t, refused.Reason, "never leaves")
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmberStrict), Messages: msg})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.False(t, refused.NeedsApproval(), "the organisation path is not wired")
	assert.Contains(t, refused.Reason, "organisation")
}

// Confined content is one bit at the compile-date wall (ADR-0254 §SD3)
// whatever the declaration says: a managed host that clears AMBER for its
// route still refuses sealed data outright, with no approval path, since
// the tier wall generalises the bit wall for labelled content and never
// loosens it (ADR-0307 §SD6). A trusted host is inside the cell and gets
// it.
func TestAManagedCellNeverOpensSealedDataToARemoteRoute(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	managed := cell.Defaults()
	managed.Managed = true
	managed.Clearances.ModelRoute = agentrun.TierAmber
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Cell: &managed, Approvals: journal})
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg, Conversation: "chat-r"})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.False(t, refused.NeedsApproval(), "the compile-date wall has no approval path")
	assert.Contains(t, refused.Reason, "sealed data")
	assert.Empty(t, remote.seen.Messages)
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-r"})
	require.NoError(t, err, "labelled AMBER is within the route's clearance")

	trusted := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ = serve(t, Config{Endpoint: "https://models.lan/v1", Model: "m", Client: trusted, TrustedHosts: []string{"models.lan"}, Cell: &managed})
	_, err = cli.Complete(context.Background(), Request{Sensitivity: queryengine.SensitivityConfined, Messages: msg})
	require.NoError(t, err, "a trusted host is inside the cell")
}

// One approval covers one exit, under concurrency too: N resends under
// one key race through the wall and exactly one reaches the provider;
// the rest find the refused call claimed or gone.
func TestOneApprovalLetsOutExactlyOneExit(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Approvals: journal})
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-r"})
	require.True(t, errors.As(err, &refused), "%v", err)
	key := journal.plant(t, "chat-r", 17, refused.CallId, refused.Sink, "person")
	const n = 8
	var wg sync.WaitGroup
	var through atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: key, Conversation: "chat-r"}); err == nil {
				through.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, through.Load(), "one approval, one exit")
	assert.EqualValues(t, 1, remote.calls.Load())
}

// An approval binds to the run the refused request named and to the app
// that owns that run: an entry in another run, a resend naming another
// conversation, and an entry in a run another app owns are all refused.
func TestAnApprovalIsBoundToItsRunAndSender(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}, owners: map[string]string{"theirs": "other.app"}}
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Approvals: journal})
	var refused *RefusedError
	refuse := func(req Request) (reason string) {
		t.Helper()
		_, err := cli.Complete(context.Background(), req)
		require.True(t, errors.As(err, &refused), "%v", err)
		assert.Empty(t, remote.seen.Messages)
		return refused.Reason
	}
	refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-a"})
	callA, sinkA := refused.CallId, refused.Sink
	assert.NotEmpty(t, refused.Digest, "the refusal names the bytes it refused")
	elsewhere := journal.plant(t, "chat-b", 5, callA, sinkA, "person")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: elsewhere, Conversation: "chat-a"}), "another run")
	mine := journal.plant(t, "chat-a", 6, callA, sinkA, "person")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: mine, Conversation: "chat-b"}), "another run")
	refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "theirs"})
	theirs := journal.plant(t, "theirs", 2, refused.CallId, refused.Sink, "person")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: theirs, Conversation: "theirs"}), "another app owns")
	_, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: mine, Conversation: "chat-a"})
	require.NoError(t, err, "the entry in the request's own run, owned by the sender, lets it go")
}

// The route encodes a request deterministically, so the digest an
// approval binds to is one value however the provider-specific extra
// map iterates; a resend of the same request is the same bytes.
func TestTheRequestDigestIsStable(t *testing.T) {
	extra := map[string]any{}
	for i := 0; i < 10; i++ {
		extra["k"+strconv.Itoa(i)] = i
	}
	r := openaichat.CompletionRequest{ModelId: "m", Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}, Extra: extra}
	var route openaichat.Route
	first, err := route.Encode(r)
	require.NoError(t, err)
	for i := 0; i < 50; i++ {
		again, aerr := route.Encode(r)
		require.NoError(t, aerr)
		assert.Equal(t, requestDigest(first), requestDigest(again))
	}
}

// The canaries read the strings the body encodes as well as the body: a
// protected body copied verbatim across a newline, which JSON escapes,
// and a token with a quote in it are both found.
func TestCanariesReadTheDecodedStrings(t *testing.T) {
	decl := cell.Defaults()
	decl.Canaries = []cell.Canary{{Scope: "hr", Token: `say "zq-7731"`, Tier: agentrun.TierAmber}}
	decl.Protected = []cell.Protected{{Scope: "board", Body: "the quarterly revenue fell by twelve percent", Tier: agentrun.TierRed}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Cell: &decl})
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "minutes: the quarterly revenue\nfell by twelve percent"}}})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "canary of scope board")
	_, err = cli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: `please say "zq-7731" back`}}})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "canary of scope hr")
	assert.Empty(t, remote.seen.Messages)
}

// The route verifies what an approval key names: an entry of another
// call, a resend whose bytes changed, an entry not on the person's path,
// a key that is not one, and a key with no journal to check it against
// all stay refused and nothing leaves.
func TestAnApprovalThatDoesNotCoverTheExitIsRefused(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	cli, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Approvals: journal})
	refuse := func(req Request) (reason string) {
		t.Helper()
		_, err := cli.Complete(context.Background(), req)
		var refused *RefusedError
		require.True(t, errors.As(err, &refused), "%v", err)
		assert.Empty(t, remote.seen.Messages, "nothing left")
		return refused.Reason
	}
	first := refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-r"})
	assert.Contains(t, first, "recorded justification")
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-r"})
	require.True(t, errors.As(err, &refused))

	other := journal.plant(t, "chat-r", 1, "llm-not-this-call", refused.Sink, "person")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: other, Conversation: "chat-r"}), "names no call this route refused")
	free := journal.plant(t, "chat-r", 2, refused.CallId, refused.Sink, "free")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: free, Conversation: "chat-r"}), "not on the person's path")
	elsewhere := journal.plant(t, "chat-r", 3, refused.CallId, "model-route other.example.net", "person")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: elsewhere, Conversation: "chat-r"}), "another sink")
	good := journal.plant(t, "chat-r", 4, refused.CallId, refused.Sink, "person")
	changed := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "thirteen"}}
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: changed, Approval: good, Conversation: "chat-r"}), "bytes changed")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: "17", Conversation: "chat-r"}), "not <run>#<ordinal>")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: cell.ApprovalKey("chat-r", 99), Conversation: "chat-r"}), "names no approval entry")
	assert.Contains(t, refuse(Request{Tier: uint8(agentrun.TierAmberStrict), Messages: msg, Approval: good, Conversation: "chat-r"}), "organisation", "the policy's path decides before the key is read")

	unwired, _, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote})
	_, err = unwired.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Approval: good, Conversation: "chat-r"})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "cannot be verified")
	assert.Empty(t, remote.seen.Messages)
}

// The canaries (ADR-0312 §SD6, law L10): a planted token or a protected
// body of a scope above the route's clearance in the request body refuses
// the exit whatever tier the caller declared; at or below the clearance,
// or on a route inside the cell, it goes.
func TestCanariesRefuseALeakAboveTheClearance(t *testing.T) {
	decl := cell.Defaults()
	decl.Canaries = []cell.Canary{{Scope: "hr", Token: "zq-7731", Tier: agentrun.TierAmber}, {Scope: "pub", Token: "ok-1", Tier: agentrun.TierGreen}}
	decl.Protected = []cell.Protected{{Scope: "board", Body: "the quarterly revenue fell by twelve percent", Tier: agentrun.TierRed}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Cell: &decl})
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "see zq-7731"}}})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "canary of scope hr")
	assert.Empty(t, remote.seen.Messages)
	_, err = cli.Complete(context.Background(), Request{Tools: []openaichat.Tool{{Name: "t", Description: "the quarterly revenue fell by twelve percent", Parameters: []byte(`{}`)}},
		Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "scope board", "the tool schemas are part of the body")
	_, err = cli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "ok-1 is fine"}}})
	require.NoError(t, err, "a GREEN canary is within the clearance")
	require.Len(t, svc.Calls(), 3)
	assert.True(t, svc.Calls()[0].Refused)

	local := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	lcfg := localCfg(local)
	lcfg.Cell = &decl
	lcli, _, _ := serve(t, lcfg)
	_, err = lcli.Complete(context.Background(), Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "see zq-7731"}}})
	require.NoError(t, err, "inside the cell nothing is withheld")
}
