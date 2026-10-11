package llm

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The approval of an exit (ADR-0312 §SD3) as the model route verifies it.
// A request above the route's clearance on the person's path is refused
// with its call id; the service remembers what it refused — the digest of
// the request bytes it would have sent, the tier, the sink, who sent it
// and for which run — for ApprovalTTL. The caller journals an approval
// entry naming that call and resends with the entry's key,
// cell.ApprovalKey(run, ordinal). The service loads the entry through its
// checker and lets the request go only when the entry is an approval of
// that call for that sink on the person's path, written in the run the
// refused request named by the app that owns that run, and the resent
// bytes and tier are the ones it refused. One approval covers one exit:
// the refused record is claimed when the key verifies, released if the
// resend is refused further down for a reason the person can mend (the
// budget, the trail), and consumed when the bytes leave. A key the
// service cannot verify opens nothing: without a checker every approval
// is refused, and the log says so once.

// ApprovalCheckerI loads the journal entry an approval key names and
// the app that owns its run; the host wires one over the run journal's
// store.
type ApprovalCheckerI interface {
	ApprovalEntry(ctx context.Context, run string, ordinal uint32) (e agentrun.Entry, owner string, found bool, err error)
}

// StoreApprovals is a checker over the run journal's store.
func StoreApprovals(store agentrun.StoreI) ApprovalCheckerI { return storeApprovals{store: store} }

type storeApprovals struct{ store agentrun.StoreI }

func (inst storeApprovals) ApprovalEntry(ctx context.Context, run string, ordinal uint32) (e agentrun.Entry, owner string, found bool, err error) {
	r, haveRun, err := inst.store.GetRun(ctx, run)
	if err != nil {
		err = eb.Build().Str("run", run).Errorf("load the approval's run: %w", err)
		return
	}
	if !haveRun {
		return
	}
	owner = r.OwnerApp
	entries, err := inst.store.Entries(ctx, run, ordinal)
	if err != nil {
		err = eb.Build().Str("run", run).Uint32("ordinal", ordinal).Errorf("load the approval entry: %w", err)
		return
	}
	for _, c := range entries {
		if c.Ordinal == ordinal {
			return c, owner, true, nil
		}
	}
	return
}

// ApprovalTTL is how long a refused exit waits for its approval.
const ApprovalTTL = time.Hour

// approvalWait bounds the checker's load.
const approvalWait = 10 * time.Second

// maxPendingExits bounds the exits held for approval; past it the oldest
// is dropped, so a flood of refusals costs bounded memory and the person
// is asked again for an exit that waited longest.
const maxPendingExits = 1024

// pendingExit is one exit the route refused pending the person's approval.
type pendingExit struct {
	digest   string
	tier     agentrun.TierE
	sink     cell.Sink
	sender   app.AppIdT
	instance uint64
	run      string
	expires  time.Time
	// claimed says a verified resend holds this exit while it goes
	// through the checks below the wall; a second resend meanwhile is
	// refused.
	claimed bool
}

// pendingExits is the refused exits by call id, oldest first by callId
// order, under its own lock so the wall never holds the service's.
type pendingExits struct {
	mu    sync.Mutex
	byId  map[string]*pendingExit
	order []string
}

func newPendingExits() *pendingExits { return &pendingExits{byId: map[string]*pendingExit{}} }

// remember notes a refused exit under its call id, dropping what lapsed
// and the oldest past the bound.
func (inst *pendingExits) remember(callId string, p pendingExit, now time.Time) {
	p.expires = now.Add(ApprovalTTL)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.sweepLocked(now)
	if _, had := inst.byId[callId]; !had {
		inst.order = append(inst.order, callId)
	}
	inst.byId[callId] = &p
	for len(inst.order) > maxPendingExits {
		delete(inst.byId, inst.order[0])
		inst.order = inst.order[1:]
	}
}

// sweepLocked drops the lapsed exits from the front of the order, which
// is also expiry order since the TTL is one constant.
func (inst *pendingExits) sweepLocked(now time.Time) {
	for len(inst.order) > 0 {
		p, ok := inst.byId[inst.order[0]]
		if ok && !p.expires.Before(now) {
			return
		}
		delete(inst.byId, inst.order[0])
		inst.order = inst.order[1:]
	}
}

// claim takes the exit under callId for one resend when check passes on
// it, in one critical section: a concurrent resend finds it claimed. The
// error is check's, or says why nothing was claimed.
func (inst *pendingExits) claim(callId string, now time.Time, check func(p pendingExit) error) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p, ok := inst.byId[callId]
	if ok && p.expires.Before(now) {
		inst.forgetLocked(callId)
		ok = false
	}
	if !ok {
		return eb.Build().Str("call", callId).Str("ttl", ApprovalTTL.String()).Errorf("the approval names no call this route refused within the approval's time to live")
	}
	if p.claimed {
		return eb.Build().Str("call", callId).Errorf("the approval is in use by another resend of the same call")
	}
	if err = check(*p); err != nil {
		return
	}
	p.claimed = true
	return nil
}

// release gives a claimed exit back, for a resend refused below the wall.
func (inst *pendingExits) release(callId string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if p, ok := inst.byId[callId]; ok {
		p.claimed = false
	}
}

// consume forgets a claimed exit: its bytes left.
func (inst *pendingExits) consume(callId string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.forgetLocked(callId)
}

