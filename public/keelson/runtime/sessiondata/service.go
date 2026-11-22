// Package sessiondata is the session data capability (ADR-0406,
// proposed): named, task-scoped data sets that SQL statements read. A data
// set is a defining read and the table that caches it, held in a ClickHouse
// HTTP session the capability owns for one data session and endpoint. The
// definition is what lasts: when the server session is lost, the
// capability re-makes what a statement reads by replaying definitions in
// dependency order, and records that it did.
//
// A data session belongs to an agent's task — every window working for the
// task reaches the same one, found from the call's on-behalf-of context —
// or to the windows of the person that opened or attached it by its
// ticket. A statement names a data set as session('name'); [Service.Prepare]
// replaces the call with the table, re-makes what is missing, attaches the
// ad-hoc datasets the statement names as keelson('alias') as external data,
// and hands the caller a [Binding] to send under its own checks.
//
// Every statement the capability composes leaves through the fairlead with
// the task mark cleared; the only text of a task it carries is a read the
// capability proved a read before wrapping it.
package sessiondata

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Limits bound what one data session may hold and how its server sessions
// are kept. A zero field takes the value its environment variable gives.
type Limits struct {
	MaxDatasets    int
	MaxRows        uint64
	MaxBytes       uint64
	MaxAttachBytes uint64
	SessionTimeout time.Duration
	LeaseTimeout   time.Duration
	// BusyWait is how long one of the service's statements is tried again
	// while the server finds its session busy; negative sends it once.
	BusyWait time.Duration
}

// withDefaults is inst with each zero field filled from its environment
// variable.
func (inst Limits) withDefaults() (l Limits) {
	l = inst
	if l.MaxDatasets <= 0 {
		l.MaxDatasets = MaxDatasetsPerSession
	}
	if l.MaxRows == 0 {
		l.MaxRows = uint64(max(MaxRowsEnv.Get(), 1))
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = uint64(max(MaxBytesEnv.Get(), 1))
	}
	if l.MaxAttachBytes == 0 {
		l.MaxAttachBytes = uint64(max(MaxAttachBytesEnv.Get(), 1))
	}
	if l.SessionTimeout <= 0 {
		l.SessionTimeout = SessionTimeoutEnv.Get()
	}
	if l.SessionTimeout < time.Second {
		l.SessionTimeout = time.Second
	}
	if l.LeaseTimeout <= 0 {
		l.LeaseTimeout = LeaseTimeoutEnv.Get()
	}
	if l.LeaseTimeout <= 0 {
		l.LeaseTimeout = 15 * time.Minute
	}
	switch {
	case l.BusyWait < 0:
		l.BusyWait = 0
	case l.BusyWait == 0:
		l.BusyWait = max(BusyWaitEnv.Get(), 0)
	}
	return
}

// Config parameterises the Service.
type Config struct {
	// Bus, when set, backs the runtime.sessiondata.* subjects and hears
	// windows close. Nil leaves only the Go methods.
	Bus *inprocbus.Inst
	// Registry is where keelson('session_datasets') registers; nil skips it.
	Registry *introspect.Registry
	Log      zerolog.Logger
	// Trail records define, replay, drop, attach and close (ADR-0277).
	Trail *trail.Recorder
	// Adhoc reads the ad-hoc datasets a statement attaches; nil refuses
	// every keelson('…') call.
	Adhoc AdhocReaderI
	// HTTPClient sends the capability's own statements; nil is a stock
	// client.
	HTTPClient *http.Client
	Limits     Limits
	// SweepInterval is how often task sessions are checked for an ended
	// task; zero is 30 s, negative turns the sweep off.
	SweepInterval time.Duration
}

// Service owns the data sessions. It is safe for concurrent use.
type Service struct {
	log    zerolog.Logger
	trail  *trail.Recorder
	reg    *introspect.Registry
	adhoc  AdhocReaderI
	http   *http.Client
	limits Limits

	dispatcher atomic.Pointer[dispatcherRef]
	auditHook  atomic.Pointer[func(AuditRecord)]

	busClient *inprocbus.Client
	unsubs    []func()

	mu       sync.Mutex
	byTicket map[string]*session
	byTask   map[string]*session
	leases   map[string]*lease
	// handOvers are the hand-over tokens not yet claimed (handover.go).
	handOvers map[string]handOver
	closed    bool

	stopSweep chan struct{}
	swept     sync.WaitGroup
}

