package adhocdata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Exposure (ADR-0407): a published dataset made readable by programs on
// this machine. An exposure names a dataset's alias and serves its live
// revision to a request on the host's loopback listener that presents the
// exposure's token; it lapses with the dataset, with the window that made
// it, at the end of its lifetime, or when it is revoked.

// Exposure bounds (ADR-0407 §SD3).
const (
	// DefaultExposureLifetime is how long an exposure lasts when the
	// request names no lifetime.
	DefaultExposureLifetime = time.Hour
	// MaxExposureLifetime is the longest lifetime a request may name.
	MaxExposureLifetime = 24 * time.Hour
	// MaxExposures caps the exposures live at once in the process.
	MaxExposures = 64
	// MaxExposureReads caps the requests one exposure serves at once: a
	// ClickHouse url() read may fetch ranges of a large stream in parallel.
	MaxExposureReads = 16
)

// DestinationClassExpose is the grant destination class an agent's
// exposure needs: "expose:<prefix>" covers every alias that starts with the
// prefix, the alias itself being the narrowest.
const DestinationClassExpose = "expose"

// DestinationExpose is the grant destination that covers exposing the
// datasets whose alias starts with prefix.
func DestinationExpose(prefix string) (destination string) {
	return DestinationClassExpose + ":" + prefix
}

// The prefixes of an exposure's id and its token.
const (
	exposureIdPrefix    = "exp_"
	exposureTokenPrefix = "bxe_"
)

// ErrNoListener refuses an exposure when no loopback listener serves this
// host's tables: nothing could read it.
var ErrNoListener = errors.New("no loopback listener serves this host's tables, so nothing could read an exposure")

// ErrNotTheTasks refuses an agent's exposure of a dataset its task did not
// publish.
var ErrNotTheTasks = errors.New("a task exposes only a dataset its own call published")

// ErrUnknownExposure is a revoke naming no live exposure.
var ErrUnknownExposure = errors.New("no live exposure by that id")

// ExposeInput is the in-process shape of an exposure request.
type ExposeInput struct {
	Alias string
	// Lifetime is how long the exposure lasts; zero is
	// DefaultExposureLifetime, and more than MaxExposureLifetime is refused.
	Lifetime time.Duration
	// By is the window that asks; the bus handler fills it from the
	// envelope.
	By Identity
	// OnBehalfOf is the agent's call the exposure is work of; nil for the
	// person's.
	OnBehalfOf *app.OnBehalfOf
}

// ExposureInfo is an exposure as the catalog shows it: never its token.
type ExposureInfo struct {
	Id     string
	Alias  string
	Bundle string
	// Revision is the dataset's live revision: what a read serves now.
	Revision  uint64
	Structure string
	Rows      uint64
	// Bytes is the length of the Arrow stream a read receives.
	Bytes     uint64
	URL       string
	CSVURL    string
	CreatedAt time.Time
	ExpiresAt time.Time
	// Reads counts the requests that received data; BytesServed what they
	// received.
	Reads       uint64
	BytesServed uint64
	By          Identity
	// Task is the task whose call made the exposure; empty for the person's.
	Task string
	// Yours is set when the caller that listed it made it: its window, or
	// a call of its task.
	Yours bool
}

// ExposeResult is the answer to an exposure: the catalog's row, and what a
// reader needs, which only this answer carries.
type ExposeResult struct {
	ExposureInfo
	// Token is the bearer token a reader presents as
	// "Authorization: Bearer <token>".
	Token string
	// ClickHouse is the table expression a ClickHouse server on this
	// machine reads the dataset with.
	ClickHouse string
	// Curl is a command line that fetches the Arrow stream.
	Curl string
}

// exposure is one live exposure. Its token exists only as its digest.
type exposure struct {
	id        string
	digest    [sha256.Size]byte
	alias     string
	bundle    string
	owner     Identity
	structure string
	by        Identity
	context   option.Option[app.CallContext]
	baseURL   string
	createdAt time.Time
	expiresAt time.Time
	timer     *time.Timer

	// Guarded by exposures.mu.
	ended  bool
	open   int
	reads  uint64
	served uint64
}

func (inst *exposure) task() (task string) {
	if inst.context.Has {
		task = inst.context.Val.Task
	}
	return
}

// exposures is the service's live exposures. Its lock is taken after the
// service's, never before.
type exposures struct {
	mu       sync.Mutex
	byId     map[string]*exposure
	byDigest map[[sha256.Size]byte]*exposure
}

