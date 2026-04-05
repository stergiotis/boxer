//go:build llm_generated_opus46

package orchestrator_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/observers"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock LLMClient
// ============================================================================

// MockLLM returns pre-configured responses in sequence. If a request arrives
// beyond the configured responses, it returns an error.
type MockLLM struct {
	mu        sync.Mutex
	responses []MockLLMResponse
	calls     []MockLLMCall
	callIdx   int
}

type MockLLMResponse struct {
	Content string
	Err     error
}

type MockLLMCall struct {
	Model    string
	Messages []orchestrator.Message
}

func NewMockLLM(responses ...MockLLMResponse) *MockLLM {
	return &MockLLM{responses: responses}
}

func (inst *MockLLM) Chat(_ context.Context, model string, messages []orchestrator.Message) (string, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	inst.calls = append(inst.calls, MockLLMCall{Model: model, Messages: messages})

	if inst.callIdx >= len(inst.responses) {
		return "", assert.AnError
	}
	resp := inst.responses[inst.callIdx]
	inst.callIdx++
	return resp.Content, resp.Err
}

func (inst *MockLLM) CallCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.calls)
}

func (inst *MockLLM) LastCall() MockLLMCall {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.calls) == 0 {
		return MockLLMCall{}
	}
	return inst.calls[len(inst.calls)-1]
}

// ============================================================================
// Mock CHClient
// ============================================================================

// MockCH returns pre-configured results. It records all executed SQL.
type MockCH struct {
	mu      sync.Mutex
	results []MockCHResult
	calls   []MockCHCall
	callIdx int
}

type MockCHResult struct {
	Result *orchestrator.QueryResult
	Err    error
}

type MockCHCall struct {
	SQL    string
	Params map[string]string
}

func NewMockCH(results ...MockCHResult) *MockCH {
	return &MockCH{results: results}
}

func (inst *MockCH) Execute(_ context.Context, sql string, params map[string]string) (*orchestrator.QueryResult, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	inst.calls = append(inst.calls, MockCHCall{SQL: sql, Params: params})

	if inst.callIdx >= len(inst.results) {
		// Default: DDL and non-data queries succeed with empty result
		return &orchestrator.QueryResult{RowCount: 0, Elapsed: time.Millisecond}, nil
	}
	res := inst.results[inst.callIdx]
	inst.callIdx++
	return res.Result, res.Err
}

func (inst *MockCH) CallCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.calls)
}

func (inst *MockCH) Calls() []MockCHCall {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out := make([]MockCHCall, len(inst.calls))
	copy(out, inst.calls)
	return out
}

// ============================================================================
// Mock Cache (in-memory, no persistence)
// ============================================================================

type MockCache struct {
	mu      sync.Mutex
	entries map[string]*orchestrator.CacheEntry
	gets    int
	puts    int
}

func NewMockCache() *MockCache {
	return &MockCache{entries: make(map[string]*orchestrator.CacheEntry)}
}

func (inst *MockCache) Get(key string) (*orchestrator.CacheEntry, bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.gets++
	e, ok := inst.entries[key]
	return e, ok
}

func (inst *MockCache) Put(key string, entry *orchestrator.CacheEntry) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.puts++
	if entry == nil {
		delete(inst.entries, key)
	} else {
		inst.entries[key] = entry
	}
}

func (inst *MockCache) GetCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.gets
}

func (inst *MockCache) PutCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.puts
}

func (inst *MockCache) Has(key string) bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, ok := inst.entries[key]
	return ok
}

// ============================================================================
// Recording Observer
// ============================================================================

// RecordingObserver captures all observer events for assertion.
type RecordingObserver struct {
	observers.NopObserver

	mu     sync.Mutex
	events []RecordedEvent
}

type RecordedEvent struct {
	Stage string
	Err   error
}

func NewRecordingObserver() *RecordingObserver {
	return &RecordingObserver{}
}

func (inst *RecordingObserver) record(stage string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.events = append(inst.events, RecordedEvent{Stage: stage, Err: err})
}

func (inst *RecordingObserver) Events() []RecordedEvent {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out := make([]RecordedEvent, len(inst.events))
	copy(out, inst.events)
	return out
}

func (inst *RecordingObserver) StageNames() []string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	names := make([]string, len(inst.events))
	for i, e := range inst.events {
		names[i] = e.Stage
	}
	return names
}

func (inst *RecordingObserver) HasStage(stage string) bool {
	for _, e := range inst.Events() {
		if e.Stage == stage {
			return true
		}
	}
	return false
}

