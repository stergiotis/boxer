package adhocdata

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// ExposureAuditDomain is the trail domain of exposure events (ADR-0407
// §SD5, proposed).
const ExposureAuditDomain = "adhoc-expose"

// The audited exposure actions, as a trail row names them.
const (
	// ExposureAuditExpose is an exposure made, or refused.
	ExposureAuditExpose = "expose"
	// ExposureAuditRead is one request that received an exposed dataset.
	ExposureAuditRead = "read"
	// ExposureAuditRevoke is an exposure revoked, or a revoke refused.
	ExposureAuditRevoke = "revoke"
	// ExposureAuditLapse is an exposure ended by its lifetime, its dataset,
	// its window or the host stopping.
	ExposureAuditLapse = "lapse"
)

const (
	auditOk     = trail.OutcomeOk
	auditDenied = trail.OutcomeDenied
	auditFailed = trail.OutcomeFailed
)

// ExposureAuditRecord is one audited exposure event. It never holds the
// token. Remote and UserAgent are what a read's request declared about
// itself, not who sent it.
type ExposureAuditRecord struct {
	At        time.Time
	Action    string
	Outcome   string
	Reason    string
	Exposure  string
	Alias     string
	Bundle    string
	Revision  uint64
	By        Identity
	Context   option.Option[app.CallContext]
	Task      string
	Rows      uint64
	Bytes     uint64
	Reads     uint64
	ExpiresAt time.Time
	Format    string
	Range     string
	Remote    string
	UserAgent string
}

// The reference and attribute names of an exposure's trail rows.
const (
	exposureRefExposure = "exposure"
	exposureRefDataset  = "adhoc-dataset"
	exposureRefTask     = "agent-task"
	exposureRefWindow   = "window"
)

// auditExposure logs r and, on a durable trail, buffers it as an audit
// event.
func (inst *Service) auditExposure(ctx context.Context, r ExposureAuditRecord) {
	if r.At.IsZero() {
		r.At = time.Now()
	}
	if hook := inst.exposureHook.Load(); hook != nil {
		(*hook)(r)
	}
	ev := inst.log.Info()
	if r.Outcome != auditOk {
		ev = inst.log.Warn()
	}
	ev.Str("action", r.Action).Str("outcome", r.Outcome).Str("exposure", r.Exposure).Str("alias", r.Alias).
		Str("reason", r.Reason).Msg("adhocdata: exposure " + r.Action)
	rec := inst.trail
	if !rec.Durable() {
		return
	}
	row := trail.AuditEvent{Domain: ExposureAuditDomain, Action: r.Action, Outcome: r.Outcome, Retention: trail.RetentionTrail}
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
	num := func(k string, v uint64) {
		if v != 0 {
			attr(k, strconv.FormatUint(v, 10))
		}
	}
	ref(exposureRefExposure, r.Exposure)
	if r.Alias != "" {
		d := r.Alias
		if r.Revision != 0 {
			d += "@" + strconv.FormatUint(r.Revision, 10)
		}
		ref(exposureRefDataset, d)
	}
	task := r.Task
	if task == "" && r.Context.Has {
		task = r.Context.Val.Task
	}
	ref(exposureRefTask, task)
	if !r.By.IsRuntime() {
		ref(exposureRefWindow, string(r.By.App)+"#"+strconv.FormatUint(r.By.Instance, 10))
	}
	attr("bundle", r.Bundle)
	attr("reason", r.Reason)
	num("rows", r.Rows)
	num("bytes", r.Bytes)
	num("reads", r.Reads)
	if !r.ExpiresAt.IsZero() {
		attr("expires", r.ExpiresAt.UTC().Format(time.RFC3339))
	}
	attr("format", r.Format)
	attr("range", r.Range)
	attr("declared-remote", r.Remote)
	attr("declared-user-agent", r.UserAgent)
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
		inst.log.Warn().Err(err).Str("action", r.Action).Msg("adhocdata: buffer the exposure audit event")
		return
	}
	rec.FlushSoon()
}

// setExposureHook makes hook see every exposure audit record as it is
// made; the package's tests read the record through it.
func (inst *Service) setExposureHook(hook func(ExposureAuditRecord)) {
	if hook == nil {
		inst.exposureHook.Store(nil)
		return
	}
	inst.exposureHook.Store(&hook)
}