type dispatcherRef struct{ d DispatcherI }

// sessionKindE says whose a data session is.
type sessionKindE uint8

const (
	sessionKindTask sessionKindE = iota + 1
	sessionKindPerson
)

func (inst sessionKindE) String() (s string) {
	if inst == sessionKindTask {
		return "task"
	}
	return "person"
}

// session is one data session.
type session struct {
	ticket    string
	kind      sessionKindE
	task      string
	owner     Identity
	createdAt time.Time

	mu       sync.Mutex
	holders  map[Identity]struct{}
	datasets map[string]*dataset
	servers  map[string]*serverSession
	closed   bool
}

// dataset is one data set's definition and what the capability knows of
// its table. Guarded by its session's mu.
type dataset struct {
	name        string
	sql         string
	deps        []string
	attachments []string
	orderBy     []string
	server      string
	revision    uint64
	builds      uint64
	replays     uint64
	state       DatasetStateE
	reason      string
	rows        uint64
	bytes       uint64
	columns     []Column
	confined    bool
	host        string
	table       string
	definedAt   time.Time
	builtAt     time.Time
	task        string
}

func (inst *dataset) info() (d DatasetInfo) {
	return DatasetInfo{
		Name: inst.name, SQL: inst.sql, Dependencies: slices.Clone(inst.deps), Attachments: slices.Clone(inst.attachments),
		OrderBy: slices.Clone(inst.orderBy), Revision: inst.revision, Builds: inst.builds, Replays: inst.replays,
		State: inst.state, Reason: inst.reason, Rows: inst.rows, Bytes: inst.bytes, Columns: slices.Clone(inst.columns),
		Confined: inst.confined, Endpoint: inst.host, Table: inst.table, DefinedAt: inst.definedAt, BuiltAt: inst.builtAt,
		Task: inst.task,
	}
}

// serverSession is the ClickHouse session one data session holds on one
// endpoint under one identity. sem admits one statement at a time; id,
// established and present are touched only by the holder of sem.
type serverSession struct {
	key  string
	host string
	conn serverConn
	sem  chan struct{}

	id          string
	established bool
	present     map[string]tableStat
}

// lease is one hold on a server session.
type lease struct {
	id    string
	srv   *serverSession
	by    Identity
	timer *time.Timer
	once  sync.Once
}

// NewService builds the Service and, with a bus, subscribes to its
// subjects.
func NewService(cfg Config) (inst *Service, err error) {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	inst = &Service{
		log: cfg.Log, trail: cfg.Trail, reg: cfg.Registry, adhoc: cfg.Adhoc, http: hc,
		limits:   cfg.Limits.withDefaults(),
		byTicket: make(map[string]*session), byTask: make(map[string]*session), leases: make(map[string]*lease),
		stopSweep: make(chan struct{}),
	}
	if inst.reg != nil {
		if regErr := inst.reg.Register(newCatalogProvider(inst)); regErr != nil {
			return nil, eb.Build().Str("table", CatalogTableName).Errorf("register the catalog: %w", regErr)
		}
	}
	if cfg.Bus != nil {
		if subErr := inst.subscribe(cfg.Bus); subErr != nil {
			if inst.reg != nil {
				inst.reg.Unregister(CatalogTableName)
			}
			return nil, subErr
		}
	}
	interval := cfg.SweepInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	if interval > 0 {
		inst.swept.Add(1)
		go inst.sweep(interval)
	}
	return inst, nil
}

// SetDispatcher installs the dispatcher that attests agent-caused requests
// and answers for grants and ended tasks. Until it is set, an agent-caused
// request is refused.
func (inst *Service) SetDispatcher(d DispatcherI) {
	inst.dispatcher.Store(&dispatcherRef{d: d})
}

// Close ends every data session — dropping the tables the capability made,
// as far as ctx allows — and stops serving. Idempotent.
func (inst *Service) Close(ctx context.Context) (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	sessions := make([]*session, 0, len(inst.byTicket))
	for _, s := range inst.byTicket {
		sessions = append(sessions, s)
	}
	unsubs := inst.unsubs
	inst.unsubs = nil
	inst.mu.Unlock()
	close(inst.stopSweep)
	inst.swept.Wait()
	for _, u := range unsubs {
		u()
	}
	// No caller will release a lease once the host stops, so the drops
	// below do not wait for one; a statement still running makes its
	// session's drops fail, and the server drops those tables at the
	// session timeout.
	for _, l := range inst.leasesWhere(func(*lease) bool { return true }) {
		inst.releaseLease(l)
	}
	for _, s := range sessions {
		inst.closeSession(ctx, s, "the host stopped")
	}
	if inst.reg != nil {
		inst.reg.Unregister(CatalogTableName)
	}
	return nil
}

