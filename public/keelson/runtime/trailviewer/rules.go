package trailviewer

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// The exception rules (ADR-0350 §SD7, proposed). A rule is data: a name, a
// one-line description, and either a SQL predicate over dm_trail_timeline
// or, for the two rules that need what the window already holds, a
// sentence saying what it tests. The rules produce a reading list, not a
// verdict and not an action; the window shows each with its predicate.
//
// A predicate is written with placeholders the probe resolves through the
// composer's column lookup, so a mart rename fails the golden instead of a
// read: {name} is a timeline column, {ae.name} an audit-event column, {ae}
// the audit-event mart, and {from}, {to} and {since} the window's start,
// its end and its start less the look-back. The Rules pane shows the same
// text with the placeholders as the logical names.

// The rules' names.
const (
	RuleOutcome      = "outcome"
	RuleModelRefused = "model-refused"
	RuleGrantWidened = "grant-widened"
	RuleTrailRow     = "trail-row"
	RuleUnsealed     = "unsealed"
	RuleNovelAction  = "novel-action"
	RuleClaimDiffers = "claim-differs"
)

// Rule is one exception rule.
type Rule struct {
	Name        string
	Description string
	// Predicate is the SQL condition over the timeline, in placeholders;
	// empty for a rule computed from what the window holds.
	Predicate string
	// Reason is the SQL expression of a raised row's one-line reason.
	Reason string
	// Held says what a rule without a predicate tests, and from what.
	Held string
}

// FromHeld reports whether the rule is computed from what the window holds
// rather than read with SQL.
func (inst Rule) FromHeld() bool { return inst.Predicate == "" }

// PredicateText is the rule's test as the Rules pane shows it.
func (inst Rule) PredicateText() string {
	if inst.FromHeld() {
		return inst.Held
	}
	return placeholderRe.ReplaceAllStringFunc(inst.Predicate, func(s string) string {
		name := s[1 : len(s)-1]
		switch name {
		case "ae":
			return trailviews.ViewAuditEvents
		case "from":
			return "window_start"
		case "to":
			return "window_end"
		case "since":
			return "(window_start - lookback)"
		}
		if c, ok := strings.CutPrefix(name, "ae."); ok {
			return c
		}
		return name
	})
}

var placeholderRe = regexp.MustCompile(`\{[a-z][a-z0-9.\-]*\}`)

func sqlList(vs ...string) string { return "(" + sqlStrings(vs) + ")" }

// Rules are the viewer's exception rules, in the order ADR-0350 §SD7 lists
// them.
var Rules = []Rule{
	{Name: RuleOutcome, Description: "a denied, failed, timeout or error outcome",
		Predicate: "{status} IN " + sqlList(audit.OutcomeDenied, audit.OutcomeFailed, audit.OutcomeTimeout, audit.OutcomeError),
		Reason:    "concat({kind}, ' status ', {status})"},
	{Name: RuleModelRefused, Description: "a model call the provider refused",
		Predicate: "{kind} = 'llmCall' AND {status} = 'refused'",
		Reason:    "concat('model call ', {subject}, ' refused')"},
	{Name: RuleGrantWidened, Description: "a grant widened during its task",
		Predicate: "{kind} = " + sqlString(kindAgentGrant) + " AND {status} = " + sqlString(trail.GrantEventWidened),
		Reason:    "concat('grant of task ', {task}, ' widened')"},
	{Name: RuleTrailRow, Description: "the trail's own rows: a gap, an invalid row, the recorder's configuration, a ditch",
		Predicate: "{kind} = " + sqlString(kindAuditEvent) + " AND {subject} IN " + sqlList(
			audit.TrailDomain+"."+audit.ActionAuditGap, audit.TrailDomain+"."+audit.ActionAuditInvalid,
			audit.TrailDomain+"."+audit.ActionAuditConfig, audit.TrailDomain+"."+audit.ActionAuditDitch),
		Reason: "concat({subject}, ' row')"},
	{Name: RuleUnsealed, Description: "a row of a writer run whose chain is broken, forks, or has rows no seal names",
		Held: "the row's writer run reads broken, fork or gap on the evidence line; computed from the verdict lines the window holds"},
	{Name: RuleNovelAction, Description: "a domain.action not seen in the look-back before the window",
		Predicate: "{kind} = " + sqlString(kindAuditEvent) + " AND {id} IN (SELECT {ae.id} FROM {ae} WHERE {ae.ts} >= {from} AND {ae.ts} < {to}" +
			" AND ({ae.domain}, {ae.action}) NOT IN (SELECT DISTINCT {ae.domain}, {ae.action} FROM {ae} WHERE {ae.ts} < {from} AND {ae.ts} >= {since}))",
		Reason: "concat({subject}, ' not seen in the look-back before the window')"},
	{Name: RuleClaimDiffers, Description: "a from claim that differs from the previous event",
		Held: "the previous-state read of an audit event naming an entity with a from claim says differs; read for at most " +
			strconv.Itoa(claimRowsPerFill) + " new rows per refill"},
}

