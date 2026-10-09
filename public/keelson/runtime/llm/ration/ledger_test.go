package ration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (inst *clock) now() time.Time {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.t
}

func (inst *clock) advance(d time.Duration) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.t = inst.t.Add(d)
}

func newTestLedger(t *testing.T) (l *Ledger, c *clock) {
	t.Helper()
	c = &clock{t: time.Date(2026, 10, 9, 12, 0, 30, 0, time.UTC)}
	l = NewLedger(WithClock(c.now))
	return
}

func call(app string, instance uint64, maxOut int64) Request {
	return Request{Chain: Chain{App: app, Instance: instance}, Class: ClassShown, EstimatedInput: 100, MaxOutput: maxOut}
}

func TestNoRulesAdmits(t *testing.T) {
	l, _ := newTestLedger(t)
	d, tk := l.Admit(context.Background(), call("a", 1, 1000))
	require.NotNil(t, tk)
	assert.Equal(t, OutcomeAdmitted, d.Outcome)
	assert.Equal(t, int64(1000), d.MaxOutput)
	l.Settle(tk, Usage{QuantityCalls: 1, QuantityInputTokens: 90, QuantityOutputTokens: 10})
	accts := l.Accounts()
	require.NotEmpty(t, accts)
	for _, a := range accts {
		assert.Equal(t, int64(100), a.Totals[QuantityTotalTokens], a.Account.String())
		assert.Zero(t, a.Inflight)
		assert.Zero(t, a.Reserved[QuantityOutputTokens], "the reservation is released")
	}
}

func TestBudgetRefusesStop(t *testing.T) {
	l, _ := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindBudget,
		Quantity: QuantityCalls, Limit: 2, Window: time.Hour}))
	for range 2 {
		d, tk := l.Admit(context.Background(), call("a", 1, 10))
		require.NotNil(t, tk, d.Reason)
		l.Settle(tk, Usage{QuantityCalls: 1})
	}
	d, tk := l.Admit(context.Background(), call("a", 1, 10))
	assert.Nil(t, tk)
	assert.Equal(t, OutcomeRefused, d.Outcome)
	assert.Equal(t, RefusalStop, d.Refusal)
	assert.Equal(t, "b", d.Rule)
	assert.Positive(t, d.RetryAfter, "a rolling budget says when it frees")
	_, tk = l.Admit(context.Background(), call("other", 2, 10))
	assert.NotNil(t, tk, "another app is not charged to a's account")
}

func TestReservationHoldsUnderConcurrency(t *testing.T) {
	l, _ := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindRun}, Kind: RuleKindBudget,
		Quantity: QuantityOutputTokens, Limit: 1500, Window: time.Hour}))
	_, t1 := l.Admit(context.Background(), call("a", 1, 1000))
	require.NotNil(t, t1)
	d, t2 := l.Admit(context.Background(), call("a", 2, 1000))
	require.NotNil(t, t2, "clamped to what the first call's reservation leaves")
	assert.Equal(t, OutcomeClamped, d.Outcome)
	assert.Equal(t, int64(500), d.MaxOutput)
	d, t3 := l.Admit(context.Background(), call("a", 3, 1000))
	assert.Nil(t, t3, "nothing left while both are in flight")
	assert.Equal(t, RefusalStop, d.Refusal)
	l.Settle(t1, Usage{QuantityOutputTokens: 100})
	d, t3 = l.Admit(context.Background(), call("a", 3, 1000))
	require.NotNil(t, t3, "settling releases what the first call did not use")
	assert.Equal(t, int64(900), d.MaxOutput)
}

