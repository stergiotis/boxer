// Package ration is the metering and admission core of the model service
// (ADR-0300): the accounts a call is charged to, the usage it is charged
// with, the rules a moderator writes over them, and the ledger that checks
// a call against those rules before it reaches the provider.
//
// The package knows nothing of the bus, the trail or windows. The service
// states who a call is charged to ([Chain]), how urgent it is ([ClassE]),
// and what it is expected to cost; the ledger answers with a [Decision] and
// a [Ticket] the service settles with what the call actually cost.
package ration

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AccountKindE is what an account is keyed by (ADR-0300 §SD1).
type AccountKindE uint8

const (
	AccountKindNone AccountKindE = iota
	// AccountKindRun is the process run: one account, key "".
	AccountKindRun
	// AccountKindApp is keyed by the app id.
	AccountKindApp
	// AccountKindInstance is keyed by the window key in decimal; "0" is the
	// account of calls no window made (services, the CLI).
	AccountKindInstance
	// AccountKindTask is keyed by the agent task id.
	AccountKindTask
	// AccountKindPurpose is keyed by "<app id>/<purpose>": a purpose is the
	// app's own word, so it is scoped to the app.
	AccountKindPurpose
)

var accountKindNames = [...]string{"", "run", "app", "instance", "task", "purpose"}

func (inst AccountKindE) String() string {
	if int(inst) < len(accountKindNames) {
		return accountKindNames[inst]
	}
	return "account-kind-" + strconv.Itoa(int(inst))
}

// ParseAccountKind is String's inverse.
func ParseAccountKind(s string) (k AccountKindE, err error) {
	for i, n := range accountKindNames {
		if i > 0 && n == s {
			return AccountKindE(i), nil
		}
	}
	err = eb.Build().Str("kind", s).Errorf("ration: unknown account kind")
	return
}

// Account is one thing a call is charged to.
type Account struct {
	Kind AccountKindE
	Key  string
}

func (inst Account) String() string { return inst.Kind.String() + ":" + inst.Key }

// Chain is who a call is charged to: every account it names, and the call
// must fit within the rules on each (ADR-0300 §SD1).
type Chain struct {
	App      string
	Instance uint64
	// Task is the agent task the call was made for, attested by the
	// dispatcher; empty for an app's own call.
	Task string
	// Purpose is the purpose the request states; empty charges no purpose
	// account.
	Purpose string
}

// Accounts lists the chain's accounts, the run's first.
func (inst Chain) Accounts() (accts []Account) {
	accts = make([]Account, 0, 5)
	accts = append(accts,
		Account{Kind: AccountKindRun},
		Account{Kind: AccountKindApp, Key: inst.App},
		Account{Kind: AccountKindInstance, Key: strconv.FormatUint(inst.Instance, 10)})
	if inst.Task != "" {
		accts = append(accts, Account{Kind: AccountKindTask, Key: inst.Task})
	}
	if inst.Purpose != "" {
		accts = append(accts, Account{Kind: AccountKindPurpose, Key: PurposeKey(inst.App, inst.Purpose)})
	}
	return
}

// PurposeKey is the key of a purpose account.
func PurposeKey(app string, purpose string) (key string) { return app + "/" + purpose }

// QuantityE names something a call uses (ADR-0300 §SD2). The set is open:
// the ledger keeps whatever it is charged with, and a rule may name any
// quantity. These are the ones the model service charges.
type QuantityE string

const (
	// QuantityCalls counts calls: 1 per call admitted.
	QuantityCalls QuantityE = "calls"
	// QuantityInputTokens, QuantityOutputTokens and their parts follow the
	// OTel GenAI usage attributes. QuantityTotalTokens is input plus
	// output, kept as its own quantity so a rule can name it.
	QuantityInputTokens       QuantityE = "tokens.input"
	QuantityOutputTokens      QuantityE = "tokens.output"
	QuantityTotalTokens       QuantityE = "tokens.total"
	QuantityCachedInputTokens QuantityE = "tokens.cached_input"
	QuantityReasoningTokens   QuantityE = "tokens.reasoning"
	// QuantityWallMs is the wall time a call took, in milliseconds.
	QuantityWallMs QuantityE = "wall_ms"
)

// KnownQuantities lists the quantities the model service charges, in the
// order tables show them.
var KnownQuantities = []QuantityE{
	QuantityCalls, QuantityInputTokens, QuantityOutputTokens, QuantityTotalTokens,
	QuantityCachedInputTokens, QuantityReasoningTokens, QuantityWallMs,
}

// Usage is what a call used, by quantity. A quantity the provider did not
// report is absent, never zero.
type Usage map[QuantityE]int64

// WithTotal adds the total of input and output when either is present.
func (inst Usage) WithTotal() (u Usage) {
	u = inst
	in, hasIn := inst[QuantityInputTokens]
	out, hasOut := inst[QuantityOutputTokens]
	if hasIn || hasOut {
		u = make(Usage, len(inst)+1)
		for k, v := range inst {
			u[k] = v
		}
		u[QuantityTotalTokens] = in + out
	}
	return
}

