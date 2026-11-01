package trailviewer

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

// The probe (ADR-0350 §SD3, proposed): SQL over the trail marts composed
// from a case, a window, the filters and a keyset cursor on (ts, id),
// newest first, under a row cap, and run through the host's executor with
// the viewer's call identity on the context, so the executor's log_comment
// stamp names the viewer and its reads land in the trail it shows.
//
// Columns are selected by their physical names, read from the composer's
// View.Columns and aliased to plain names; nothing here spells a minted
// name. Every read is bounded by the window or, for the per-row reads, by
// the look-back.

// arrowSettings make strings arrive as strings, as the generated stores
// ask.
const arrowSettings = " SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"

// Purpose is the claim the viewer's reads carry in their log_comment stamp.
const Purpose = "trailviewer"

// refConversation is the reference type an audit event names a
// conversation by (the attention writer's).
const refConversation = "conversation"

// facetLimit bounds the values one facet lists.
const facetLimit = 50

// contextRows is how many rows before and after the selected one the
// context sub-pane shows.
const contextRows = 5

// gapRowsLimit bounds the audit-gap rows a previous-state read checks.
const gapRowsLimit = 16

// marts is the composed views, by name, and the database they live in.
type marts struct {
	db    string
	views map[string]trailviews.View
}

var (
	martsOnce sync.Once
	martsErr  error
	martsBase map[string]trailviews.View
)

// loadMarts composes the views once per process; the composition is pure
// and its column names are what every statement selects by.
func loadMarts(db string) (m *marts, err error) {
	martsOnce.Do(func() {
		views, cerr := trailviews.Compose("", "")
		if cerr != nil {
			martsErr = cerr
			return
		}
		martsBase = make(map[string]trailviews.View, len(views))
		for _, v := range views {
			martsBase[v.Name] = v
		}
	})
	if martsErr != nil {
		return nil, eh.Errorf("trailviewer: compose the trail views: %w", martsErr)
	}
	return &marts{db: db, views: martsBase}, nil
}

// col is the quoted physical name of a view's column, by logical name. A
// name the view does not have panics: the statements are fixed, and a
// mart rename must break the build's tests loudly, not a read quietly.
func (inst *marts) col(view string, name string) (q string) {
	v, ok := inst.views[view]
	if !ok {
		panic("trailviewer: no view " + view)
	}
	for _, c := range v.Columns {
		if c.Name == name {
			return `"` + c.Physical + `"`
		}
	}
	panic("trailviewer: view " + view + " has no column " + name)
}

func (inst *marts) table(view string) string { return inst.db + "." + view }

// selectList renders name AS alias for each logical name, the alias the
// name with dashes as underscores.
func (inst *marts) selectList(view string, names []string) (s string) {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, inst.col(view, n)+" AS "+aliasOf(n))
	}
	return strings.Join(parts, ", ")
}

func aliasOf(name string) string { return strings.ReplaceAll(name, "-", "_") }

// sqlString renders s as a ClickHouse string literal.
func sqlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

func sqlStrings(vs []string) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, sqlString(v))
	}
	return strings.Join(parts, ", ")
}

// sqlTime renders t as a DateTime64(9, 'UTC') value.
func sqlTime(t time.Time) string {
	return "fromUnixTimestamp64Nano(toInt64(" + strconv.FormatInt(t.UnixNano(), 10) + "), 'UTC')"
}

// timelineNames are the timeline columns a row carries, in select order.
var timelineNames = []string{"id", "kind", "run", "app", "instance", "conversation", "turn", "round", "task", "task-epoch", "task-call",
	"ref", "seq", "parent-ref", "status", "subject", "headline", "text", "confined", "tainted", "tokens-in", "tokens-out", "elapsed-ms"}

