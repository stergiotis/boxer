package sessiondata

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// op is one request's work on a server session it holds: the caller, the
// dispatcher's record of its call, the data session and the endpoint as
// the caller named it.
type op struct {
	ctx context.Context
	c   Caller
	cc  option.Option[app.CallContext]
	s   *session
	srv *serverSession
	e   Endpoint
}

// stamp is the log_comment of the capability's statements: who it is and
// whose work the statement is.
func (inst *Service) stamp(s *session, cc option.Option[app.CallContext]) (st logcomment.Stamp) {
	st = logcomment.Stamp{App: string(ServiceAppId)}
	if s != nil && s.kind == sessionKindTask {
		st.Task = s.task
	}
	if cc.Has {
		st.Task, st.TaskEpoch, st.TaskCall = cc.Val.Task, cc.Val.Epoch, cc.Val.Call
	}
	return
}

// exec sends one of the capability's statements in o's server session.
// The first statement opens the session on the server; every later one
// asks the server to refuse rather than open it again, so a lost session
// is seen as lost.
func (inst *Service) exec(o op, site string, sql string, external []queryengine.ExternalTable) (body []byte, err error) {
	body, err = o.srv.conn.exec(o.ctx, execSpec{
		site: site, sessionID: o.srv.id, timeout: inst.limits.SessionTimeout, check: o.srv.established,
		external: external, stamp: inst.stamp(o.s, o.cc),
	}, sql)
	if err == nil {
		o.srv.established = true
	}
	return
}

// probe reads which of the capability's tables the server session holds
// and their sizes, drops the ones no data set owns, and marks every data
// set of the server session whose table is gone as expired. The first
// probe of a server session opens it, and sees a table a statement made
// whose answer never arrived; a session the server no longer has is
// replaced by one under a new id, which the next statement opens.
func (inst *Service) probe(o op) (lost bool, err error) {
	body, err := o.srv.conn.exec(o.ctx, execSpec{site: SiteProbe, sessionID: o.srv.id, timeout: inst.limits.SessionTimeout,
		check: o.srv.established, stamp: inst.stamp(o.s, o.cc)}, probeStatement)
	switch {
	case IsSessionLost(err):
		id, tErr := newToken("sd_")
		if tErr != nil {
			return false, tErr
		}
		o.srv.id, o.srv.established, o.srv.present = id, false, map[string]tableStat{}
		lost, err = true, nil
	case err != nil:
		return false, eb.Build().Str("endpoint", o.srv.host).Errorf("probe the server session: %w", err)
	default:
		o.srv.established = true
		rows, dErr := decodeRows[tableStat](body)
		if dErr != nil {
			return false, dErr
		}
		o.srv.present = make(map[string]tableStat, len(rows))
		for _, r := range rows {
			o.srv.present[r.Name] = r
		}
		inst.dropOrphans(o)
	}
	o.s.mu.Lock()
	var expired []string
	for _, d := range o.s.datasets {
		if d.server != o.srv.key || d.table == "" {
			continue
		}
		st, ok := o.srv.present[d.table]
		switch {
		case !ok && d.state == DatasetStateLive:
			d.state, d.reason = DatasetStateExpired, expiredReason(lost)
			expired = append(expired, datasetRef(d.name, d.revision))
		case ok && d.state == DatasetStateLive:
			if st.TotalRows != nil {
				d.rows = *st.TotalRows
			}
			if st.TotalBytes != nil {
				d.bytes = *st.TotalBytes
			}
		}
	}
	o.s.mu.Unlock()
	if len(expired) > 0 {
		inst.audit(o.ctx, AuditRecord{Action: AuditExpire, Outcome: trail.OutcomeOk, Session: o.s, Context: o.cc, By: o.c.By,
			Endpoint: o.srv.host, Datasets: expired, Reason: expiredReason(lost)})
	}
	return
}

// dropOrphans drops the tables of o's server session that no data set
// owns: one a create made after its caller stopped waiting, or that a drop
// did not reach. Left, it would hold server bytes no bound counts and make
// the next create of its name fail.
func (inst *Service) dropOrphans(o op) {
	o.s.mu.Lock()
	owned := make(map[string]bool, len(o.s.datasets))
	for _, d := range o.s.datasets {
		if d.server == o.srv.key && d.table != "" {
			owned[d.table] = true
		}
	}
	o.s.mu.Unlock()
	var orphans []string
	for name := range o.srv.present {
		if !owned[name] {
			orphans = append(orphans, name)
		}
	}
	for _, t := range orphans {
		inst.dropTable(o, t)
	}
}

