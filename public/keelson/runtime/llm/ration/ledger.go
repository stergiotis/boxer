package ration

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"
)

// DefaultAgingStep is how long a waiting call takes to move up one class
// (ADR-0300 §SD6): a background loop is delayed behind what the person is
// looking at, never starved by it.
const DefaultAgingStep = 5 * time.Second

// DefaultMinClamp is the smallest output ceiling a clamp hands out; below
// it the call is refused, since an answer that short is rarely of use.
const DefaultMinClamp = 64

// Ledger keeps what each account used, reserves what admitted calls are
// expected to use, and checks calls against the rules (ADR-0300 §SD3–§SD6).
// Safe for concurrent use.
type Ledger struct {
	mu        sync.Mutex
	now       func() time.Time
	rules     map[string]Rule
	order     []string
	accounts  map[Account]*account
	waiters   []*waiter
	agingStep time.Duration
	minClamp  int64
	// refuseAll, when set, refuses every call with it as the reason: the
	// ledger could not be rebuilt and the deployment chose to refuse.
	refuseAll string
	// prunedAt is when idle accounts were last dropped.
	prunedAt time.Time
}

// PruneEvery is how often Settle drops the accounts nothing can count any
// more: idle past HourHorizon, with nothing reserved, in flight or waiting.
const PruneEvery = 10 * time.Minute

type account struct {
	series   map[QuantityE]*series
	reserved Usage
	totals   Usage
	inflight int64
	refused  int64
	queued   int64
	lastAt   time.Time
}

// Ticket is an admitted call's reservation, to be settled exactly once.
type Ticket struct {
	accounts []Account
	reserved Usage
	done     bool
}

type grant struct {
	d Decision
	t *Ticket
}

type waiter struct {
	req   Request
	since time.Time
	ch    chan grant
}

// Request is a call as admission sees it.
type Request struct {
	Chain Chain
	Class ClassE
	// EstimatedInput is the input tokens expected from the prompt's size.
	EstimatedInput int64
	// MaxOutput is the call's output ceiling; what a clamp lowers.
	MaxOutput int64
}

// Option configures a Ledger.
type Option func(*Ledger)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option { return func(l *Ledger) { l.now = now } }

// WithAgingStep replaces DefaultAgingStep.
func WithAgingStep(d time.Duration) Option { return func(l *Ledger) { l.agingStep = d } }

// NewLedger is an empty ledger with no rules: every call is admitted.
func NewLedger(opts ...Option) (inst *Ledger) {
	inst = &Ledger{now: time.Now, rules: map[string]Rule{}, accounts: map[Account]*account{},
		agingStep: DefaultAgingStep, minClamp: DefaultMinClamp}
	for _, o := range opts {
		o(inst)
	}
	return
}

// SetRefuseAll makes the ledger refuse every call, with reason; "" lifts it.
func (inst *Ledger) SetRefuseAll(reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.refuseAll = reason
	inst.pump(inst.now())
}

// Set adds a rule, or replaces the one with its id.
func (inst *Ledger) Set(r Rule) (err error) {
	if err = r.Validate(); err != nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if r.SetAt.IsZero() {
		r.SetAt = inst.now().UTC()
	}
	inst.rules[r.Id] = r
	inst.order = sortedRuleIds(inst.rules)
	inst.pump(inst.now())
	return
}

// Remove drops the rule with id; it says whether there was one.
func (inst *Ledger) Remove(id string) (removed bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, removed = inst.rules[id]; removed {
		delete(inst.rules, id)
		inst.order = sortedRuleIds(inst.rules)
		inst.pump(inst.now())
	}
	return
}

// Rule is the rule with id.
func (inst *Ledger) Rule(id string) (r Rule, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	r, ok = inst.rules[id]
	return
}

// Rules lists the rules by id.
func (inst *Ledger) Rules() (rules []Rule) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, id := range inst.order {
		rules = append(rules, inst.rules[id])
	}
	return
}

// RecordPast charges usage that happened before the ledger existed — a
// rebuild from the trail (ADR-0300 §SD3). Nothing is reserved or admitted.
func (inst *Ledger) RecordPast(at time.Time, accts []Account, u Usage) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	u = u.WithTotal()
	for _, a := range accts {
		acc := inst.account(a)
		inst.charge(acc, at, u)
	}
}