// Row is one timeline row as the viewer holds it, with the audit event's
// own columns when the row is one.
type Row struct {
	Id           uint64 `ch:"id"`
	TsNs         int64  `ch:"ts_ns"`
	Kind         string `ch:"kind"`
	Run          string `ch:"run"`
	App          string `ch:"app"`
	Instance     uint64 `ch:"instance"`
	Conversation string `ch:"conversation"`
	Turn         string `ch:"turn"`
	Round        uint32 `ch:"round"`
	Task         string `ch:"task"`
	TaskEpoch    uint64 `ch:"task_epoch"`
	TaskCall     string `ch:"task_call"`
	Ref          string `ch:"ref"`
	Seq          uint32 `ch:"seq"`
	ParentRef    string `ch:"parent_ref"`
	Status       string `ch:"status"`
	Subject      string `ch:"subject"`
	Headline     string `ch:"headline"`
	Text         string `ch:"text"`
	Confined     bool   `ch:"confined"`
	Tainted      bool   `ch:"tainted"`
	TokensIn     uint64 `ch:"tokens_in"`
	TokensOut    uint64 `ch:"tokens_out"`
	ElapsedMs    uint64 `ch:"elapsed_ms"`

	// Audit is set for an auditEvent row once its event is read.
	Audit *AuditDetail `ch:"-"`
}

// Ts is the row's timestamp.
func (inst *Row) Ts() time.Time { return time.Unix(0, inst.TsNs).UTC() }

// kindAuditEvent and kindAgentGrant are the timeline's kind labels the
// viewer treats apart.
const (
	kindAuditEvent = "auditEvent"
	kindAgentGrant = "agentGrant"
)

// auditNames are the audit-event columns a detail carries.
var auditNames = []string{"id", "domain", "action", "outcome", "principal", "principal-by", "subject", "entity",
	"run", "ref-types", "ref-values", "attr-keys", "attr-values"}

// AuditDetail is an audit event's own columns.
type AuditDetail struct {
	Id          uint64   `ch:"id"`
	TsNs        int64    `ch:"ts_ns"`
	Domain      string   `ch:"domain"`
	Action      string   `ch:"action"`
	Outcome     string   `ch:"outcome"`
	Principal   string   `ch:"principal"`
	PrincipalBy string   `ch:"principal_by"`
	Subject     uint64   `ch:"subject"`
	Entity      string   `ch:"entity"`
	Run         string   `ch:"run"`
	RefTypes    []string `ch:"ref_types"`
	RefValues   []string `ch:"ref_values"`
	AttrKeys    []string `ch:"attr_keys"`
	AttrValues  []string `ch:"attr_values"`
	// From and FromBy are TRAIL_ATTR's reads of the `from` claim.
	From   string `ch:"claim_from"`
	FromBy string `ch:"claim_from_by"`
}

// Ts is the event's timestamp.
func (inst *AuditDetail) Ts() time.Time { return time.Unix(0, inst.TsNs).UTC() }

// Attr is the value at key, as TRAIL_ATTR reads it.
func (inst *AuditDetail) Attr(key string) (v string, ok bool) {
	i := slices.Index(inst.AttrKeys, key)
	if i < 0 || i >= len(inst.AttrValues) {
		return
	}
	return inst.AttrValues[i], true
}

// grantNames are the grant columns the previous-state diff compares.
var grantNames = []string{"id", "task", "event", "request", "plan", "plan-digest", "entries", "launches", "destinations",
	"calls-budget", "deadline-ms", "decided-by", "reason"}

// GrantDetail is an agent grant row's own columns.
type GrantDetail struct {
	Id           uint64   `ch:"id"`
	TsNs         int64    `ch:"ts_ns"`
	Task         string   `ch:"task"`
	Event        string   `ch:"event"`
	Request      string   `ch:"request"`
	Plan         string   `ch:"plan"`
	PlanDigest   string   `ch:"plan_digest"`
	Entries      []string `ch:"entries"`
	Launches     []string `ch:"launches"`
	Destinations []string `ch:"destinations"`
	CallsBudget  uint32   `ch:"calls_budget"`
	DeadlineMs   int64    `ch:"deadline_ms"`
	DecidedBy    string   `ch:"decided_by"`
	Reason       []string `ch:"reason"`
}