// leasesWhere lists the leases out that keep holds.
func (inst *Service) leasesWhere(keep func(l *lease) bool) (out []*lease) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, l := range inst.leases {
		if keep(l) {
			out = append(out, l)
		}
	}
	return
}

// sweep ends the data sessions of tasks that ended.
//
// lifetime: the service's own, stopped by Close.
func (inst *Service) sweep(interval time.Duration) {
	defer inst.swept.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-inst.stopSweep:
			return
		case <-t.C:
			inst.SweepEndedTasks(context.Background()) //boxer:lint disable=CS020 reason="the sweep is the service's own root; Close stops it"
		}
	}
}

// SweepEndedTasks ends the data session of every task the dispatcher no
// longer knows as live. The sweep calls it; a test calls it directly.
func (inst *Service) SweepEndedTasks(ctx context.Context) (ended int) {
	ref := inst.dispatcher.Load()
	if ref == nil || ref.d == nil {
		return
	}
	inst.mu.Lock()
	var gone []*session
	for task, s := range inst.byTask {
		if !ref.d.TaskLive(task) {
			gone = append(gone, s)
		}
	}
	inst.mu.Unlock()
	for _, s := range gone {
		inst.closeSession(ctx, s, "the task ended")
		ended++
	}
	return
}

// Open starts a data session owned by by's window and returns its ticket,
// the identifier another window attaches by. It is the person's: an
// agent's work uses its task's data session.
func (inst *Service) Open(ctx context.Context, by Identity) (ticket string, err error) {
	s, err := inst.newSession(sessionKindPerson, "", by)
	if err != nil {
		return
	}
	inst.audit(ctx, AuditRecord{Action: AuditOpen, Outcome: trail.OutcomeOk, Session: s, By: by})
	return s.ticket, nil
}

// Attach makes by's window a holder of the data session ticket names. A
// ticket is a bearer capability: whoever presents it attaches.
func (inst *Service) Attach(ctx context.Context, by Identity, ticket string) (err error) {
	s := inst.lookupTicket(ticket)
	if s == nil {
		return eh.Errorf("no data session by that ticket")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return eh.Errorf("the data session has ended")
	}
	s.holders[by] = struct{}{}
	s.mu.Unlock()
	inst.audit(ctx, AuditRecord{Action: AuditAttachSession, Outcome: trail.OutcomeOk, Session: s, By: by})
	return
}

// CloseSession ends the person's data session ticket names; any window
// holding it may. A task's data session ends with the task.
func (inst *Service) CloseSession(ctx context.Context, by Identity, ticket string) (err error) {
	s := inst.lookupTicket(ticket)
	if s == nil {
		return eh.Errorf("no data session by that ticket")
	}
	if s.kind == sessionKindTask {
		return eh.Errorf("a task's data session ends with the task")
	}
	if !s.holds(by) {
		return eh.Errorf("the window does not hold the data session")
	}
	inst.closeSession(ctx, s, "closed by a window holding it")
	return
}

// holds reports whether by is a holder.
func (inst *session) holds(by Identity) (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, yes = inst.holders[by]
	return
}

func (inst *Service) lookupTicket(ticket string) (s *session) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.byTicket[ticket]
}

// newSession registers a data session.
func (inst *Service) newSession(kind sessionKindE, task string, owner Identity) (s *session, err error) {
	ticket, err := newToken("sds_")
	if err != nil {
		return
	}
	s = &session{ticket: ticket, kind: kind, task: task, owner: owner, createdAt: time.Now(),
		holders: make(map[Identity]struct{}), datasets: make(map[string]*dataset), servers: make(map[string]*serverSession)}
	if kind == sessionKindPerson {
		s.holders[owner] = struct{}{}
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil, eh.Errorf("the session data service has stopped")
	}
	if kind == sessionKindTask {
		if existing := inst.byTask[task]; existing != nil {
			return existing, nil
		}
		inst.byTask[task] = s
	}
	inst.byTicket[ticket] = s
	return
}

