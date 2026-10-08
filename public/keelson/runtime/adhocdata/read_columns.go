package adhocdata

import (
	"encoding/json/v2"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/adhocrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ColumnsResult is a dataset's column summaries, read on their own.
type ColumnsResult struct {
	Alias         string
	Bundle        string
	Handle        string
	Revision      uint64
	PublisherTask string
	Columns       ColumnSummaries
}

// ReadColumns returns the column summaries of the newest live dataset under
// alias. A minimum, a maximum and a sample are values of the data, so they
// are read as the data is (ADR-0288 §SD5): audited, and an
// agent's read attested and held to the task's grant, or to its having
// published the dataset. keelson('adhoc_bundles') and the trail carry only
// the statistics that are not values.
func (inst *Service) ReadColumns(alias string, by Identity, obo *app.OnBehalfOf) (res ColumnsResult, err error) {
	cc, err := inst.attest(by, obo)
	if err == nil {
		res, err = inst.columns(alias)
	}
	if err == nil && cc.Has {
		err = inst.checkGrant(obo, ReadResult{Alias: res.Alias, Bundle: res.Bundle, PublisherTask: res.PublisherTask})
		if err != nil {
			res = ColumnsResult{Alias: res.Alias, Bundle: res.Bundle}
		}
	}
	r := AuditRecord{Operation: AuditReadColumns, Bundle: res.Bundle, By: by, Context: cc, Aliases: []string{alias}}
	if err != nil {
		r.Outcome, r.Reason = AuditRefused, err.Error()
	} else {
		r.Outcome, r.Revision, r.Handles = AuditApplied, res.Revision, []string{res.Handle}
	}
	inst.audit(r)
	return
}

func (inst *Service) columns(alias string) (res ColumnsResult, err error) {
	best, err := inst.newest(alias)
	if err != nil {
		return
	}
	best.mu.RLock()
	defer best.mu.RUnlock()
	res = ColumnsResult{Alias: alias, Bundle: best.bundle, Handle: best.handle, Revision: best.revision, Columns: best.columns}
	if best.context.Has {
		res.PublisherTask = best.context.Val.Task
	}
	return
}

func (inst *Service) handleReadColumns(msg *app.Msg, req adhocrequest.AdhocRequest) {
	res, err := inst.ReadColumns(req.Alias, sender(msg), callContextFields(req.OboTask, req.OboEpoch, req.OboCall))
	if err != nil {
		inst.reply(msg.Reply, readRefusal(err))
		return
	}
	doc, err := json.Marshal(res.Columns)
	if err != nil {
		inst.reply(msg.Reply, readRefusal(eh.Errorf("encode column summaries: %w", err)))
		return
	}
	inst.reply(msg.Reply, adhocreply.AdhocReply{At: time.Now().UTC(), Ok: true, Handle: res.Handle, Bundle: res.Bundle,
		Revision: res.Revision, PublisherTask: res.PublisherTask, ColumnSummaries: doc})
}

// ReadColumns reads the column summaries of the newest live dataset under
// alias, values included, over adhoc.read: the read ReadAll makes, held
// to the same grant, without the stream. obo is the agent's call the read
// is work of, nil when it is none.
func ReadColumns(bus app.BusI, alias string, obo *app.OnBehalfOf) (res ColumnsResult, err error) {
	payload, err := buscodec.Encode(adhocrequest.AdhocRequest{
		At: time.Now().UTC(), Op: adhocrequest.OpRead, Alias: alias, ColumnsOnly: true,
		OboTask: oboTask(obo), OboEpoch: oboEpoch(obo), OboCall: oboCall(obo),
	})
	if err != nil {
		return res, eh.Errorf("encode columns read: %w", err)
	}
	raw, err := bus.Request(SubjectRead, payload)
	if err != nil {
		return res, eb.Build().Str("alias", alias).Errorf("columns read request: %w", err)
	}
	rep, err := buscodec.Decode[adhocreply.AdhocReply](raw)
	if err != nil {
		return res, eh.Errorf("decode columns read reply: %w", err)
	}
	if !rep.Ok {
		return res, readReplyError(alias, rep)
	}
	res = ColumnsResult{Alias: alias, Bundle: rep.Bundle, Handle: rep.Handle, Revision: rep.Revision, PublisherTask: rep.PublisherTask}
	if err = json.Unmarshal(rep.ColumnSummaries, &res.Columns); err != nil {
		return ColumnsResult{}, eh.Errorf("decode column summaries: %w", err)
	}
	return res, nil
}
