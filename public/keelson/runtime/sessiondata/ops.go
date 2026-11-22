package sessiondata

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefineInput is one definition: a name, the SELECT that defines it, the
// endpoint and identity its table lives under, and an optional sort key.
type DefineInput struct {
	Caller   Caller
	Ticket   string
	Endpoint Endpoint
	Name     string
	SQL      string
	OrderBy  []string
}

// DefineResult is the data set as defined, the data sets re-made first so
// its definition could read them, and the data session's ticket.
type DefineResult struct {
	Dataset  DatasetInfo
	Replayed []string
	Ticket   string
}

// Define defines a data set, or redefines one under a new revision, and
// makes its table now so a definition that does not run is refused at
// once. The SELECT reads server tables, other data sets of the session as
// session('name') and ad-hoc datasets as keelson('alias'); it is proven a
// read after its macros are replaced, then wrapped in the capability's own
// create. An agent's definition needs its task's grant to list the
// endpoint and the right to keep session tables on it. A redefinition
// keeps the old table until the new one is made, then drops it, and leaves
// every data set that reads it stale, to be rebuilt by the next statement
// that reads it.
func (inst *Service) Define(ctx context.Context, in DefineInput) (res DefineResult, err error) {
	switch {
	case !ValidName(in.Name):
		return res, eb.Build().Str("name", in.Name).Errorf("a data set's name is an identifier of at most 64 bytes")
	case len(in.SQL) > MaxDefinitionBytes:
		return res, eb.Build().Int("bound", MaxDefinitionBytes).Errorf("the definition is longer than a definition may be")
	case in.Endpoint.URL == "":
		return res, eh.Errorf("a definition names the endpoint its table lives on")
	}
	if err = checkOrderBy(in.OrderBy); err != nil {
		return
	}
	if err = checkEndpointURL(in.Endpoint.URL); err != nil {
		return
	}
	pr, err := parseStatement(in.SQL)
	if err != nil {
		return
	}
	calls, err := findMacroCalls(pr)
	if err != nil {
		return
	}
	deps, _ := splitCalls(calls)
	if slices.Contains(deps, in.Name) {
		return res, eb.Build().Str("name", in.Name).Errorf("a definition does not read the data set it defines; name the new one differently")
	}
	s, cc, err := inst.resolve(in.Caller, in.Ticket, true)
	if err != nil {
		return
	}
	host := EndpointHost(in.Endpoint.URL)
	if err = inst.allow(in.Caller, DestinationServer(host), DestinationSession(host)); err != nil {
		inst.audit(ctx, AuditRecord{Action: AuditDefine, Outcome: outcomeOf(err), Session: s, Context: cc, By: in.Caller.By,
			Endpoint: host, Datasets: []string{in.Name}, Definition: in.SQL, Reason: err.Error()})
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return res, eh.Errorf("the data session has ended")
	}
	old := s.datasets[in.Name]
	if old == nil && len(s.datasets) >= inst.limits.MaxDatasets {
		s.mu.Unlock()
		return res, eb.Build().Int("bound", inst.limits.MaxDatasets).Errorf("the data session holds as many data sets as it may")
	}
	srv, err := inst.serverForLocked(s, in.Endpoint)
	if err != nil {
		s.mu.Unlock()
		return
	}
	if old != nil && old.server != srv.key {
		// Its table and the tables of what reads it live in the other
		// server session, which this definition does not hold.
		s.mu.Unlock()
		return res, eb.Build().Str("name", in.Name).Str("livesOn", old.host).
			Errorf("the data set lives on another endpoint or under another identity; drop it before defining it there")
	}
	for _, dep := range deps {
		dd := s.datasets[dep]
		switch {
		case dd == nil:
			err = eb.Build().Str("name", dep).Str("available", availableLocked(s)).Errorf("the definition reads a data set the session does not hold")
		case dd.server != srv.key:
			err = eb.Build().Str("name", dep).Errorf("the definition reads a data set that lives on another endpoint or under another identity")
		}
		if err != nil {
			s.mu.Unlock()
			return
		}
	}
	if old != nil {
		for _, dd := range dependantsLocked(s, in.Name) {
			if slices.Contains(deps, dd.name) {
				s.mu.Unlock()
				return res, eb.Build().Str("name", in.Name).Str("reads", dd.name).Errorf("the new definition would read a data set that reads it")
			}
		}
	}
	revision := uint64(1)
	if old != nil {
		revision = old.revision + 1
	}
	cand := &dataset{name: in.Name, sql: in.SQL, deps: deps, orderBy: slices.Clone(in.OrderBy), server: srv.key, revision: revision,
		host: host, definedAt: time.Now(), task: s.task}
	_, cand.attachments = splitCalls(calls)
	if in.Caller.OnBehalfOf == nil {
		cand.task = ""
	}
	s.mu.Unlock()

	l, err := inst.acquire(ctx, srv, in.Caller.By)
	if err != nil {
		return
	}
	defer inst.releaseLease(l)
	o := op{ctx: ctx, c: in.Caller, cc: cc, s: s, srv: srv, e: in.Endpoint}
	var made AuditRecord
	err = inst.retryLost(o, func() (rErr error) {
		if _, rErr = inst.probe(o); rErr != nil {
			return
		}
		if res.Replayed, rErr = inst.ensure(o, deps); rErr != nil {
			return
		}
		made, rErr = inst.build(o, cand, buildModeDefine)
		return
	})
	if err != nil {
		return
	}

	s.mu.Lock()
	switch {
	case s.closed:
		err = eh.Errorf("the data session ended while the data set was made")
	case s.datasets[in.Name] != old:
		err = eb.Build().Str("name", in.Name).Errorf("the data set was defined or dropped by another request while this one was made; define it again")
	case old == nil && len(s.datasets) >= inst.limits.MaxDatasets:
		err = eb.Build().Int("bound", inst.limits.MaxDatasets).Errorf("the data session holds as many data sets as it may")
	}
	if err != nil {
		s.mu.Unlock()
		inst.dropTable(o, cand.table)
		inst.audit(ctx, AuditRecord{Action: AuditDefine, Outcome: trail.OutcomeFailed, Session: s, Context: cc, By: in.Caller.By,
			Endpoint: host, Datasets: []string{datasetRef(in.Name, revision)}, Definition: in.SQL, Reason: err.Error()})
		return DefineResult{}, err
	}
	var oldTable string
	var stale []*dataset
	if old != nil {
		oldTable = old.table
		stale = dependantsLocked(s, in.Name)
		cand.builds += old.builds
		cand.replays = old.replays
	}
	s.datasets[in.Name] = cand
	var staleTables []string
	var staleRefs []string
	for _, dd := range stale {
		if dd.table != "" && dd.state == DatasetStateLive {
			staleTables = append(staleTables, dd.table)
		}
		dd.state, dd.reason = DatasetStateStale, "it reads "+in.Name+", which was redefined"
		staleRefs = append(staleRefs, datasetRef(dd.name, dd.revision))
	}
	res.Dataset, res.Ticket = cand.info(), s.ticket
	s.mu.Unlock()
	inst.audit(ctx, made)
	if oldTable != "" && oldTable != cand.table {
		inst.dropTable(o, oldTable)
	}
	for _, t := range staleTables {
		inst.dropTable(o, t)
	}
	if len(staleRefs) > 0 {
		inst.audit(ctx, AuditRecord{Action: AuditStale, Outcome: trail.OutcomeOk, Session: s, Context: cc, By: in.Caller.By,
			Endpoint: host, Datasets: staleRefs, Dependencies: []string{datasetRef(in.Name, revision)}, Reason: "a data set they read was redefined"})
	}
	return
}