// Expose makes the newest live dataset under in.Alias readable by programs
// on this machine that present the token the answer carries (ADR-0407).
// A person's request needs nothing more; an agent's call is attested, its
// task's grant must list expose:<prefix> for a prefix of the alias, the
// task must have published the dataset, and neither that publish nor the
// call may be labelled above TLP:CLEAR. Every request is audited, refused
// or not.
func (inst *Service) Expose(ctx context.Context, in ExposeInput) (res ExposeResult, err error) {
	cc, err := inst.attest(in.By, in.OnBehalfOf)
	if err == nil {
		res, err = inst.expose(in, cc)
	}
	r := ExposureAuditRecord{Action: ExposureAuditExpose, Alias: in.Alias, By: in.By, Context: cc}
	if err != nil {
		r.Outcome, r.Reason = exposureOutcome(err), err.Error()
	} else {
		r.Outcome, r.Exposure, r.Revision, r.Bundle = auditOk, res.Id, res.Revision, res.Bundle
		r.Rows, r.Bytes, r.ExpiresAt = res.Rows, res.Bytes, res.ExpiresAt
	}
	inst.auditExposure(ctx, r)
	return
}

func (inst *Service) expose(in ExposeInput, cc option.Option[app.CallContext]) (res ExposeResult, err error) {
	lifetime := in.Lifetime
	switch {
	case lifetime == 0:
		lifetime = DefaultExposureLifetime
	case lifetime < 0 || lifetime > MaxExposureLifetime:
		return res, eb.Build().Str("alias", in.Alias).Str("lifetime", lifetime.String()).
			Errorf("an exposure lasts at most %s", MaxExposureLifetime) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text and names the bound"
	case !validAlias(in.Alias):
		return res, eb.Build().Str("alias", in.Alias).Errorf("invalid alias (want [A-Za-z_][A-Za-z0-9_]*, <=64)")
	}
	base := inst.reg.ServedAt()
	if base == "" {
		return res, ErrNoListener
	}
	rec, err := inst.newest(in.Alias)
	if err != nil {
		return
	}
	if cc.Has {
		if err = inst.checkExposeGrant(in.OnBehalfOf, in.Alias); err != nil {
			return
		}
		if err = checkTaskMayExpose(rec, cc.Val); err != nil {
			return
		}
	}
	id, token, err := newExposureCredentials()
	if err != nil {
		return
	}
	now := time.Now()
	rec.mu.RLock()
	e := &exposure{
		id: id, digest: sha256.Sum256([]byte(token)), alias: in.Alias, bundle: rec.bundle, owner: rec.owner,
		structure: rec.structure, by: in.By, context: cc, baseURL: base, createdAt: now, expiresAt: now.Add(lifetime),
	}
	rec.mu.RUnlock()
	inst.exp.mu.Lock()
	if len(inst.exp.byId) >= MaxExposures {
		inst.exp.mu.Unlock()
		return res, eb.Build().Int("quotaCount", MaxExposures).Errorf("the host holds as many exposures as it admits; revoke one first")
	}
	inst.exp.byId[id] = e
	inst.exp.byDigest[e.digest] = e
	e.timer = time.AfterFunc(lifetime, func() {
		inst.endExposure(context.Background(), e, ExposureAuditLapse, "its lifetime ended")
	})
	info := inst.infoLocked(e, rec, in.By, cc)
	inst.exp.mu.Unlock()
	res = ExposeResult{ExposureInfo: info, Token: token,
		ClickHouse: ClickHouseExpression(info.URL, info.Structure, token), Curl: CurlLine(info.URL, token)}
	return
}

// ClickHouseExpression is the url() table expression a ClickHouse server on
// this machine reads an exposed dataset with: the token travels in a
// header, whose value ClickHouse hides in its query log.
func ClickHouseExpression(url string, structure string, token string) (expr string) {
	return "url(" + sqlString(url) + ", 'ArrowStream', " + sqlString(structure) +
		", headers('Authorization' = " + sqlString("Bearer "+token) + "))"
}

// CurlLine is a command line that fetches an exposed dataset's Arrow stream.
func CurlLine(url string, token string) (line string) {
	return "curl -sf -H 'Authorization: Bearer " + token + "' '" + url + "'"
}