// resolve finds the data session a caller's request is about. An agent's
// call is attested and reaches its task's data session, created on first
// use when create is set, whatever ticket it names; the person's request
// names a ticket its window holds.
func (inst *Service) resolve(c Caller, ticket string, create bool) (s *session, cc option.Option[app.CallContext], err error) {
	if c.OnBehalfOf != nil && c.OnBehalfOf.Task != "" {
		cc, err = inst.attest(c)
		if err != nil {
			return
		}
		task := c.OnBehalfOf.Task
		inst.mu.Lock()
		s = inst.byTask[task]
		inst.mu.Unlock()
		if s == nil && create {
			s, err = inst.newSession(sessionKindTask, task, c.By)
		}
		return
	}
	if ticket == "" {
		return nil, cc, eh.Errorf("the person's request names a data session by its ticket; open one first")
	}
	s = inst.lookupTicket(ticket)
	if s == nil {
		return nil, cc, eh.Errorf("no data session by that ticket")
	}
	if !s.holds(c.By) {
		return nil, cc, eh.Errorf("the window does not hold the data session; attach it with its ticket first")
	}
	return
}

// closeSession ends s: it leaves the maps, and the tables the capability
// made are dropped where a server session can be had within ctx.
func (inst *Service) closeSession(ctx context.Context, s *session, why string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	servers := make([]*serverSession, 0, len(s.servers))
	for _, srv := range s.servers {
		servers = append(servers, srv)
	}
	tables := make(map[string][]string, len(servers))
	for _, d := range s.datasets {
		if d.table != "" {
			tables[d.server] = append(tables[d.server], d.table)
		}
	}
	s.mu.Unlock()
	inst.mu.Lock()
	delete(inst.byTicket, s.ticket)
	if s.kind == sessionKindTask && inst.byTask[s.task] == s {
		delete(inst.byTask, s.task)
	}
	inst.mu.Unlock()
	dropCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		l, err := inst.acquire(dropCtx, srv, Identity{})
		if err != nil {
			inst.log.Warn().Err(err).Str("endpoint", srv.host).Msg("sessiondata: the server session stayed busy; its tables expire with it")
			continue
		}
		if srv.established {
			for _, t := range tables[srv.key] {
				if _, dErr := srv.conn.exec(dropCtx, execSpec{site: SiteDrop, sessionID: srv.id, timeout: inst.limits.SessionTimeout, check: true,
					stamp: inst.stamp(s, option.None[app.CallContext]()), busyWait: inst.limits.BusyWait}, composeDrop(t)); dErr != nil && !IsSessionLost(dErr) {
					inst.log.Warn().Err(dErr).Str("table", t).Msg("sessiondata: drop on close")
				}
			}
		}
		inst.releaseLease(l)
	}
	inst.audit(ctx, AuditRecord{Action: AuditClose, Outcome: trail.OutcomeOk, Session: s, Reason: why})
}

// serverForLocked is s's server session for e, made on first use. The caller
// holds s.mu.
func (inst *Service) serverForLocked(s *session, e Endpoint) (srv *serverSession, err error) {
	key := serverKey(e)
	if srv = s.servers[key]; srv != nil {
		return
	}
	id, err := newToken("sd_")
	if err != nil {
		return
	}
	srv = &serverSession{key: key, host: EndpointHost(e.URL), id: id, sem: make(chan struct{}, 1),
		conn: serverConn{url: e.URL, user: e.User, password: e.Password, http: inst.http}}
	s.servers[key] = srv
	return
}

// acquire takes srv's one slot, waiting under ctx.
func (inst *Service) acquire(ctx context.Context, srv *serverSession, by Identity) (l *lease, err error) {
	select {
	case srv.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, eb.Build().Str("endpoint", srv.host).Errorf("waiting for the data session's server session: %w", ctx.Err())
	}
	id, err := newToken("sdl_")
	if err != nil {
		<-srv.sem
		return nil, err
	}
	l = &lease{id: id, srv: srv, by: by}
	return
}

// handOut registers l for a caller, released by Release or when the lease
// timeout passes; a stopped service hands out none.
func (inst *Service) handOut(l *lease) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return eh.Errorf("the session data service has stopped")
	}
	inst.leases[l.id] = l
	l.timer = time.AfterFunc(inst.limits.LeaseTimeout, func() {
		inst.log.Warn().Str("endpoint", l.srv.host).Msg("sessiondata: a lease was not released; it expired")
		inst.releaseLease(l)
	})
	return
}