// Admit checks a call against the rules. An admitted call gets a ticket the
// caller must Settle; a refused one gets none. A call that only a
// concurrency rule holds waits in the queue until a slot frees, a rule
// changes, or ctx ends — its deadline is the queue's bound.
func (inst *Ledger) Admit(ctx context.Context, r Request) (d Decision, t *Ticket) {
	inst.mu.Lock()
	now := inst.now()
	d, need, blocked := inst.check(r, now)
	if d.Outcome == OutcomeRefused {
		inst.countRefused(r.Chain)
		inst.mu.Unlock()
		return
	}
	if !blocked {
		t = inst.reserve(r.Chain, need, now)
		d.Remaining = inst.remaining(r.Chain, now)
		inst.mu.Unlock()
		return
	}
	if ctx.Err() != nil {
		inst.countRefused(r.Chain)
		inst.mu.Unlock()
		return refusedEnded(ctx, d.Rule, inst.rules[d.Rule]), nil
	}
	w := &waiter{req: r, since: now, ch: make(chan grant, 1)}
	inst.waiters = append(inst.waiters, w)
	for _, a := range r.Chain.Accounts() {
		inst.account(a).queued++
	}
	blockedBy := d.Rule
	inst.mu.Unlock()
	select {
	case g := <-w.ch:
		return g.d, g.t
	case <-ctx.Done():
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if i := slices.Index(inst.waiters, w); i >= 0 {
		inst.waiters = slices.Delete(inst.waiters, i, i+1)
		inst.countRefused(r.Chain)
		return refusedEnded(ctx, blockedBy, inst.rules[blockedBy]), nil
	}
	// pump decided the waiter as ctx ended. A refusal stands as pump made
	// it, and is already counted; a grant's slot goes back.
	g := <-w.ch
	if g.t == nil {
		return g.d, nil
	}
	inst.settle(g.t, nil, inst.now())
	inst.pump(inst.now())
	inst.countRefused(r.Chain)
	return refusedEnded(ctx, blockedBy, inst.rules[blockedBy]), nil
}

// refusedEnded is the refusal of a call whose ctx ended while a
// concurrency rule held it: at its deadline the caller may try again
// later; cancelled — by a moderator, or by the caller — it should not, and
// the reason carries the cancel's cause.
func refusedEnded(ctx context.Context, id string, r Rule) (d Decision) {
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		return Decision{Outcome: OutcomeRefused, Rule: id, Refusal: RefusalWait,
			Reason: "waited for a slot until the call's deadline under the " + r.Describe()}
	}
	reason := "cancelled while waiting for a slot under the " + r.Describe()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		reason = cause.Error() + " (" + reason + ")"
	}
	return Decision{Outcome: OutcomeRefused, Rule: id, Refusal: RefusalStop, Reason: reason}
}

// Settle charges an admitted call with what it used and releases its
// reservation; a call that failed before reaching the provider settles
// with nil. Settling twice is a no-op. The events are the soft thresholds
// the call crossed.
func (inst *Ledger) Settle(t *Ticket, actual Usage) (events []Event) {
	if t == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	now := inst.now()
	events = inst.settle(t, actual, now)
	inst.pump(now)
	if now.Sub(inst.prunedAt) >= PruneEvery {
		inst.prune(now)
	}
	return
}

// prune drops the accounts no window can reach and no call holds. A rule
// naming such an account by key still lists it, at zero. The caller holds
// the mutex.
func (inst *Ledger) prune(now time.Time) {
	inst.prunedAt = now
	waiting := map[Account]bool{}
	for _, w := range inst.waiters {
		for _, a := range w.req.Chain.Accounts() {
			waiting[a] = true
		}
	}
	horizon := now.Add(-HourHorizon - time.Hour)
	for a, acc := range inst.accounts {
		if acc.inflight != 0 || waiting[a] || acc.lastAt.After(horizon) {
			continue
		}
		held := false
		for _, v := range acc.reserved {
			if v != 0 {
				held = true
				break
			}
		}
		if !held {
			delete(inst.accounts, a)
		}
	}
}