// RuleKindE is what a rule limits (ADR-0300 §SD4).
type RuleKindE uint8

const (
	RuleKindNone RuleKindE = iota
	// RuleKindBudget limits a quantity's total over a window; a call it refuses
	// is told to stop.
	RuleKindBudget
	// RuleKindRate limits a quantity per short window (at most MaxRateWindow); a
	// call it refuses is told to wait.
	RuleKindRate
	// RuleKindConcurrency limits the calls in flight; a call over it waits in
	// the queue.
	RuleKindConcurrency
	// RuleKindDeny refuses every call on the accounts it selects.
	RuleKindDeny
)

var ruleKindNames = [...]string{"", "budget", "rate", "concurrency", "deny"}

func (inst RuleKindE) String() string {
	if int(inst) < len(ruleKindNames) {
		return ruleKindNames[inst]
	}
	return "rule-kind-" + strconv.Itoa(int(inst))
}

// ParseRuleKind is String's inverse.
func ParseRuleKind(s string) (k RuleKindE, err error) {
	for i, n := range ruleKindNames {
		if i > 0 && n == s {
			return RuleKindE(i), nil
		}
	}
	err = eb.Build().Str("kind", s).Errorf("ration: unknown rule kind")
	return
}

// The windows a rule may name. A window longer than MinuteHorizon is summed
// by the hour, so its start is rounded down to the hour and it may count up
// to an hour more than it names.
const (
	// MinuteHorizon is how far back the ledger keeps per-minute counts.
	MinuteHorizon = 2 * time.Hour
	// HourHorizon is how far back it keeps per-hour counts: the longest
	// window a budget may name.
	HourHorizon = 32 * 24 * time.Hour
	// MaxRateWindow is the longest window a rate may name.
	MaxRateWindow = MinuteHorizon
)

// Selector picks the accounts a rule applies to: every account of Kind
// when Key is empty, each counted on its own, else the one with Key.
type Selector struct {
	Kind AccountKindE
	Key  string
}

func (inst Selector) matches(a Account) (yes bool) {
	return inst.Kind == a.Kind && (inst.Key == "" || inst.Key == a.Key)
}

func (inst Selector) String() string {
	if inst.Key == "" {
		return inst.Kind.String() + ":*"
	}
	return inst.Kind.String() + ":" + inst.Key
}

// Rule is one limit a moderator, or the person, set (ADR-0300 §SD4).
type Rule struct {
	// Id names the rule; setting a rule with an id that exists replaces it.
	Id     string
	Select Selector
	Kind   RuleKindE
	// Quantity is what a budget or rate counts; ignored by concurrency and
	// deny rules.
	Quantity QuantityE
	// Limit is the most a budget or rate admits in Window, or the most
	// calls in flight a concurrency rule admits. A concurrency limit of 0
	// holds every call in the queue.
	Limit int64
	// Window is a budget's or a rate's span. Aligned windows start at whole
	// multiples of Window since the Unix epoch (UTC days for 24h); others
	// roll.
	Window  time.Duration
	Aligned bool
	// SoftPercent are the shares of Limit at which an event is raised when
	// a call crosses them.
	SoftPercent []uint8
	// Raise adds to Limit until RaiseUntil; it never changes Limit.
	Raise      int64
	RaiseUntil time.Time
	// Author is who set the rule: an app id, or "person". Reason is theirs.
	Author string
	Reason string
	SetAt  time.Time
}

// Validate says whether the ledger can enforce the rule.
func (inst Rule) Validate() (err error) {
	b := eb.Build().Str("rule", inst.Id)
	switch {
	case inst.Id == "":
		return eh.Errorf("ration: a rule needs an id")
	case inst.Select.Kind == AccountKindNone || int(inst.Select.Kind) >= len(accountKindNames):
		return b.Errorf("ration: the rule selects no account kind")
	case inst.Limit < 0 || inst.Raise < 0:
		return b.Errorf("ration: the rule's limit or raise is negative")
	}
	switch inst.Kind {
	case RuleKindBudget, RuleKindRate:
		if inst.Quantity == "" {
			return b.Errorf("ration: a budget or a rate names a quantity")
		}
		if inst.Window <= 0 || inst.Window > HourHorizon {
			return b.Errorf("ration: a window is positive and at most HourHorizon (32 days)")
		}
		if inst.Kind == RuleKindRate && inst.Window > MaxRateWindow {
			return b.Errorf("ration: a rate's window is at most MaxRateWindow (2 hours); use a budget for longer")
		}
	case RuleKindConcurrency, RuleKindDeny:
	default:
		return b.Errorf("ration: the rule has no kind")
	}
	for _, p := range inst.SoftPercent {
		if p == 0 || p > 100 {
			return b.Errorf("ration: a soft threshold is a percentage in 1..100")
		}
	}
	return
}

