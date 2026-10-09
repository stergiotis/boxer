package llm

import (
	"context"
	"iter"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// WindowClassI says how urgent a window's calls are from the state of the
// window (ADR-0300 §SD6): focused, shown, or behind. The host's window
// host answers it; instance 0 never reaches it.
type WindowClassI interface {
	WindowClass(instance uint64) ration.ClassE
}

type windowsRef struct{ w WindowClassI }

// SetWindows installs the window state the queue orders by. Until it is
// set, every window's calls are ClassShown.
func (inst *Service) SetWindows(w WindowClassI) { inst.windows.Store(&windowsRef{w: w}) }

// Question is what a moderator puts to the person (ADR-0300 §SD9).
type Question struct {
	Moderator app.AppIdT
	// Text is the moderator's question; Rule describes the rule a yes sets.
	Text string
	Rule string
}

// AskerI puts a moderator's question to the person and waits for the
// answer until ctx ends. decided false is no answer — the question expired
// or no one was there to see it.
type AskerI interface {
	AskPerson(ctx context.Context, q Question) (granted bool, decided bool, err error)
}

type askerRef struct{ a AskerI }

// SetAsker installs the dialog llm.ration.ask uses. Until it is set, an ask
// is answered with a refusal.
func (inst *Service) SetAsker(a AskerI) { inst.asker.Store(&askerRef{a: a}) }

// Ledger is the service's usage ledger, for the introspection tables.
func (inst *Service) Ledger() (l *ration.Ledger) { return inst.ledger }

// AskTimeout bounds how long llm.ration.ask waits for the person.
const AskTimeout = 10 * time.Minute

// rebuildTimeout bounds the ledger's rebuild from the trail at start.
const rebuildTimeout = 15 * time.Second

// activeCall is a call between admission and its reply: what
// llm.ration.cancel can reach.
type activeCall struct {
	chain  ration.Chain
	cancel context.CancelFunc
}

// classOf is the queue class of a sender's calls.
func (inst *Service) classOf(instance uint64) (c ration.ClassE) {
	if instance == 0 {
		return ration.ClassNoWindow
	}
	if ref := inst.windows.Load(); ref != nil {
		return ref.w.WindowClass(instance)
	}
	return ration.ClassShown
}

// estimateInput is the input tokens a request is expected to use: about
// four bytes of text a token, and a flat allowance per image.
func estimateInput(ms []openaichat.Message) (n int64) {
	var text int64
	for _, m := range ms {
		text += int64(len(m.Content))
		n += int64(len(m.Images)) * imageTokenEstimate
	}
	n += text / 4
	return
}

// imageTokenEstimate is what an attached image is reserved as; providers
// count images in ways a byte size does not predict.
const imageTokenEstimate = 1000

// usageOf is what a finished call used, as the provider reported it.
func usageOf(resp openaichat.CompletionResponse, elapsed time.Duration) (u ration.Usage) {
	u = ration.Usage{
		ration.QuantityCalls: 1, ration.QuantityInputTokens: int64(resp.InputTokens), ration.QuantityOutputTokens: int64(resp.OutputTokens),
		ration.QuantityWallMs: elapsed.Milliseconds(),
	}
	if resp.CachedInputTokens.Has {
		u[ration.QuantityCachedInputTokens] = int64(resp.CachedInputTokens.Val)
	}
	if resp.ReasoningTokens.Has {
		u[ration.QuantityReasoningTokens] = int64(resp.ReasoningTokens.Val)
	}
	return u.WithTotal()
}

// usageOfRecord is usageOf for a row read back from the trail.
func usageOfRecord(rec CallRecord) (u ration.Usage) {
	u = ration.Usage{
		ration.QuantityCalls: 1, ration.QuantityInputTokens: int64(rec.InputTokens), ration.QuantityOutputTokens: int64(rec.OutputTokens),
		ration.QuantityWallMs: rec.Elapsed.Milliseconds(),
	}
	if rec.CachedInputTokens.Has {
		u[ration.QuantityCachedInputTokens] = int64(rec.CachedInputTokens.Val)
	}
	if rec.ReasoningTokens.Has {
		u[ration.QuantityReasoningTokens] = int64(rec.ReasoningTokens.Val)
	}
	return
}

func (inst *Service) track(callId string, a activeCall) {
	inst.mu.Lock()
	inst.active[callId] = a
	inst.mu.Unlock()
}

func (inst *Service) untrack(callId string) {
	inst.mu.Lock()
	delete(inst.active, callId)
	inst.mu.Unlock()
}

// publish sends an event; a failure is logged, never the caller's.
func (inst *Service) publish(subject string, v any) {
	payload, err := encode(v)
	if err != nil {
		inst.log.Error().Err(err).Str("subject", subject).Msg("llm: encode event")
		return
	}
	if err = inst.busClient.Publish(subject, payload); err != nil {
		inst.log.Debug().Err(err).Str("subject", subject).Msg("llm: publish event")
	}
}

// publishCall is llm.event.call for a call admission decided.
func (inst *Service) publishCall(rec CallRecord, chain ration.Chain, class ration.ClassE, d ration.Decision, u ration.Usage) {
	inst.publish(SubjectEventCall, wireCallEvent{V: wireVersion, CallId: rec.CallId, At: unixNs(rec.At), App: chain.App,
		Instance: chain.Instance, Task: chain.Task, Purpose: chain.Purpose, Class: class.String(),
		Admission: d.Outcome.String(), Rule: d.Rule, Refusal: refusalName(d.Refusal), Reason: d.Reason, QueuedNs: int64(d.Queued),
		Usage: wireOfUsage(u), Failed: rec.Error != "" && !rec.Refused})
}

func refusalName(r ration.RefusalE) (s string) {
	if r != ration.RefusalNone {
		s = r.String()
	}
	return
}

func (inst *Service) publishThresholds(events []ration.Event) {
	for _, e := range events {
		inst.publish(SubjectEventThreshold, wireThresholdEvent{V: wireVersion, Rule: e.Rule, AccountKind: e.Account.Kind.String(),
			AccountKey: e.Account.Key, Quantity: string(e.Quantity), Used: e.Used, Limit: e.Limit, Percent: e.Percent})
	}
}

// rebuildLedger charges the ledger with the calls on the trail inside the
// longest window a rule can name (ADR-0300 §SD3). Run and instance
// accounts are charged only from this run's rows: a window key means
// another window in another run.
func (inst *Service) rebuildLedger() (rows int, err error) {
	if !inst.cfg.Trail.Durable() {
		return
	}
	ctx, cancel := context.WithTimeout(inst.base, rebuildTimeout)
	defer cancel()
	since := time.Now().Add(-ration.HourHorizon)
	opts := recordstore.ScanOpts{ExtraPredicate: trail.TrailColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")"}
	ents, err := inst.cfg.Trail.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] { return st.ScanLlmCall(ctx, opts) })
	if err != nil {
		return
	}
	run := inst.cfg.Trail.OriginOf("", 0).Run
	for _, ent := range ents {
		rec := RecordOf(ent)
		if rec.Refused {
			continue
		}
		chain := ration.Chain{App: string(rec.Sender), Instance: rec.SenderInstance, Task: rec.Task, Purpose: rec.Purpose}
		accts := chain.Accounts()
		if !ent.Origin.Has || ent.Origin.Val.Run != run {
			accts = slices.DeleteFunc(accts, func(a ration.Account) bool {
				return a.Kind == ration.AccountKindRun || a.Kind == ration.AccountKindInstance
			})
		}
		inst.ledger.RecordPast(ent.Ts, accts, usageOfRecord(rec))
		rows++
	}
	return
}

