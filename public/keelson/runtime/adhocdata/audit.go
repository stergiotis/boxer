package adhocdata

import (
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrUnattested refuses an agent-caused bundle request whose on-behalf-of
// context the dispatcher does not confirm, or that no dispatcher can
// confirm (ADR-0288 §SD5).
var ErrUnattested = errors.New("on-behalf-of context not attested by the dispatcher")

// The audited bundle operations and their outcomes (ADR-0288
// §SD5), as the trail row and the in-process record name them.
const (
	AuditPublish   = "publish"
	AuditRepublish = "republish"
	AuditRetract   = "retract"
	// AuditWithdraw is the runtime's retract when the publishing window
	// closed (ADR-0240 §SD5).
	AuditWithdraw = "withdraw"
	AuditResolve  = "resolve"
	// AuditReadColumns is a read of a dataset's column summaries alone:
	// values of the data, held to the grant a read is (§SD5).
	AuditReadColumns = "read-columns"

	AuditApplied = "applied"
	AuditRefused = "refused"
)

// AuditRecord is one audited bundle operation (ADR-0288 §SD5).
// Context is set when an agent's call caused the operation and the
// dispatcher attested it.
type AuditRecord struct {
	At         time.Time
	Operation  string
	Outcome    string
	Reason     string
	Bundle     string
	Revision   uint64
	By         Identity
	Owner      Identity
	LocalNames []string
	Aliases    []string
	Handles    []string
	Rows       []uint64
	// Bytes are the streams' lengths as a reader receives them, the bytes
	// their digests are over.
	Bytes          []uint64
	StreamDigests  []string
	DocumentDigest string
	Context        option.Option[app.CallContext]
	// Provenance and shape, on a publish and a republish (§SD5): the
	// document as published, the statement that produced the rows, the
	// datasets it read with their digests as the service found them, and
	// each dataset's columns with their summaries, ColumnDatasets naming
	// the dataset by its position.
	Document       []byte
	SourceSql      string
	InputHandles   []string
	InputAliases   []string
	InputDigests   []string
	ColumnDatasets []uint32
	Columns        ColumnSummaries
}

// DispatcherI is what the dataset service asks the host's agent
// dispatcher: who an agent-caused request is work of (§SD5), and whether
// the task's grant lists what a read reaches (§SD6).
type DispatcherI interface {
	app.CallContextI
	app.DelegationI
}

type callContextRef struct{ c DispatcherI }

// SetDispatcher installs the dispatcher that attests agent-caused bundle
// requests and checks an agent's read against its grant (ADR-0288
// §SD5, §SD6). Until it is set, an agent-caused request is
// refused: nothing could confirm what it claims.
func (inst *Service) SetDispatcher(d DispatcherI) {
	inst.callCtx.Store(&callContextRef{c: d})
}

// attest confirms obo with the dispatcher for a request by. A nil obo is
// the person's or the app's own request and needs nothing. A host whose
// trail must be durable (trail.RequiredEnv) and is not refuses
// agent-caused work, as the egress service does.
func (inst *Service) attest(by Identity, obo *app.OnBehalfOf) (cc option.Option[app.CallContext], err error) {
	if obo == nil || obo.Task == "" {
		return
	}
	ref := inst.callCtx.Load()
	if ref == nil || ref.c == nil {
		return cc, eb.Build().Str("task", obo.Task).Str("call", obo.Call).Errorf("no dispatcher to attest the call: %w", ErrUnattested)
	}
	got, ok, why := ref.c.CallContext(obo.Task, obo.Epoch, obo.Call, by.App, by.Instance)
	if !ok {
		return cc, eb.Build().Str("task", obo.Task).Str("call", obo.Call).Errorf("%w: %s", ErrUnattested, why) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text; the dispatcher's reason is what the caller can act on"
	}
	if refuse := inst.trail.Admit(); refuse != nil {
		return cc, eb.Build().Str("task", obo.Task).Errorf("agent-caused bundle operation: %w", refuse)
	}
	return option.Some(got), nil
}

// audit buffers r as a trail row and hands it to the test hook.
func (inst *Service) audit(r AuditRecord) {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	if hook := inst.auditHook.Load(); hook != nil {
		(*hook)(r)
	}
	inst.emitAudit(r.Operation+"-bundle-"+r.Outcome, "", r.Bundle, r.Revision)
	inst.persistAudit(r)
}

// persistAudit buffers r as an AdhocDataset row on the trail (ADR-0277)
// and wakes the flusher; without a durable trail it does nothing.
func (inst *Service) persistAudit(r AuditRecord) {
	rec := inst.trail
	if !rec.Durable() {
		return
	}
	by := r.By
	if by.IsRuntime() {
		by.App = ServiceAppId
	}
	c := trail.Context{Origin: rec.OriginOf(by.App, by.Instance)}
	var cause option.Option[trail.Cause]
	if r.Context.Has {
		cc := r.Context.Val
		c.Delegation = option.Some(trail.Delegation{Task: cc.Task, Epoch: cc.Epoch, Call: option.Some(cc.Call)})
		if cc.Conversation != "" {
			conv := trail.Conversation{Conversation: cc.Conversation}
			if cc.Turn != "" {
				conv.Turn = option.Some(cc.Turn)
			}
			c.Conversation = option.Some(conv)
		}
		if cc.ModelCall != "" {
			cv := trail.Cause{ModelCall: cc.ModelCall, ToolIndex: cc.ToolIndex}
			if cc.ToolCall != "" {
				cv.ToolCall = option.Some(cc.ToolCall)
			}
			cause = option.Some(cv)
		}
	}
	row := trail.AdhocDataset{
		Operation: r.Operation, Outcome: r.Outcome, Bundle: r.Bundle, Revision: r.Revision,
		OwnerApp: string(r.Owner.App), OwnerInstance: r.Owner.Instance,
		LocalNames: r.LocalNames, Aliases: r.Aliases, Handles: r.Handles, Rows: r.Rows, Bytes: r.Bytes,
		StreamDigests: r.StreamDigests, DocumentDigest: r.DocumentDigest, Attested: r.Context.Has,
		InFlight: r.Context.Has && r.Context.Val.InFlight,
		Document: string(r.Document), SourceSql: r.SourceSql,
		InputHandles: r.InputHandles, InputAliases: r.InputAliases, InputDigests: r.InputDigests,
		ColumnDatasets: r.ColumnDatasets, ColumnNames: r.Columns.Names, ColumnTypes: r.Columns.Types,
		ColumnNulls: r.Columns.Nulls, ColumnDistinct: r.Columns.Distinct,
	}
	if r.Reason != "" {
		row.Reason = []string{r.Reason}
	}
	if err := rec.AdhocDataset(r.At, c, cause, row); err != nil {
		inst.log.Warn().Err(err).Str("bundle", r.Bundle).Msg("adhocdata: buffer bundle audit row")
		return
	}
	rec.FlushSoon()
}

// callContextFields is the on-behalf-of context of a request as the bus
// carried it; nil when the request carried none.
func callContextFields(task string, epoch uint64, call string) (obo *app.OnBehalfOf) {
	if task == "" {
		return nil
	}
	return &app.OnBehalfOf{Task: task, Epoch: epoch, Call: call}
}