// retryLost runs f, and once more when the server session turned out lost
// part-way: the second run probes a fresh session and replays.
func (inst *Service) retryLost(o op, f func() error) (err error) {
	err = f()
	if IsSessionLost(err) {
		if _, pErr := inst.probe(o); pErr != nil {
			return pErr
		}
		err = f()
	}
	return
}

// DropInput names a data set to drop. Cascade drops the data sets that
// read it too; without it, a data set others read is refused with their
// names.
type DropInput struct {
	Caller  Caller
	Ticket  string
	Name    string
	Cascade bool
}

// Drop removes a data set's definition and its table.
func (inst *Service) Drop(ctx context.Context, in DropInput) (dropped []string, err error) {
	s, cc, err := inst.resolve(in.Caller, in.Ticket, false)
	if err != nil {
		return
	}
	if s == nil {
		return nil, eb.Build().Str("name", in.Name).Errorf("no data set by that name in the data session")
	}
	s.mu.Lock()
	d := s.datasets[in.Name]
	if d == nil {
		avail := availableLocked(s)
		s.mu.Unlock()
		return nil, eb.Build().Str("name", in.Name).Str("available", avail).Errorf("no data set by that name in the data session")
	}
	victims := append([]*dataset{d}, dependantsLocked(s, in.Name)...)
	if len(victims) > 1 && !in.Cascade {
		names := make([]string, 0, len(victims)-1)
		for _, v := range victims[1:] {
			names = append(names, v.name)
		}
		s.mu.Unlock()
		return nil, eb.Build().Str("name", in.Name).Strs("readBy", names).Errorf("other data sets read it; drop them first or cascade")
	}
	byServer := make(map[string][]string)
	for _, v := range victims {
		delete(s.datasets, v.name)
		dropped = append(dropped, datasetRef(v.name, v.revision))
		if v.table != "" {
			byServer[v.server] = append(byServer[v.server], v.table)
		}
	}
	servers := make(map[string]*serverSession, len(byServer))
	for key := range byServer {
		servers[key] = s.servers[key]
	}
	s.mu.Unlock()
	for key, tables := range byServer {
		srv := servers[key]
		if srv == nil {
			continue
		}
		l, aErr := inst.acquire(ctx, srv, in.Caller.By)
		if aErr != nil {
			inst.log.Warn().Err(aErr).Msg("sessiondata: drop waited too long; the tables expire with the session")
			continue
		}
		o := op{ctx: ctx, c: in.Caller, cc: cc, s: s, srv: srv}
		for _, t := range tables {
			inst.dropTable(o, t)
		}
		inst.releaseLease(l)
	}
	inst.audit(ctx, AuditRecord{Action: AuditDrop, Outcome: trail.OutcomeOk, Session: s, Context: cc, By: in.Caller.By, Datasets: dropped})
	return
}