// isModerator says the app is listed in BOXER_LLM_MODERATORS.
func (inst *Service) isModerator(id app.AppIdT) (yes bool) {
	return slices.Contains(inst.cfg.Moderators, string(id))
}

// handleRation answers the moderator's verbs (ADR-0300 §SD4, §SD8, §SD9).
func (inst *Service) handleRation(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	if !inst.isModerator(msg.Sender) {
		inst.reply(msg.Reply, wireRationReply{V: wireVersion, Reason: string(msg.Sender) + " is not listed in BOXER_LLM_MODERATORS"})
		return
	}
	switch msg.Subject {
	case SubjectRationSet:
		inst.reply(msg.Reply, inst.rationSet(msg))
	case SubjectRationList:
		inst.reply(msg.Reply, wireRationReply{V: wireVersion, Ok: true, States: wireOfStates(inst.ledger.States())})
	case SubjectRationCancel:
		inst.reply(msg.Reply, inst.rationCancel(msg))
	case SubjectRationAsk:
		inst.startAsk(msg)
	default:
		inst.reply(msg.Reply, wireRationReply{V: wireVersion, Reason: "unknown verb " + strings.TrimPrefix(msg.Subject, SubjectPrefix)})
	}
}

func (inst *Service) rationSet(msg *app.Msg) (rep wireRationReply) {
	rep.V = wireVersion
	req, err := decode[wireRationSet](msg.Payload)
	if err != nil {
		rep.Reason = "malformed request: " + err.Error()
		return
	}
	if req.Remove {
		old, ok := inst.ledger.Rule(req.Rule.Id)
		if !ok {
			rep.Reason = "no rule " + strconv.Quote(req.Rule.Id)
			return
		}
		if inst.affectsSelf(msg, old.Select) {
			rep.Reason = "removing " + strconv.Quote(old.Id) + " loosens a rule on the moderator's own calls; ask the person (llm.ration.ask)"
			inst.auditRule(msg, "rule-remove", old, trail.OutcomeDenied, rep.Reason)
			return
		}
		inst.ledger.Remove(old.Id)
		inst.auditRule(msg, "rule-remove", old, trail.OutcomeOk, "")
		rep.Ok = true
		return
	}
	r, err := ruleOfWire(req.Rule)
	if err == nil {
		r.Author, r.SetAt = string(msg.Sender), time.Now().UTC()
		err = r.Validate()
	}
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if old, ok := inst.ledger.Rule(r.Id); ok && inst.affectsSelf(msg, old.Select) && loosens(old, r) {
		rep.Reason = "the change to " + strconv.Quote(r.Id) + " loosens a rule on the moderator's own calls; ask the person (llm.ration.ask)"
		inst.auditRule(msg, "rule-set", r, trail.OutcomeDenied, rep.Reason)
		return
	}
	if err = inst.ledger.Set(r); err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.auditRule(msg, "rule-set", r, trail.OutcomeOk, "")
	rep.Ok = true
	return
}