// releaseLease frees l's slot once.
func (inst *Service) releaseLease(l *lease) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		inst.mu.Lock()
		if l.timer != nil {
			l.timer.Stop()
		}
		delete(inst.leases, l.id)
		inst.mu.Unlock()
		<-l.srv.sem
	})
}

// Release ends the lease a binding carried. The statement it was for has
// been answered, its response drained or abandoned; releasing earlier
// lets another statement into the server session, which the server
// refuses while the first runs.
func (inst *Service) Release(by Identity, leaseId string) (err error) {
	inst.mu.Lock()
	l := inst.leases[leaseId]
	inst.mu.Unlock()
	if l == nil {
		return eh.Errorf("no lease by that id; it was released or expired")
	}
	if l.by != by && !by.isRuntime() {
		return eh.Errorf("the lease is another window's")
	}
	inst.releaseLease(l)
	return
}

func (inst Identity) isRuntime() (yes bool) { return inst.App == "" }

// List returns the data sets of the caller's data session, sorted by name,
// and the session's ticket; nothing when the caller's task has none yet.
func (inst *Service) List(ctx context.Context, c Caller, ticket string) (infos []DatasetInfo, sessionTicket string, err error) {
	s, _, err := inst.resolve(c, ticket, false)
	if err != nil || s == nil {
		return
	}
	s.mu.Lock()
	for _, d := range s.datasets {
		infos = append(infos, d.info())
	}
	s.mu.Unlock()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, s.ticket, nil
}

// instanceClosed releases the leases a closed window still had out — it
// will not release them, and the statements it sent have lost their
// reader — and removes it from the person's data sessions it held; one
// whose last holder closed ends.
func (inst *Service) instanceClosed(ctx context.Context, who Identity) {
	for _, l := range inst.leasesWhere(func(l *lease) bool { return l.by == who }) {
		inst.releaseLease(l)
	}
	inst.mu.Lock()
	var sessions []*session
	for _, s := range inst.byTicket {
		if s.kind == sessionKindPerson {
			sessions = append(sessions, s)
		}
	}
	inst.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		_, held := s.holders[who]
		delete(s.holders, who)
		empty := len(s.holders) == 0
		s.mu.Unlock()
		if held && empty {
			inst.closeSession(ctx, s, "the last window holding it closed")
		}
	}
}

// datasetRef renders name@revision.
func datasetRef(name string, revision uint64) (ref string) {
	return name + "@" + strconv.FormatUint(revision, 10)
}

// closureLocked is names and every data set they read, transitively, in an
// order where each comes after what it reads. The caller holds s.mu.
func closureLocked(s *session, names []string) (order []*dataset, err error) {
	state := make(map[string]uint8, len(names))
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			return eb.Build().Str("cycle", strings.Join(append(path, name), " -> ")).Errorf("the data sets read each other")
		}
		d := s.datasets[name]
		if d == nil {
			return eb.Build().Str("name", name).Errorf("no data set by that name in the data session")
		}
		state[name] = 1
		for _, dep := range d.deps {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, d)
		return nil
	}
	for _, n := range names {
		if err = visit(n, nil); err != nil {
			return nil, err
		}
	}
	return
}

// dependantsLocked lists the data sets that read name, transitively. The
// caller holds s.mu.
func dependantsLocked(s *session, name string) (out []*dataset) {
	seen := map[string]bool{name: true}
	queue := []string{name}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range s.datasets {
			if !seen[d.name] && slices.Contains(d.deps, n) {
				seen[d.name] = true
				out = append(out, d)
				queue = append(queue, d.name)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return
}

// availableLocked names the data sets s holds, for a refusal.
func availableLocked(s *session) (names string) {
	all := make([]string, 0, len(s.datasets))
	for n := range s.datasets {
		all = append(all, n)
	}
	sort.Strings(all)
	if len(all) == 0 {
		return "(none)"
	}
	return strings.Join(all, ", ")
}

// usageLocked sums rows and bytes of s's data sets other than skip. The
// caller holds s.mu.
func usageLocked(s *session, skip string) (rows uint64, bytes uint64) {
	for _, d := range s.datasets {
		if d.name == skip {
			continue
		}
		rows += d.rows
		bytes += d.bytes
	}
	return
}