// PrepareInput is a statement to send inside a data session.
type PrepareInput struct {
	Caller   Caller
	Ticket   string
	Endpoint Endpoint
	// SQL is the statement as its author wrote it, before the caller's own
	// alias binding: data sets as session('name'), ad-hoc datasets as
	// keelson('alias').
	SQL string
}

// Prepare readies a statement to run inside the caller's data session on
// the endpoint. It replaces each session('name') with the data set's table,
// re-making first — in dependency order, and recorded — every data set the
// statement reads whose table the server session no longer holds; reads
// each keelson('alias') as the dataset service lets the caller read it and
// attaches it as external data; and holds the server session for the
// statement, which the caller ends with [Service.Release]. A statement
// naming neither macro comes back as it was, holding nothing. An agent's
// statement must be a read.
func (inst *Service) Prepare(ctx context.Context, in PrepareInput) (b Binding, err error) {
	pr, pErr := parseStatement(in.SQL)
	if pErr != nil {
		if mentionsMacro(in.SQL) {
			return b, pErr
		}
		return Binding{SQL: in.SQL}, nil
	}
	calls, err := findMacroCalls(pr)
	if err != nil {
		return
	}
	names, aliases := splitCalls(calls)
	if len(names) == 0 && len(aliases) == 0 {
		return Binding{SQL: in.SQL}, nil
	}
	if in.Endpoint.URL == "" {
		return b, eh.Errorf("a statement naming a data set or an ad-hoc dataset names the endpoint it runs on")
	}
	if err = checkEndpointURL(in.Endpoint.URL); err != nil {
		return
	}
	if len(names) == 0 {
		// Attachments alone need no data session.
		cc, aErr := inst.attest(in.Caller)
		if aErr != nil {
			return b, aErr
		}
		return inst.bindAttachments(ctx, in, cc, pr, calls, aliases)
	}
	s, cc, err := inst.resolve(in.Caller, in.Ticket, false)
	if err != nil {
		return
	}
	if s == nil {
		return b, eb.Build().Strs("names", names).Errorf("the task holds no data set yet; define one first")
	}
	s.mu.Lock()
	// A data set lives in a server session its definition opened, so one
	// this endpoint and identity have no server session for holds none.
	key := serverKey(in.Endpoint)
	srv := s.servers[key]
	if s.closed {
		err = eh.Errorf("the data session has ended")
	}
	for _, n := range names {
		if err != nil {
			break
		}
		d := s.datasets[n]
		switch {
		case d == nil:
			err = eb.Build().Str("name", n).Str("available", availableLocked(s)).Errorf("no data set by that name in the data session")
		case d.server != key || srv == nil:
			err = eb.Build().Str("name", n).Str("livesOn", d.host).Errorf("the data set lives on another endpoint or under another identity")
		}
	}
	s.mu.Unlock()
	if err != nil {
		return
	}
	atts, err := inst.attachAll(ctx, in.Caller, in.Endpoint, aliases)
	if err != nil {
		return
	}
	l, err := inst.acquire(ctx, srv, in.Caller.By)
	if err != nil {
		return
	}
	handed := false
	defer func() {
		if !handed {
			inst.releaseLease(l)
		}
	}()
	o := op{ctx: ctx, c: in.Caller, cc: cc, s: s, srv: srv, e: in.Endpoint}
	err = inst.retryLost(o, func() (rErr error) {
		if _, rErr = inst.probe(o); rErr != nil {
			return
		}
		b.Replayed, rErr = inst.ensure(o, names)
		return
	})
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return b, eh.Errorf("the data session ended while the statement was being prepared")
	}
	tableOf := make(map[string]string, len(names))
	confined := len(atts) > 0
	for _, n := range names {
		d := s.datasets[n]
		if d == nil {
			s.mu.Unlock()
			return b, eb.Build().Str("name", n).Errorf("the data set was dropped while the statement was being prepared")
		}
		tableOf[n] = d.table
		confined = confined || d.confined
		b.Datasets = append(b.Datasets, datasetRef(d.name, d.revision))
	}
	b.Ticket = s.ticket
	s.mu.Unlock()
	if confined && !in.Endpoint.ServesConfined {
		return b, eb.Build().Str("endpoint", srv.host).Errorf("the statement reads a data set derived from sealed data, and the endpoint is not declared as allowed to see sealed data")
	}
	extOf := make(map[string]string, len(atts))
	for _, a := range atts {
		extOf[a.alias] = a.external.Name
		b.External = append(b.External, a.external)
	}
	b.SQL = rewriteCalls(pr, calls, func(c macroCall) string {
		if c.session {
			return tableOf[c.name]
		}
		return extOf[c.name]
	})
	if in.Caller.OnBehalfOf != nil {
		if err = statementIsRead(b.SQL); err != nil {
			return Binding{}, err
		}
	}
	if confined {
		b.Sensitivity = queryengine.SensitivityConfined
	}
	b.Settings = map[string]string{
		"session_id":      srv.id,
		"session_timeout": strconv.FormatInt(int64(inst.limits.SessionTimeout/time.Second), 10),
		"session_check":   "1",
	}
	if err = inst.handOut(l); err != nil {
		return Binding{}, err
	}
	b.Lease = l.id
	handed = true
	inst.auditAttachments(ctx, s, cc, in.Caller, srv.host, atts)
	return
}

// bindAttachments is Prepare for a statement that names ad-hoc datasets and
// no data set: no server session, only external data.
func (inst *Service) bindAttachments(ctx context.Context, in PrepareInput, cc option.Option[app.CallContext], pr *nanopass.ParseResult, calls []macroCall, aliases []string) (b Binding, err error) {
	atts, err := inst.attachAll(ctx, in.Caller, in.Endpoint, aliases)
	if err != nil {
		return
	}
	extOf := make(map[string]string, len(atts))
	for _, a := range atts {
		extOf[a.alias] = a.external.Name
		b.External = append(b.External, a.external)
	}
	b.SQL = rewriteCalls(pr, calls, func(c macroCall) string { return extOf[c.name] })
	if in.Caller.OnBehalfOf != nil {
		if err = statementIsRead(b.SQL); err != nil {
			return Binding{}, err
		}
	}
	b.Sensitivity = queryengine.SensitivityConfined
	inst.auditAttachments(ctx, nil, cc, in.Caller, EndpointHost(in.Endpoint.URL), atts)
	return
}