// affectsSelf says a rule on sel could hold the sender's own calls: a
// moderator may tighten such a rule, but loosening it is the person's
// (ADR-0300 §SD7).
func (inst *Service) affectsSelf(msg *app.Msg, sel ration.Selector) (yes bool) {
	self := string(msg.Sender)
	switch sel.Kind {
	case ration.AccountKindRun:
		return true
	case ration.AccountKindApp:
		return sel.Key == "" || sel.Key == self
	case ration.AccountKindInstance:
		return sel.Key == "" || sel.Key == strconv.FormatUint(msg.SenderInstance, 10)
	case ration.AccountKindPurpose:
		return sel.Key == "" || strings.HasPrefix(sel.Key, self+"/")
	default:
		return false
	}
}

// loosens says replacing old by r could admit a call old refused.
func loosens(old ration.Rule, r ration.Rule) (yes bool) {
	if old.Kind != r.Kind || old.Select != r.Select || old.Quantity != r.Quantity || old.Aligned != r.Aligned {
		return true
	}
	if r.Limit > old.Limit || r.Raise > 0 && r.Raise > old.Raise {
		return true
	}
	return r.Kind == ration.RuleKindBudget && r.Window < old.Window || r.Kind == ration.RuleKindRate && r.Window != old.Window
}

func (inst *Service) rationCancel(msg *app.Msg) (rep wireRationReply) {
	rep.V = wireVersion
	req, err := decode[wireRationCancel](msg.Payload)
	if err != nil {
		rep.Reason = "malformed request: " + err.Error()
		return
	}
	var target option.Option[ration.Account]
	if req.AccountKind != "" {
		k, perr := ration.ParseAccountKind(req.AccountKind)
		if perr != nil {
			rep.Reason = perr.Error()
			return
		}
		target = option.Some(ration.Account{Kind: k, Key: req.AccountKey})
	}
	if req.CallId == "" && !target.Has {
		rep.Reason = "name a call id or an account"
		return
	}
	var cancels []context.CancelFunc
	inst.mu.Lock()
	for id, a := range inst.active {
		if id == req.CallId || target.Has && slices.Contains(a.chain.Accounts(), target.Val) {
			cancels = append(cancels, a.cancel)
		}
	}
	inst.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	rep.Ok, rep.Cancelled = true, len(cancels)
	attrs := []string{"cancelled", strconv.Itoa(len(cancels))}
	if req.CallId != "" {
		attrs = append(attrs, "call-id", req.CallId)
	}
	if target.Has {
		attrs = append(attrs, "account", target.Val.String())
	}
	if req.Reason != "" {
		attrs = append(attrs, "reason", req.Reason)
	}
	inst.audit(msg, "call-cancel", trail.OutcomeOk, nil, attrs)
	return
}

