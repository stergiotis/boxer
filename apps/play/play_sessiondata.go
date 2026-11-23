package play

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_sessiondata.go — play's side of the session data service
// (ADR-0406): a statement that reads a session data set, session('name'),
// or joins an ad-hoc dataset with server tables, is prepared by the
// service, gated and rewritten here as any statement is, sent to the
// server endpoint inside the data session, and its lease released when its
// result stream closes. Whose data session it is follows the run: an
// agent's run reaches its task's, through the on-behalf-of context it
// carries; the person's reaches the session the window opened at its first
// definition, or the one ExecOptions.SessionTicket names.

// sessionReleaseTimeout bounds the release of a lease on a run's way out.
const sessionReleaseTimeout = 5 * time.Second

// sessionState is a client's link to the session data service: the bus it
// asks over, as the window it serves, and the ticket of the person's data
// session the window holds, empty until its first definition or a
// hand-over.
type sessionState struct {
	mu     sync.Mutex
	bus    app.BusI
	ticket string
	// opening serialises the lazy open, so two definitions racing on a
	// fresh window open one data session.
	opening sync.Mutex
}

// SetSessionBus wires the client to the session data service over bus,
// which must be the bus of the window the client runs for: the service
// attests an agent's call against that window, and a person's data session
// is held by it. Without it a statement that reads a session data set, or
// joins an ad-hoc dataset with server tables, is refused at dispatch.
func (inst *Client) SetSessionBus(bus app.BusI) {
	inst.session.mu.Lock()
	inst.session.bus = bus
	inst.session.mu.Unlock()
}

// sessionBus is the bus SetSessionBus wired, nil when none.
func (inst *Client) sessionBus() (bus app.BusI) {
	if inst == nil {
		return nil
	}
	inst.session.mu.Lock()
	defer inst.session.mu.Unlock()
	return inst.session.bus
}

// windowTicket is the ticket of the person's data session the window
// holds, empty when it holds none.
func (inst *Client) windowTicket() (ticket string) {
	inst.session.mu.Lock()
	defer inst.session.mu.Unlock()
	return inst.session.ticket
}

// setWindowTicket records the person's data session the window holds.
func (inst *Client) setWindowTicket(ticket string) {
	inst.session.mu.Lock()
	inst.session.ticket = ticket
	inst.session.mu.Unlock()
}

// ensureWindowTicket opens the person's data session for the window when
// it holds none, and returns its ticket.
func (inst *Client) ensureWindowTicket(ctx context.Context) (ticket string, err error) {
	inst.session.opening.Lock()
	defer inst.session.opening.Unlock()
	if ticket = inst.windowTicket(); ticket != "" {
		return
	}
	bus := inst.sessionBus()
	if bus == nil {
		return "", errNoSessionBus
	}
	ticket, err = sessiondata.Open(ctx, bus)
	if err != nil {
		return "", eh.Errorf("open a data session for the window: %w", err)
	}
	inst.setWindowTicket(ticket)
	return
}

// claimHandOver attaches the window to the data session a hand-over token
// names and records its ticket.
func (inst *Client) claimHandOver(ctx context.Context, token string) (err error) {
	bus := inst.sessionBus()
	if bus == nil {
		return errNoSessionBus
	}
	ticket, err := sessiondata.Claim(ctx, bus, token)
	if err != nil {
		return eh.Errorf("take over the opener's data session: %w", err)
	}
	inst.setWindowTicket(ticket)
	return
}

// handOverToken mints a hand-over token for the person's data session the
// window holds; empty, without error, when it holds none.
func (inst *Client) handOverToken(ctx context.Context) (token string, err error) {
	ticket := inst.windowTicket()
	bus := inst.sessionBus()
	if ticket == "" || bus == nil {
		return "", nil
	}
	return sessiondata.HandOver(ctx, bus, ticket)
}

var errNoSessionBus = errors.New("this window is not wired to the session data service")

// sessionTarget is the endpoint a data set lives on: the pinned endpoint,
// which must be a ClickHouse server rather than this process's
// introspection plane.
func (inst *Client) sessionTarget() (target string, err error) {
	target = inst.URL()
	if target == "" || target == introspect.LocalQueryEndpoint() {
		return "", eh.Errorf("session data sets live on a ClickHouse server endpoint, and this window is pinned to the introspection plane")
	}
	return
}

// sessionEndpoint is target as the service takes it: the client's
// credentials, and whether target may see sealed plaintext — the same
// declaration the server engine takes from the reach proof.
func (inst *Client) sessionEndpoint(target string) (e sessiondata.Endpoint) {
	return sessiondata.Endpoint{URL: target, User: inst.cfg.User, Password: inst.cfg.Password, ServesConfined: inst.servesConfined(target)}
}