// sqlString is s as a ClickHouse string literal.
func sqlString(s string) (lit string) {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// checkExposeGrant asks the dispatcher whether the task's grant lists
// expose:<prefix> for a prefix of alias, the alias itself first.
func (inst *Service) checkExposeGrant(obo *app.OnBehalfOf, alias string) (err error) {
	ref := inst.callCtx.Load()
	if ref == nil || ref.c == nil {
		return eh.Errorf("no dispatcher to check the grant: %w", ErrUnattested)
	}
	for i := len(alias); i > 0; i-- {
		if ok, _ := ref.c.AllowDestination(obo.Task, obo.Epoch, DestinationExpose(alias[:i])); ok {
			return nil
		}
	}
	d := DestinationExpose(alias)
	return &GrantError{Destination: d, Reason: "exposing " + alias + " needs " + d + ", or expose: with a shorter prefix of it, in the task's grant"}
}

// checkTaskMayExpose refuses an agent's exposure of a dataset its task did
// not publish, or one published or asked for under a label above
// TLP:CLEAR: what leaves by an exposure is the task's own work, at the
// lowest tier.
func checkTaskMayExpose(rec *record, cc app.CallContext) (err error) {
	rec.mu.RLock()
	pub := rec.context
	alias := rec.alias
	rec.mu.RUnlock()
	switch {
	case !pub.Has || pub.Val.Task != cc.Task:
		return eb.Build().Str("alias", alias).Str("task", cc.Task).
			Errorf("%w; %s was not published by this task's call, so only the person exposes it, from play's Data sets tab", ErrNotTheTasks, alias) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text and names who may expose it"
	case pub.Val.Tier > uint8(agentrun.TierClear):
		return eb.Build().Str("alias", alias).Errorf("%s was published by a call labelled %s; a task exposes only what it published at TLP:CLEAR, and the person exposes the rest", alias, agentrun.TierE(pub.Val.Tier)) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text"
	case cc.Tier > uint8(agentrun.TierClear):
		return eb.Build().Str("alias", alias).Errorf("the call is labelled %s; a task exposes data only from a run at TLP:CLEAR, and the person exposes the rest", agentrun.TierE(cc.Tier)) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text"
	}
	return nil
}

// newExposureCredentials mints an exposure's id, which names it in the
// catalog and on the trail, and its token, 256 random bits that only the
// answer to the exposure carries.
func newExposureCredentials() (id string, token string, err error) {
	var idBytes [8]byte
	var tokenBytes [32]byte
	if _, err = rand.Read(idBytes[:]); err == nil {
		_, err = rand.Read(tokenBytes[:])
	}
	if err != nil {
		return "", "", eh.Errorf("mint exposure credentials: %w", err)
	}
	return exposureIdPrefix + hex.EncodeToString(idBytes[:]), exposureTokenPrefix + hex.EncodeToString(tokenBytes[:]), nil
}

// infoLocked is e as the catalog shows it to the caller by, cc. The caller
// holds exp.mu; rec is the live dataset, nil when none.
func (inst *Service) infoLocked(e *exposure, rec *record, by Identity, cc option.Option[app.CallContext]) (info ExposureInfo) {
	url := e.baseURL + introspectExposedPath + e.alias
	info = ExposureInfo{
		Id: e.id, Alias: e.alias, Bundle: e.bundle, Structure: e.structure, URL: url, CSVURL: url + "/csv",
		CreatedAt: e.createdAt, ExpiresAt: e.expiresAt, Reads: e.reads, BytesServed: e.served, By: e.by, Task: e.task(),
	}
	if rec != nil {
		rec.mu.RLock()
		info.Revision, info.Rows, info.Bytes = rec.revision, rec.rows, rec.plainBytes
		rec.mu.RUnlock()
	}
	switch {
	case cc.Has:
		info.Yours = info.Task == cc.Val.Task
	default:
		info.Yours = e.by == by && !by.IsRuntime()
	}
	return
}

// introspectExposedPath is the listener's path for an exposed dataset;
// introspecthttp.ExposedPathPrefix spells it there.
const introspectExposedPath = "/exposed/"

// liveFor is the dataset e serves now, or why e has lapsed: the alias holds
// no live dataset, another publisher holds it, or its columns changed.
func (inst *Service) liveFor(e *exposure) (rec *record, lapsed string) {
	rec, err := inst.newest(e.alias)
	if err != nil {
		return nil, "the dataset was retracted"
	}
	rec.mu.RLock()
	defer rec.mu.RUnlock()
	switch {
	case rec.owner != e.owner || rec.bundle != e.bundle:
		return nil, "another publisher holds the alias now"
	case rec.structure != e.structure:
		return nil, "a republish changed its columns"
	case e.context.Has && rec.context.Has && rec.context.Val.Tier > uint8(agentrun.TierClear):
		return nil, "a republish was labelled above TLP:CLEAR"
	}
	return
}

// OpenExposed implements introspect.ExposureSourceI: it opens the dataset
// exposed under alias for a request presenting token.
func (inst *Service) OpenExposed(alias string, token string, req introspect.ExposedRequest) (rd introspect.ExposedRead, status introspect.ExposedStatusE) {
	if !strings.HasPrefix(token, exposureTokenPrefix) {
		return
	}
	digest := sha256.Sum256([]byte(token))
	now := time.Now()
	inst.exp.mu.Lock()
	e := inst.exp.byDigest[digest]
	switch {
	case e == nil || e.ended || e.alias != alias:
		inst.exp.mu.Unlock()
		return
	case !now.Before(e.expiresAt):
		inst.exp.mu.Unlock()
		inst.endExposure(context.Background(), e, ExposureAuditLapse, "its lifetime ended")
		return
	case e.open >= MaxExposureReads:
		inst.exp.mu.Unlock()
		return rd, introspect.ExposedBusy
	}
	e.open++
	inst.exp.mu.Unlock()
	release := func() {
		inst.exp.mu.Lock()
		e.open--
		inst.exp.mu.Unlock()
	}
	rec, lapsed := inst.liveFor(e)
	if lapsed != "" {
		release()
		inst.endExposure(context.Background(), e, ExposureAuditLapse, lapsed)
		return
	}
	rc, revision, err := rec.Open()
	if err != nil {
		release()
		return
	}
	rd = introspect.ExposedRead{Reader: rc, Revision: revision, Done: func(sent uint64, rerr error) {
		_ = rc.Close()
		inst.exp.mu.Lock()
		e.open--
		if !req.Head {
			e.reads++
			e.served += sent
		}
		inst.exp.mu.Unlock()
		if req.Head {
			return
		}
		r := ExposureAuditRecord{Action: ExposureAuditRead, Outcome: auditOk, Exposure: e.id, Alias: e.alias, Bundle: e.bundle,
			Revision: revision, Task: e.task(), Bytes: sent, Format: req.Format.String(), Range: req.Range,
			Remote: req.Remote, UserAgent: req.UserAgent}
		if rerr != nil {
			r.Outcome, r.Reason = auditFailed, rerr.Error()
		}
		inst.auditExposure(context.Background(), r)
	}}
	return rd, introspect.ExposedOpen
}

var _ introspect.ExposureSourceI = (*Service)(nil)

// RevokeExposure ends the exposure id. The window that made it may, as may
// a call of the task that made it, a request without an agent from the
// same app, and the runtime.
func (inst *Service) RevokeExposure(ctx context.Context, id string, by Identity, obo *app.OnBehalfOf) (err error) {
	cc, err := inst.attest(by, obo)
	inst.exp.mu.Lock()
	e := inst.exp.byId[id]
	inst.exp.mu.Unlock()
	switch {
	case err != nil:
	case e == nil:
		err = eb.Build().Str("exposure", id).Errorf("revoke: %w", ErrUnknownExposure)
	case !mayRevoke(e, by, cc):
		err = eb.Build().Str("exposure", id).Errorf("revoke: only the window or the task that made the exposure, or the person, revokes it")
	}
	if err != nil {
		inst.auditExposure(ctx, ExposureAuditRecord{Action: ExposureAuditRevoke, Outcome: exposureOutcome(err), Reason: err.Error(),
			Exposure: id, By: by, Context: cc})
		return
	}
	inst.endExposureBy(ctx, e, ExposureAuditRevoke, "revoked", by, cc)
	return
}

// mayRevoke is the revoke rule.
func mayRevoke(e *exposure, by Identity, cc option.Option[app.CallContext]) (ok bool) {
	switch {
	case by.IsRuntime():
		return true
	case cc.Has:
		return e.task() != "" && e.task() == cc.Val.Task
	}
	return e.by == by || by.App == e.by.App
}

// Exposures lists the live exposures, each marked whether the caller made
// it; an agent's call is attested first. None carries its token.
func (inst *Service) Exposures(by Identity, obo *app.OnBehalfOf) (out []ExposureInfo, err error) {
	cc, err := inst.attest(by, obo)
	if err != nil {
		return
	}
	inst.sweepExposures(context.Background())
	inst.exp.mu.Lock()
	es := make([]*exposure, 0, len(inst.exp.byId))
	for _, e := range inst.exp.byId {
		es = append(es, e)
	}
	inst.exp.mu.Unlock()
	for _, e := range es {
		rec, _ := inst.liveFor(e)
		inst.exp.mu.Lock()
		if !e.ended {
			out = append(out, inst.infoLocked(e, rec, by, cc))
		}
		inst.exp.mu.Unlock()
	}
	sortExposures(out)
	return
}

// sortExposures orders exposures by alias, then by when they were made.
func sortExposures(es []ExposureInfo) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && exposureLess(es[j], es[j-1]); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}

