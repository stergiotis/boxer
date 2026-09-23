package llm

import (
	"context"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/llmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// CallRecord is one completion the service answered or refused (ADR-0254
// §SD4): who asked, why, what it cost, how it ended. Prompt and Completion
// are kept only under Config.KeepMessages, and only in the in-process
// record; the durable row (llmfacts.LlmCall) carries the counts and never
// the text.
type CallRecord struct {
	Id              uint64
	CallId          string
	At              time.Time
	Sender          app.AppIdT
	SenderInstance  uint64
	Purpose         string
	Sensitivity     queryengine.SensitivityE
	Model           string
	EndpointHost    string
	Messages        int
	Tools           int
	PromptBytes     int
	CompletionBytes int
	InputTokens     int32
	OutputTokens    int32
	ToolCalls       int
	FinishReason    string
	Elapsed         time.Duration
	Incomplete      bool
	Refused         bool
	Error           string
	Prompt          string
	Completion      string
}

// record appends rec to the bounded ring and, where the host holds
// boxer.facts, lands it there as a row. A failed write is logged and the
// record kept: the table is the audit, the ring is the window's view, and
// a call already answered is not un-answered by a store that is down.
func (inst *Service) record(rec CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.next++
	rec.Id = inst.next
	inst.calls = append(inst.calls, rec)
	if over := len(inst.calls) - inst.cfg.KeepCalls; over > 0 {
		inst.calls = append([]CallRecord(nil), inst.calls[over:]...)
	}
	if inst.facts == nil {
		return
	}
	row := RowOf(rec)
	if err := inst.facts.Begin(row.Id, row.Ts, llmfacts.CallEnvelope{NaturalKey: row.NaturalKey}).AddLlmCall(row).Commit(); err != nil {
		inst.log.Warn().Err(err).Str("callId", rec.CallId).Msg("llm: buffer call row")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), factsFlushTimeout)
	defer cancel()
	if _, err := inst.facts.Flush(ctx); err != nil {
		inst.log.Warn().Err(err).Str("callId", rec.CallId).Msg("llm: flush call row")
	}
}

// factsFlushTimeout bounds one row's write; a call is already answered by
// then, so the bound is what keeps a slow server from stalling the next.
const factsFlushTimeout = 5 * time.Second

// Calls returns the kept records, oldest first: the in-process ring.
func (inst *Service) Calls() (recs []CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	recs = append([]CallRecord(nil), inst.calls...)
	return
}

// ScanCalls reads the durable rows since a point in time, oldest first,
// up to limit; nil, nil without a store. The table's own view, for a reader
// that outlives this process's ring.
func (inst *Service) ScanCalls(ctx context.Context, since time.Time, limit int) (recs []CallRecord, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.facts == nil {
		return nil, nil
	}
	opts := recordstore.ScanOpts{
		ExtraPredicate: llmfacts.CallColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")",
		Limit:          limit,
	}
	for ent, serr := range inst.facts.ScanLlmCall(ctx, opts) {
		if serr != nil {
			return nil, eh.Errorf("llm: scan calls: %w", serr)
		}
		if ent != nil && ent.LlmCall.Has {
			recs = append(recs, RecordOf(ent.LlmCall.Val))
		}
	}
	return
}

// kindLabel is the facts row's kind label.
const kindLabel = "llmCall"

// RowOf is the durable row of a record: the counts and the verdict, never
// the text.
func RowOf(rec CallRecord) (row llmfacts.LlmCall) {
	row = llmfacts.LlmCall{
		Id: xxh3.HashString(rec.CallId), NaturalKey: []byte(rec.CallId), Ts: rec.At.UTC(),
		Kind: kindLabel, CallId: rec.CallId, App: string(rec.Sender), Instance: rec.SenderInstance,
		Purpose: rec.Purpose, Sensitivity: sensitivityName(rec.Sensitivity),
		Model: rec.Model, EndpointHost: rec.EndpointHost,
		Messages: uint32(max(rec.Messages, 0)), Tools: uint32(max(rec.Tools, 0)),
		PromptBytes: uint64(max(rec.PromptBytes, 0)), CompletionBytes: uint64(max(rec.CompletionBytes, 0)),
		InputTokens: uint32(max(rec.InputTokens, 0)), OutputTokens: uint32(max(rec.OutputTokens, 0)),
		ToolCalls: uint32(max(rec.ToolCalls, 0)), FinishReason: rec.FinishReason,
		ElapsedMs:  uint64(max(rec.Elapsed.Milliseconds(), 0)),
		Incomplete: rec.Incomplete, Refused: rec.Refused,
	}
	if rec.Error != "" {
		row.Error = []string{rec.Error}
	}
	return
}