// ruleRank orders a row's exceptions as Rules lists the rules.
var ruleRank = func() map[string]int {
	m := make(map[string]int, len(Rules))
	for i, r := range Rules {
		m[r.Name] = i
	}
	return m
}()

// LookupRule returns the rule named name.
func LookupRule(name string) (r Rule, ok bool) {
	i, found := ruleRank[name]
	if !found {
		return
	}
	return Rules[i], true
}

// claimRowsPerFill bounds the previous-state reads one refill makes for
// the claim rule; rows judged once are not read again.
const claimRowsPerFill = 16

// maxJudged bounds the judged claims a window remembers.
const maxJudged = 20000

// expand resolves a rule's placeholders for the window [from, to) and its
// look-back.
func (inst *marts) expand(tmpl string, from, to, since time.Time) string {
	return placeholderRe.ReplaceAllStringFunc(tmpl, func(s string) string {
		name := s[1 : len(s)-1]
		switch name {
		case "ae":
			return inst.table(trailviews.ViewAuditEvents)
		case "from":
			return sqlTime(from)
		case "to":
			return sqlTime(to)
		case "since":
			return sqlTime(since)
		}
		if c, ok := strings.CutPrefix(name, "ae."); ok {
			return inst.col(trailviews.ViewAuditEvents, c)
		}
		return inst.col(trailviews.ViewTimeline, name)
	})
}

// rowsWhere is the table read's conditions: the window, the case, the
// filter, the cursor (rows older than it) and a lower bound (rows no older
// than it), so a rule and the reviews read cover the rows a read holds.
func (inst *marts) rowsWhere(q listQuery, lower *Cursor) (where []string) {
	tl := trailviews.ViewTimeline
	key := "(" + inst.col(tl, "ts") + ", " + inst.col(tl, "id") + ")"
	where = []string{inst.windowPred(tl, q.From, q.To), inst.casePred(tl, q.Case, q.From, q.To)}
	where = append(where, inst.filterPred(q.Filter, q.From, q.To)...)
	if q.Cursor != nil {
		where = append(where, key+" < ("+sqlTime(time.Unix(0, q.Cursor.TsNs))+", toUInt64("+strconv.FormatUint(q.Cursor.Id, 10)+"))")
	}
	if lower != nil {
		where = append(where, key+" >= ("+sqlTime(time.Unix(0, lower.TsNs))+", toUInt64("+strconv.FormatUint(lower.Id, 10)+"))")
	}
	return
}

// ruleSQL is one SQL rule's read: the rows of the read it raises, with
// the rule's name and reason.
func (inst *marts) ruleSQL(r Rule, q listQuery, lower *Cursor, look time.Duration) string {
	tl := trailviews.ViewTimeline
	expand := func(s string) string { return inst.expand(s, q.From, q.To, q.From.Add(-look)) }
	where := append(inst.rowsWhere(q, lower), "("+expand(r.Predicate)+")")
	return "SELECT " + inst.col(tl, "id") + " AS id, toUnixTimestamp64Nano(" + inst.col(tl, "ts") + ") AS ts_ns, " + sqlString(r.Name) + " AS rule, " +
		"toString(" + expand(r.Reason) + ") AS reason FROM " + inst.table(tl) + " WHERE " + strings.Join(where, " AND ")
}