func (inst *Ledger) settle(t *Ticket, actual Usage, now time.Time) (events []Event) {
	if t.done {
		return
	}
	t.done = true
	for _, a := range t.accounts {
		acc := inst.account(a)
		for q, v := range t.reserved {
			acc.reserved[q] -= v
		}
		acc.inflight--
	}
	actual = actual.WithTotal()
	type mark struct {
		rule Rule
		a    Account
		used int64
	}
	var marks []mark
	for _, id := range inst.order {
		r := inst.rules[id]
		if len(r.SoftPercent) == 0 || (r.Kind != RuleKindBudget && r.Kind != RuleKindRate) {
			continue
		}
		if _, charged := actual[r.Quantity]; !charged {
			continue
		}
		for _, a := range t.accounts {
			if r.Select.matches(a) {
				marks = append(marks, mark{rule: r, a: a, used: inst.used(r, a, now)})
			}
		}
	}
	for _, a := range t.accounts {
		inst.charge(inst.account(a), now, actual)
	}
	for _, m := range marks {
		after := inst.used(m.rule, m.a, now)
		limit := m.rule.limitAt(now)
		for _, p := range m.rule.SoftPercent {
			at := limit * int64(p) / 100
			if m.used < at && after >= at {
				events = append(events, Event{Kind: EventKindThreshold, Rule: m.rule.Id, Account: m.a,
					Quantity: m.rule.Quantity, Used: after, Limit: limit, Percent: p})
			}
		}
	}
	return
}

func (inst *Ledger) charge(acc *account, at time.Time, u Usage) {
	for q, v := range u {
		s := acc.series[q]
		if s == nil {
			s = &series{}
			acc.series[q] = s
		}
		s.add(at, v)
		acc.totals[q] += v
	}
	if len(u) > 0 && at.After(acc.lastAt) {
		acc.lastAt = at
	}
}

func (inst *Ledger) account(a Account) (acc *account) {
	acc = inst.accounts[a]
	if acc == nil {
		acc = &account{series: map[QuantityE]*series{}, reserved: Usage{}, totals: Usage{}}
		inst.accounts[a] = acc
	}
	return
}

func (inst *Ledger) countRefused(c Chain) {
	for _, a := range c.Accounts() {
		inst.account(a).refused++
	}
}

// used is what the rule counts on a: the window's charges plus what calls
// in flight have reserved.
func (inst *Ledger) used(r Rule, a Account, now time.Time) (used int64) {
	acc := inst.accounts[a]
	if acc == nil {
		return
	}
	if s := acc.series[r.Quantity]; s != nil {
		used = s.sum(r.windowStart(now), now)
	}
	used += acc.reserved[r.Quantity]
	return
}

// need is what a call is expected to use with output ceiling maxOut.
func need(r Request, maxOut int64) (u Usage) {
	return Usage{
		QuantityCalls: 1, QuantityInputTokens: r.EstimatedInput, QuantityOutputTokens: maxOut,
		QuantityTotalTokens: r.EstimatedInput + maxOut,
	}
}