// sessionText is sql as the service takes it: each keelson('…') name the
// window binds under a local name replaced by the dataset's global alias,
// which the dataset service's grant rules read. Handles and unbound names
// pass through.
func (inst *Client) sessionText(sql string) (text string) {
	inst.mu.RLock()
	var aliases map[string]string
	for local, o := range inst.datasetOrigins {
		if o.alias != "" && o.alias != local {
			if aliases == nil {
				aliases = make(map[string]string, len(inst.datasetOrigins))
			}
			aliases[local] = o.alias
		}
	}
	inst.mu.RUnlock()
	if len(aliases) == 0 {
		return sql
	}
	return keelsonsql.RewriteAliases(sql, aliases)
}

// sessionError is a refusal of the service as play reports it: a missing
// grant destination as an AgentLimitError naming it, as the agent limits
// report one.
func sessionError(what string, err error) error {
	if ge, ok := errors.AsType[*sessiondata.GrantError](err); ok {
		return &AgentLimitError{Reason: ge.Reason, Destination: ge.Destination}
	}
	if advice := runErrorAdvice(err.Error()); advice != "" {
		return eh.Errorf("%s: %w — %s", what, err, advice) //boxer:lint disable=CS013 reason="what names the step the service refused, and the advice what the server's text leaves unsaid"
	}
	return eh.Errorf("%s: %w", what, err) //boxer:lint disable=CS013 reason="what names the step the service refused"
}

// sessionHold is a statement prepared in its data session: the binding,
// and the lease it holds until the statement's response is drained or
// abandoned.
type sessionHold struct {
	bus  app.BusI
	ctx  context.Context
	b    sessiondata.Binding
	once sync.Once
}

// release ends the hold's lease, once. A nil hold is a no-op.
func (inst *sessionHold) release() {
	if inst == nil {
		return
	}
	inst.once.Do(func() {
		if inst.b.Lease == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(inst.ctx), sessionReleaseTimeout)
		defer cancel()
		if err := sessiondata.Release(ctx, inst.bus, inst.b.Lease); err != nil {
			log.Debug().Err(err).Msg("play: release a session lease; it lapses on its own")
		}
	})
}

// hasTask reports whether obo is an agent's call of a task, rather than
// nil or a gate without a task: the person's statement held to play's
// agent limits.
func hasTask(obo *app.OnBehalfOf) (yes bool) { return obo != nil && obo.Task != "" }

// sessionCaller is the caller the service sees for obo: the task's call,
// whose data session the service finds by attesting it, or the person's
// when obo carries no task — a gate whose limits are the client's to apply.
func sessionCaller(obo *app.OnBehalfOf) (c sessiondata.Caller) {
	if hasTask(obo) {
		c.OnBehalfOf = obo
	}
	return
}

// prepareSession readies sql, as its author wrote it, for a run that dec
// placed in a data session: an agent's run in its task's, the person's —
// gated by an OnBehalfOf without a task or not — in opts' ticket or the
// window's.
func (inst *Client) prepareSession(ctx context.Context, sql string, opts *ExecOptions, dec dispatchDecision) (hold *sessionHold, err error) {
	bus := inst.sessionBus()
	if bus == nil {
		return nil, errNoSessionBus
	}
	target, err := dec.target()
	if err != nil {
		return
	}
	agent, _ := inst.runAgent(opts)
	in := sessiondata.PrepareInput{Caller: sessionCaller(agent), Endpoint: inst.sessionEndpoint(target), SQL: inst.sessionText(sql)}
	if !hasTask(agent) {
		if opts != nil {
			in.Ticket = opts.SessionTicket
		}
		if in.Ticket == "" {
			in.Ticket = inst.windowTicket()
		}
		if in.Ticket == "" {
			if datasets, _ := sessiondata.References(in.SQL); len(datasets) > 0 {
				return nil, eb.Build().Strs("datasets", datasets).Errorf("this window holds no data session yet: keep a data set first, or open the window from one that holds the session")
			}
		}
	}
	b, err := sessiondata.Prepare(ctx, bus, in)
	if err != nil {
		return nil, sessionError("the session data service refused the statement", err)
	}
	return &sessionHold{bus: bus, ctx: ctx, b: b}, nil
}