// fields renders the grant's columns as attribute pairs, for the diff.
func (inst *GrantDetail) fields() (keys []string, values []string) {
	add := func(k, v string) { keys, values = append(keys, k), append(values, v) }
	add("event", inst.Event)
	add("request", inst.Request)
	add("plan", inst.Plan)
	add("plan-digest", inst.PlanDigest)
	add("entries", strings.Join(inst.Entries, ", "))
	add("launches", strings.Join(inst.Launches, ", "))
	add("destinations", strings.Join(inst.Destinations, ", "))
	add("calls-budget", strconv.FormatUint(uint64(inst.CallsBudget), 10))
	add("deadline-ms", strconv.FormatInt(inst.DeadlineMs, 10))
	add("decided-by", inst.DecidedBy)
	add("reason", strings.Join(inst.Reason, " | "))
	return
}

// Cursor is a keyset position: rows strictly older than (Ts, Id).
type Cursor struct {
	TsNs int64
	Id   uint64
}

// listQuery is one read of the table.
type listQuery struct {
	Case     Case
	From, To time.Time
	Filter   Filter
	Cursor   *Cursor
	Limit    int
}

// windowPred bounds a view's ts to [from, to).
func (inst *marts) windowPred(view string, from, to time.Time) string {
	ts := inst.col(view, "ts")
	return ts + " >= " + sqlTime(from) + " AND " + ts + " < " + sqlTime(to)
}

// auditIds is a semi-join over the audit events of the window.
func (inst *marts) auditIds(from, to time.Time, pred string) string {
	return inst.col(trailviews.ViewTimeline, "id") + " IN (SELECT " + inst.col(trailviews.ViewAuditEvents, "id") +
		" FROM " + inst.table(trailviews.ViewAuditEvents) + " WHERE " + inst.windowPred(trailviews.ViewAuditEvents, from, to) +
		" AND " + pred + ")"
}

// casePred selects the case's rows of view, which is the timeline or the
// audit-event mart. The subject, principal and entity cases read the audit
// events' own columns: over the timeline they are a semi-join on the
// window's audit events, whose ids are the timeline's (both are 1:1 with
// facts rows). Egress destinations semi-join the egress mart; over the
// audit mart that case selects nothing. A conversation selects its agentic
// rows and the audit events that reference it.
func (inst *marts) casePred(view string, c Case, from, to time.Time) (pred string) {
	tl := view == trailviews.ViewTimeline
	col := func(n string) string { return inst.col(view, n) }
	ae := func(n string) string { return inst.col(trailviews.ViewAuditEvents, n) }
	v := strings.TrimSpace(c.Value)
	switch c.Kind {
	case CaseRun:
		pred = col("run") + " = " + sqlString(v)
	case CaseWindow:
		run, instance, _ := c.window()
		pred = col("run") + " = " + sqlString(run) + " AND " + col("instance") + " = " + strconv.FormatUint(instance, 10)
	case CaseApp:
		pred = col("app") + " = " + sqlString(v)
	case CaseTask:
		pred = col("task") + " = " + sqlString(v)
	case CaseConversation:
		refs := "has(" + trailviews.FuncRefs + "(" + ae("ref-types") + ", " + ae("ref-values") + ", " + sqlString(refConversation) + "), " + sqlString(v) + ")"
		if tl {
			pred = "(" + col("conversation") + " = " + sqlString(v) + " OR " + inst.auditIds(from, to, refs) + ")"
		} else {
			pred = "(" + col("conversation") + " = " + sqlString(v) + " OR " + refs + ")"
		}
	case CaseSubject, CasePrincipal, CaseEntity:
		var p string
		switch c.Kind {
		case CaseSubject:
			n, _ := strconv.ParseUint(v, 10, 64)
			p = ae("subject") + " = " + strconv.FormatUint(n, 10)
		case CasePrincipal:
			p = ae("principal") + " = " + sqlString(v)
		default:
			p = ae("entity") + " = " + sqlString(v)
		}
		if tl {
			pred = inst.auditIds(from, to, p)
		} else {
			pred = p
		}
	case CaseDestination:
		if !tl {
			return "0"
		}
		hf := trailviews.ViewHttpFetches
		pred = inst.col(view, "id") + " IN (SELECT " + inst.col(hf, "id") + " FROM " + inst.table(hf) + " WHERE " +
			inst.windowPred(hf, from, to) + " AND " + inst.col(hf, "destination") + " = " + sqlString(v) + ")"
	default:
		pred = "1"
	}
	return
}