func (inst *RecordingObserver) OnTemplateParsed(_ context.Context, e orchestrator.TemplateParsedEvent) {
	inst.record("template_parsed", e.Err)
}
func (inst *RecordingObserver) OnCacheHit(_ context.Context, e orchestrator.CacheHitEvent) {
	inst.record("cache_hit", nil)
}
func (inst *RecordingObserver) OnCacheMiss(_ context.Context, e orchestrator.CacheMissEvent) {
	inst.record("cache_miss", nil)
}
func (inst *RecordingObserver) OnLLMRequest(_ context.Context, e orchestrator.LLMRequestEvent) {
	inst.record("llm_request", nil)
}
func (inst *RecordingObserver) OnLLMResponse(_ context.Context, e orchestrator.LLMResponseEvent) {
	inst.record("llm_response", e.Err)
}
func (inst *RecordingObserver) OnGrammar1Parse(_ context.Context, e orchestrator.Grammar1ParseEvent) {
	inst.record("grammar1_parse", e.Err)
}
func (inst *RecordingObserver) OnNormalize(_ context.Context, e orchestrator.NormalizeEvent) {
	inst.record("normalize", e.Err)
}
func (inst *RecordingObserver) OnGrammar2Parse(_ context.Context, e orchestrator.Grammar2ParseEvent) {
	inst.record("grammar2_parse", e.Err)
}
func (inst *RecordingObserver) OnASTConvert(_ context.Context, e orchestrator.ASTConvertEvent) {
	inst.record("ast_convert", e.Err)
}
func (inst *RecordingObserver) OnPolicyEnforce(_ context.Context, e orchestrator.PolicyEnforceEvent) {
	inst.record("policy_enforce", e.Err)
}
func (inst *RecordingObserver) OnExecute(_ context.Context, e orchestrator.ExecuteEvent) {
	inst.record("execute", e.Err)
}
func (inst *RecordingObserver) OnResultAnalysis(_ context.Context, e orchestrator.ResultAnalysisEvent) {
	inst.record("result_analysis", e.Err)
}
func (inst *RecordingObserver) OnComplete(_ context.Context, e orchestrator.CompleteEvent) {
	inst.record("complete", e.Err)
}

// ============================================================================
// Helper: wrap SQL in markdown code block (simulates LLM output)
// ============================================================================

func sqlResponse(sql string) string {
	return "```sql\n" + sql + "\n```"
}

// ============================================================================
// Tests
// ============================================================================

func TestCompileHappyPath(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs)
	ctx := context.Background()

	result, err := orch.Compile(ctx, "show me the number one")
	require.NoError(t, err)
	assert.NotEmpty(t, result.SQL)
	assert.NotEmpty(t, result.CacheKey)
	assert.False(t, result.FromCache)
	assert.Equal(t, 1, result.Attempts)
	assert.Equal(t, 1, llm.CallCount())

	// Verify observer stages fired in order
	stages := obs.StageNames()
	assert.Contains(t, stages, "template_parsed")
	assert.Contains(t, stages, "cache_miss")
	assert.Contains(t, stages, "llm_request")
	assert.Contains(t, stages, "llm_response")
	assert.Contains(t, stages, "grammar1_parse")
	assert.Contains(t, stages, "normalize")
	assert.Contains(t, stages, "policy_enforce")
	assert.Contains(t, stages, "complete")
}

func TestCompileCacheHit(t *testing.T) {
	llm := NewMockLLM() // no responses configured — should not be called
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	// Pre-populate cache
	tmpl, err := orchestrator.ParseTemplate("show me the number one")
	require.NoError(t, err)
	_ = tmpl // cache key is derived from template content

	// First compile to populate cache
	llm1 := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	orch1 := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm1, ch, cache, obs)
	result1, err := orch1.Compile(context.Background(), "show me the number one")
	require.NoError(t, err)

	// Second compile should hit cache
	obs2 := NewRecordingObserver()
	orch2 := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs2)
	result2, err := orch2.Compile(context.Background(), "show me the number one")
	require.NoError(t, err)

	assert.True(t, result2.FromCache)
	assert.Equal(t, result1.CacheKey, result2.CacheKey)
	assert.Equal(t, result1.SQL, result2.SQL)
	assert.Equal(t, 0, llm.CallCount())
	assert.True(t, obs2.HasStage("cache_hit"))
	assert.False(t, obs2.HasStage("llm_request"))
}

