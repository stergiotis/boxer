// Package orchestrator implements the query pipeline for the ClickHouse
// semantic proxy. It coordinates template parsing, LLM-based SQL compilation,
// grammar validation, policy enforcement, parameterization, and execution.
//
// The pipeline is fully observable: an ObserverI interface receives structured
// events at each stage, enabling logging, tracing, and metrics collection.
package orchestrator

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ============================================================================
// Abstract interfaces — caller supplies implementations
// ============================================================================

// LLMClientI generates SQL from a structured prompt.
type LLMClientI interface {
	// Chat sends a message history and returns the assistant's response.
	// The implementation handles model selection, temperature, etc.
	Chat(ctx context.Context, model string, messages []Message) (response string, err error)
}

// Message is a single message in an LLM conversation.
type Message struct {
	Role    string // "system", "user", "assistant", "tool"
	Content string
	// ToolCallId names the call a role=tool message answers; ToolCalls are
	// the calls a role=assistant message made (ADR-0139 §SD9). Both empty
	// on the plain path.
	ToolCallId string
	ToolCalls  []ToolCall
}

// Tool is one function the model may call while composing a query
// (ADR-0139 §SD8): a name, what it does, and its parameters as a JSON
// Schema object, serialized.
type Tool struct {
	Name        string
	Description string
	Parameters  string
}

// ToolCall is one call the model made: the provider's id, the tool, and
// the arguments as the JSON text the model wrote.
type ToolCall struct {
	Id        string
	Name      string
	Arguments string
}

// ToolClientI is an LLMClientI that can carry tool definitions and return
// the model's tool calls (ADR-0139 §SD9). A client that is not one degrades
// the loop to seed-only single-shot generation.
type ToolClientI interface {
	LLMClientI
	// ChatTools is Chat with tools offered. A turn answers EITHER with
	// content (no calls) or with calls to make; a provider that returns
	// both is read as calls first.
	ChatTools(ctx context.Context, model string, messages []Message, tools []Tool) (response string, calls []ToolCall, err error)
}

// ToolExecutorI is what runs the model's calls — in the caller's process,
// under the caller's own grants (ADR-0254 §SD5): the orchestrator never
// holds a capability of its own. Call returns the result as text the model
// reads, or an error the model reads as text too (a failed call is
// information, not a failure of the question).
type ToolExecutorI interface {
	Tools() []Tool
	Call(ctx context.Context, call ToolCall) (result string, err error)
}

// ToolCallEvent is one executed call, for a ToolObserverI.
type ToolCallEvent struct {
	Attempt  int
	Call     ToolCall
	Result   string
	Duration time.Duration
	Err      error
}

// ToolObserverI is an ObserverI that also hears tool calls. Optional, so an
// observer written before the loop existed keeps compiling.
type ToolObserverI interface {
	OnToolCall(ctx context.Context, event ToolCallEvent)
}

// CHClientI executes queries against ClickHouse.
type CHClientI interface {
	// Execute runs a parameterized query and returns the result.
	// The params map contains ClickHouse parameter values keyed by name.
	// The result is opaque to the orchestrator — the ObserverI receives it for inspection.
	Execute(ctx context.Context, sql string, params map[string]string) (result *QueryResult, err error)
}

// QueryResult holds the result of a ClickHouse query execution.
type QueryResult struct {
	Data     io.Reader // raw result stream (Arrow, JSON, etc.)
	Closer   io.Closer // optional closer for the underlying connection (may be nil)
	Format   string    // result format
	RowCount int64     // number of rows returned (-1 if unknown)
	DataSize int64     // total bytes read (-1 if unknown/streaming)
	Elapsed  time.Duration
}

// CompiledQueryCacheI stores and retrieves compiled SQL by template key.
type CompiledQueryCacheI interface {
	Get(key string) (entry *CacheEntry, found bool)
	Put(key string, entry *CacheEntry)
}

// CacheEntry is a cached compilation result.
type CacheEntry struct {
	CanonicalSQL string
	PolicySQL    string
	AST          ast.Query
	CompiledAt   time.Time
	Pinned       bool
}

// PolicyPass transforms SQL after AST conversion (e.g. tenant isolation).
// It operates on the canonical SQL string, same signature as nanopass.Pass.
type PolicyPass func(sql string) (string, error)

// ============================================================================
// ObserverI — structured events at each pipeline stage
// ============================================================================

