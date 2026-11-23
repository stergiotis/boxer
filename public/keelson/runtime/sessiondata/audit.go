package sessiondata

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// AuditDomain is the trail domain of the capability's events.
const AuditDomain = "sessiondata"

// The audited actions, as a trail row names them.
const (
	// AuditOpen is a person's data session opened.
	AuditOpen = "open"
	// AuditAttachSession is a window attached to a data session.
	AuditAttachSession = "attach-session"
	// AuditClose is a data session ended.
	AuditClose = "close"
	// AuditDefine is a definition and the first build of its table.
	AuditDefine = "define"
	// AuditReplay re-makes a table from a definition already recorded.
	AuditReplay = "replay"
	// AuditExpire is the capability finding tables gone from the server.
	AuditExpire = "expire"
	// AuditStale is data sets left to rebuild after one they read changed.
	AuditStale = "stale"
	// AuditDrop is data sets dropped.
	AuditDrop = "drop"
	// AuditAttach is ad-hoc datasets attached to a statement as external
	// data.
	AuditAttach = "attach"
)

// The labels a trail row names for what was made of sealed data.
const (
	auditLabelConfined = "confined"
	auditLabelOwnClear = "own-clear"
)

// The reference and attribute names of the capability's trail rows.
const (
	auditRefTask       = "agent-task"
	auditRefDataset    = "session-dataset"
	auditRefDependency = "session-dataset-read"
	auditRefAdhoc      = "adhoc-dataset"
	auditRefWindow     = "window"
)

// AuditRecord is one audited operation. Datasets and Dependencies name data
// sets as name@revision; Attachments name ad-hoc datasets as
// alias@handle#revision:digest. Lineage is read from define and replay
// rows: the data set made, what it read, and the definition's digest.
type AuditRecord struct {
	At           time.Time
	Action       string
	Outcome      string
	Reason       string
	Session      *session
	Context      option.Option[app.CallContext]
	By           Identity
	Endpoint     string
	Datasets     []string
	Dependencies []string
	Attachments  []string
	// OwnClear are the attachments the dataset service answered as the
	// reading task's own publication at TLP:CLEAR. Label says how what was
	// made of sealed data is labelled, empty where none was read:
	// "own-clear" when everything sealed was the reading task's own
	// publication at TLP:CLEAR, so no label follows, "confined" otherwise.
	OwnClear   []string
	Label      string
	Definition string
	Stratum    string
	Table      string
	Rows       uint64
	Bytes      uint64
	Elapsed    time.Duration
}

// outcomeOf is the trail outcome of a refusal or a failure.
func outcomeOf(err error) (outcome string) {
	var ge *GrantError
	switch {
	case err == nil:
		return trail.OutcomeOk
	case errors.As(err, &ge), errors.Is(err, ErrUnattested):
		return trail.OutcomeDenied
	}
	return trail.OutcomeFailed
}

// digest is the trail's content digest of a definition.
func digest(s string) (d string) {
	sum := blake3.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// audit logs r and, on a durable trail, buffers it as an audit event.
func (inst *Service) audit(ctx context.Context, r AuditRecord) {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	if hook := inst.auditHook.Load(); hook != nil {
		(*hook)(r)
	}
	ev := inst.log.Info()
	if r.Outcome != trail.OutcomeOk {
		ev = inst.log.Warn()
	}
	ev.Str("action", r.Action).Str("outcome", r.Outcome).Strs("datasets", r.Datasets).Str("endpoint", r.Endpoint).
		Str("reason", r.Reason).Msg("sessiondata: " + r.Action)
	rec := inst.trail
	if !rec.Durable() {
		return
	}
	row := trail.AuditEvent{Domain: AuditDomain, Action: r.Action, Outcome: r.Outcome, Retention: trail.RetentionTrail}
	ref := func(ty, v string) {
		if v != "" && len(row.RefTypes) < trail.MaxRefs {
			row.RefTypes, row.RefValues = append(row.RefTypes, ty), append(row.RefValues, trail.Bounded(v))
		}
	}
	attr := func(k, v string) {
		if v != "" && len(row.AttrKeys) < trail.MaxAttrs {
			row.AttrKeys, row.AttrValues = append(row.AttrKeys, k), append(row.AttrValues, trail.Bounded(v))
		}
	}
	if r.Session != nil {
		ref(auditRefTask, r.Session.task)
		attr("session-kind", r.Session.kind.String())
		// The ticket is a bearer capability; the row names its digest.
		attr("session", digest(r.Session.ticket))
	}
	if !r.By.isRuntime() {
		ref(auditRefWindow, string(r.By.App)+"#"+strconv.FormatUint(r.By.Instance, 10))
	}
	for _, d := range r.Datasets {
		ref(auditRefDataset, d)
	}
	for _, d := range r.Dependencies {
		ref(auditRefDependency, d)
	}
	for _, a := range r.Attachments {
		ref(auditRefAdhoc, a)
	}
	attr("endpoint", r.Endpoint)
	attr("reason", r.Reason)
	attr("label", r.Label)
	attr("own-clear", strings.Join(r.OwnClear, " "))
	if r.Definition != "" {
		attr("definition-digest", digest(r.Definition))
		attr("definition", r.Definition)
	}
	attr("stratum", r.Stratum)
	attr("table", r.Table)
	if r.Outcome == trail.OutcomeOk && (r.Action == AuditDefine || r.Action == AuditReplay) {
		attr("rows", strconv.FormatUint(r.Rows, 10))
		attr("bytes", strconv.FormatUint(r.Bytes, 10))
		attr("elapsed-ms", strconv.FormatInt(r.Elapsed.Milliseconds(), 10))
	}
	c := trail.Context{Origin: rec.OriginOf(ServiceAppId, 0)}
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
	}
	if err := rec.Event(ctx, r.At, c, row); err != nil {
		inst.log.Warn().Err(err).Str("action", r.Action).Msg("sessiondata: buffer the audit event")
		return
	}
	rec.FlushSoon()
}

// auditAttachments records the ad-hoc datasets a statement attached, which
// of them were read as the reading task's own publication at TLP:CLEAR,
// and, with ownClear, that the statement's result carries no label for
// what it read.
func (inst *Service) auditAttachments(ctx context.Context, s *session, cc option.Option[app.CallContext], c Caller, host string, atts []attachment, ownClear bool) {
	if len(atts) == 0 {
		return
	}
	r := AuditRecord{Action: AuditAttach, Outcome: trail.OutcomeOk, Session: s, Context: cc, By: c.By, Endpoint: host,
		OwnClear: ownClearRefs(atts), Label: auditLabelConfined}
	if ownClear {
		r.Label = auditLabelOwnClear
	}
	for _, a := range atts {
		r.Attachments = append(r.Attachments, a.ref())
	}
	inst.audit(ctx, r)
}

// setAuditHook makes hook see every audit record as it is made; the
// package's tests read the record through it.
func (inst *Service) setAuditHook(hook func(AuditRecord)) {
	if hook == nil {
		inst.auditHook.Store(nil)
		return
	}
	inst.auditHook.Store(&hook)
}