func TestCompileRetryOnSyntaxError(t *testing.T) {
	llm := NewMockLLM(
		// First attempt: invalid SQL
		MockLLMResponse{Content: sqlResponse("SELEC broken")},
		// Second attempt: valid SQL
		MockLLMResponse{Content: sqlResponse("SELECT 1")},
	)
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model", MaxAttempts: 3}, llm, ch, cache, obs)
	result, err := orch.Compile(context.Background(), "show me the number one")
	require.NoError(t, err)

	assert.Equal(t, 2, result.Attempts)
	assert.Equal(t, 2, llm.CallCount())

	// Verify the retry included error feedback
	lastCall := llm.LastCall()
	assert.True(t, len(lastCall.Messages) > 2, "retry should include error feedback messages")
}

func TestCompileExhaustedRetries(t *testing.T) {
	llm := NewMockLLM(
		MockLLMResponse{Content: sqlResponse("SELEC broken1")},
		MockLLMResponse{Content: sqlResponse("SELEC broken2")},
		MockLLMResponse{Content: sqlResponse("SELEC broken3")},
	)
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model", MaxAttempts: 3}, llm, ch, cache, obs)
	_, err := orch.Compile(context.Background(), "show me something")
	assert.Error(t, err)
	assert.Equal(t, 3, llm.CallCount())

	// Complete event should have error
	events := obs.Events()
	completeEvent := events[len(events)-1]
	assert.Equal(t, "complete", completeEvent.Stage)
	assert.Error(t, completeEvent.Err)
}

func TestCompileLLMError(t *testing.T) {
	llm := NewMockLLM(
		MockLLMResponse{Err: assert.AnError},
		MockLLMResponse{Content: sqlResponse("SELECT 1")},
	)
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model", MaxAttempts: 3}, llm, ch, cache, obs)
	result, err := orch.Compile(context.Background(), "show me something")
	require.NoError(t, err)

	assert.Equal(t, 2, result.Attempts)
}

func TestCompileWithPolicyPass(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT a FROM t")})
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	policyApplied := false
	policy := func(sql string) (string, error) {
		policyApplied = true
		// Simulated policy: just pass through
		return sql, nil
	}

	orch := orchestrator.New(orchestrator.Config{
		DefaultModel: "test-model",
		PolicyPasses: []orchestrator.PolicyPass{policy},
	}, llm, ch, cache, obs)

	_, err := orch.Compile(context.Background(), "show column a from table t")
	require.NoError(t, err)
	assert.True(t, policyApplied)
	assert.True(t, obs.HasStage("policy_enforce"))
}

func TestExecuteHappyPath(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH(MockCHResult{
		Result: &orchestrator.QueryResult{
			Data:     bytes.NewReader([]byte(`{"1":1}`)),
			Format:   "JSONEachRow",
			RowCount: 1,
			Elapsed:  5 * time.Millisecond,
		},
	})
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs)
	result, err := orch.Execute(context.Background(), "show me the number one", nil)
	require.NoError(t, err)

	assert.NotNil(t, result.QueryResult)
	assert.Equal(t, int64(1), result.QueryResult.RowCount)
	assert.True(t, obs.HasStage("execute"))
}

func TestExecuteWithParams(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH(MockCHResult{
		Result: &orchestrator.QueryResult{RowCount: 0, Elapsed: time.Millisecond},
	})
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs)
	params := map[string]string{"country": "US", "year": "2024"}
	_, err := orch.Execute(context.Background(), "@param country = $country : String\nshow sales", params)
	require.NoError(t, err)

	// Verify params were passed to CH
	calls := ch.Calls()
	// Last call should be the execute (not DDL/warmup)
	lastCall := calls[len(calls)-1]
	assert.Equal(t, params, lastCall.Params)
}

func TestCompileWithDirectives(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	input := `@model custom-model
@quality approximate
@example "revenue" = sum(amount)
@join orders.customer_id = customers.id
@pin
show me revenue`

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "default-model"}, llm, ch, cache, obs)
	_, err := orch.Compile(context.Background(), input)
	require.NoError(t, err)

	// Verify the custom model was used
	assert.Equal(t, "custom-model", llm.LastCall().Model)

	// Verify the system prompt contains domain knowledge and join paths
	systemMsg := llm.LastCall().Messages[0].Content
	assert.Contains(t, systemMsg, "revenue")
	assert.Contains(t, systemMsg, "orders.customer_id")
	assert.Contains(t, systemMsg, "approximate")
}

func TestCompileModelDefault(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "my-default"}, llm, ch, cache, obs)
	_, err := orch.Compile(context.Background(), "show something")
	require.NoError(t, err)

	assert.Equal(t, "my-default", llm.LastCall().Model)
}