// ObserverI receives events at each pipeline stage. All methods are called
// synchronously. Implementations must not block.
//
// Each method receives the stage's inputs, outputs, duration, and error.
// The attempt parameter indicates retry iteration (1 = first try).
type ObserverI interface {
	// OnTemplateParsed is called after the template text is parsed into directives.
	OnTemplateParsed(ctx context.Context, event TemplateParsedEvent)

	// OnCacheHit is called when a compiled query is found in the cache.
	OnCacheHit(ctx context.Context, event CacheHitEvent)

	// OnCacheMiss is called when no cached compilation exists.
	OnCacheMiss(ctx context.Context, event CacheMissEvent)

	// OnLLMRequest is called before sending a request to the LLM.
	OnLLMRequest(ctx context.Context, event LLMRequestEvent)

	// OnLLMResponse is called after receiving the LLM's response.
	OnLLMResponse(ctx context.Context, event LLMResponseEvent)

	// OnGrammar1Parse is called after attempting Grammar1 parse of LLM output.
	OnGrammar1Parse(ctx context.Context, event Grammar1ParseEvent)

	// OnNormalize is called after the canonicalization pipeline.
	OnNormalize(ctx context.Context, event NormalizeEvent)

	// OnGrammar2Parse is called after Grammar2 (canonical) validation.
	OnGrammar2Parse(ctx context.Context, event Grammar2ParseEvent)

	// OnASTConvert is called after CST→AST conversion.
	OnASTConvert(ctx context.Context, event ASTConvertEvent)

	// OnPolicyEnforce is called after policy passes are applied.
	OnPolicyEnforce(ctx context.Context, event PolicyEnforceEvent)

	// OnExecute is called after ClickHouse query execution.
	OnExecute(ctx context.Context, event ExecuteEvent)

	// OnResultAnalysis is called after LLM result verification (if enabled).
	OnResultAnalysis(ctx context.Context, event ResultAnalysisEvent)

	// OnComplete is called when the full pipeline finishes (success or failure).
	OnComplete(ctx context.Context, event CompleteEvent)
}

// --- Event types ---

type TemplateParsedEvent struct {
	RawInput string
	Template Template
	Duration time.Duration
	Err      error
}

type CacheHitEvent struct {
	CacheKey string
	Entry    *CacheEntry
}

type CacheMissEvent struct {
	CacheKey string
}

type LLMRequestEvent struct {
	Model    string
	Messages []Message
	Attempt  int
}

type LLMResponseEvent struct {
	Model        string
	RawResponse  string
	ExtractedSQL string
	Attempt      int
	Duration     time.Duration
	Err          error
}

type Grammar1ParseEvent struct {
	SQL      string
	Attempt  int
	Duration time.Duration
	Err      error
}

type NormalizeEvent struct {
	InputSQL  string
	OutputSQL string
	Attempt   int
	Duration  time.Duration
	Err       error
}

type Grammar2ParseEvent struct {
	SQL      string
	Attempt  int
	Duration time.Duration
	Err      error
}

type ASTConvertEvent struct {
	SQL      string
	AST      *ast.Query
	Attempt  int
	Duration time.Duration
	Err      error
}

type PolicyEnforceEvent struct {
	InputSQL  string
	OutputSQL string
	Duration  time.Duration
	Err       error
}

type ExecuteEvent struct {
	SQL      string
	Params   map[string]string
	Result   *QueryResult
	Duration time.Duration
	Err      error
}

type ResultAnalysisEvent struct {
	SQL        string
	Summary    string
	LLMVerdict string
	Duration   time.Duration
	Err        error
}

type CompleteEvent struct {
	CacheKey      string
	FinalSQL      string
	TotalDuration time.Duration
	Attempts      int
	FromCache     bool
	Err           error
}

// ============================================================================
// Template — parsed @directives + query text
// ============================================================================

// Template is the parsed representation of a semantic query template.
type Template struct {
	Model     string        // @model directive (empty = default)
	Quality   []string      // @quality flags
	Params    []ParamDecl   // @param declarations
	Examples  []string      // @example lines
	Joins     []string      // @join declarations
	Pinned    bool          // @pin directive
	TTL       time.Duration // @ttl directive
	QueryText string        // everything that isn't a directive
}

// ParamDecl is a declared parameter.
type ParamDecl struct {
	Name       string // parameter name
	GrafanaVar string // Grafana variable reference (e.g. $country)
	Type       string // ClickHouse type (e.g. String, Date)
}

