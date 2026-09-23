package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

const appId app.AppIdT = "test.llm.app"

// fakeProvider scripts one answer and records the request it saw.
type fakeProvider struct {
	resp openaichat.CompletionResponse
	err  error
	seen openaichat.CompletionRequest
}

func (f *fakeProvider) Complete(_ context.Context, req openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.seen = req
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
	assert.False(t, cfg.KeepMessages)
}
