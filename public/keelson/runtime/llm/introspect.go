package llm

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// CallRecord is one completion the service answered or refused (ADR-0254
// §SD4): who asked, why, what it cost, how it ended. Prompt and Completion
// are kept only under Config.KeepMessages.
//
// deferred: the record is an in-process bounded ring, not a facts-store
// row. A durable kind is a generated record store (ADR-0100), and the
// shape here is what its DTO would carry.
type CallRecord struct {
	Id              uint64
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

// record appends rec to the bounded ring.
func (inst *Service) record(rec CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.next++
	rec.Id = inst.next
	inst.calls = append(inst.calls, rec)
	if over := len(inst.calls) - inst.cfg.KeepCalls; over > 0 {
		inst.calls = append([]CallRecord(nil), inst.calls[over:]...)
	}
}

// Calls returns the kept records, oldest first.
func (inst *Service) Calls() (recs []CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	recs = append([]CallRecord(nil), inst.calls...)
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
