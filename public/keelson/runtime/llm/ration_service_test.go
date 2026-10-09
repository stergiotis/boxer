package llm

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

const moderatorId app.AppIdT = "test.llm.moderator"

// rationHost is serve with a moderator beside the app: the service lists
// it in Moderators, and it holds ModeratorCaps and ClientCaps.
type rationHost struct {
	bus    *inprocbus.Inst
	cli    *Client
	svc    *Service
	mod    *Moderator
	modCli *Client
	events chan *app.Msg
}

func serveRation(t *testing.T, cfg Config) (h rationHost) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	h.bus = bus
	cfg.Moderators = []string{string(moderatorId)}
	var err error
	h.svc, err = NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(h.svc.Close)
	h.cli = NewClient(bus.NewClient(appId, ClientCaps("test: ask")))
	h.cli.Timeout = 5 * time.Second
	modBus := bus.NewClient(moderatorId, append(ModeratorCaps("test: moderate"), ClientCaps("test: moderator's own calls")...))
	h.mod = NewModerator(modBus)
	h.modCli = NewClient(modBus)
	h.modCli.Timeout = 5 * time.Second
	h.events = make(chan *app.Msg, 64)
	unsub, err := modBus.Subscribe(SubjectEventAll, func(m *app.Msg) {
		c := *m
		c.Payload = append([]byte(nil), m.Payload...)
		h.events <- &c
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	return
}

func hi() []openaichat.Message {
	return []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
}

func nextEvent(t *testing.T, events chan *app.Msg, subject string) (m *app.Msg) {
	t.Helper()
	for {
		select {
		case m = <-events:
			if m.Subject == subject {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s event", subject)
		}
	}
}

// Every call is metered on its accounts and announced to moderators; the
// reply and the record carry the provider's token breakdown.
func TestCallsAreMeteredAndAnnounced(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "hello", InputTokens: 30, OutputTokens: 5,
		CachedInputTokens: option.Some(int32(20))}}
	h := serveRation(t, localCfg(p))
	res, err := h.cli.Complete(context.Background(), Request{Messages: hi(), Purpose: "book/greet"})
	require.NoError(t, err)
	assert.Equal(t, option.Some(int32(20)), res.CachedInputTokens)
	assert.False(t, res.ReasoningTokens.Has, "not reported stays absent")
	assert.Equal(t, "admitted", res.Admission)

	e, err := DecodeCallEvent(nextEvent(t, h.events, SubjectEventCall).Payload)
	require.NoError(t, err)
	assert.Equal(t, res.CallId, e.CallId)
	assert.Equal(t, string(appId), e.Chain.App)
	assert.Equal(t, "book/greet", e.Chain.Purpose)
	assert.Equal(t, int64(35), e.Usage[ration.QuantityTotalTokens])
	assert.Equal(t, int64(20), e.Usage[ration.QuantityCachedInputTokens])

	var sawPurpose bool
	for _, a := range h.svc.Ledger().Accounts() {
		if a.Account == (ration.Account{Kind: ration.AccountKindPurpose, Key: ration.PurposeKey(string(appId), "book/greet")}) {
			sawPurpose = true
			assert.Equal(t, int64(1), a.Totals[ration.QuantityCalls])
		}
	}
	assert.True(t, sawPurpose, "the purpose is an account")
	rec := h.svc.Calls()[0]
	assert.Equal(t, "admitted", rec.Admission)
	row := RowOf(rec, false)
	assert.Equal(t, option.Some(uint32(20)), row.CachedInputTokens)
	assert.Equal(t, option.Some("admitted"), row.Admission)
}

// A moderator's budget refuses the app's next call with "stop" and names
// the rule; the refusal is announced and recorded.
func TestABudgetRefusesWithStop(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok", InputTokens: 10, OutputTokens: 2}}
	h := serveRation(t, localCfg(p))
	ctx := context.Background()
	require.NoError(t, h.mod.Set(ctx, ration.Rule{Id: "one-call", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 1, Window: time.Hour, Reason: "test"}))
	_, err := h.cli.Complete(ctx, Request{Messages: hi()})
	require.NoError(t, err)
	_, err = h.cli.Complete(ctx, Request{Messages: hi()})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, ration.RefusalStop, refused.Refusal)
	assert.Equal(t, "one-call", refused.Rule)
	assert.Positive(t, refused.RetryAfter)

	calls := h.svc.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "refused", calls[1].Admission)
	assert.Equal(t, "one-call", calls[1].AdmissionRule)
	assert.True(t, calls[1].Refused)

	states, err := h.mod.List(ctx)
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, int64(1), states[0].Used)
	assert.Equal(t, string(moderatorId), states[0].Rule.Author)
}