// ============================================================================
// Orchestrator
// ============================================================================

// Config holds orchestrator configuration.
type Config struct {
	DefaultModel  string
	MaxAttempts   int
	Schema        string       // pre-formatted schema text for LLM prompt
	PolicyPasses  []PolicyPass // post-AST policy transformations
	VerifyResults bool         // enable LLM result verification
	// Tools, when set and the client is a ToolClientI, turns generation
	// into the in-conversation tool loop of ADR-0139 §SD9: the model may
	// answer a turn with calls, the executor runs them, and the results are
	// appended as tool turns until the model emits SQL or the budget is
	// spent. The tool history stays in the conversation across repair
	// attempts, so what the model learned is not lost.
	Tools ToolExecutorI
	// MaxToolCalls bounds the calls per question; zero is DefaultMaxToolCalls.
	MaxToolCalls int
}

// DefaultMaxToolCalls bounds the tool loop when Config.MaxToolCalls is zero.
const DefaultMaxToolCalls = 8

// Orchestrator coordinates the query pipeline.
type Orchestrator struct {
	cfg          Config
	llm          LLMClientI
	ch           CHClientI
	cache        CompiledQueryCacheI
	observer     ObserverI
	canonicalize nanopass.Pass
}

// New creates an Orchestrator.
func New(cfg Config, llm LLMClientI, ch CHClientI, cache CompiledQueryCacheI, observer ObserverI) *Orchestrator {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "qwen3-coder-next"
	}
	if cfg.MaxToolCalls <= 0 {
		cfg.MaxToolCalls = DefaultMaxToolCalls
	}
	return &Orchestrator{
		cfg:          cfg,
		llm:          llm,
		ch:           ch,
		cache:        cache,
		observer:     observer,
		canonicalize: passes.CanonicalizeFull(128),
	}
}

// Result is the final output of the orchestrator pipeline.
type Result struct {
	SQL         string       // final executed SQL (with policy, before param substitution)
	AST         ast.Query    // validated AST
	QueryResult *QueryResult // ClickHouse result (nil if execute not requested)
	CacheKey    string       // template cache key
	FromCache   bool         // whether the compilation was cached
	Attempts    int          // LLM attempts used
}