// startAsk waits for the person off the requester's goroutine, as
// startComplete does.
func (inst *Service) startAsk(msg *app.Msg) {
	m := *msg
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.inflight.Add(1)
	inst.mu.Unlock()
	go func() {
		defer inst.inflight.Done()
		inst.reply(m.Reply, inst.rationAsk(&m))
	}()
}

func (inst *Service) rationAsk(msg *app.Msg) (rep wireRationReply) {
	rep.V = wireVersion
	req, err := decode[wireRationAsk](msg.Payload)
	if err != nil {
		rep.Reason = "malformed request: " + err.Error()
		return
	}
	r, err := ruleOfWire(req.Rule)
	if err == nil {
		r.Author = "person"
		err = r.Validate()
	}
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	ref := inst.asker.Load()
	if ref == nil {
		rep.Reason = "this host has no dialog to put the question to the person"
		return
	}
	ctx, cancel := context.WithTimeout(inst.base, AskTimeout)
	defer cancel()
	granted, decided, err := ref.a.AskPerson(ctx, Question{Moderator: msg.Sender, Text: req.Question, Rule: r.Describe()})
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok, rep.Decided, rep.Granted = true, decided, granted
	outcome := trail.OutcomeDenied
	if granted {
		r.SetAt = time.Now().UTC()
		if err = inst.ledger.Set(r); err != nil {
			rep.Ok, rep.Reason = false, err.Error()
			return
		}
		outcome = trail.OutcomeOk
	}
	decidedBy := "nobody"
	if decided {
		decidedBy = "person"
	}
	inst.auditRule(msg, "rule-ask", r, outcome, "decided by "+decidedBy)
	return
}

// The audit vocabulary of rule changes (ADR-0300 §SD4, ADR-0296).
const (
	auditDomain    = "llm-ration"
	auditRefRule   = "llm-ration-rule"
	auditRetention = trail.RetentionTrail
)

// auditRule records a change to a rule: who, what, and the rule in full.
func (inst *Service) auditRule(msg *app.Msg, action string, r ration.Rule, outcome string, why string) {
	attrs := []string{"select", r.Select.String(), "kind", r.Kind.String()}
	if r.Quantity != "" {
		attrs = append(attrs, "quantity", string(r.Quantity))
	}
	attrs = append(attrs, "limit", strconv.FormatInt(r.Limit, 10))
	if r.Window > 0 {
		attrs = append(attrs, "window", r.Window.String())
	}
	if r.Aligned {
		attrs = append(attrs, "aligned", "true")
	}
	if r.Raise > 0 {
		attrs = append(attrs, "raise", strconv.FormatInt(r.Raise, 10), "raise-until", r.RaiseUntil.UTC().Format(time.RFC3339))
	}
	if r.Author != "" {
		attrs = append(attrs, "author", r.Author)
	}
	if r.Reason != "" {
		attrs = append(attrs, "reason", r.Reason)
	}
	if why != "" {
		attrs = append(attrs, "why", why)
	}
	inst.audit(msg, action, outcome, []string{auditRefRule, r.Id}, attrs)
}

// audit writes one llm-ration event; ref and attrs are key, value pairs.
func (inst *Service) audit(msg *app.Msg, action string, outcome string, ref []string, attrs []string) {
	row := trail.AuditEvent{Domain: auditDomain, Action: action, Outcome: outcome, Retention: auditRetention}
	for i := 0; i+1 < len(ref); i += 2 {
		row.RefTypes, row.RefValues = append(row.RefTypes, ref[i]), append(row.RefValues, bound(ref[i+1]))
	}
	for i := 0; i+1 < len(attrs) && len(row.AttrKeys) < trail.MaxAttrs; i += 2 {
		if attrs[i+1] != "" {
			row.AttrKeys, row.AttrValues = append(row.AttrKeys, attrs[i]), append(row.AttrValues, bound(attrs[i+1]))
		}
	}
	c := trail.Context{Origin: inst.cfg.Trail.OriginOf(msg.Sender, msg.SenderInstance)}
	if err := inst.cfg.Trail.Event(inst.base, time.Now().UTC(), c, row); err != nil {
		inst.log.Warn().Err(err).Str("action", action).Msg("llm: write the rule's audit event")
	}
	if err := inst.cfg.Trail.Flush(inst.base); err != nil {
		inst.log.Warn().Err(err).Str("action", action).Msg("llm: flush the rule's audit event")
	}
}

// bound cuts s to what an audit value may hold.
func bound(s string) (b string) {
	if r := []rune(s); len(r) > trail.MaxValueRunes {
		return string(r[:trail.MaxValueRunes-1]) + "…"
	}
	return s
}