// exceptionsSQL is every SQL rule's read in one statement, bounded by the
// read's row cap per rule.
func (inst *marts) exceptionsSQL(q listQuery, lower *Cursor, look time.Duration) string {
	var branches []string
	for _, r := range Rules {
		if !r.FromHeld() {
			branches = append(branches, inst.ruleSQL(r, q, lower, look))
		}
	}
	return "SELECT id, ts_ns, rule, reason FROM (" + strings.Join(branches, " UNION ALL ") + ") ORDER BY ts_ns DESC, id DESC, rule LIMIT " +
		strconv.Itoa(q.Limit*len(branches))
}

// reviewsSQL is the latest review mark of each row of the read, from
// dm_trail_reviews: a mark is written after the row it marks, so its time
// is at or after the window's start.
func (inst *marts) reviewsSQL(q listQuery, lower *Cursor) string {
	rv, tl := trailviews.ViewReviews, trailviews.ViewTimeline
	key := "(" + inst.col(rv, "ts") + ", " + inst.col(rv, "id") + ")"
	last := func(name string) string { return "argMax(" + inst.col(rv, name) + ", " + key + ")" }
	return "SELECT " + inst.col(rv, "trail-row") + " AS row_id, toUnixTimestamp64Nano(max(" + inst.col(rv, "ts") + ")) AS ts_ns, " +
		last("mark") + " AS mark, " + last("reason") + " AS reason, " + last("rule") + " AS rule, " + last("principal") + " AS principal, " +
		"toUInt64(count()) AS marks FROM " + inst.table(rv) + " WHERE " + inst.col(rv, "ts") + " >= " + sqlTime(q.From) +
		" AND " + inst.col(rv, "trail-row") + " IN (SELECT " + inst.col(tl, "id") + " FROM " + inst.table(tl) + " WHERE " +
		strings.Join(inst.rowsWhere(q, lower), " AND ") + ") GROUP BY row_id"
}

// asOfDomains bounds the domains an as-of read reports.
const asOfDomains = 16

// asOfSQL is SD5's as-of read: the entity's last event at or before
// bound, within the look-back, per domain. State is keyed by (domain,
// entity), as the previous-state read and agg_trail_audit_entities key
// it, and the entity case names no domain: one entity string in two
// domains is two states.
func (inst *marts) asOfSQL(entity string, bound time.Time, look time.Duration) string {
	ae := trailviews.ViewAuditEvents
	ts := inst.col(ae, "ts")
	return "SELECT " + inst.auditSelect() + " FROM " + inst.table(ae) + " WHERE " + inst.col(ae, "entity") + " = " + sqlString(entity) +
		" AND " + ts + " <= " + sqlTime(bound) + " AND " + ts + " >= " + sqlTime(bound.Add(-look)) +
		" ORDER BY " + ts + " DESC, " + inst.col(ae, "id") + " DESC LIMIT 1 BY " + inst.col(ae, "domain") + " LIMIT " + strconv.Itoa(asOfDomains)
}

// Exception is one row a rule raised, with the rule and its reason.
type Exception struct {
	Id     uint64 `ch:"id"`
	TsNs   int64  `ch:"ts_ns"`
	Rule   string `ch:"rule"`
	Reason string `ch:"reason"`
}

// Review is a row's latest review mark, as dm_trail_reviews lists it.
type Review struct {
	Id        uint64 `ch:"row_id"`
	TsNs      int64  `ch:"ts_ns"`
	Mark      string `ch:"mark"`
	Reason    string `ch:"reason"`
	Rule      string `ch:"rule"`
	Principal string `ch:"principal"`
	// Marks counts the row's marks; the others are the latest's.
	Marks uint64 `ch:"marks"`
}

// Ts is when the mark was written.
func (inst Review) Ts() time.Time { return time.Unix(0, inst.TsNs).UTC() }

// claimJudgment is the claim rule's reading of one row.
type claimJudgment struct {
	differs bool
	reason  string
}

// hasClaim reports whether a row is one the claim rule reads: an audit
// event naming an entity, with a from claim.
func hasClaim(r *Row) bool {
	if r.Kind != kindAuditEvent || r.Audit == nil || r.Audit.Entity == "" {
		return false
	}
	_, ok := r.Audit.Attr(audit.AttrFrom)
	return ok
}