func TestRateRefusesWaitWithRetry(t *testing.T) {
	l, c := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "r", Select: Selector{Kind: AccountKindInstance}, Kind: RuleKindRate,
		Quantity: QuantityCalls, Limit: 1, Window: time.Minute}))
	_, tk := l.Admit(context.Background(), call("a", 1, 10))
	require.NotNil(t, tk)
	l.Settle(tk, Usage{QuantityCalls: 1})
	d, tk := l.Admit(context.Background(), call("a", 1, 10))
	assert.Nil(t, tk)
	assert.Equal(t, RefusalWait, d.Refusal)
	assert.Positive(t, d.RetryAfter)
	assert.LessOrEqual(t, d.RetryAfter, 2*time.Minute)
	_, tk = l.Admit(context.Background(), call("a", 2, 10))
	assert.NotNil(t, tk, "each instance has its own count under a wildcard selector")
	c.advance(d.RetryAfter)
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	assert.NotNil(t, tk, "admitted once the window has moved past the charge")
}

func TestDenyAndRaise(t *testing.T) {
	l, c := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "d", Select: Selector{Kind: AccountKindTask, Key: "t1"}, Kind: RuleKindDeny, Reason: "runaway"}))
	r := call("a", 1, 10)
	r.Chain.Task = "t1"
	d, tk := l.Admit(context.Background(), r)
	assert.Nil(t, tk)
	assert.Equal(t, RefusalStop, d.Refusal)
	assert.Contains(t, d.Reason, "runaway")
	assert.True(t, l.Remove("d"))

	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindTask, Key: "t1"}, Kind: RuleKindBudget,
		Quantity: QuantityCalls, Limit: 0, Window: time.Hour, Raise: 1, RaiseUntil: c.now().Add(time.Minute)}))
	_, tk = l.Admit(context.Background(), r)
	require.NotNil(t, tk, "the raise admits one call")
	l.Settle(tk, Usage{QuantityCalls: 1})
	c.advance(2 * time.Minute)
	_, tk = l.Admit(context.Background(), r)
	assert.Nil(t, tk, "the raise has expired and the base limit is 0")
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	assert.NotNil(t, tk, "the app's own call is not the task's")
}

func TestSoftThresholdFiresOnCrossing(t *testing.T) {
	l, _ := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindBudget,
		Quantity: QuantityTotalTokens, Limit: 1000, Window: time.Hour, SoftPercent: []uint8{50, 90}}))
	_, tk := l.Admit(context.Background(), call("a", 1, 10))
	ev := l.Settle(tk, Usage{QuantityInputTokens: 400, QuantityOutputTokens: 0})
	assert.Empty(t, ev)
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	ev = l.Settle(tk, Usage{QuantityInputTokens: 200})
	require.Len(t, ev, 1)
	assert.Equal(t, uint8(50), ev[0].Percent)
	assert.Equal(t, int64(600), ev[0].Used)
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	ev = l.Settle(tk, Usage{QuantityInputTokens: 50})
	assert.Empty(t, ev, "a threshold fires once per crossing")
}

func TestAlignedWindowResets(t *testing.T) {
	l, c := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "day", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindBudget,
		Quantity: QuantityCalls, Limit: 1, Window: 24 * time.Hour, Aligned: true}))
	_, tk := l.Admit(context.Background(), call("a", 1, 10))
	l.Settle(tk, Usage{QuantityCalls: 1})
	d, tk := l.Admit(context.Background(), call("a", 1, 10))
	require.Nil(t, tk)
	assert.Equal(t, 12*time.Hour-30*time.Second, d.RetryAfter, "until the UTC day starts over")
	c.advance(d.RetryAfter)
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	assert.NotNil(t, tk)
}

func TestRecordPastCounts(t *testing.T) {
	l, c := newTestLedger(t)
	l.RecordPast(c.now().Add(-3*time.Hour), []Account{{Kind: AccountKindApp, Key: "a"}}, Usage{QuantityCalls: 5})
	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindBudget,
		Quantity: QuantityCalls, Limit: 5, Window: 24 * time.Hour}))
	d, tk := l.Admit(context.Background(), call("a", 1, 10))
	assert.Nil(t, tk, "past use counts against a window that covers it")
	assert.Equal(t, RefusalStop, d.Refusal)
	require.NoError(t, l.Set(Rule{Id: "b", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindBudget,
		Quantity: QuantityCalls, Limit: 5, Window: time.Hour}))
	_, tk = l.Admit(context.Background(), call("a", 1, 10))
	assert.NotNil(t, tk, "and not against one that does not")
}

