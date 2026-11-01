package trailviewer

import (
	"strconv"
	"time"
)

// Compare (ADR-0350 §SD5, proposed): two rows the person picked, of any
// kind, diffed with the previous-state diff's field table over the
// timeline's columns and, where both are audit events, their own columns
// and attributes. The base is stated in words above the table, the older
// row on the left; it is never a toggle.

// Comparison is two rows in order and their field table.
type Comparison struct {
	Older, Newer Row
	// Base says which row is which, in words.
	Base   string
	Fields []FieldDiff
}

// compareFields is a row's fields as key/value lists for the diff: the
// timeline's columns, then an audit event's own and its attributes, which
// are named attr.<key> so the claim keys are diffed too. Empty values are
// left out, so a field one row lacks reads as added or removed.
func compareFields(r *Row, withAudit bool) (keys []string, values []string) {
	add := func(k, v string) {
		if v != "" {
			keys, values = append(keys, k), append(values, v)
		}
	}
	num := func(k string, v uint64) {
		if v != 0 {
			add(k, strconv.FormatUint(v, 10))
		}
	}
	add("kind", r.Kind)
	add("status", r.Status)
	add("subject", r.Subject)
	add("headline", r.Headline)
	add("run", r.Run)
	add("app", r.App)
	num("instance", r.Instance)
	add("conversation", r.Conversation)
	add("turn", r.Turn)
	num("round", uint64(r.Round))
	add("task", r.Task)
	num("task-epoch", r.TaskEpoch)
	add("task-call", r.TaskCall)
	add("ref", r.Ref)
	add("parent-ref", r.ParentRef)
	add("text", r.Text)
	if r.Confined {
		add("confined", "true")
	}
	if r.Tainted {
		add("tainted", "true")
	}
	num("tokens-in", r.TokensIn)
	num("tokens-out", r.TokensOut)
	num("elapsed-ms", r.ElapsedMs)
	if a := r.Audit; withAudit && a != nil {
		add("domain", a.Domain)
		add("action", a.Action)
		add("outcome", a.Outcome)
		add("principal", a.Principal)
		add("principal-by", a.PrincipalBy)
		num("data subject", a.Subject)
		add("entity", a.Entity)
		for i, k := range a.AttrKeys {
			if i < len(a.AttrValues) {
				add("attr."+k, a.AttrValues[i])
			}
		}
	}
	return
}

// older reports whether a is older than b in (ts, id).
func older(a, b *Row) bool {
	return a.TsNs < b.TsNs || (a.TsNs == b.TsNs && a.Id < b.Id)
}

// compareRows orders a and b, older first, and diffs them; format renders
// a time for the base sentence.
func compareRows(a, b Row, format func(t time.Time) string) (cmp Comparison) {
	if older(&b, &a) {
		a, b = b, a
	}
	cmp.Older, cmp.Newer = a, b
	both := a.Audit != nil && b.Audit != nil
	ok, ov := compareFields(&a, both)
	nk, nv := compareFields(&b, both)
	cmp.Fields = diffFields(ok, ov, nk, nv)
	cmp.Base = "Left is the older row, id " + strconv.FormatUint(a.Id, 10) + " (" + a.Kind + ") at " + format(a.Ts()) +
		"; right is the newer row, id " + strconv.FormatUint(b.Id, 10) + " (" + b.Kind + ") at " + format(b.Ts()) +
		". Change says how the newer row differs from the older."
	if !both {
		cmp.Base += " The audit columns and attributes are compared only when both rows are audit events."
	}
	return
}