func exposureLess(a, b ExposureInfo) bool {
	if a.Alias != b.Alias {
		return a.Alias < b.Alias
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

// sweepExposures ends every exposure that has lapsed: past its lifetime, or
// whose dataset it can no longer serve.
func (inst *Service) sweepExposures(ctx context.Context) {
	now := time.Now()
	inst.exp.mu.Lock()
	es := make([]*exposure, 0, len(inst.exp.byId))
	for _, e := range inst.exp.byId {
		es = append(es, e)
	}
	inst.exp.mu.Unlock()
	for _, e := range es {
		if !now.Before(e.expiresAt) {
			inst.endExposure(ctx, e, ExposureAuditLapse, "its lifetime ended")
			continue
		}
		if _, lapsed := inst.liveFor(e); lapsed != "" {
			inst.endExposure(ctx, e, ExposureAuditLapse, lapsed)
		}
	}
}

// hasExposureOfLocked reports whether an exposure names alias. The caller
// holds the service's lock, which is taken before exp.mu.
func (inst *Service) hasExposureOfLocked(alias string) (yes bool) {
	inst.exp.mu.Lock()
	defer inst.exp.mu.Unlock()
	for _, e := range inst.exp.byId {
		if e.alias == alias {
			return true
		}
	}
	return false
}

// endExposuresBy ends every exposure the closed window made.
func (inst *Service) endExposuresBy(ctx context.Context, who Identity) (ended int) {
	inst.exp.mu.Lock()
	var es []*exposure
	for _, e := range inst.exp.byId {
		if e.by == who {
			es = append(es, e)
		}
	}
	inst.exp.mu.Unlock()
	for _, e := range es {
		if inst.endExposure(ctx, e, ExposureAuditLapse, "the window that made it closed") {
			ended++
		}
	}
	return
}

// endExposure ends e once, for the runtime, and audits why; it reports
// whether this call ended it.
func (inst *Service) endExposure(ctx context.Context, e *exposure, action string, reason string) (ended bool) {
	return inst.endExposureBy(ctx, e, action, reason, Identity{}, option.None[app.CallContext]())
}

func (inst *Service) endExposureBy(ctx context.Context, e *exposure, action string, reason string, by Identity, cc option.Option[app.CallContext]) (ended bool) {
	inst.exp.mu.Lock()
	if e.ended {
		inst.exp.mu.Unlock()
		return false
	}
	e.ended = true
	delete(inst.exp.byId, e.id)
	delete(inst.exp.byDigest, e.digest)
	if e.timer != nil {
		e.timer.Stop()
	}
	reads, served := e.reads, e.served
	inst.exp.mu.Unlock()
	inst.auditExposure(ctx, ExposureAuditRecord{Action: action, Outcome: auditOk, Reason: reason, Exposure: e.id, Alias: e.alias,
		Bundle: e.bundle, Task: e.task(), By: by, Context: cc, Reads: reads, Bytes: served})
	return true
}

// endAllExposures ends every exposure as the service closes.
func (inst *Service) endAllExposures(ctx context.Context) {
	inst.exp.mu.Lock()
	es := make([]*exposure, 0, len(inst.exp.byId))
	for _, e := range inst.exp.byId {
		es = append(es, e)
	}
	inst.exp.mu.Unlock()
	for _, e := range es {
		inst.endExposure(ctx, e, ExposureAuditLapse, "the host stopped")
	}
}

// exposureOutcome is the trail outcome of a refused exposure request.
func exposureOutcome(err error) (outcome string) {
	var ge *GrantError
	if errors.As(err, &ge) || errors.Is(err, ErrUnattested) || errors.Is(err, ErrNotTheTasks) {
		return auditDenied
	}
	return auditFailed
}