// filterPred narrows the timeline by the filter's terms.
func (inst *marts) filterPred(f Filter, from, to time.Time) (preds []string) {
	tl := trailviews.ViewTimeline
	for _, facet := range AllFacets {
		incl, excl := f.split(facet)
		for i, vs := range [][]string{incl, excl} {
			if len(vs) == 0 {
				continue
			}
			not := i == 1
			var p string
			switch facet {
			case FacetKind, FacetStatus, FacetApp:
				p = inst.col(tl, facet.String()) + " IN (" + sqlStrings(vs) + ")"
			case FacetDomainAction:
				// The timeline's subject of an audit event is domain.action.
				p = "(" + inst.col(tl, "kind") + " = " + sqlString(kindAuditEvent) + " AND " + inst.col(tl, "subject") + " IN (" + sqlStrings(vs) + "))"
			case FacetPrincipal:
				p = inst.auditIds(from, to, inst.col(trailviews.ViewAuditEvents, "principal")+" IN ("+sqlStrings(vs)+")")
			}
			if not {
				p = "NOT " + p
			}
			preds = append(preds, p)
		}
	}
	return
}

// listSQL is the table's read: the case's rows in the window that pass
// the filter, older than the cursor, newest first, at most Limit.
func (inst *marts) listSQL(q listQuery) (sql string) {
	tl := trailviews.ViewTimeline
	where := []string{inst.windowPred(tl, q.From, q.To), inst.casePred(tl, q.Case, q.From, q.To)}
	where = append(where, inst.filterPred(q.Filter, q.From, q.To)...)
	if q.Cursor != nil {
		where = append(where, "("+inst.col(tl, "ts")+", "+inst.col(tl, "id")+") < ("+sqlTime(time.Unix(0, q.Cursor.TsNs))+", toUInt64("+strconv.FormatUint(q.Cursor.Id, 10)+"))")
	}
	sql = "SELECT toUnixTimestamp64Nano(" + inst.col(tl, "ts") + ") AS ts_ns, " + inst.selectList(tl, timelineNames) +
		" FROM " + inst.table(tl) + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY " + inst.col(tl, "ts") + " DESC, " + inst.col(tl, "id") + " DESC LIMIT " + strconv.Itoa(q.Limit)
	return
}

// auditSelect is the audit detail's columns with TRAIL_ATTR's from and
// from-by.
func (inst *marts) auditSelect() string {
	ae := trailviews.ViewAuditEvents
	attr := func(key string) string {
		return trailviews.FuncAttr + "(" + inst.col(ae, "attr-keys") + ", " + inst.col(ae, "attr-values") + ", " + sqlString(key) + ")"
	}
	return "toUnixTimestamp64Nano(" + inst.col(ae, "ts") + ") AS ts_ns, " + inst.selectList(ae, auditNames) +
		", " + attr(audit.AttrFrom) + " AS claim_from, " + attr(audit.AttrFromBy) + " AS claim_from_by"
}

// auditByIdsSQL reads the audit events among ids, in [from, to].
func (inst *marts) auditByIdsSQL(ids []uint64, from, to time.Time) (sql string) {
	ae := trailviews.ViewAuditEvents
	ts := inst.col(ae, "ts")
	list := make([]string, 0, len(ids))
	for _, id := range ids {
		list = append(list, strconv.FormatUint(id, 10))
	}
	return "SELECT " + inst.auditSelect() + " FROM " + inst.table(ae) + " WHERE " + ts + " >= " + sqlTime(from) + " AND " + ts + " <= " + sqlTime(to) +
		" AND " + inst.col(ae, "id") + " IN (" + strings.Join(list, ", ") + ")"
}

