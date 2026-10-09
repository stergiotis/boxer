package llm

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
)

// The wire forms of the moderator's verbs and the events (ADR-0300), in
// the shape of the completion's: plain CBOR structs with a version byte.

// wireRule is a rule. Times are Unix nanoseconds, 0 for none.
type wireRule struct {
	Id          string  `json:"id"`
	SelectKind  string  `json:"select_kind"`
	SelectKey   string  `json:"select_key,omitempty"`
	Kind        string  `json:"kind"`
	Quantity    string  `json:"quantity,omitempty"`
	Limit       int64   `json:"limit,omitempty"`
	WindowNs    int64   `json:"window_ns,omitempty"`
	Aligned     bool    `json:"aligned,omitempty"`
	SoftPercent []uint8 `json:"soft_percent,omitempty"`
	Raise       int64   `json:"raise,omitempty"`
	RaiseUntil  int64   `json:"raise_until,omitempty"`
	Until       int64   `json:"until,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	// Author and SetAt are the service's; a request's are ignored.
	Author string `json:"author,omitempty"`
	SetAt  int64  `json:"set_at,omitempty"`
}

// wireRationSet is the request on llm.ration.set: set Rule, or remove the
// rule with Rule.Id.
type wireRationSet struct {
	V      uint8    `json:"v"`
	Rule   wireRule `json:"rule"`
	Remove bool     `json:"remove,omitempty"`
}

// wireRationCancel is the request on llm.ration.cancel: the call CallId,
// or every call charged to the account.
type wireRationCancel struct {
	V           uint8  `json:"v"`
	CallId      string `json:"call_id,omitempty"`
	AccountKind string `json:"account_kind,omitempty"`
	AccountKey  string `json:"account_key,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// wireRationAsk is the request on llm.ration.ask: a rule the moderator
// proposes, and the question the person is shown.
type wireRationAsk struct {
	V        uint8    `json:"v"`
	Rule     wireRule `json:"rule"`
	Question string   `json:"question"`
}

// wireRuleState is a rule on one account.
type wireRuleState struct {
	Rule        wireRule `json:"rule"`
	AccountKind string   `json:"account_kind"`
	AccountKey  string   `json:"account_key"`
	Used        int64    `json:"used,omitempty"`
	Limit       int64    `json:"limit,omitempty"`
	Inflight    int64    `json:"inflight,omitempty"`
}

// wireRationReply is the reply to every llm.ration verb.
type wireRationReply struct {
	V      uint8           `json:"v"`
	Ok     bool            `json:"ok"`
	Reason string          `json:"reason,omitempty"`
	States []wireRuleState `json:"states,omitempty"`
	// Cancelled counts the calls a cancel stopped; Decided says whether the
	// person answered an ask, and Granted whether they set the rule.
	Cancelled int  `json:"cancelled,omitempty"`
	Decided   bool `json:"decided,omitempty"`
	Granted   bool `json:"granted,omitempty"`
}

// wireRemaining is what a rule leaves one account.
type wireRemaining struct {
	Rule        string `json:"rule"`
	AccountKind string `json:"account_kind"`
	AccountKey  string `json:"account_key"`
	Quantity    string `json:"quantity"`
	Limit       int64  `json:"limit"`
	Remaining   int64  `json:"remaining"`
	ResetAt     int64  `json:"reset_at,omitempty"`
}

// wireCallEvent is the event on llm.event.call.
type wireCallEvent struct {
	V        uint8  `json:"v"`
	CallId   string `json:"call_id"`
	At       int64  `json:"at"`
	App      string `json:"app"`
	Instance uint64 `json:"instance,omitempty"`
	Task     string `json:"task,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
	// Conversation, Turn and Round are what the request said the call
	// belongs to (ADR-0302 §SD2); Round is read only beside a Turn.
	Conversation string           `json:"conversation,omitempty"`
	Turn         string           `json:"turn,omitempty"`
	Round        uint32           `json:"round,omitempty"`
	Class        string           `json:"class,omitempty"`
	Admission    string           `json:"admission"`
	Rule         string           `json:"rule,omitempty"`
	Refusal      string           `json:"refusal,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	QueuedNs     int64            `json:"queued_ns,omitempty"`
	Usage        map[string]int64 `json:"usage,omitempty"`
	// Failed says the provider call failed; its usage is what it reported.
	Failed bool `json:"failed,omitempty"`
}

// wireThresholdEvent is the event on llm.event.threshold.
type wireThresholdEvent struct {
	V           uint8  `json:"v"`
	Rule        string `json:"rule"`
	AccountKind string `json:"account_kind"`
	AccountKey  string `json:"account_key"`
	Quantity    string `json:"quantity"`
	Used        int64  `json:"used"`
	Limit       int64  `json:"limit"`
	Percent     uint8  `json:"percent"`
}

func unixNs(t time.Time) (ns int64) {
	if !t.IsZero() {
		ns = t.UnixNano()
	}
	return
}

func timeOfNs(ns int64) (t time.Time) {
	if ns != 0 {
		t = time.Unix(0, ns).UTC()
	}
	return
}

func wireOfRule(r ration.Rule) (w wireRule) {
	w = wireRule{Id: r.Id, SelectKind: r.Select.Kind.String(), SelectKey: r.Select.Key, Kind: r.Kind.String(),
		Quantity: string(r.Quantity), Limit: r.Limit, WindowNs: int64(r.Window), Aligned: r.Aligned,
		SoftPercent: r.SoftPercent, Raise: r.Raise, RaiseUntil: unixNs(r.RaiseUntil), Until: unixNs(r.Until), Reason: r.Reason,
		Author: r.Author, SetAt: unixNs(r.SetAt)}
	return
}

func ruleOfWire(w wireRule) (r ration.Rule, err error) {
	r = ration.Rule{Id: w.Id, Select: ration.Selector{Key: w.SelectKey}, Quantity: ration.QuantityE(w.Quantity),
		Limit: w.Limit, Window: time.Duration(w.WindowNs), Aligned: w.Aligned, SoftPercent: w.SoftPercent,
		Raise: w.Raise, RaiseUntil: timeOfNs(w.RaiseUntil), Until: timeOfNs(w.Until), Reason: w.Reason, Author: w.Author, SetAt: timeOfNs(w.SetAt)}
	if r.Select.Kind, err = ration.ParseAccountKind(w.SelectKind); err != nil {
		return
	}
	r.Kind, err = ration.ParseRuleKind(w.Kind)
	return
}

func wireOfStates(states []ration.RuleState) (out []wireRuleState) {
	out = make([]wireRuleState, 0, len(states))
	for _, st := range states {
		out = append(out, wireRuleState{Rule: wireOfRule(st.Rule), AccountKind: st.Account.Kind.String(), AccountKey: st.Account.Key,
			Used: st.Used, Limit: st.Limit, Inflight: st.Inflight})
	}
	return
}

func statesOfWire(ws []wireRuleState) (out []ration.RuleState, err error) {
	out = make([]ration.RuleState, 0, len(ws))
	for _, w := range ws {
		st := ration.RuleState{Used: w.Used, Limit: w.Limit, Inflight: w.Inflight, Account: ration.Account{Key: w.AccountKey}}
		if st.Rule, err = ruleOfWire(w.Rule); err != nil {
			return
		}
		if st.Account.Kind, err = ration.ParseAccountKind(w.AccountKind); err != nil {
			return
		}
		out = append(out, st)
	}
	return
}

func wireOfRemaining(rs []ration.Remaining) (out []wireRemaining) {
	for _, r := range rs {
		out = append(out, wireRemaining{Rule: r.Rule, AccountKind: r.Account.Kind.String(), AccountKey: r.Account.Key,
			Quantity: string(r.Quantity), Limit: r.Limit, Remaining: r.Remaining, ResetAt: unixNs(r.ResetAt)})
	}
	return
}

func remainingOfWire(ws []wireRemaining) (out []ration.Remaining) {
	for _, w := range ws {
		r := ration.Remaining{Rule: w.Rule, Account: ration.Account{Key: w.AccountKey}, Quantity: ration.QuantityE(w.Quantity),
			Limit: w.Limit, Remaining: w.Remaining, ResetAt: timeOfNs(w.ResetAt)}
		r.Account.Kind, _ = ration.ParseAccountKind(w.AccountKind)
		out = append(out, r)
	}
	return
}

func wireOfUsage(u ration.Usage) (m map[string]int64) {
	if len(u) == 0 {
		return
	}
	m = make(map[string]int64, len(u))
	for q, v := range u {
		m[string(q)] = v
	}
	return
}