// check decides a call against the rules without changing anything.
// blocked says only a concurrency rule holds it; d.Rule then names that
// rule.
func (inst *Ledger) check(r Request, now time.Time) (d Decision, u Usage, blocked bool) {
	accts := r.Chain.Accounts()
	if inst.refuseAll != "" {
		d = Decision{Outcome: OutcomeRefused, Refusal: RefusalStop, Reason: inst.refuseAll}
		return
	}
	maxOut := r.MaxOutput
	clampRule := ""
	for _, id := range inst.order {
		rule := inst.rules[id]
		for _, a := range accts {
			if !rule.Select.matches(a) {
				continue
			}
			switch rule.Kind {
			case RuleKindDeny:
				d = Decision{Outcome: OutcomeRefused, Rule: id, Refusal: RefusalStop, Reason: "refused by the " + rule.Describe()}
				return
			case RuleKindBudget, RuleKindRate:
				if r.MaxOutput <= 0 || (rule.Quantity != QuantityOutputTokens && rule.Quantity != QuantityTotalTokens) {
					continue
				}
				room := rule.limitAt(now) - inst.used(rule, a, now)
				if rule.Quantity == QuantityTotalTokens {
					room -= r.EstimatedInput
				}
				if room < maxOut {
					maxOut, clampRule = room, id
				}
			}
		}
	}
	if clampRule != "" && maxOut < inst.minClamp {
		rule := inst.rules[clampRule]
		d = refusal(rule, "leaves the call fewer than "+strconv.FormatInt(inst.minClamp, 10)+" output tokens")
		if rule.Kind == RuleKindRate {
			d.RetryAfter = inst.retryAfter(rule, accts, now, inst.minClamp-maxOut)
		}
		return
	}
	u = need(r, maxOut)
	for _, id := range inst.order {
		rule := inst.rules[id]
		if rule.Kind != RuleKindBudget && rule.Kind != RuleKindRate {
			continue
		}
		// A quantity known only after the call — wall time, the token parts,
		// any the service charges but cannot predict — has nothing to
		// reserve: the call is refused once the window is spent, and the
		// call that spends it overshoots by its own use.
		n := u[rule.Quantity]
		for _, a := range accts {
			if !rule.Select.matches(a) {
				continue
			}
			over := inst.used(rule, a, now) + n - rule.limitAt(now)
			if n == 0 {
				over++
			}
			if over > 0 {
				d = refusal(rule, "is spent on "+a.String())
				if rule.Kind == RuleKindRate || !rule.Aligned {
					d.RetryAfter = inst.retryAfter(rule, []Account{a}, now, over)
				} else {
					d.RetryAfter = rule.windowStart(now).Add(rule.Window).Sub(now)
				}
				return
			}
		}
	}
	d = Decision{Outcome: OutcomeAdmitted, MaxOutput: maxOut}
	if clampRule != "" {
		d.Outcome, d.Rule = OutcomeClamped, clampRule
	}
	for _, id := range inst.order {
		rule := inst.rules[id]
		if rule.Kind != RuleKindConcurrency {
			continue
		}
		for _, a := range accts {
			if rule.Select.matches(a) && inst.inflight(a)+1 > rule.limitAt(now) {
				d.Rule, blocked = id, true
				return
			}
		}
	}
	return
}

func refusal(rule Rule, what string) (d Decision) {
	d = Decision{Outcome: OutcomeRefused, Rule: rule.Id, Refusal: RefusalStop, Reason: "the " + rule.Describe() + " " + what}
	if rule.Kind == RuleKindRate {
		d.Refusal = RefusalWait
	}
	return
}

func (inst *Ledger) inflight(a Account) (n int64) {
	if acc := inst.accounts[a]; acc != nil {
		n = acc.inflight
	}
	return
}

// retryAfter is when the rule's rolling window on accts has freed excess.
func (inst *Ledger) retryAfter(rule Rule, accts []Account, now time.Time, excess int64) (d time.Duration) {
	if rule.Aligned {
		return rule.windowStart(now).Add(rule.Window).Sub(now)
	}
	for _, a := range accts {
		if !rule.Select.matches(a) {
			continue
		}
		if acc := inst.accounts[a]; acc != nil {
			if s := acc.series[rule.Quantity]; s != nil {
				d = max(d, s.freedBy(rule.Window, now, excess))
			}
		}
	}
	return
}

func (inst *Ledger) reserve(c Chain, u Usage, now time.Time) (t *Ticket) {
	t = &Ticket{accounts: c.Accounts(), reserved: u}
	for _, a := range t.accounts {
		acc := inst.account(a)
		for q, v := range u {
			acc.reserved[q] += v
		}
		acc.inflight++
		if now.After(acc.lastAt) {
			acc.lastAt = now
		}
	}
	return
}

// remaining is what each budget and rate rule leaves the chain's accounts.
func (inst *Ledger) remaining(c Chain, now time.Time) (rs []Remaining) {
	for _, id := range inst.order {
		rule := inst.rules[id]
		if rule.Kind != RuleKindBudget && rule.Kind != RuleKindRate {
			continue
		}
		for _, a := range c.Accounts() {
			if !rule.Select.matches(a) {
				continue
			}
			limit := rule.limitAt(now)
			rem := Remaining{Rule: id, Account: a, Quantity: rule.Quantity, Limit: limit, Remaining: max(limit-inst.used(rule, a, now), 0)}
			if rule.Aligned {
				rem.ResetAt = rule.windowStart(now).Add(rule.Window)
			}
			rs = append(rs, rem)
		}
	}
	return
}