// sessionRoute is the decision for a statement the session data service
// serves: one naming a session data set, or naming an ad-hoc dataset and a
// server table. ok is false for every other statement, which the resolver
// places. A statement the service would serve but cannot is refused here
// with the reason.
func (inst *Client) sessionRoute(residual string, base string) (dec dispatchDecision, ok bool) {
	datasets, keel := sessiondata.References(residual)
	plain := append(plainTables(residual), ladingMounts(residual)...)
	if len(datasets) == 0 && (len(keel) == 0 || len(plain) == 0) {
		return
	}
	ok = true
	var tables, adhoc []string
	for _, k := range keel {
		if isAdhocName(k) {
			adhoc = append(adhoc, k)
		} else {
			tables = append(tables, k)
		}
	}
	what := func() (s string) {
		var parts []string
		if len(datasets) > 0 {
			parts = append(parts, "session data sets ("+nameList(datasets)+")")
		}
		if len(adhoc) > 0 {
			parts = append(parts, "ad-hoc datasets ("+nameList(adhoc)+")")
		}
		if len(plain) > 0 {
			parts = append(parts, "plain tables ("+nameList(plain)+")")
		}
		return strings.Join(parts, " and ")
	}
	refuse := func(why string) (dispatchDecision, bool) {
		return dispatchDecision{class: dispatchClassRefused, reason: "names " + what() + "; " + why}, true
	}
	switch {
	case len(tables) > 0:
		return refuse("keelson tables (" + nameList(tables) + ") live only in this process, and the session data service attaches only ad-hoc datasets to a server statement, so no endpoint serves both")
	case inst.sessionBus() == nil:
		return refuse("only a window wired to the session data service runs it, and this one is not, so no endpoint serves it")
	case base == "" || base == introspect.LocalQueryEndpoint():
		return refuse("session data lives on a ClickHouse server endpoint, and this window is pinned to the introspection plane")
	}
	dec = dispatchDecision{targetURL: base, class: dispatchClassSession, reason: "names " + what() + ", run in the data session"}
	return
}

// isAdhocName reports whether a keelson('…') name reads an ad-hoc dataset —
// a handle, or a name no introspection table holds — rather than a table
// of this process's plane.
func isAdhocName(name string) (yes bool) {
	if adhocdata.IsHandle(name) {
		return true
	}
	_, table := introspect.Default.Lookup(name)
	return !table
}

// sessionStandIn is sql with each session('name') call replaced by a table
// name, so play's classifiers judge a statement before the service binds
// it: they do not know the macro, which the service replaces before the
// statement is sent. Unparseable SQL comes back unchanged.
func sessionStandIn(sql string) (out string) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return sql
	}
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		return ok && fn.Identifier() != nil && strings.EqualFold(nanopass.DecodeIdentifier(fn.Identifier().GetText()), sessiondata.FuncName)
	})
	if len(nodes) == 0 {
		return sql
	}
	rw := nanopass.NewRewriter(pr)
	for _, n := range nodes {
		name := "session"
		if al := n.(*grammar1.TableFunctionExprContext).TableArgList(); al != nil && len(al.AllTableArgExpr()) == 1 {
			name = strings.Trim(al.AllTableArgExpr()[0].GetText(), "'`\"")
		}
		nanopass.ReplaceNode(rw, n, "`__sd_"+strings.ReplaceAll(name, "`", "")+"`")
	}
	return nanopass.GetText(rw)
}

// SessionDefineInput is a definition DefineSessionDataset makes.
type SessionDefineInput struct {
	// Agent is the agent's call the definition is work of: the data set
	// joins its task's data session, and its grant must list
	// clickhouse:<host> and clickhouse-session:<host> of the pinned
	// endpoint and every ad-hoc dataset the definition reads. An
	// OnBehalfOf without a task holds the person's definition to the same
	// limits, in the person's data session. Nil is the person's,
	// unlimited.
	Agent *app.OnBehalfOf
	// SessionTicket names the person's data session when Agent names no
	// task; empty takes the one the client's window holds, which the
	// first definition opens. With a task it is not used.
	SessionTicket string
	// Name is the data set's name, an identifier; SQL its defining SELECT,
	// as its author wrote it: session('name') reads another data set and
	// keelson('name') an ad-hoc dataset, by a name the window binds or by
	// its global alias.
	Name string
	SQL  string
	// OrderBy is the kept table's sort key, by column name.
	OrderBy []string
}

// DefineSessionDataset defines, or redefines, a session data set on the
// pinned endpoint, as play's keep_as does: each keelson('…') name the
// window binds under a local name is sent as the dataset's global alias,
// the endpoint is declared as seeing sealed data only once it passed play's
// reach proof, and, for an Agent, play's agent limits judge the definition
// before it is sent. A destination the grant lacks comes back as an
// *AgentLimitError naming it; a definition the dispatch would not place —
// sealed data on an endpoint without the proof — as a *NotDispatchedError.
// The client must be wired with SetSessionBus.
func (inst *Client) DefineSessionDataset(ctx context.Context, in SessionDefineInput) (res sessiondata.DefineResult, err error) {
	if in.Agent != nil {
		if err = inst.sessionDefineLimits(ctx, in.Agent, in.SQL); err != nil {
			return
		}
	}
	return inst.defineSession(ctx, in.Agent, in.SessionTicket, in.Name, in.SQL, in.OrderBy)
}