func TestQueueServesByClassThenAge(t *testing.T) {
	l, c := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "c", Select: Selector{Kind: AccountKindRun}, Kind: RuleKindConcurrency, Limit: 1}))
	_, holder := l.Admit(context.Background(), call("a", 1, 10))
	require.NotNil(t, holder)

	type result struct {
		name string
		d    Decision
		t    *Ticket
	}
	results := make(chan result, 3)
	enqueue := func(name string, class ClassE) {
		r := call("a", 2, 10)
		r.Class = class
		go func() {
			d, tk := l.Admit(context.Background(), r)
			results <- result{name: name, d: d, t: tk}
		}()
	}
	enqueue("background", ClassBackground)
	waitFor(t, l, 1)
	enqueue("focused", ClassFocused)
	waitFor(t, l, 2)

	l.Settle(holder, Usage{QuantityCalls: 1})
	first := <-results
	assert.Equal(t, "focused", first.name, "the focused window goes first")
	assert.Equal(t, OutcomeQueued, first.d.Outcome)
	l.Settle(first.t, Usage{QuantityCalls: 1})
	second := <-results
	assert.Equal(t, "background", second.name)
	l.Settle(second.t, Usage{QuantityCalls: 1})

	// Aging: a background call that has waited past two steps outranks a
	// shown one that just arrived.
	_, holder = l.Admit(context.Background(), call("a", 1, 10))
	require.NotNil(t, holder)
	enqueue("old-background", ClassBackground)
	waitFor(t, l, 1)
	c.advance(2 * DefaultAgingStep)
	enqueue("new-shown", ClassShown)
	waitFor(t, l, 2)
	l.Settle(holder, Usage{QuantityCalls: 1})
	first = <-results
	assert.Equal(t, "old-background", first.name)
	l.Settle(first.t, nil)
	second = <-results
	l.Settle(second.t, nil)
}

func TestQueueGivesUpAtDeadline(t *testing.T) {
	l, _ := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "c", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindConcurrency, Limit: 0}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	d, tk := l.Admit(ctx, call("a", 1, 10))
	assert.Nil(t, tk)
	assert.Equal(t, RefusalWait, d.Refusal)
	assert.Equal(t, "c", d.Rule)
	assert.Zero(t, waiting(l))
}

func TestRuleChangeReleasesQueue(t *testing.T) {
	l, _ := newTestLedger(t)
	require.NoError(t, l.Set(Rule{Id: "c", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindConcurrency, Limit: 0}))
	done := make(chan Decision, 1)
	go func() {
		d, tk := l.Admit(context.Background(), call("a", 1, 10))
		l.Settle(tk, nil)
		done <- d
	}()
	waitFor(t, l, 1)
	require.NoError(t, l.Set(Rule{Id: "c", Select: Selector{Kind: AccountKindApp, Key: "a"}, Kind: RuleKindDeny}))
	d := <-done
	assert.Equal(t, OutcomeRefused, d.Outcome, "a rule that now denies refuses the waiting call")
}

func TestValidate(t *testing.T) {
	assert.Error(t, Rule{Id: "x", Select: Selector{Kind: AccountKindApp}, Kind: RuleKindRate, Quantity: QuantityCalls, Limit: 1, Window: 3 * time.Hour}.Validate())
	assert.Error(t, Rule{Id: "x", Select: Selector{Kind: AccountKindApp}, Kind: RuleKindBudget, Limit: 1, Window: time.Hour}.Validate())
	assert.Error(t, Rule{Id: "x", Kind: RuleKindDeny}.Validate())
	assert.NoError(t, Rule{Id: "x", Select: Selector{Kind: AccountKindTask}, Kind: RuleKindDeny}.Validate())
}

func waiting(l *Ledger) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.waiters)
}

func waitFor(t *testing.T, l *Ledger, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return waiting(l) >= n }, time.Second, time.Millisecond)
}
