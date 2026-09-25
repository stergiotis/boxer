package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
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
	row := RowOf(rec)
	assert.Equal(t, kindLabel, row.Kind)
	assert.Equal(t, []byte("llm-x-1"), row.NaturalKey)
	assert.Equal(t, "confined", row.Sensitivity)
	assert.Equal(t, []string{"boom"}, row.Error)

	back := RecordOf(row)
	rec.Id, rec.Prompt, rec.Completion = 0, "", ""
	assert.Equal(t, rec, back, "everything but the id and the bodies survives the row")
	assert.Empty(t, RowOf(CallRecord{CallId: "c"}).Error, "no error, no element")
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
	cfg.Exec = exec
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
// the call's prompt size (ADR-0257, proposed, §SD7).
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
	assert.Equal(t, 1+len(img.Data), svc.Calls()[0].PromptBytes)
}