// SessionDropInput is a drop DropSessionDataset makes.
type SessionDropInput struct {
	// Agent and SessionTicket name the data session as for
	// SessionDefineInput.
	Agent         *app.OnBehalfOf
	SessionTicket string
	// Name is the data set to drop; Cascade drops the data sets that read
	// it too, without which one others read is refused.
	Name    string
	Cascade bool
}

// DropSessionDataset drops a session data set from the data session in
// names, returning what it dropped as name@revision. The client must be
// wired with SetSessionBus.
func (inst *Client) DropSessionDataset(ctx context.Context, in SessionDropInput) (dropped []string, err error) {
	return inst.dropSession(ctx, in.Agent, in.SessionTicket, in.Name, in.Cascade)
}

// sessionDefineLimits is play's agent limits on a definition for obo: a
// plain read, the ad-hoc datasets it reads and the endpoint, judged as a
// statement placed in the data session, and the right to keep session
// tables there.
func (inst *Client) sessionDefineLimits(ctx context.Context, obo *app.OnBehalfOf, sql string) (err error) {
	target, err := inst.sessionTarget()
	if err != nil {
		return
	}
	text := inst.sessionText(strings.TrimSpace(sql))
	dec := inst.confine(text, dispatchDecision{targetURL: target, class: dispatchClassSession, reason: "a definition in the data session"})
	if err = checkAgentLimits(text, dec, obo, inst.withPublishers(ctx, text, obo, nil)); err != nil {
		return
	}
	if d := sessiondata.DestinationSession(endpointHost(target)); !slices.Contains(obo.Destinations, d) {
		return &AgentLimitError{Reason: "the grant does not list " + d, Destination: d}
	}
	return
}

// defineSession defines, or redefines, the data set name from sql in the
// data session of obo's task, or the person's — the one ticket names, else
// the window's, opened at its first definition — when obo names no task.
// The definition is sent as its author wrote it, its keelson('…') names
// mapped to global aliases; play's pre-execute rewrites do not run on it.
func (inst *Client) defineSession(ctx context.Context, obo *app.OnBehalfOf, ticket string, name string, sql string, orderBy []string) (res sessiondata.DefineResult, err error) {
	bus := inst.sessionBus()
	if bus == nil {
		return res, errNoSessionBus
	}
	target, err := inst.sessionTarget()
	if err != nil {
		return
	}
	in := sessiondata.DefineInput{Caller: sessionCaller(obo), Endpoint: inst.sessionEndpoint(target), Name: name,
		SQL: inst.sessionText(strings.TrimSpace(sql)), OrderBy: orderBy}
	if !hasTask(obo) {
		if in.Ticket = ticket; in.Ticket == "" {
			if in.Ticket, err = inst.ensureWindowTicket(ctx); err != nil {
				return
			}
		}
	}
	res, err = sessiondata.Define(ctx, bus, in)
	if err != nil {
		return res, sessionError("the data set was not kept", err)
	}
	return
}

// listSession lists the data sets of obo's task's data session, or of the
// window's when obo names no task; nothing when there is none yet.
func (inst *Client) listSession(ctx context.Context, obo *app.OnBehalfOf) (infos []sessiondata.DatasetInfo, err error) {
	bus := inst.sessionBus()
	if bus == nil {
		return nil, nil
	}
	ticket := ""
	if !hasTask(obo) {
		if ticket = inst.windowTicket(); ticket == "" {
			return nil, nil
		}
	}
	infos, _, err = sessiondata.List(ctx, bus, ticket, sessionCaller(obo).OnBehalfOf)
	if err != nil {
		return nil, sessionError("the data sets could not be listed", err)
	}
	return
}

// dropSession drops a data set of obo's task's data session, or of the
// person's — ticket's, else the window's — when obo names no task; cascade
// drops the data sets that read it too.
func (inst *Client) dropSession(ctx context.Context, obo *app.OnBehalfOf, ticket string, name string, cascade bool) (dropped []string, err error) {
	bus := inst.sessionBus()
	if bus == nil {
		return nil, errNoSessionBus
	}
	in := sessiondata.DropInput{Caller: sessionCaller(obo), Name: name, Cascade: cascade}
	if !hasTask(obo) {
		if in.Ticket = ticket; in.Ticket == "" {
			in.Ticket = inst.windowTicket()
		}
		if in.Ticket == "" {
			return nil, eh.Errorf("this window holds no data session")
		}
	}
	dropped, err = sessiondata.Drop(ctx, bus, in)
	if err != nil {
		return nil, sessionError("the data set was not dropped", err)
	}
	return
}