// stripSQL counts the case's filtered rows per bucket of step and per
// kind or status.
func (inst *marts) stripSQL(q listQuery, step time.Duration, by StripByE) (sql string) {
	tl := trailviews.ViewTimeline
	where := []string{inst.windowPred(tl, q.From, q.To), inst.casePred(tl, q.Case, q.From, q.To)}
	where = append(where, inst.filterPred(q.Filter, q.From, q.To)...)
	key := "kind"
	if by == StripByStatus {
		key = "status"
	}
	return "SELECT toInt64(toUnixTimestamp(toStartOfInterval(" + inst.col(tl, "ts") + ", INTERVAL " + strconv.FormatInt(int64(step/time.Second), 10) + " SECOND))) AS bucket, " +
		inst.col(tl, key) + " AS key, count() AS n FROM " + inst.table(tl) + " WHERE " + strings.Join(where, " AND ") +
		" GROUP BY bucket, key ORDER BY bucket, key"
}

// facetSQL counts the case's rows in the window by one facet's value: the
// timeline's own column for kind, status and app, the audit events' for
// domain.action and principal.
func (inst *marts) facetSQL(f FacetE, c Case, from, to time.Time) (sql string) {
	view, value := trailviews.ViewTimeline, ""
	switch f {
	case FacetKind, FacetStatus, FacetApp:
		value = inst.col(view, f.String())
	case FacetDomainAction:
		view = trailviews.ViewAuditEvents
		value = "concat(" + inst.col(view, "domain") + ", '.', " + inst.col(view, "action") + ")"
	case FacetPrincipal:
		view = trailviews.ViewAuditEvents
		value = inst.col(view, "principal")
	}
	return "SELECT " + value + " AS value, count() AS n FROM " + inst.table(view) + " WHERE " + inst.windowPred(view, from, to) +
		" AND " + inst.casePred(view, c, from, to) + " GROUP BY value ORDER BY n DESC, value LIMIT " + strconv.Itoa(facetLimit)
}

// contextSQL is the rows of r's writer run before (older) or after r,
// regardless of the filter, nearest first, within the look-back.
func (inst *marts) contextSQL(r *Row, after bool, look time.Duration) (sql string) {
	tl := trailviews.ViewTimeline
	ts, id := inst.col(tl, "ts"), inst.col(tl, "id")
	at := time.Unix(0, r.TsNs)
	op, dir, bound := "<", "DESC", ts+" >= "+sqlTime(at.Add(-look))
	if after {
		op, dir, bound = ">", "ASC", ts+" <= "+sqlTime(at.Add(look))
	}
	return "SELECT toUnixTimestamp64Nano(" + ts + ") AS ts_ns, " + inst.selectList(tl, timelineNames) + " FROM " + inst.table(tl) +
		" WHERE " + inst.col(tl, "run") + " = " + sqlString(r.Run) + " AND " + bound +
		" AND (" + ts + ", " + id + ") " + op + " (" + sqlTime(at) + ", toUInt64(" + strconv.FormatUint(r.Id, 10) + "))" +
		" ORDER BY " + ts + " " + dir + ", " + id + " " + dir + " LIMIT " + strconv.Itoa(contextRows)
}