// RecordOf is RowOf's inverse, minus what the row never carried.
func RecordOf(row llmfacts.LlmCall) (rec CallRecord) {
	rec = CallRecord{
		CallId: row.CallId, At: row.Ts, Sender: app.AppIdT(row.App), SenderInstance: row.Instance,
		Purpose: row.Purpose, Model: row.Model, EndpointHost: row.EndpointHost,
		Messages: int(row.Messages), Tools: int(row.Tools),
		PromptBytes: int(row.PromptBytes), CompletionBytes: int(row.CompletionBytes),
		InputTokens: int32(row.InputTokens), OutputTokens: int32(row.OutputTokens), ToolCalls: int(row.ToolCalls),
		FinishReason: row.FinishReason, Elapsed: time.Duration(row.ElapsedMs) * time.Millisecond,
		Incomplete: row.Incomplete, Refused: row.Refused,
	}
	if len(row.Error) > 0 {
		rec.Error = row.Error[0]
	}
	if row.Sensitivity == "confined" {
		rec.Sensitivity = queryengine.SensitivityConfined
	}
	return
}

// CallsI is the read side the introspection provider needs.
type CallsI interface {
	Calls() []CallRecord
}

// RegisterIntrospect registers keelson('llm_calls') over calls; nil
// leaves the table empty rather than absent.
func RegisterIntrospect(reg *introspect.Registry, calls CallsI) (err error) {
	return reg.Register(callsProvider{calls: calls})
}

type callsProvider struct{ calls CallsI }

func (callsProvider) Name() string                         { return TableCalls }
func (callsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (callsProvider) Schema() *arrow.Schema                { return callsTable(nil).Schema() }

func (p callsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []CallRecord
	if p.calls != nil {
		rows = p.calls.Calls()
	}
	rec = callsTable(rows).Build(proj, len(rows))
	return
}

func callsTable(rows []CallRecord) *introspect.Table {
	return introspect.NewTable().
		Uint64("id", func(i int) uint64 { return rows[i].Id }).
		String("call_id", func(i int) string { return rows[i].CallId }).
		String("at", func(i int) string { return rows[i].At.UTC().Format(time.RFC3339Nano) }).
		String("app_id", func(i int) string { return string(rows[i].Sender) }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].SenderInstance }).
		String("purpose", func(i int) string { return rows[i].Purpose }).
		String("sensitivity", func(i int) string { return sensitivityName(rows[i].Sensitivity) }).
		String("model", func(i int) string { return rows[i].Model }).
		String("endpoint_host", func(i int) string { return rows[i].EndpointHost }).
		Int64("messages", func(i int) int64 { return int64(rows[i].Messages) }).
		Int64("tools", func(i int) int64 { return int64(rows[i].Tools) }).
		Int64("prompt_bytes", func(i int) int64 { return int64(rows[i].PromptBytes) }).
		Int64("completion_bytes", func(i int) int64 { return int64(rows[i].CompletionBytes) }).
		Int64("input_tokens", func(i int) int64 { return int64(rows[i].InputTokens) }).
		Int64("output_tokens", func(i int) int64 { return int64(rows[i].OutputTokens) }).
		Int64("tool_calls", func(i int) int64 { return int64(rows[i].ToolCalls) }).
		String("finish_reason", func(i int) string { return rows[i].FinishReason }).
		Int64("elapsed_ms", func(i int) int64 { return rows[i].Elapsed.Milliseconds() }).
		Bool("incomplete", func(i int) bool { return rows[i].Incomplete }).
		Bool("refused", func(i int) bool { return rows[i].Refused }).
		String("error", func(i int) string { return rows[i].Error }).
		String("prompt", func(i int) string { return rows[i].Prompt }).
		String("completion", func(i int) string { return rows[i].Completion })
}

func sensitivityName(s queryengine.SensitivityE) (name string) {
	if s == queryengine.SensitivityConfined {
		return "confined"
	}
	return "ordinary"
}