// Compile parses and compiles a template to SQL without executing.
// Uses cache if available. Calls LLM if needed.
func (inst *Orchestrator) Compile(ctx context.Context, input string) (result Result, err error) {
	start := time.Now()

	// Stage 1: Parse template
	t0 := time.Now()
	tmpl, parseErr := ParseTemplate(input)
	inst.observer.OnTemplateParsed(ctx, TemplateParsedEvent{
		RawInput: input, Template: tmpl, Duration: time.Since(t0), Err: parseErr,
	})
	if parseErr != nil {
		err = eh.Errorf("template parse: %w", parseErr)
		inst.emitComplete(ctx, "", "", time.Since(start), 0, false, err)
		return
	}

	// Stage 2: Cache check
	cacheKey := templateCacheKey(tmpl)
	result.CacheKey = cacheKey

	if entry, found := inst.cache.Get(cacheKey); found {
		inst.observer.OnCacheHit(ctx, CacheHitEvent{CacheKey: cacheKey, Entry: entry})
		result.SQL = entry.PolicySQL
		result.AST = entry.AST
		result.FromCache = true
		inst.emitComplete(ctx, cacheKey, entry.PolicySQL, time.Since(start), 0, true, nil)
		return
	}
	inst.observer.OnCacheMiss(ctx, CacheMissEvent{CacheKey: cacheKey})

	// Stage 3-7: Compile via LLM with retry loop
	model := tmpl.Model
	if model == "" {
		model = inst.cfg.DefaultModel
	}

	systemPrompt := inst.buildSystemPrompt(tmpl)
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: tmpl.QueryText},
	}

	var compiledAST ast.Query
	var canonicalSQL string

	// The tool loop (ADR-0139 §SD9) is on when the caller supplied an
	// executor and the client can carry tools; the call budget is per
	// question and survives repair attempts, as the tool history does.
	toolClient, _ := inst.llm.(ToolClientI)
	tools := []Tool(nil)
	if inst.cfg.Tools != nil && toolClient != nil {
		tools = inst.cfg.Tools.Tools()
	}
	callsLeft := inst.cfg.MaxToolCalls

	for attempt := 1; attempt <= inst.cfg.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Stage 3: LLM generate — through the tool loop when it is on.
		inst.observer.OnLLMRequest(ctx, LLMRequestEvent{Model: model, Messages: messages, Attempt: attempt})

		t0 = time.Now()
		var rawResponse string
		var llmErr error
		if len(tools) > 0 {
			rawResponse, messages, callsLeft, llmErr = inst.toolTurns(ctx, toolClient, model, messages, tools, callsLeft, attempt)
		} else {
			rawResponse, llmErr = inst.llm.Chat(ctx, model, messages)
		}
		extractedSQL := extractSQL(rawResponse)
		inst.observer.OnLLMResponse(ctx, LLMResponseEvent{
			Model: model, RawResponse: rawResponse, ExtractedSQL: extractedSQL,
			Attempt: attempt, Duration: time.Since(t0), Err: llmErr,
		})
		if llmErr != nil {
			err = eb.Build().Int("attempt", attempt).Errorf("LLM: %w", llmErr)
			if attempt == inst.cfg.MaxAttempts {
				inst.emitComplete(ctx, cacheKey, "", time.Since(start), attempt, false, err)
				return
			}
			continue
		}

		// Stage 4: Grammar1 parse
		t0 = time.Now()
		_, g1Err := nanopass.Parse(extractedSQL)
		inst.observer.OnGrammar1Parse(ctx, Grammar1ParseEvent{
			SQL: extractedSQL, Attempt: attempt, Duration: time.Since(t0), Err: g1Err,
		})
		if g1Err != nil {
			messages = appendRetry(messages, rawResponse, extractedSQL, eh.Errorf("SQL syntax error: %w", g1Err))
			continue
		}

		// Stage 5: Normalize
		t0 = time.Now()
		normalized, normErr := inst.canonicalize.Run(extractedSQL)
		inst.observer.OnNormalize(ctx, NormalizeEvent{
			InputSQL: extractedSQL, OutputSQL: normalized, Attempt: attempt, Duration: time.Since(t0), Err: normErr,
		})
		if normErr != nil {
			messages = appendRetry(messages, rawResponse, extractedSQL, eh.Errorf("normalization error: %w", normErr))
			continue
		}

		// Stage 6: Grammar2 parse
		t0 = time.Now()
		pr, g2Err := nanopass.ParseCanonical(normalized)
		inst.observer.OnGrammar2Parse(ctx, Grammar2ParseEvent{
			SQL: normalized, Attempt: attempt, Duration: time.Since(t0), Err: g2Err,
		})
		if g2Err != nil {
			messages = appendRetry(messages, rawResponse, extractedSQL, eh.Errorf("canonical validation error: %w", g2Err))
			continue
		}

		// Stage 7: AST convert
		t0 = time.Now()
		compiledAST, err = ast.ConvertCSTToAST(pr)
		inst.observer.OnASTConvert(ctx, ASTConvertEvent{
			SQL: normalized, AST: &compiledAST, Attempt: attempt, Duration: time.Since(t0), Err: err,
		})
		if err != nil {
			messages = appendRetry(messages, rawResponse, extractedSQL, eh.Errorf("AST conversion error: %w", err))
			err = nil
			continue
		}

		canonicalSQL = normalized
		break
	}

	if canonicalSQL == "" {
		if err == nil {
			err = eb.Build().Int("attempts", inst.cfg.MaxAttempts).Errorf("compilation failed")
		}
		inst.emitComplete(ctx, cacheKey, "", time.Since(start), result.Attempts, false, err)
		return
	}

	// Stage 8: Policy enforcement
	t0 = time.Now()
	policySQL := canonicalSQL
	for _, pass := range inst.cfg.PolicyPasses {
		policySQL, err = pass(policySQL)
		if err != nil {
			inst.observer.OnPolicyEnforce(ctx, PolicyEnforceEvent{
				InputSQL: canonicalSQL, OutputSQL: policySQL, Duration: time.Since(t0), Err: err,
			})
			inst.emitComplete(ctx, cacheKey, "", time.Since(start), result.Attempts, false, err)
			return
		}
	}
	inst.observer.OnPolicyEnforce(ctx, PolicyEnforceEvent{
		InputSQL: canonicalSQL, OutputSQL: policySQL, Duration: time.Since(t0),
	})

	// Cache the result
	entry := &CacheEntry{
		CanonicalSQL: canonicalSQL,
		PolicySQL:    policySQL,
		AST:          compiledAST,
		CompiledAt:   time.Now(),
		Pinned:       tmpl.Pinned,
	}
	inst.cache.Put(cacheKey, entry)

	result.SQL = policySQL
	result.AST = compiledAST
	inst.emitComplete(ctx, cacheKey, policySQL, time.Since(start), result.Attempts, false, nil)
	return
}

