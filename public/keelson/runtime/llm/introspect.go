package llm

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// CallRecord is one completion the service answered or refused (ADR-0254
// §SD4): who asked, why, what it cost, how it ended, and what it belongs to
// (ADR-0277). Prompt and Completion are kept only at a Config.Retain of
// ring or above, and only in the in-process record; the durable row
// (trail.LlmCall) carries the counts and never the text, which a kept
// request adds to its message rows (ADR-0264).
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
	ReportedModel   string
	ProviderId      string
	Messages        int
	Tools           int
	ToolsDigest     string
	MaxTokens       int32
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
	// Conversation, Turn and Round are what the app said the call belongs
	// to (ADR-0277 §SD5); Round is read only beside a Turn. ParentCallId
	// is the call this one continues.
	Conversation string
	Turn         string
	Round        int
	ParentCallId string
	// Task, TaskEpoch and TaskCall name the agent task whose work the
	// call was and the dispatcher's call that caused it, empty for an
	// app's own.
	Task      string
	TaskEpoch uint64
	TaskCall  string
	// Durable says the call's rows landed on boxer.facts, the request's
	// before it left the machine (ADR-0277 §SD3). RetainAsked says
	// the request came on the retained subject, and Kept that its text
	// landed with the rows.
	Durable     bool
	RetainAsked bool
	Kept        bool
	// MessagesFrom is the ordinal the call's message rows start at;
	// HistoryHash the hash over the conversation after it. OmitFrom and
	// OmitTo are the declared omission; OmitTo 0 is none.
	MessagesFrom int
	HistoryHash  string
	OmitFrom     int
	OmitTo       int
	// CachedInputTokens and ReasoningTokens are the provider's breakdown,
	// absent when it reports none. Admission, AdmissionRule and Queued are
	// how the metering rules decided the call (ADR-0300 §SD5); Admission is
	// empty on a call refused before admission ran.
	CachedInputTokens option.Option[int32]
	ReasoningTokens   option.Option[int32]
	Admission         string
	AdmissionRule     string
	Queued            time.Duration
}

// contextOf is the trail context of a call: its origin, and the
// conversation and the task when the request named them.
func (inst *Service) contextOf(rec CallRecord) (c trail.Context) {
	c.Origin = inst.cfg.Trail.OriginOf(rec.Sender, rec.SenderInstance)
	if rec.Conversation != "" {
		conv := trail.Conversation{Conversation: rec.Conversation}
		if rec.Turn != "" {
			conv.Turn, conv.Round = option.Some(rec.Turn), option.Some(uint32(max(rec.Round, 0)))
		}
		c.Conversation = option.Some(conv)
	}
	if rec.Task != "" {
		d := trail.Delegation{Task: rec.Task, Epoch: rec.TaskEpoch}
		if rec.TaskCall != "" {
			d.Call = option.Some(rec.TaskCall)
		}
		c.Delegation = option.Some(d)
	}
	return
}

// parentOf is what the service remembers of the call a turn continues.
func (inst *Service) parentOf(t *turn) (parent option.Option[seen]) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if k, ok := inst.seen[t.parent]; ok && t.parent != "" {
		parent = option.Some(k)
	}
	return
}

// writeRequest buffers the rows of the request's new messages, once: before
// the request leaves the machine on the path that sends it (ADR-0277 §SD3),
// and with the call row on a path that refuses it.
func (inst *Service) writeRequest(rec *CallRecord, t *turn) {
	if t.requested {
		return
	}
	t.requested = true
	if !t.planned {
		t.plan(inst.parentOf(t))
	}
	rec.MessagesFrom = t.from
	t.body = rec.RetainAsked && inst.cfg.Retain == RetainDurable && inst.cfg.Trail.Durable()
	inst.writeMessages(*rec, t, t.messages[t.covered:], t.from, "")
}

// writeMessages buffers ms as trail rows from ordinal first on.
func (inst *Service) writeMessages(rec CallRecord, t *turn, ms []openaichat.Message, first int, reasoning string) {
	c := inst.contextOf(rec)
	for _, row := range messageRows(rec, ms, first, reasoning) {
		var body option.Option[trail.LlmMessageBody]
		if t.body {
			body = option.Some(row.body)
		}
		if err := inst.cfg.Trail.LlmMessage(rec.At, c, row.audit, body); err != nil && t.failed == nil {
			t.failed = err
		}
	}
}