// expiredReason is why a probe found tables gone.
func expiredReason(lost bool) (why string) {
	if lost {
		return "the server session was lost"
	}
	return "the table was gone from the server session"
}

// ensure makes every data set names reads, transitively, present in o's
// server session, oldest dependency first, and returns the ones it re-made.
func (inst *Service) ensure(o op, names []string) (replayed []string, err error) {
	o.s.mu.Lock()
	order, err := closureLocked(o.s, names)
	o.s.mu.Unlock()
	if err != nil {
		return
	}
	for _, d := range order {
		o.s.mu.Lock()
		state, server, ref := d.state, d.server, datasetRef(d.name, d.revision)
		o.s.mu.Unlock()
		if server != o.srv.key {
			return replayed, eb.Build().Str("name", d.name).Errorf("the data set lives on another endpoint or under another identity")
		}
		if state == DatasetStateLive {
			continue
		}
		if _, err = inst.build(o, d, buildModeReplay); err != nil {
			return
		}
		replayed = append(replayed, ref)
	}
	return
}

// buildModeE says why a table is made.
type buildModeE uint8

const (
	buildModeDefine buildModeE = iota + 1
	buildModeReplay
)

// build makes d's table from its definition in o's server session: the
// definition's macros are replaced by the tables and external tables it
// reads now, the result is checked as a read, wrapped in the capability's
// create, sent, measured and held to the data session's bounds. On a
// define, d is the candidate the caller commits and records with rec; on a
// replay, d is the recorded data set, its fields are updated and the
// replay recorded here.
func (inst *Service) build(o op, d *dataset, mode buildModeE) (rec AuditRecord, err error) {
	site, action := SiteCreate, AuditDefine
	if mode == buildModeReplay {
		site, action = SiteReplay, AuditReplay
		// Re-making a table is a create on the endpoint like the first one.
		if err = inst.allow(o.c, DestinationServer(o.srv.host), DestinationSession(o.srv.host)); err != nil {
			inst.failed(o, d, action, err)
			return
		}
	}
	rec = AuditRecord{Action: action, Session: o.s, Context: o.cc, By: o.c.By, Endpoint: o.srv.host}
	pr, err := parseStatement(d.sql)
	if err != nil {
		inst.failed(o, d, action, err)
		return
	}
	calls, err := findMacroCalls(pr)
	if err != nil {
		inst.failed(o, d, action, err)
		return
	}
	_, aliases := splitCalls(calls)
	atts, err := inst.attachAll(o.ctx, o.c, o.e, aliases)
	if err != nil {
		inst.failed(o, d, action, err)
		return
	}
	extOf := make(map[string]string, len(atts))
	external := make([]queryengine.ExternalTable, 0, len(atts))
	for _, a := range atts {
		extOf[a.alias] = a.external.Name
		external = append(external, a.external)
		rec.Attachments = append(rec.Attachments, a.ref())
	}
	o.s.mu.Lock()
	confined := len(atts) > 0
	tableOf := make(map[string]string, len(d.deps))
	for _, dep := range d.deps {
		dd := o.s.datasets[dep]
		if dd == nil {
			o.s.mu.Unlock()
			err = eb.Build().Str("name", dep).Errorf("the definition reads a data set the session no longer holds")
			inst.failed(o, d, action, err)
			return
		}
		tableOf[dep] = dd.table
		confined = confined || dd.confined
		rec.Dependencies = append(rec.Dependencies, datasetRef(dd.name, dd.revision))
	}
	rowsUsed, _ := usageLocked(o.s, d.name)
	o.s.mu.Unlock()
	if confined && !o.e.ServesConfined {
		err = eb.Build().Str("endpoint", o.srv.host).Errorf("the data set is derived from sealed data and the endpoint is not declared as allowed to see sealed data")
		inst.failed(o, d, action, err)
		return
	}
	rewritten := rewriteCalls(pr, calls, func(c macroCall) string {
		if c.session {
			return tableOf[c.name]
		}
		return extOf[c.name]
	})
	read, err := checkRead(rewritten)
	if err != nil {
		inst.failed(o, d, action, err)
		return
	}
	if rowsUsed >= inst.limits.MaxRows {
		err = eb.Build().Uint64("bound", inst.limits.MaxRows).Errorf("the data session holds as many rows as it may")
		inst.failed(o, d, action, err)
		return
	}
	room := inst.limits.MaxRows - rowsUsed
	table := tableName(d.name, d.revision)
	if _, there := o.srv.present[table]; there {
		o.s.mu.Lock()
		cur := o.s.datasets[d.name]
		taken := mode == buildModeDefine && cur != nil && cur.table == table
		o.s.mu.Unlock()
		if taken {
			err = eb.Build().Str("name", d.name).Errorf("the data set was defined by another request while this one waited; define it again")
			inst.failed(o, d, action, err)
			return
		}
		// Left by a build whose create or measure did not finish here.
		inst.dropTable(o, table)
	}
	create := composeCreate(createSpec{table: table, orderBy: d.orderBy, limit: room + 1}, read)
	started := time.Now()
	if _, err = inst.exec(o, site, create, external); err != nil {
		err = eb.Build().Str("name", d.name).Errorf("create the data set's table: %w", err)
		inst.failed(o, d, action, err)
		return
	}
	stat, cols, err := inst.measure(o, table)
	o.s.mu.Lock()
	if err == nil {
		// Measured again under the lock: a build on another server session
		// of this data session may have added rows or bytes since.
		rowsUsed, bytesUsed := usageLocked(o.s, d.name)
		switch {
		case o.s.closed:
			err = eh.Errorf("the data session has ended")
		case rowsUsed+stat.rows > inst.limits.MaxRows:
			err = eb.Build().Uint64("bound", inst.limits.MaxRows).Errorf("the data set has more rows than the data session may still hold")
		case bytesUsed+stat.bytes > inst.limits.MaxBytes:
			err = eb.Build().Uint64("bound", inst.limits.MaxBytes).Uint64("bytes", stat.bytes).Errorf("the data set takes more bytes than the data session may still hold")
		}
	}
	if err != nil {
		o.s.mu.Unlock()
		inst.dropTable(o, table)
		inst.failed(o, d, action, err)
		return
	}
	d.table, d.state, d.reason = table, DatasetStateLive, ""
	d.rows, d.bytes, d.columns, d.confined = stat.rows, stat.bytes, cols, confined
	d.builds++
	if mode == buildModeReplay {
		d.replays++
	}
	d.builtAt = time.Now()
	ref := datasetRef(d.name, d.revision)
	o.s.mu.Unlock()
	rec.Outcome, rec.Datasets, rec.Table = trail.OutcomeOk, []string{ref}, table
	rec.Definition, rec.Stratum, rec.Rows, rec.Bytes, rec.Elapsed = d.sql, read.stratum.String(), stat.rows, stat.bytes, time.Since(started)
	if mode == buildModeReplay {
		inst.audit(o.ctx, rec)
	}
	return
}