// A budget on output tokens lowers the ceiling the provider is asked for,
// and the reply says what is left.
func TestABudgetClampsTheCeiling(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok", InputTokens: 10, OutputTokens: 100}}
	cfg := localCfg(p)
	cfg.MaxTokens = 1000
	h := serveRation(t, cfg)
	ctx := context.Background()
	require.NoError(t, h.mod.Set(ctx, ration.Rule{Id: "out", Select: ration.Selector{Kind: ration.AccountKindInstance},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityOutputTokens, Limit: 300, Window: time.Hour}))
	res, err := h.cli.Complete(ctx, Request{Messages: hi()})
	require.NoError(t, err)
	assert.Equal(t, int32(300), p.seen.MaxTokens)
	assert.Equal(t, "clamped", res.Admission)
	require.Len(t, res.Remaining, 1)
	res, err = h.cli.Complete(ctx, Request{Messages: hi()})
	require.NoError(t, err)
	assert.Equal(t, int32(200), p.seen.MaxTokens, "what the first call used is gone")
	require.Len(t, res.Remaining, 1)
	assert.Equal(t, int64(300), res.Remaining[0].Limit)
	assert.Zero(t, res.Remaining[0].Remaining, "the call's own reservation is counted")
}

// Only a listed moderator reaches the rule verbs, and a moderator cannot
// loosen a rule on its own calls.
func TestModeratorBounds(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok"}}
	h := serveRation(t, localCfg(p))
	ctx := context.Background()
	stranger := NewModerator(h.bus.NewClient("test.llm.stranger", ModeratorCaps("test: not listed")))
	err := stranger.Set(ctx, ration.Rule{Id: "x", Select: ration.Selector{Kind: ration.AccountKindRun}, Kind: ration.RuleKindDeny})
	require.ErrorIs(t, err, ErrModeratorRefused)
	assert.Contains(t, err.Error(), "BOXER_LLM_MODERATORS")

	own := ration.Rule{Id: "own", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(moderatorId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 5, Window: time.Hour}
	require.NoError(t, h.mod.Set(ctx, own), "tightening its own calls is the moderator's to do")
	own.Limit = 3
	require.NoError(t, h.mod.Set(ctx, own))
	own.Limit = 10
	err = h.mod.Set(ctx, own)
	require.ErrorIs(t, err, ErrModeratorRefused, "loosening is the person's")
	require.ErrorIs(t, h.mod.Remove(ctx, "own"), ErrModeratorRefused)

	// A raise on its own calls is the person's, renewed or extended alike.
	raised := ration.Rule{Id: "raised", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(moderatorId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 5, Window: time.Hour}
	require.NoError(t, h.mod.Set(ctx, raised))
	raised.Raise, raised.RaiseUntil = 5, time.Now().Add(time.Hour)
	require.ErrorIs(t, h.mod.Set(ctx, raised), ErrModeratorRefused, "adding a raise")
	_, _, err = h.mod.Ask(ctx, raised, "raise?")
	require.ErrorIs(t, err, ErrModeratorRefused, "no dialog in this test; the rule stays as it was")

	other := ration.Rule{Id: "other", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 5, Window: time.Hour}
	require.NoError(t, h.mod.Set(ctx, other))
	other.Limit = 50
	require.NoError(t, h.mod.Set(ctx, other), "loosening another app's rule is the moderator's")
	require.NoError(t, h.mod.Remove(ctx, "other"))

	// A task the moderator's own calls were charged to holds the moderator
	// too.
	h.svc.SetDelegation(&fakeDelegation{allow: true})
	_, err = h.modCli.Complete(ctx, Request{Messages: hi(), OnBehalfOf: &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "call-1"}})
	require.NoError(t, err)
	onTask := ration.Rule{Id: "task", Select: ration.Selector{Kind: ration.AccountKindTask, Key: "task-1"},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 5, Window: time.Hour}
	require.NoError(t, h.mod.Set(ctx, onTask))
	onTask.Limit = 50
	require.ErrorIs(t, h.mod.Set(ctx, onTask), ErrModeratorRefused)
	require.ErrorIs(t, h.mod.Remove(ctx, "task"), ErrModeratorRefused)
}

// Renewing a lapsed raise, or extending one, loosens; a smaller or shorter
// one does not.
func TestLoosensCountsRaises(t *testing.T) {
	now := time.Now()
	base := ration.Rule{Id: "r", Select: ration.Selector{Kind: ration.AccountKindApp, Key: "a"}, Kind: ration.RuleKindBudget,
		Quantity: ration.QuantityCalls, Limit: 5, Window: time.Hour, Raise: 10, RaiseUntil: now.Add(-time.Hour)}
	renewed := base
	renewed.RaiseUntil = now.Add(24 * time.Hour)
	assert.True(t, loosens(base, renewed), "a lapsed raise renewed")
	shorter := base
	shorter.Raise, shorter.RaiseUntil = 5, now.Add(-2*time.Hour)
	assert.False(t, loosens(base, shorter))
	assert.False(t, loosens(base, base))
}

// A call held by a concurrency rule waits in the queue, and a moderator's
// cancel by account reaches it there.
func TestCancelReachesAQueuedCall(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok"}}
	h := serveRation(t, localCfg(p))
	ctx := context.Background()
	require.NoError(t, h.mod.Set(ctx, ration.Rule{Id: "hold", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindConcurrency, Limit: 0}))
	done := make(chan error, 1)
	go func() {
		_, err := h.cli.Complete(ctx, Request{Messages: hi()})
		done <- err
	}()
	require.Eventually(t, func() bool {
		for _, a := range h.svc.Ledger().Accounts() {
			if a.Account.Kind == ration.AccountKindApp && a.Account.Key == string(appId) && a.Waiting == 1 {
				return true
			}
		}
		return false
	}, 2*time.Second, time.Millisecond)
	n, err := h.mod.CancelAccount(ctx, ration.Account{Kind: ration.AccountKindApp, Key: string(appId)}, "test")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	err = <-done
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, ration.RefusalStop, refused.Refusal, "a moderator's cancel is not a reason to retry")
	assert.Equal(t, "hold", refused.Rule)
	assert.Contains(t, refused.Reason, "cancelled by the moderator "+string(moderatorId)+": test")
}