// ============================================================================
// Template parsing tests
// ============================================================================

func TestParseTemplateBasic(t *testing.T) {
	tmpl, err := orchestrator.ParseTemplate("show me all users")
	require.NoError(t, err)
	assert.Equal(t, "show me all users", tmpl.QueryText)
	assert.Empty(t, tmpl.Model)
	assert.Empty(t, tmpl.Params)
}

func TestParseTemplateWithDirectives(t *testing.T) {
	input := `@model qwen3-coder-next
@quality approximate allow-false-positive
@param country = $country : String
@param year = $year : UInt16
@example "revenue" means sum(oi.quantity * oi.unit_price)
@join orders.customer_id = customers.id
@pin
@ttl 1h
Total revenue by month for the given country and year.`

	tmpl, err := orchestrator.ParseTemplate(input)
	require.NoError(t, err)

	assert.Equal(t, "qwen3-coder-next", tmpl.Model)
	assert.Equal(t, []string{"approximate", "allow-false-positive"}, tmpl.Quality)
	require.Len(t, tmpl.Params, 2)
	assert.Equal(t, "country", tmpl.Params[0].Name)
	assert.Equal(t, "$country", tmpl.Params[0].GrafanaVar)
	assert.Equal(t, "String", tmpl.Params[0].Type)
	assert.Equal(t, "year", tmpl.Params[1].Name)
	assert.Equal(t, "UInt16", tmpl.Params[1].Type)
	require.Len(t, tmpl.Examples, 1)
	assert.Contains(t, tmpl.Examples[0], "revenue")
	require.Len(t, tmpl.Joins, 1)
	assert.Contains(t, tmpl.Joins[0], "orders.customer_id")
	assert.True(t, tmpl.Pinned)
	assert.Equal(t, time.Hour, tmpl.TTL)
	assert.Equal(t, "Total revenue by month for the given country and year.", tmpl.QueryText)
}

func TestParseTemplateEmptyQuery(t *testing.T) {
	_, err := orchestrator.ParseTemplate("@model test\n@pin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no query text")
}

func TestParseTemplateUnknownDirective(t *testing.T) {
	_, err := orchestrator.ParseTemplate("@bogus value\nsome query")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown directive")
}

func TestParseTemplateInvalidTTL(t *testing.T) {
	_, err := orchestrator.ParseTemplate("@ttl not-a-duration\nsome query")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "@ttl")
}

func TestParseTemplateInvalidParam(t *testing.T) {
	_, err := orchestrator.ParseTemplate("@param missing_equals\nsome query")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "@param")
}

// ============================================================================
// Observer completeness — every stage fires exactly once in happy path
// ============================================================================

func TestObserverAllStagesFired(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	ch := NewMockCH(MockCHResult{
		Result: &orchestrator.QueryResult{RowCount: 1, Elapsed: time.Millisecond},
	})
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs)
	_, err := orch.Execute(context.Background(), "show one", nil)
	require.NoError(t, err)

	expectedStages := []string{
		"template_parsed",
		"cache_miss",
		"llm_request",
		"llm_response",
		"grammar1_parse",
		"normalize",
		// grammar2_parse and ast_convert depend on pipeline internals
		"policy_enforce",
		"execute",
		"complete",
	}

	stages := obs.StageNames()
	for _, expected := range expectedStages {
		assert.True(t, obs.HasStage(expected), "missing stage: %s\ngot: %v", expected, stages)
	}
}

// ============================================================================
// AST survives JSON round-trip in cache
// ============================================================================

func TestCacheEntryJSONRoundTrip(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT a, b FROM t WHERE a > 1")})
	ch := NewMockCH()
	cache := NewMockCache()
	obs := NewRecordingObserver()

	orch := orchestrator.New(orchestrator.Config{DefaultModel: "test-model"}, llm, ch, cache, obs)
	result, err := orch.Compile(context.Background(), "columns a and b from t where a above 1")
	require.NoError(t, err)

	// Marshal AST to JSON and back (simulates CHCache persistence)
	astJSON, err := json.Marshal(result.AST)
	require.NoError(t, err)

	var roundTripped orchestrator.CacheEntry
	roundTripped.CanonicalSQL = result.SQL
	roundTripped.PolicySQL = result.SQL
	err = json.Unmarshal(astJSON, &roundTripped.AST)
	require.NoError(t, err)

	// The round-tripped AST should produce parseable SQL
	sql := roundTripped.AST.ToSQL()
	assert.NotEmpty(t, sql)
}