// failed records a build that did not happen. On a replay the data set
// keeps its definition and shows the failure.
func (inst *Service) failed(o op, d *dataset, action string, err error) {
	if action == AuditReplay {
		o.s.mu.Lock()
		d.state, d.reason = DatasetStateFailed, err.Error()
		o.s.mu.Unlock()
	}
	inst.audit(o.ctx, AuditRecord{Action: action, Outcome: outcomeOf(err), Session: o.s, Context: o.cc, By: o.c.By,
		Endpoint: o.srv.host, Datasets: []string{datasetRef(d.name, d.revision)}, Definition: d.sql, Reason: err.Error()})
}

// tableSize is what the server reports of one table.
type tableSize struct {
	rows  uint64
	bytes uint64
}

// measure reads a table's size and columns.
func (inst *Service) measure(o op, table string) (stat tableSize, cols []Column, err error) {
	body, err := inst.exec(o, SiteProbe, probeStatement+" AND name = '"+table+"'", nil)
	if err != nil {
		return stat, nil, eh.Errorf("measure the table: %w", err)
	}
	rows, err := decodeRows[tableStat](body)
	if err != nil {
		return
	}
	if len(rows) != 1 {
		return stat, nil, eb.Build().Str("table", table).Errorf("the server session does not list the table it just made")
	}
	if rows[0].TotalRows != nil {
		stat.rows = *rows[0].TotalRows
	}
	if rows[0].TotalBytes != nil {
		stat.bytes = *rows[0].TotalBytes
	}
	o.srv.present[table] = rows[0]
	body, err = inst.exec(o, SiteProbe, describeStatement(table), nil)
	if err != nil {
		return stat, nil, eh.Errorf("describe the table: %w", err)
	}
	described, err := decodeRows[columnRow](body)
	if err != nil {
		return
	}
	for _, c := range described {
		cols = append(cols, Column(c))
	}
	return
}

// dropTable drops a table the capability made, best effort.
func (inst *Service) dropTable(o op, table string) {
	if table == "" || !o.srv.established {
		return
	}
	if _, err := inst.exec(o, SiteDrop, composeDrop(table), nil); err != nil && !IsSessionLost(err) {
		inst.log.Warn().Err(err).Str("table", table).Msg("sessiondata: drop")
	}
	delete(o.srv.present, table)
}