// The queue takes its class from the window state the host reports.
func TestQueueClassComesFromTheWindow(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok"}}
	h := serveRation(t, localCfg(p))
	h.svc.SetWindows(fixedClass(ration.ClassFocused))
	bc := h.bus.NewClient(appId, ClientCaps("test: ask"))
	bc.SetInstanceKey(7)
	cli := NewClient(bc)
	cli.Timeout = 5 * time.Second
	_, err := cli.Complete(context.Background(), Request{Messages: hi()})
	require.NoError(t, err)
	e, err := DecodeCallEvent(nextEvent(t, h.events, SubjectEventCall).Payload)
	require.NoError(t, err)
	assert.Equal(t, "focused", e.Class)
}

type fixedClass ration.ClassE

func (inst fixedClass) WindowClass(uint64) ration.ClassE { return ration.ClassE(inst) }

// A moderator's question to the person sets the rule with the person as
// author when they say yes; with no dialog the ask is refused.
func TestAskThePerson(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok"}}
	h := serveRation(t, localCfg(p))
	ctx := context.Background()
	r := ration.Rule{Id: "more", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(moderatorId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 100, Window: time.Hour}
	_, _, err := h.mod.Ask(ctx, r, "allow more?")
	require.ErrorIs(t, err, ErrModeratorRefused)

	asker := &fakeAsker{granted: true}
	h.svc.SetAsker(asker)
	granted, decided, err := h.mod.Ask(ctx, r, "allow more?")
	require.NoError(t, err)
	assert.True(t, granted)
	assert.True(t, decided)
	assert.Equal(t, "allow more?", asker.seen.Text)
	assert.Equal(t, moderatorId, asker.seen.Moderator)
	got, ok := h.svc.Ledger().Rule("more")
	require.True(t, ok)
	assert.Equal(t, "person", got.Author)
}

type fakeAsker struct {
	mu      sync.Mutex
	granted bool
	seen    Question
}

func (f *fakeAsker) AskPerson(_ context.Context, q Question) (granted bool, decided bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = q
	return f.granted, true, nil
}

// A ledger that cannot be rebuilt refuses every call when the deployment
// says so.
func TestUnruledRefuse(t *testing.T) {
	l := ration.NewLedger()
	l.SetRefuseAll("rebuild failed")
	cfg := localCfg(&fakeProvider{resp: openaichat.CompletionResponse{Content: "ok"}})
	cfg.Ledger = l
	h := serveRation(t, cfg)
	_, err := h.cli.Complete(context.Background(), Request{Messages: hi()})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, ration.RefusalStop, refused.Refusal)
	assert.Contains(t, refused.Reason, "rebuild failed")
}