// previousAuditSQL is SD5's read: the previous event of the same
// (domain, entity), (ts, id) below the row's, newest first, one row,
// within the look-back.
func (inst *marts) previousAuditSQL(a *AuditDetail, look time.Duration) (sql string) {
	ae := trailviews.ViewAuditEvents
	ts, id := inst.col(ae, "ts"), inst.col(ae, "id")
	at := time.Unix(0, a.TsNs)
	return "SELECT " + inst.auditSelect() + " FROM " + inst.table(ae) +
		" WHERE " + inst.col(ae, "domain") + " = " + sqlString(a.Domain) + " AND " + inst.col(ae, "entity") + " = " + sqlString(a.Entity) +
		" AND " + ts + " >= " + sqlTime(at.Add(-look)) +
		" AND (" + ts + ", " + id + ") < (" + sqlTime(at) + ", toUInt64(" + strconv.FormatUint(a.Id, 10) + "))" +
		" ORDER BY " + ts + " DESC, " + id + " DESC LIMIT 1"
}

// gapsSQL lists the run's audit-gap rows written at or after since and no
// later than until: a gap row is written after the rows it reports.
func (inst *marts) gapsSQL(run string, since, until time.Time) (sql string) {
	ae := trailviews.ViewAuditEvents
	ts := inst.col(ae, "ts")
	return "SELECT " + inst.auditSelect() + " FROM " + inst.table(ae) +
		" WHERE " + inst.col(ae, "domain") + " = " + sqlString(audit.TrailDomain) + " AND " + inst.col(ae, "action") + " = " + sqlString(audit.ActionAuditGap) +
		" AND " + inst.col(ae, "run") + " = " + sqlString(run) +
		" AND " + ts + " >= " + sqlTime(since) + " AND " + ts + " <= " + sqlTime(until) +
		" ORDER BY " + ts + " LIMIT " + strconv.Itoa(gapRowsLimit)
}

// grantPairSQL reads the grant row r and the grant row of the same task
// before it, within the look-back: the newest two at or below r's (ts, id).
func (inst *marts) grantPairSQL(r *Row, look time.Duration) (sql string) {
	g := trailviews.ViewAgentGrants
	ts, id := inst.col(g, "ts"), inst.col(g, "id")
	at := time.Unix(0, r.TsNs)
	return "SELECT toUnixTimestamp64Nano(" + ts + ") AS ts_ns, " + inst.selectList(g, grantNames) + " FROM " + inst.table(g) +
		" WHERE " + inst.col(g, "task") + " = " + sqlString(r.Task) + " AND " + ts + " >= " + sqlTime(at.Add(-look)) +
		" AND (" + ts + ", " + id + ") <= (" + sqlTime(at) + ", toUInt64(" + strconv.FormatUint(r.Id, 10) + "))" +
		" ORDER BY " + ts + " DESC, " + id + " DESC LIMIT 2"
}

// probe runs statements through the source's executor under the viewer's
// call identity.
type probe struct {
	exec  recordstore.ExecutorI
	marts *marts
	ident callident.CallIdentity
}

func newProbe(src *Source, appId string, instance uint64) (p *probe, err error) {
	m, err := loadMarts(src.database())
	if err != nil {
		return
	}
	p = &probe{exec: src.Exec, marts: m, ident: callident.CallIdentity{
		Origin: callident.Origin{Run: src.run(), App: appId, Instance: instance},
		Claims: callident.Claims{Purpose: Purpose},
	}}
	return
}

// queryRows runs sql and decodes every batch into T.
func queryRows[T any](ctx context.Context, p *probe, sql string) (rows []T, err error) {
	ctx = callident.WithCallIdentity(ctx, p.ident)
	for rec, rerr := range p.exec.QueryArrow(ctx, sql+arrowSettings) {
		if rerr != nil {
			return nil, eb.Build().Str("sql", cutText(sql, 400)).Errorf("trailviewer: read the marts: %w", rerr)
		}
		var batch []T
		batch, err = chrows.DecodeRows[T](rec)
		rec.Release()
		if err != nil {
			return nil, eh.Errorf("trailviewer: decode the marts: %w", err)
		}
		rows = append(rows, batch...)
	}
	return
}

// listRows reads one page of the table and the audit events among it.
func (inst *probe) listRows(ctx context.Context, q listQuery) (rows []Row, err error) {
	rows, err = queryRows[Row](ctx, inst, inst.marts.listSQL(q))
	if err != nil || len(rows) == 0 {
		return
	}
	err = inst.attachAudit(ctx, rows)
	return
}