// Execute compiles (or retrieves from cache) and executes a query.
func (inst *Orchestrator) Execute(ctx context.Context, input string, params map[string]string) (result Result, err error) {
	result, err = inst.Compile(ctx, input)
	if err != nil {
		return
	}

	// Stage 9: Parameterize — ClickHouse handles {param: Type} natively,
	// so we pass params to the client.

	// Stage 10: Execute
	t0 := time.Now()
	qr, execErr := inst.ch.Execute(ctx, result.SQL, params)
	inst.observer.OnExecute(ctx, ExecuteEvent{
		SQL: result.SQL, Params: params, Result: qr, Duration: time.Since(t0), Err: execErr,
	})

	if execErr != nil {
		// Feed ClickHouse error back to LLM for correction
		result, err = inst.retryWithExecutionError(ctx, input, params, result, execErr)
		return
	}

	result.QueryResult = qr

	// Stage 11: Result analysis (optional)
	if inst.cfg.VerifyResults && qr != nil {
		inst.verifyResults(ctx, result)
	}

	return
}

// retryWithExecutionError re-compiles after a ClickHouse execution error.
func (inst *Orchestrator) retryWithExecutionError(ctx context.Context, input string, params map[string]string, prev Result, execErr error) (result Result, err error) {
	// Invalidate cache for this template
	inst.cache.Put(prev.CacheKey, nil)

	// For now, return the execution error. A full implementation would
	// re-enter the compile loop with the ClickHouse error message appended
	// to the conversation.
	result = prev
	err = eh.Errorf("ClickHouse execution error: %w", execErr)
	return
}

// verifyResults asks the LLM to check the query results for anomalies.
func (inst *Orchestrator) verifyResults(ctx context.Context, result Result) {
	summary := summarizeResult(result.QueryResult)
	if summary == "" {
		return
	}

	model := inst.cfg.DefaultModel
	messages := []Message{
		{Role: "system", Content: "You are a data quality checker. Given a SQL query and its results summary, identify potential issues: empty results, suspicious values, missing aggregations, or possible data type mismatches. Respond with OK if everything looks reasonable, or describe the concern briefly."},
		{Role: "user", Content: "SQL:\n" + result.SQL + "\n\nResult summary:\n" + summary},
	}

	t0 := time.Now()
	verdict, llmErr := inst.llm.Chat(ctx, model, messages)
	inst.observer.OnResultAnalysis(ctx, ResultAnalysisEvent{
		SQL: result.SQL, Summary: summary, LLMVerdict: verdict,
		Duration: time.Since(t0), Err: llmErr,
	})
}

func (inst *Orchestrator) emitComplete(ctx context.Context, cacheKey, finalSQL string, duration time.Duration, attempts int, fromCache bool, err error) {
	inst.observer.OnComplete(ctx, CompleteEvent{
		CacheKey: cacheKey, FinalSQL: finalSQL, TotalDuration: duration,
		Attempts: attempts, FromCache: fromCache, Err: err,
	})
}

// ============================================================================
// System prompt construction
// ============================================================================