// judgeClaims runs the previous-state read for up to claimRowsPerFill rows
// with a claim that judged does not hold, newest first.
func (inst *probe) judgeClaims(ctx context.Context, rows []Row, judged map[uint64]bool, look time.Duration) (out map[uint64]claimJudgment, err error) {
	out = map[uint64]claimJudgment{}
	for i := range rows {
		if len(out) == claimRowsPerFill {
			return
		}
		r := &rows[i]
		if !hasClaim(r) || judged[r.Id] {
			continue
		}
		var p *Previous
		p, err = inst.previousOf(ctx, r, look)
		if err != nil {
			return
		}
		j := claimJudgment{}
		if p != nil && p.Claim == ClaimDiffers {
			j = claimJudgment{differs: true, reason: p.Verdict}
		}
		out[r.Id] = j
	}
	return
}

// AsOf is the entity case's state as of the window's end (ADR-0350 §SD5),
// one per domain the entity has an event in.
type AsOf struct {
	Entity string
	Bound  time.Time
	// Events are each domain's last event at or before Bound within the
	// look-back, newest first; none were found when it is empty.
	Events []AuditDetail
}

// readAsOf is the as-of state for the entity case. It is always read: the
// held rows cover the window, and a domain whose last event is older than
// the window's start has its state there too.
func (inst *probe) readAsOf(ctx context.Context, q listQuery, look time.Duration) (a *AsOf, err error) {
	if q.Case.Kind != CaseEntity {
		return
	}
	entity := strings.TrimSpace(q.Case.Value)
	a = &AsOf{Entity: entity, Bound: q.To}
	a.Events, err = queryRows[AuditDetail](ctx, inst, inst.marts.asOfSQL(entity, q.To, look))
	return
}

// exceptionSet is the rules' reading of the held rows, the marks applied:
// a marked row's exceptions leave the open list.
type exceptionSet struct {
	// open is the unmarked rows' exceptions in the held rows' order, each
	// row's in the rules' order.
	open []Exception
	// byRow is every held row's exceptions, open or cleared.
	byRow   map[uint64][]Exception
	cleared int
}

// unsealedStates are the verdicts the unsealed rule raises a run's rows
// for.
func unsealed(l verdict.Line) bool {
	return l.State == verdict.StateBroken || l.State == verdict.StateFork || l.State == verdict.StateGap
}

// evaluateExceptions applies the rules to rows: the SQL rules' raised
// rows, the claim rule's judgments and the unsealed rule over the verdict
// lines; a row in marks has its exceptions cleared.
func evaluateExceptions(rows []Row, raised []Exception, claims map[uint64]claimJudgment, lines []verdict.Line, marks map[uint64]Review) (out exceptionSet) {
	out.byRow = map[uint64][]Exception{}
	byId := make(map[uint64][]Exception, len(raised))
	for _, e := range raised {
		byId[e.Id] = append(byId[e.Id], e)
	}
	lineOf := map[string]verdict.Line{}
	for _, l := range lines {
		lineOf[l.Run] = l
	}
	for i := range rows {
		r := &rows[i]
		var es []Exception
		for _, e := range byId[r.Id] {
			if _, known := ruleRank[e.Rule]; known && !hasRule(es, e.Rule) {
				es = append(es, e)
			}
		}
		if l, ok := lineOf[r.Run]; ok && unsealed(l) {
			es = append(es, Exception{Id: r.Id, TsNs: r.TsNs, Rule: RuleUnsealed, Reason: "writer run " + shortRun(r.Run) + " reads " + l.Label()})
		}
		if j, ok := claims[r.Id]; ok && j.differs {
			es = append(es, Exception{Id: r.Id, TsNs: r.TsNs, Rule: RuleClaimDiffers, Reason: j.reason})
		}
		if len(es) == 0 {
			continue
		}
		sortByRule(es)
		out.byRow[r.Id] = es
		if _, marked := marks[r.Id]; marked {
			out.cleared += len(es)
			continue
		}
		out.open = append(out.open, es...)
	}
	return
}

func hasRule(es []Exception, rule string) bool {
	for _, e := range es {
		if e.Rule == rule {
			return true
		}
	}
	return false
}

func sortByRule(es []Exception) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && ruleRank[es[j-1].Rule] > ruleRank[es[j].Rule]; j-- {
			es[j-1], es[j] = es[j], es[j-1]
		}
	}
}

// ruleNames is the rules of es, comma-separated.
func ruleNames(es []Exception) string {
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Rule)
	}
	return strings.Join(names, ",")
}
