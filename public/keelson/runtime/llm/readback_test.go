package llm

import (
	"context"
	"iter"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// ScanCalls reads the durable rows since a point in time, oldest first,
// up to limit, as records; nil, nil without a store. The tests' view of
// what landed: keelson('llm_calls') reads the ring, and a reader of the
// table reads the trail views.
func (inst *Service) ScanCalls(ctx context.Context, since time.Time, limit int) (recs []CallRecord, err error) {
	opts := recordstore.ScanOpts{
		ExtraPredicate: trail.TrailColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")",
		Limit:          limit,
	}
	ents, err := inst.cfg.Trail.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] { return st.ScanLlmCall(ctx, opts) })
	if err != nil {
		return nil, err
	}
	for _, ent := range ents {
		recs = append(recs, RecordOf(ent))
	}
	return
}

// RecordOf is the record of a durable row and its context components,
// minus what the row never carried: the inverse of RowOf, which the tests
// hold the row to.
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
	return
}