func (inst *pendingExits) forgetLocked(callId string) {
	if _, ok := inst.byId[callId]; !ok {
		return
	}
	delete(inst.byId, callId)
	for i, id := range inst.order {
		if id == callId {
			inst.order = append(inst.order[:i], inst.order[i+1:]...)
			break
		}
	}
}

// requestDigest is the identity of what would leave: the request body's
// content digest, over the bytes the route would send. The route encodes
// deterministically, so the same request gives the same digest.
func requestDigest(body []byte) string { return agentrun.ContentDigest(string(body)) }

// approvalClaim is what a verified approval holds until the exit is
// settled: the call it covers.
type approvalClaim struct {
	callId string
}

// verifyApproval says whether the approval key lets a resent request go:
// the entry it names is an approval, of a call this route refused for
// the person's path, for this sink, in the run the refused request named,
// written by the app that owns that run and sent the request, and the
// request and tier are the refused ones. On success the refused exit is
// claimed for this resend; the caller releases or consumes the claim.
func (inst *Service) verifyApproval(key string, body []byte, tier agentrun.TierE, sink cell.Sink, sender app.AppIdT, instance uint64, run string) (claim approvalClaim, err error) {
	if inst.cfg.Approvals == nil {
		inst.approvalsUnwired.Do(func() {
			inst.log.Error().Msg("llm: an approval key was offered and no journal store is wired to verify it; every approved exit is refused")
		})
		err = eh.Errorf("the approval cannot be verified: no run journal is wired to the model service")
		return
	}
	keyRun, ordinal, ok := cell.ParseApprovalKey(key)
	if !ok {
		err = eb.Build().Str("key", key).Errorf("the approval key is not <run>#<ordinal>")
		return
	}
	if keyRun != run {
		err = eb.Build().Str("approval", keyRun).Str("request", run).Errorf("the approval is of another run than the request names")
		return
	}
	ctx, cancel := context.WithTimeout(inst.base, approvalWait)
	defer cancel()
	e, owner, found, err := inst.cfg.Approvals.ApprovalEntry(ctx, keyRun, ordinal)
	if err != nil {
		err = eh.Errorf("the approval could not be loaded: %w", err)
		return
	}
	if !found || e.Kind != agentrun.KindApproval {
		err = eb.Build().Str("key", key).Errorf("the approval key names no approval entry")
		return
	}
	if owner != string(sender) {
		err = eb.Build().Str("owner", owner).Str("sender", string(sender)).Errorf("the approval is in a run another app owns")
		return
	}
	a, err := cell.ParseApproval(e.Body)
	if err != nil {
		err = eh.Errorf("the approval entry does not parse: %w", err)
		return
	}
	if path, parsed := cell.ParseApprovalTier(a.Path); !parsed || path != cell.ApprovalTierPerson {
		err = eb.Build().Str("path", a.Path).Errorf("the approval is not on the person's path")
		return
	}
	if e.Name != sink.Edge && e.Name != sink.Kind.String()+" "+sink.Edge {
		err = eb.Build().Str("sink", e.Name).Errorf("the approval names another sink")
		return
	}
	digest := requestDigest(body)
	err = inst.pending.claim(e.Key, time.Now(), func(p pendingExit) error {
		switch {
		case p.sender != sender || p.instance != instance:
			return eh.Errorf("the refused call was another app's")
		case p.run != run:
			return eh.Errorf("the refused call was for another run")
		case p.sink != sink:
			return eh.Errorf("the refused call left by another sink")
		case p.tier != tier:
			return eb.Build().Str("refused", p.tier.String()).Str("sent", tier.String()).Errorf("the request's tier is not the one refused")
		case p.digest != digest:
			return eh.Errorf("the request is not the one refused: its bytes changed since the approval")
		}
		return nil
	})
	if err != nil {
		return
	}
	claim = approvalClaim{callId: e.Key}
	return
}

// encodeRequest is the request as the route would send it: the bytes that
// leave, what the exit budget reserves, the canaries read and the
// approval binds to.
func (inst *Service) encodeRequest(r openaichat.CompletionRequest) (body []byte, err error) {
	body, err = inst.cfg.Route.Encode(r)
	if err != nil {
		err = eh.Errorf("encode the request: %w", err)
	}
	return
}

// leakSurface is the text the canaries read (ADR-0312 §SD6): the request
// body as encoded, and beside it every string the body carries decoded —
// message contents, tool names, descriptions and schemas, the arguments
// of earlier tool calls, the extra values — so a token or a protected
// body that JSON escaping would split (a newline, a quote) is still
// found as the words it is.
func leakSurface(body []byte, r openaichat.CompletionRequest) string {
	var b strings.Builder
	b.Write(body)
	b.WriteByte('\n')
	for _, m := range r.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
		for _, c := range m.ToolCalls {
			b.WriteString(c.Name)
			b.WriteByte('\n')
			b.WriteString(c.Arguments)
			b.WriteByte('\n')
		}
	}
	for _, t := range r.Tools {
		b.WriteString(t.Name)
		b.WriteByte('\n')
		b.WriteString(t.Description)
		b.WriteByte('\n')
		b.Write(t.Parameters)
		b.WriteByte('\n')
	}
	for _, v := range r.Extra {
		writeAny(&b, v)
	}
	return b.String()
}

// writeAny writes the strings inside a decoded JSON value, one per line.
func writeAny(b *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		b.WriteString(x)
		b.WriteByte('\n')
	case []any:
		for _, e := range x {
			writeAny(b, e)
		}
	case map[string]any:
		for _, e := range x {
			writeAny(b, e)
		}
	}
}
