package llm

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// RecordOf is the record of a durable row and its context components,
// minus what the row never carried: the inverse of RowOf, which the tests
// hold the row to, and what the ledger is rebuilt from (ADR-0300 §SD3).
func RecordOf(ent *trail.TrailEntity) (rec CallRecord) {
	row := ent.LlmCall.Val
	rec = CallRecord{
		CallId: row.CallId, At: ent.Ts, Purpose: row.Purpose, Model: row.Model, EndpointHost: row.EndpointHost,
		ReportedModel: row.ReportedModel.Val, ProviderId: row.ProviderId.Val,
		Messages: int(row.Messages), Tools: int(row.Tools), ToolsDigest: row.ToolsDigest.Val, MaxTokens: int32(row.MaxTokens),
		PromptBytes: int(row.PromptBytes), CompletionBytes: int(row.CompletionBytes),
		InputTokens: int32(row.InputTokens), OutputTokens: int32(row.OutputTokens), ToolCalls: int(row.ToolCalls),
		FinishReason: row.FinishReason, Elapsed: time.Duration(row.ElapsedMs) * time.Millisecond,
		Incomplete: row.Incomplete, Refused: row.Refused,
		ParentCallId: row.Parent.Val, Durable: true,
		RetainAsked: row.Retention != retentionNotAsked, Kept: row.Retention == retentionKept,
		MessagesFrom: int(row.MessagesFrom), HistoryHash: row.HistoryHash,
	}
	if len(row.Error) > 0 {
		rec.Error = row.Error[0]
	}
	if row.Sensitivity == "confined" {
		rec.Sensitivity = queryengine.SensitivityConfined
	}
	if o := ent.Origin; o.Has {
		rec.Sender, rec.SenderInstance = app.AppIdT(o.Val.App), o.Val.Instance
	}
	if c := ent.Conversation; c.Has {
		rec.Conversation, rec.Turn, rec.Round = c.Val.Conversation, c.Val.Turn.Val, int(c.Val.Round.Val)
	}
	if d := ent.Delegation; d.Has {
		rec.Task, rec.TaskEpoch, rec.TaskCall = d.Val.Task, d.Val.Epoch, d.Val.Call.Val
	}
	if row.OmitTo.Has {
		rec.OmitFrom, rec.OmitTo = int(row.OmitFrom.Val), int(row.OmitTo.Val)
	}
	if row.CachedInputTokens.Has {
		rec.CachedInputTokens = option.Some(int32(row.CachedInputTokens.Val))
	}
	if row.ReasoningTokens.Has {
		rec.ReasoningTokens = option.Some(int32(row.ReasoningTokens.Val))
	}
	rec.Admission, rec.AdmissionRule = row.Admission.Val, row.AdmissionRule.Val
	rec.Queued = time.Duration(row.QueuedMs.Val) * time.Millisecond
	return
}