// limitAt is the limit with any raise in force at now.
func (inst Rule) limitAt(now time.Time) (limit int64) {
	limit = inst.Limit
	if inst.Raise > 0 && now.Before(inst.RaiseUntil) {
		limit += inst.Raise
	}
	return
}

// windowStart is where the rule's window begins for a check at now.
func (inst Rule) windowStart(now time.Time) (start time.Time) {
	if inst.Aligned {
		w := int64(inst.Window)
		return time.Unix(0, now.UnixNano()/w*w).UTC()
	}
	return now.Add(-inst.Window)
}

// Describe is a one-line account of the rule, for a refusal's reason.
func (inst Rule) Describe() (s string) {
	var b strings.Builder
	b.WriteString(inst.Kind.String())
	b.WriteString(" rule ")
	b.WriteString(strconv.Quote(inst.Id))
	b.WriteString(" on ")
	b.WriteString(inst.Select.String())
	switch inst.Kind {
	case RuleKindBudget, RuleKindRate:
		b.WriteString(": ")
		b.WriteString(strconv.FormatInt(inst.Limit, 10))
		b.WriteString(" ")
		b.WriteString(string(inst.Quantity))
		b.WriteString(" per ")
		b.WriteString(inst.Window.String())
		if inst.Aligned {
			b.WriteString(" (aligned)")
		}
	case RuleKindConcurrency:
		b.WriteString(": ")
		b.WriteString(strconv.FormatInt(inst.Limit, 10))
		b.WriteString(" in flight")
	}
	if inst.Reason != "" {
		b.WriteString(" — ")
		b.WriteString(inst.Reason)
	}
	return b.String()
}

// ClassE is how urgent a call is, from the state of the window that made
// it (ADR-0300 §SD6). Lower is served first.
type ClassE uint8

const (
	ClassFocused ClassE = iota
	ClassShown
	ClassBackground
	ClassNoWindow
)

var classNames = [...]string{"focused", "shown", "background", "no-window"}

func (inst ClassE) String() string {
	if int(inst) < len(classNames) {
		return classNames[inst]
	}
	return "class-" + strconv.Itoa(int(inst))
}

// OutcomeE is how admission decided a call.
type OutcomeE uint8

const (
	OutcomeAdmitted OutcomeE = iota
	// OutcomeClamped is admitted with a lower output ceiling.
	OutcomeClamped
	// OutcomeQueued is admitted after waiting in the queue.
	OutcomeQueued
	OutcomeRefused
)

var outcomeNames = [...]string{"admitted", "clamped", "queued", "refused"}

func (inst OutcomeE) String() string {
	if int(inst) < len(outcomeNames) {
		return outcomeNames[inst]
	}
	return "outcome-" + strconv.Itoa(int(inst))
}

// RefusalE says what a refused caller should do.
type RefusalE uint8

const (
	RefusalNone RefusalE = iota
	// RefusalWait: a rate or concurrency limit; try again after RetryAfter.
	RefusalWait
	// RefusalStop: a budget or a deny rule; trying again soon will not help.
	RefusalStop
)

var refusalNames = [...]string{"", "wait", "stop"}

func (inst RefusalE) String() string {
	if int(inst) < len(refusalNames) {
		return refusalNames[inst]
	}
	return "refusal-" + strconv.Itoa(int(inst))
}

// ParseRefusal is String's inverse; "" is RefusalNone.
func ParseRefusal(s string) (r RefusalE) {
	for i, n := range refusalNames {
		if n == s {
			return RefusalE(i)
		}
	}
	return RefusalNone
}

// Remaining is what a budget or rate rule leaves an account after the
// call's reservation.
type Remaining struct {
	Rule      string
	Account   Account
	Quantity  QuantityE
	Limit     int64
	Remaining int64
	// ResetAt is when an aligned window starts over; zero for a rolling one.
	ResetAt time.Time
}

// Decision is the ledger's answer to a call.
type Decision struct {
	Outcome OutcomeE
	// Rule is the rule that refused, clamped or queued the call; empty
	// when none did.
	Rule string
	// Refusal and Reason say why a refused call was refused; RetryAfter is
	// a waiting caller's hint, zero when none can be given.
	Refusal    RefusalE
	Reason     string
	RetryAfter time.Duration
	// MaxOutput is the output ceiling the call may use: the request's, or
	// lower when clamped.
	MaxOutput int64
	// Queued is how long the call waited for a slot.
	Queued    time.Duration
	Remaining []Remaining
}

// EventKindE is what an event reports.
type EventKindE uint8

const (
	EventKindThreshold EventKindE = iota + 1
)

// Event is something a moderator subscribes to that the ledger, not the
// service, can tell: a soft threshold crossed.
type Event struct {
	Kind     EventKindE
	Rule     string
	Account  Account
	Quantity QuantityE
	Used     int64
	Limit    int64
	Percent  uint8
}

// sortedRuleIds orders rules for a deterministic check.
func sortedRuleIds(rules map[string]Rule) (ids []string) {
	ids = make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return
}