// attachAudit reads the audit events among rows in one statement, bounded
// by the rows' own time span, and hangs each on its row.
func (inst *probe) attachAudit(ctx context.Context, rows []Row) (err error) {
	var ids []uint64
	lo, hi := int64(0), int64(0)
	for i := range rows {
		if rows[i].Kind != kindAuditEvent {
			continue
		}
		if len(ids) == 0 || rows[i].TsNs < lo {
			lo = rows[i].TsNs
		}
		if len(ids) == 0 || rows[i].TsNs > hi {
			hi = rows[i].TsNs
		}
		ids = append(ids, rows[i].Id)
	}
	if len(ids) == 0 {
		return
	}
	details, err := queryRows[AuditDetail](ctx, inst, inst.marts.auditByIdsSQL(ids, time.Unix(0, lo), time.Unix(0, hi)))
	if err != nil {
		return
	}
	byId := make(map[uint64]*AuditDetail, len(details))
	for i := range details {
		byId[details[i].Id] = &details[i]
	}
	for i := range rows {
		if a, ok := byId[rows[i].Id]; ok {
			rows[i].Audit = a
		}
	}
	return
}

// Bucket is one bar of the strip: a bucket's start, a key (a kind or a
// status) and the rows it counts.
type Bucket struct {
	Bucket int64  `ch:"bucket"`
	Key    string `ch:"key"`
	N      uint64 `ch:"n"`
}

// FacetValue is one value of a facet with its count.
type FacetValue struct {
	Value string `ch:"value" desc:"the value; empty is the column's empty value"`
	N     uint64 `ch:"n" desc:"rows in the case and window with the value"`
}

func (inst *probe) strip(ctx context.Context, q listQuery, step time.Duration, by StripByE) (buckets []Bucket, err error) {
	return queryRows[Bucket](ctx, inst, inst.marts.stripSQL(q, step, by))
}

func (inst *probe) facet(ctx context.Context, f FacetE, c Case, from, to time.Time) (vs []FacetValue, err error) {
	return queryRows[FacetValue](ctx, inst, inst.marts.facetSQL(f, c, from, to))
}

// contextOf reads the rows around r in its run, oldest first.
func (inst *probe) contextOf(ctx context.Context, r *Row, look time.Duration) (before []Row, after []Row, err error) {
	before, err = queryRows[Row](ctx, inst, inst.marts.contextSQL(r, false, look))
	if err != nil {
		return
	}
	slices.Reverse(before)
	after, err = queryRows[Row](ctx, inst, inst.marts.contextSQL(r, true, look))
	if err != nil {
		return
	}
	// The audit columns, so a context row selected next shows them.
	if err = inst.attachAudit(ctx, before); err != nil {
		return
	}
	err = inst.attachAudit(ctx, after)
	return
}

// stepFor is the strip's bucket for a window split into about n buckets:
// the smallest step of the ladder that keeps the count at or under n.
func stepFor(span time.Duration, n int) (step time.Duration) {
	ladder := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
		time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
		time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour}
	n = max(n, 1)
	for _, s := range ladder {
		if span/s <= time.Duration(n) {
			return s
		}
	}
	step = ladder[len(ladder)-1]
	for span/step > time.Duration(n) {
		step *= 2
	}
	return
}

// stripBuckets is how many buckets a strip of width px gets: one per 8 px,
// within 60..120.
func stripBuckets(px float32) (n int) {
	return min(max(int(px/8), 60), 120)
}

func cutText(s string, n int) (out string) {
	out = s
	if len([]rune(out)) > n {
		out = string([]rune(out)[:n]) + "…"
	}
	return
}

// describeCase is the case and window in one line, for diagnostics.
func describeCase(c Case, from, to time.Time) string {
	return fmt.Sprintf("%s, %s – %s", c, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
}