func (inst *Orchestrator) buildSystemPrompt(tmpl Template) string {
	var b strings.Builder

	b.WriteString("You are a ClickHouse SQL expert. Generate a single SELECT query.\n")
	b.WriteString("Output ONLY SQL wrapped in a ```sql code block.\n")
	b.WriteString("Use standard ClickHouse syntax:\n")
	b.WriteString("- Use single = for equality\n")
	b.WriteString("- Use if()/multiIf() instead of CASE WHEN\n")
	b.WriteString("- Use CAST(expr, 'Type') not CAST(expr AS Type)\n")
	b.WriteString("- Use array()/tuple() not []/() for constructors\n")
	b.WriteString("- Always include FROM when querying tables\n\n")

	if len(tmpl.Quality) > 0 {
		b.WriteString("QUALITY CONSTRAINTS: ")
		b.WriteString(strings.Join(tmpl.Quality, ", "))
		b.WriteString("\n\n")
	}

	if inst.cfg.Schema != "" {
		b.WriteString("SCHEMA:\n")
		b.WriteString(inst.cfg.Schema)
		b.WriteString("\n\n")
	}

	if len(tmpl.Joins) > 0 {
		b.WriteString("VALID JOIN PATHS:\n")
		for _, j := range tmpl.Joins {
			b.WriteString("  ")
			b.WriteString(j)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if len(tmpl.Examples) > 0 {
		b.WriteString("DOMAIN KNOWLEDGE:\n")
		for _, ex := range tmpl.Examples {
			b.WriteString("  - ")
			b.WriteString(ex)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if len(tmpl.Params) > 0 {
		b.WriteString("PARAMETERS (use ClickHouse {name: Type} syntax):\n")
		for _, p := range tmpl.Params {
			b.WriteString("  - ")
			b.WriteString(p.Name)
			b.WriteString(": ")
			b.WriteString(p.Type)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// ============================================================================
// Template parsing
// ============================================================================

// ParseTemplate extracts @directives and query text from raw input.
func ParseTemplate(input string) (tmpl Template, err error) {
	lines := strings.Split(input, "\n")
	var queryLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") {
			err = parseDirective(trimmed, &tmpl)
			if err != nil {
				return
			}
		} else {
			queryLines = append(queryLines, line)
		}
	}

	tmpl.QueryText = strings.TrimSpace(strings.Join(queryLines, "\n"))
	if tmpl.QueryText == "" {
		err = eh.Errorf("template has no query text")
	}
	return
}

func parseDirective(line string, tmpl *Template) error {
	// Split "@directive rest..."
	parts := strings.SplitN(line, " ", 2)
	directive := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch directive {
	case "@model":
		tmpl.Model = arg
	case "@quality":
		tmpl.Quality = append(tmpl.Quality, strings.Fields(arg)...)
	case "@pin":
		tmpl.Pinned = true
	case "@ttl":
		d, err := time.ParseDuration(arg)
		if err != nil {
			return eb.Build().Str("arg", arg).Errorf("invalid @ttl: %w", err)
		}
		tmpl.TTL = d
	case "@param":
		p, err := parseParamDirective(arg)
		if err != nil {
			return err
		}
		tmpl.Params = append(tmpl.Params, p)
	case "@example":
		tmpl.Examples = append(tmpl.Examples, arg)
	case "@join":
		tmpl.Joins = append(tmpl.Joins, arg)
	default:
		return eb.Build().Str("directive", directive).Errorf("unknown directive")
	}
	return nil
}

// parseParamDirective parses "name = $var : Type"
func parseParamDirective(s string) (p ParamDecl, err error) {
	// Split on "="
	eqParts := strings.SplitN(s, "=", 2)
	if len(eqParts) != 2 {
		err = eb.Build().Str("directive", s).Errorf("invalid @param syntax expected 'name = $var : Type'")
		return
	}
	p.Name = strings.TrimSpace(eqParts[0])

	rest := strings.TrimSpace(eqParts[1])
	// Split on ":"
	colonParts := strings.SplitN(rest, ":", 2)
	p.GrafanaVar = strings.TrimSpace(colonParts[0])
	if len(colonParts) == 2 {
		p.Type = strings.TrimSpace(colonParts[1])
	}
	return
}

// ============================================================================
// Helpers
// ============================================================================

func templateCacheKey(tmpl Template) string {
	h := blake3.New(512/8, nil)
	_, _ = h.Write([]byte(tmpl.QueryText))
	for _, ex := range tmpl.Examples {
		_, _ = h.Write([]byte(ex))
	}
	for _, j := range tmpl.Joins {
		_, _ = h.Write([]byte(j))
	}
	for _, q := range tmpl.Quality {
		_, _ = h.Write([]byte(q))
	}
	return fmt.Sprintf("%016x", h.Sum(nil))
}

// toolTurns runs one generation through the tool loop: the model answers
// with calls, each is executed and appended as a tool turn, until it
// answers with content or the budget is spent — then the loop asks once
// more without tools so the question still gets an answer. The
// conversation comes back extended, since the calls and their results are
// what the model learned and the repair loop must not lose them.
func (inst *Orchestrator) toolTurns(ctx context.Context, client ToolClientI, model string, messages []Message, tools []Tool, callsLeft int, attempt int) (response string, out []Message, left int, err error) {
	out, left = messages, callsLeft
	for {
		offered := tools
		if left <= 0 {
			offered = nil
		}
		var calls []ToolCall
		response, calls, err = client.ChatTools(ctx, model, out, offered)
		if err != nil {
			return
		}
		if len(calls) == 0 || len(offered) == 0 {
			return
		}
		out = append(out, Message{Role: "assistant", Content: response, ToolCalls: calls})
		for _, call := range calls {
			t0 := time.Now()
			var text string
			var callErr error
			if left <= 0 {
				callErr = eb.Build().Str("tool", call.Name).Errorf("tool budget exhausted")
			} else {
				left--
				text, callErr = inst.cfg.Tools.Call(ctx, call)
			}
			if callErr != nil {
				text = "error: " + callErr.Error()
			}
			if obs, ok := inst.observer.(ToolObserverI); ok {
				obs.OnToolCall(ctx, ToolCallEvent{Attempt: attempt, Call: call, Result: text, Duration: time.Since(t0), Err: callErr})
			}
			out = append(out, Message{Role: "tool", Content: text, ToolCallId: call.Id})
		}
	}
}

// Validate is the model's self-check and the caller's (ADR-0139 §SD8's
// validate tool): grammar1 parse, canonicalize, grammar2 parse. It returns
// the canonical statement or the first stage's error.
func Validate(sql string) (canonical string, err error) {
	if _, err = nanopass.Parse(sql); err != nil {
		return "", eh.Errorf("SQL syntax error: %w", err)
	}
	canonical, err = passes.CanonicalizeFull(128).Run(sql)
	if err != nil {
		return "", eh.Errorf("normalization error: %w", err)
	}
	if _, err = nanopass.ParseCanonical(canonical); err != nil {
		return "", eh.Errorf("canonical validation error: %w", err)
	}
	return
}

func appendRetry(messages []Message, rawResponse, extractedSQL string, validationErr error) []Message {
	return append(messages,
		Message{Role: "assistant", Content: rawResponse},
		Message{Role: "user", Content: formatFeedback(extractedSQL, validationErr)},
	)
}

func formatFeedback(sql string, err error) string {
	var b strings.Builder
	b.WriteString("Your SQL has an error. Fix it and output only the corrected SQL in a ```sql code block.\n\n")
	b.WriteString("Your SQL:\n```sql\n")
	b.WriteString(sql)
	b.WriteString("\n```\n\nError:\n")
	b.WriteString(err.Error())
	b.WriteByte('\n')
	return b.String()
}

func extractSQL(response string) string {
	if idx := strings.Index(response, "```sql"); idx >= 0 {
		start := idx + 6
		if start < len(response) && response[start] == '\n' {
			start++
		}
		end := strings.Index(response[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(response[start : start+end])
		}
	}
	if idx := strings.Index(response, "```"); idx >= 0 {
		start := idx + 3
		if start < len(response) && response[start] == '\n' {
			start++
		}
		end := strings.Index(response[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(response[start : start+end])
		}
	}
	// Fallback: find SELECT or WITH
	lines := strings.Split(response, "\n")
	var sqlLines []string
	inSQL := false
	for _, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if !inSQL && (strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")) {
			inSQL = true
		}
		if inSQL {
			if strings.TrimSpace(line) == "" && len(sqlLines) > 0 {
				break
			}
			sqlLines = append(sqlLines, line)
		}
	}
	if len(sqlLines) > 0 {
		return strings.TrimSpace(strings.Join(sqlLines, "\n"))
	}
	return strings.TrimSpace(response)
}

func summarizeResult(qr *QueryResult) string {
	if qr == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Format: ")
	b.WriteString(qr.Format)
	b.WriteString("\nRows: ")
	if qr.RowCount >= 0 {
		b.WriteString(strconv.FormatInt(qr.RowCount, 10))
	} else {
		b.WriteString("unknown")
	}
	b.WriteString("\nData size: ")
	if qr.DataSize >= 0 {
		b.WriteString(strconv.FormatInt(qr.DataSize, 10))
		b.WriteString(" bytes")
	} else {
		b.WriteString("streaming")
	}
	b.WriteString("\nElapsed: ")
	b.WriteString(qr.Elapsed.String())
	return b.String()
}