// record lands a finished call: the reply's message row and the call row on
// the trail, in one flush with whatever of the request is still buffered,
// and the record in the bounded ring. A failed write is logged and the
// record kept: the table is the audit, the ring is the window's view, and a
// call already answered is not un-answered by a store that is down.
//
// The verdict is what a retained request's reply carries (ADR-0264 §SD4):
// whether its text landed, and why not. Nil t is a request that could not
// be read; it leaves a call row and no messages.
func (inst *Service) record(rec CallRecord, t *turn) (retention uint8, reason string) {
	var failed error
	if t != nil {
		inst.writeRequest(&rec, t)
		if t.reply.Has {
			inst.writeMessages(rec, t, []openaichat.Message{t.reply.Val}, t.from+len(t.messages)-t.covered, t.reasoning)
		}
		rec.HistoryHash = historyHash(t.logical())
		failed = t.failed
	}
	if err := inst.cfg.Trail.LlmCall(rec.At, inst.contextOf(rec), RowOf(rec, t != nil && t.body)); err != nil && failed == nil {
		failed = err
	}
	if err := inst.cfg.Trail.Flush(inst.base); err != nil && failed == nil {
		failed = err
	}
	if failed != nil {
		inst.log.Warn().Err(failed).Str("callId", rec.CallId).Msg("llm: write the call's trail rows (they stay buffered and may land with a later flush)")
	}
	// A write-ahead that failed leaves the call not durable even when a
	// later flush lands its rows: the request left before they did.
	rec.Durable = inst.cfg.Trail.Durable() && failed == nil && (t == nil || !t.notAhead)
	rec.Kept = rec.Durable && t != nil && t.body
	if rec.RetainAsked {
		retention = uint8(RetentionNotKept)
		switch {
		case rec.Kept:
			retention = uint8(RetentionKept)
		case inst.cfg.Retain != RetainDurable:
			level := inst.cfg.Retain
			if level == "" {
				level = RetainOff
			}
			reason = "this host's BOXER_LLM_RETAIN is " + string(level) + ", not durable"
		case !inst.cfg.Trail.Durable():
			reason = "this host has no durable backend for boxer.facts"
		default:
			reason = "the write failed (the rows stay buffered and may land with a later one): " + failed.Error()
		}
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if rec.Durable && t != nil {
		inst.remember(rec.CallId, seen{conversation: t.conversation, hashes: t.logical()})
	}
	inst.next++
	rec.Id = inst.next
	inst.calls = append(inst.calls, rec)
	if over := len(inst.calls) - inst.cfg.KeepCalls; over > 0 {
		inst.calls = append([]CallRecord(nil), inst.calls[over:]...)
	}
	return
}

// remember notes a written call for its successor, bounded to KeepCalls.
// The caller holds the mutex.
func (inst *Service) remember(callId string, k seen) {
	inst.seen[callId] = k
	inst.seenOrder = append(inst.seenOrder, callId)
	if over := len(inst.seenOrder) - inst.cfg.KeepCalls; over > 0 {
		for _, id := range inst.seenOrder[:over] {
			delete(inst.seen, id)
		}
		inst.seenOrder = append([]string(nil), inst.seenOrder[over:]...)
	}
}

// Calls returns the kept records, oldest first: the in-process ring.
func (inst *Service) Calls() (recs []CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	recs = append([]CallRecord(nil), inst.calls...)
	return
}

// The retention verdicts as the call row spells them.
const (
	retentionNotAsked = "not-asked"
	retentionKept     = "kept"
	retentionNotKept  = "not-kept"
)

// RowOf is the durable row of a record: the counts and the verdict, never
// the text. kept says the call's message rows carry their text.
func RowOf(rec CallRecord, kept bool) (row trail.LlmCall) {
	row = trail.LlmCall{
		CallId: rec.CallId, Purpose: rec.Purpose, Sensitivity: sensitivityName(rec.Sensitivity),
		Model: rec.Model, EndpointHost: rec.EndpointHost,
		Messages: uint32(max(rec.Messages, 0)), Tools: uint32(max(rec.Tools, 0)), MaxTokens: uint32(max(rec.MaxTokens, 0)),
		PromptBytes: uint64(max(rec.PromptBytes, 0)), CompletionBytes: uint64(max(rec.CompletionBytes, 0)),
		InputTokens: uint32(max(rec.InputTokens, 0)), OutputTokens: uint32(max(rec.OutputTokens, 0)),
		ToolCalls: uint32(max(rec.ToolCalls, 0)), FinishReason: rec.FinishReason,
		ElapsedMs:  uint64(max(rec.Elapsed.Milliseconds(), 0)),
		Incomplete: rec.Incomplete, Refused: rec.Refused,
		Retention:    retentionNotAsked,
		MessagesFrom: uint32(max(rec.MessagesFrom, 0)), HistoryHash: rec.HistoryHash,
	}
	switch {
	case kept:
		row.Retention = retentionKept
	case rec.RetainAsked:
		row.Retention = retentionNotKept
	}
	some := func(s string) (o option.Option[string]) {
		if s != "" {
			o = option.Some(s)
		}
		return
	}
	row.Parent, row.ReportedModel, row.ProviderId, row.ToolsDigest = some(rec.ParentCallId), some(rec.ReportedModel), some(rec.ProviderId), some(rec.ToolsDigest)
	if rec.Error != "" {
		row.Error = []string{rec.Error}
	}
	if rec.OmitTo > 0 {
		row.OmitFrom, row.OmitTo = option.Some(uint32(max(rec.OmitFrom, 0))), option.Some(uint32(rec.OmitTo))
	}
	if rec.CachedInputTokens.Has {
		row.CachedInputTokens = option.Some(uint32(max(rec.CachedInputTokens.Val, 0)))
	}
	if rec.ReasoningTokens.Has {
		row.ReasoningTokens = option.Some(uint32(max(rec.ReasoningTokens.Val, 0)))
	}
	if rec.Admission != "" {
		row.Admission, row.AdmissionRule = option.Some(rec.Admission), some(rec.AdmissionRule)
		row.QueuedMs = option.Some(uint64(max(rec.Queued.Milliseconds(), 0)))
	}
	return
}

// CallsI is the read side the introspection provider needs.
type CallsI interface {
	Calls() []CallRecord
}

// LedgerI is the read side of the usage tables (ADR-0300 §SD3); the
// service implements it beside CallsI.
type LedgerI interface {
	Ledger() *ration.Ledger
}

// RegisterIntrospect registers keelson('llm_calls') over calls, and
// keelson('llm_usage') and keelson('llm_rations') over its ledger when
// calls also implements LedgerI; nil leaves the tables empty rather than
// absent.
func RegisterIntrospect(reg *introspect.Registry, calls CallsI) (err error) {
	if err = reg.Register(callsProvider{calls: calls}); err != nil {
		return
	}
	var ledger *ration.Ledger
	if l, ok := calls.(LedgerI); ok && l != nil {
		ledger = l.Ledger()
	}
	if err = reg.Register(usageProvider{ledger: ledger}); err != nil {
		return
	}
	return reg.Register(rationsProvider{ledger: ledger})
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
		String("completion", func(i int) string { return rows[i].Completion }).
		String("conversation", func(i int) string { return rows[i].Conversation }).
		String("turn", func(i int) string { return rows[i].Turn }).
		Int64("round", func(i int) int64 { return int64(rows[i].Round) }).
		String("parent_call_id", func(i int) string { return rows[i].ParentCallId }).
		String("task", func(i int) string { return rows[i].Task }).
		String("task_call", func(i int) string { return rows[i].TaskCall }).
		String("provider_id", func(i int) string { return rows[i].ProviderId }).
		String("reported_model", func(i int) string { return rows[i].ReportedModel }).
		String("tools_digest", func(i int) string { return rows[i].ToolsDigest }).
		Int64("max_tokens", func(i int) int64 { return int64(rows[i].MaxTokens) }).
		Bool("durable", func(i int) bool { return rows[i].Durable }).
		Bool("retain_asked", func(i int) bool { return rows[i].RetainAsked }).
		Bool("kept", func(i int) bool { return rows[i].Kept }).
		Int64("messages_from", func(i int) int64 { return int64(rows[i].MessagesFrom) }).
		Int64("omit_from", func(i int) int64 { return int64(rows[i].OmitFrom) }).
		Int64("omit_to", func(i int) int64 { return int64(rows[i].OmitTo) }).
		Int64("cached_input_tokens", func(i int) int64 { return optionalCount(rows[i].CachedInputTokens) }).
		Int64("reasoning_tokens", func(i int) int64 { return optionalCount(rows[i].ReasoningTokens) }).
		String("admission", func(i int) string { return rows[i].Admission }).
		String("admission_rule", func(i int) string { return rows[i].AdmissionRule }).
		Int64("queued_ms", func(i int) int64 { return rows[i].Queued.Milliseconds() })
}

// optionalCount is a count a provider may not report, -1 when it did not.
func optionalCount(o option.Option[int32]) (n int64) {
	if !o.Has {
		return -1
	}
	return int64(o.Val)
}

func sensitivityName(s queryengine.SensitivityE) (name string) {
	if s == queryengine.SensitivityConfined {
		return "confined"
	}
	return "ordinary"
}