// rank is a waiter's place: its class, less one per aging step waited.
func (inst *Ledger) rank(w *waiter, now time.Time) (r int) {
	r = int(w.req.Class)
	if inst.agingStep > 0 {
		r -= int(now.Sub(w.since) / inst.agingStep)
	}
	return max(r, 0)
}

// pump hands freed slots to the waiters, best rank first and first come
// within a rank, and refuses those a changed rule now refuses. The caller
// holds the mutex.
func (inst *Ledger) pump(now time.Time) {
	if len(inst.waiters) == 0 {
		return
	}
	slices.SortStableFunc(inst.waiters, func(a, b *waiter) int {
		if c := cmp.Compare(inst.rank(a, now), inst.rank(b, now)); c != 0 {
			return c
		}
		return a.since.Compare(b.since)
	})
	kept := inst.waiters[:0]
	for _, w := range inst.waiters {
		d, u, blocked := inst.check(w.req, now)
		switch {
		case d.Outcome == OutcomeRefused:
			inst.countRefused(w.req.Chain)
			w.ch <- grant{d: d}
		case blocked:
			kept = append(kept, w)
		default:
			t := inst.reserve(w.req.Chain, u, now)
			d.Outcome, d.Queued = OutcomeQueued, now.Sub(w.since)
			d.Remaining = inst.remaining(w.req.Chain, now)
			w.ch <- grant{d: d, t: t}
		}
	}
	clear(inst.waiters[len(kept):])
	inst.waiters = kept
}

// AccountUsage is one account as keelson('llm_usage') shows it.
type AccountUsage struct {
	Account  Account
	Totals   Usage
	Reserved Usage
	Inflight int64
	Refused  int64
	Queued   int64
	Waiting  int
	LastAt   time.Time
}

// Accounts lists every account the ledger has charged or refused, by kind
// and key.
func (inst *Ledger) Accounts() (out []AccountUsage) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	waiting := map[Account]int{}
	for _, w := range inst.waiters {
		for _, a := range w.req.Chain.Accounts() {
			waiting[a]++
		}
	}
	for a, acc := range inst.accounts {
		out = append(out, AccountUsage{Account: a, Totals: clone(acc.totals), Reserved: clone(acc.reserved),
			Inflight: acc.inflight, Refused: acc.refused, Queued: acc.queued, Waiting: waiting[a], LastAt: acc.lastAt})
	}
	slices.SortFunc(out, func(x, y AccountUsage) int {
		if c := cmp.Compare(x.Account.Kind, y.Account.Kind); c != 0 {
			return c
		}
		return cmp.Compare(x.Account.Key, y.Account.Key)
	})
	return
}

// RuleState is a rule on one account, as llm.ration.list and
// keelson('llm_rations') show it.
type RuleState struct {
	Rule     Rule
	Account  Account
	Used     int64
	Limit    int64
	Inflight int64
}

// States lists each rule on each account it applies to: the one it names,
// or every account of its kind the ledger knows.
func (inst *Ledger) States() (out []RuleState) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	now := inst.now()
	for _, id := range inst.order {
		rule := inst.rules[id]
		var accts []Account
		if rule.Select.Key != "" {
			accts = []Account{{Kind: rule.Select.Kind, Key: rule.Select.Key}}
		} else {
			for a := range inst.accounts {
				if a.Kind == rule.Select.Kind {
					accts = append(accts, a)
				}
			}
			slices.SortFunc(accts, func(x, y Account) int { return cmp.Compare(x.Key, y.Key) })
		}
		for _, a := range accts {
			st := RuleState{Rule: rule, Account: a, Limit: rule.limitAt(now), Inflight: inst.inflight(a)}
			if rule.Kind == RuleKindBudget || rule.Kind == RuleKindRate {
				st.Used = inst.used(rule, a, now)
			}
			out = append(out, st)
		}
	}
	return
}

func clone(u Usage) (c Usage) {
	c = make(Usage, len(u))
	for k, v := range u {
		c[k] = v
	}
	return
}