// Over clickhouse-local: the token breakdown and the admission land on the
// call row; a new run's ledger is rebuilt from the rows of the last one,
// charging its apps, tasks and purposes but not its windows; and a rule a
// moderator sets lands as an audit event.
func TestLedgerRebuildsFromTheTrail(t *testing.T) {
	exec := localFacts(t)
	ctx := context.Background()
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok", InputTokens: 40, OutputTokens: 2,
		CachedInputTokens: option.Some(int32(32)), ReasoningTokens: option.Some(int32(0))}}
	first := localCfg(p)
	first.Trail = trail.NewRecorder(exec, "run-a", zerolog.Nop())
	t.Cleanup(first.Trail.Close)
	h := serveRation(t, first)
	for range 2 {
		_, err := h.cli.Complete(ctx, Request{Messages: hi(), Purpose: "book/greet"})
		require.NoError(t, err)
	}
	require.NoError(t, h.mod.Set(ctx, ration.Rule{Id: "cap", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 10, Window: 24 * time.Hour, Reason: "a test cap"}))
	calls, err := h.svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, option.Some(int32(32)), calls[0].CachedInputTokens)
	assert.Equal(t, option.Some(int32(0)), calls[0].ReasoningTokens, "a reported zero survives the row")
	assert.Equal(t, "admitted", calls[0].Admission)

	events, err := first.Trail.Scan(func(st *trail.TrailStore) iter.Seq2[*trail.TrailEntity, error] {
		return st.ScanAuditEvent(ctx, recordstore.ScanOpts{})
	})
	require.NoError(t, err)
	var ruleSet *trail.AuditEvent
	for _, e := range events {
		if e.AuditEvent.Has && e.AuditEvent.Val.Domain == auditDomain {
			ev := e.AuditEvent.Val
			ruleSet = &ev
		}
	}
	require.NotNil(t, ruleSet, "the rule change is on the trail")
	assert.Equal(t, "rule-set", ruleSet.Action)
	assert.Equal(t, []string{"cap"}, ruleSet.RefValues)
	assert.Contains(t, ruleSet.AttrValues, "a test cap")

	second := localCfg(p)
	second.Trail = trail.NewRecorder(exec, "run-b", zerolog.Nop())
	t.Cleanup(second.Trail.Close)
	svc, err := NewService(inprocbus.NewInst(zerolog.Nop()), zerolog.Nop(), second)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	byAccount := map[ration.Account]ration.AccountUsage{}
	for _, a := range svc.Ledger().Accounts() {
		byAccount[a.Account] = a
	}
	assert.Equal(t, int64(2), byAccount[ration.Account{Kind: ration.AccountKindApp, Key: string(appId)}].Totals[ration.QuantityCalls])
	assert.Equal(t, int64(64), byAccount[ration.Account{Kind: ration.AccountKindPurpose, Key: ration.PurposeKey(string(appId), "book/greet")}].Totals[ration.QuantityCachedInputTokens])
	_, hasRun := byAccount[ration.Account{Kind: ration.AccountKindRun}]
	assert.False(t, hasRun, "another run's rows charge no run account")
	_, hasWindow := byAccount[ration.Account{Kind: ration.AccountKindInstance, Key: "0"}]
	assert.False(t, hasWindow, "nor a window, whose key means another window now")
}
