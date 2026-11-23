package play

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
)

// Play's agent limits (ADR-0270 §SD2): a run an agent's work causes is
// checked on the statement about to be sent — the residual, after the
// client-side rewrites — and fails before sending when the grant does not
// cover it. The limits fail a run; they never change it.

// AgentLimitError is a run an agent's work caused that the limits refused.
// Destination is set when what was missing is a destination of the grant.
type AgentLimitError struct {
	Reason      string
	Destination string
}

func (inst *AgentLimitError) Error() string { return "agent limit: " + inst.Reason }

// DestinationKeelson is how a grant names an introspection table.
func DestinationKeelson(table string) (name string) { return "keelson:" + table }

// DestinationKeelsonBundle is how a grant names an ad-hoc bundle; it
// covers every dataset of the bundle (ADR-0288 §SD4).
func DestinationKeelsonBundle(bundle string) (name string) {
	return adhocdata.DestinationBundle(bundle)
}

// DestinationClickHouse is how a grant names an endpoint, by host as
// endpointHost gives it; the session data service spells it alike.
func DestinationClickHouse(host string) (name string) { return sessiondata.DestinationServer(host) }

// syntaxDetail is ": " and the parser's line:column diagnostics for a
// statement the grammar rejected, or "" when err carries none.
func syntaxDetail(err error) (detail string) {
	if d := nanopass.SyntaxErrorDiagnostics(err); len(d) > 0 {
		detail = ": " + strings.Join(d, "; ")
	}
	return
}

// bracketHint names the first bracket of sql that has no partner, or is ""
// when they all pair. A parser reports a missing bracket where the text
// stops making sense, which is often lines after the bracket that was left
// open; the pairing itself says where the fault is. Brackets inside string
// literals, quoted identifiers and comments are not brackets, which the
// lexer knows.
func bracketHint(sql string) (hint string) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	closerOf := map[int]int{
		grammar1.ClickHouseLexerLPAREN:   grammar1.ClickHouseLexerRPAREN,
		grammar1.ClickHouseLexerLBRACKET: grammar1.ClickHouseLexerRBRACKET,
		grammar1.ClickHouseLexerLBRACE:   grammar1.ClickHouseLexerRBRACE,
	}
	at := func(tok antlr.Token) string {
		return "'" + tok.GetText() + "' at " + strconv.Itoa(tok.GetLine()) + ":" + strconv.Itoa(tok.GetColumn())
	}
	var open []antlr.Token
	for _, tok := range stream.GetAllTokens() {
		if tok.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		typ := tok.GetTokenType()
		if _, opens := closerOf[typ]; opens {
			open = append(open, tok)
			continue
		}
		if typ != grammar1.ClickHouseLexerRPAREN && typ != grammar1.ClickHouseLexerRBRACKET && typ != grammar1.ClickHouseLexerRBRACE {
			continue
		}
		if len(open) == 0 {
			return "; the " + at(tok) + " closes nothing"
		}
		last := open[len(open)-1]
		if closerOf[last.GetTokenType()] != typ {
			return "; the " + at(tok) + " stands where the " + at(last) + " is still open"
		}
		open = open[:len(open)-1]
	}
	if len(open) > 0 {
		return "; the " + at(open[len(open)-1]) + " is never closed"
	}
	return ""
}

// describeHint says what to call instead of a DESCRIBE statement, or "" for
// any other text. The grammar reads queries, and a DESCRIBE is not one: the
// server takes it neither as a subquery nor under the wrappers a run adds,
// so it has no place among the statements the limits classify. What it asks
// for is in the catalog: describe_table reads a table's columns under the
// same grant entry a run needs. For `DESCRIBE [TABLE] name` the hint
// carries the name as describe_table takes it.
func describeHint(sql string) (hint string) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	var toks []antlr.Token
	for _, tok := range stream.GetAllTokens() {
		if tok.GetChannel() == antlr.TokenDefaultChannel && tok.GetTokenType() != antlr.TokenEOF {
			toks = append(toks, tok)
		}
	}
	if len(toks) == 0 {
		return ""
	}
	if t := toks[0].GetTokenType(); t != grammar1.ClickHouseLexerDESCRIBE && t != grammar1.ClickHouseLexerDESC {
		return ""
	}
	hint = "DESCRIBE is not a statement an agent runs; the " + opDescribeTable + " operation reads a table's columns"
	rest := toks[1:]
	if len(rest) > 0 && rest[0].GetTokenType() == grammar1.ClickHouseLexerTABLE {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[len(rest)-1].GetTokenType() == grammar1.ClickHouseLexerSEMICOLON {
		rest = rest[:len(rest)-1]
	}
	// A name is an identifier, quoted or bare, or a keyword written where
	// a name stands (`system`, `tables`).
	name := func(tok antlr.Token) (decoded string, ok bool) {
		decoded = tok.GetText()
		if tok.GetTokenType() == grammar1.ClickHouseLexerIDENTIFIER {
			decoded = nanopass.DecodeIdentifier(decoded)
			return decoded, decoded != "" && !strings.ContainsAny(decoded, ".\"\n")
		}
		for _, r := range decoded {
			if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				return "", false
			}
		}
		return decoded, decoded != ""
	}
	var table string
	switch len(rest) {
	case 1:
		if n, ok := name(rest[0]); ok {
			table = n
		}
	case 3:
		db, dbOk := name(rest[0])
		n, ok := name(rest[2])
		if dbOk && ok && rest[1].GetTokenType() == grammar1.ClickHouseLexerDOT {
			table = db + "." + n
		}
	}
	if table != "" {
		hint += ": call it with table \"" + table + "\""
	} else {
		hint += " when called with a table's name, and " + opGetSchema + " the columns of a result"
	}
	hint += "; " + opListTables + " lists the endpoint's tables"
	return
}

// checkAgentLimits checks a residual against the grant's destinations:
// a plain read, keelson() tables it lists, and an endpoint it lists unless
// the run goes to the host's introspection engine. aliasOf maps a bound
// dataset handle to the names a grant lists it by: its global alias, and
// its bundle when it belongs to one.
func checkAgentLimits(residual string, dec dispatchDecision, obo *app.OnBehalfOf, aliasOf map[string]datasetName) (err error) {
	// A statement no endpoint may serve is refused for that reason, not for
	// a destination: its decision names no endpoint a grant could list.
	if dec.class == dispatchClassRefused {
		_, err = dec.target()
		return
	}
	classified := residual
	if dec.class == dispatchClassSession {
		// The session data service replaces session('…') by its table
		// before the statement is sent, and checks the bound statement
		// again; the classifiers here judge it as the table it becomes.
		classified = sessionStandIn(residual)
	}
	pr, perr := nanopass.Parse(classified)
	if perr != nil {
		if hint := describeHint(classified); hint != "" {
			return &AgentLimitError{Reason: "boxer's grammar does not parse the statement, so it cannot be classified and an agent cannot run it: " + hint}
		}
		return &AgentLimitError{Reason: "boxer's grammar does not parse the statement, so it cannot be classified and an agent cannot run it" + syntaxDetail(perr) + bracketHint(classified)}
	}
	class, witnesses, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil || class != analysis.QuerySecurityRead {
		why := "the statement is " + class.String() + ", not a plain read"
		if len(witnesses) > 0 && witnesses[0].Name != "" {
			why += " (" + witnesses[0].Name + ": " + witnesses[0].Describe() + ")"
		}
		return &AgentLimitError{Reason: why}
	}
	for _, t := range keelsonsql.References(residual) {
		// A bound dataset reaches the residual as its ephemeral handle; the
		// grant names it by its global alias, or its bundle, never by the
		// local name the buffer wrote — that names another dataset in
		// another window — and a refusal asks for the bundle first.
		n := datasetNameIn(aliasOf, t)
		if !n.coveredFor(obo, t) {
			dests := n.destinations()
			return &AgentLimitError{Reason: "the grant does not list " + dests[0], Destination: dests[0]}
		}
	}
	if dec.class == dispatchClassIntrospection {
		return
	}
	host := endpointHost(dec.targetURL)
	if host == "" {
		// No grant entry names a run without an endpoint, and none is asked
		// for: "clickhouse:" would cover nothing.
		return &AgentLimitError{Reason: "the run names no endpoint, so no grant destination covers it"}
	}
	if !slices.Contains(obo.Destinations, DestinationClickHouse(host)) {
		return &AgentLimitError{Reason: "the grant does not list " + DestinationClickHouse(host), Destination: DestinationClickHouse(host)}
	}
	return
}

// datasetNameIn is the grant's view of the keelson('…') name t a residual
// reads: its entry in aliasOf when t is a bound handle, else the name
// itself, still named by its bundle.
func datasetNameIn(aliasOf map[string]datasetName, t string) (n datasetName) {
	n, bound := aliasOf[t]
	if !bound {
		n = datasetName{local: t, alias: t}
		if bundle, _, ok := strings.Cut(t, adhocdata.BundleAliasSeparator); ok {
			n.bundle = bundle
		}
	}
	return
}

// coveredFor reports whether obo's grant lets a run read the dataset the
// residual names as t: the grant lists it, its alias or its bundle, or the
// task published it — a task runs on what it published without a grant
// entry (ADR-0288 §SD4), the dataset service having attested the publish.
func (inst datasetName) coveredFor(obo *app.OnBehalfOf, t string) (covered bool) {
	covered = slices.Contains(obo.Destinations, DestinationKeelson(t)) ||
		(inst.publisherTask != "" && inst.publisherTask == obo.Task)
	for _, d := range inst.destinations() {
		covered = covered || slices.Contains(obo.Destinations, d)
	}
	return
}

// publisherLookupTimeout bounds asking the dataset service who published
// a dataset.
const publisherLookupTimeout = 2 * time.Second

// withPublishers is aliasOf with the publishing task of each ad-hoc
// dataset residual reads that obo's grant does not list and the client
// holds no record of, as the ad-hoc dataset service answers it, so the
// rule that a task reads what it published holds for a client no follower
// told. A bound handle takes the publisher only when it is still the
// alias's newest dataset. aliasOf is the caller's own map, as
// datasetAliasOf makes it, and gains the entries. For the person, or a
// client without a bus, it comes back as it is. It asks over the bus, so it
// is not called on the render goroutine.
func (inst *Client) withPublishers(ctx context.Context, residual string, obo *app.OnBehalfOf, aliasOf map[string]datasetName) (out map[string]datasetName) {
	out = aliasOf
	if obo == nil || obo.Task == "" {
		return
	}
	bus := inst.sessionBus()
	if bus == nil {
		return
	}
	for _, t := range keelsonsql.References(residual) {
		n := datasetNameIn(out, t)
		_, bound := out[t]
		if n.publisherTask != "" || n.coveredFor(obo, t) || adhocdata.IsHandle(n.alias) {
			continue
		}
		if introspect.IsLocalTable(t) && !bound {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, publisherLookupTimeout)
		res, err := adhocdata.ResolveRequest(rctx, bus, n.alias)
		cancel()
		if err != nil || res.Origin.PublisherTask == "" || (bound && res.Handle != t) {
			continue
		}
		if out == nil {
			out = make(map[string]datasetName, 1)
		}
		n.publisherTask = res.Origin.PublisherTask
		out[t] = n
	}
	return
}

// refuseAgentRun checks the buffer the way the run will, before the run is
// accepted, so a run the grant does not cover is refused with the
// destination it needs rather than failing after it was applied.
func (inst *PlayApp) refuseAgentRun(obo *app.OnBehalfOf) (err error) {
	return inst.refuseAgentRunOf(obo, inst.sql)
}

// refuseAgentRunOf is refuseAgentRun for a given text: a subquery run is
// checked on the narrowed text it ships.
func (inst *PlayApp) refuseAgentRunOf(obo *app.OnBehalfOf, sql string) (err error) {
	return refuseAgentRun(inst.client, obo, sql)
}

// refuseAgentRun is refuseAgentStatement for a call that starts a run: a
// statement refused only because its endpoint has not yet demonstrated it
// may see sealed data, while a demonstration is under way, is judged as
// the placement it waits for, since the run waits for the demonstration
// off the frame and decides again (dispatchAwaitingReach).
func refuseAgentRun(client *Client, obo *app.OnBehalfOf, sql string) (err error) {
	return refuseAgentStatementAs(client, obo, sql, true)
}

// refuseAgentStatement is the agent limits' refusal of sql as the client
// would send it for obo, naming the destination a grant lacks: the check
// run makes at the call, and explain_sql's for the statement it explains.
// A nil client refuses nothing.
func refuseAgentStatement(client *Client, obo *app.OnBehalfOf, sql string) (err error) {
	return refuseAgentStatementAs(client, obo, sql, false)
}

// refuseAgentStatementAs is refuseAgentStatement; run says a run follows
// the check, which then judges a placement waiting on a reach
// demonstration as that placement.
func refuseAgentStatementAs(client *Client, obo *app.OnBehalfOf, sql string, run bool) (err error) {
	if client == nil {
		return
	}
	residual, _, _ := client.rewriteFor(obo, sql, nil)
	dec := client.dispatchResidual(residual, "")
	if run && dec.pending != nil {
		dec = *dec.pending
	}
	lerr := checkAgentLimits(residual, dec, obo, client.datasetAliasOf())
	if limit, ok := lerr.(*AgentLimitError); ok {
		if limit.Destination != "" {
			return app.RefuseForDestinations(lerr.Error(), limit.Destination)
		}
		return app.RefuseOperation(lerr.Error())
	}
	if nd, ok := errors.AsType[*NotDispatchedError](lerr); ok {
		return app.RefuseOperation("play does not send the statement: " + nd.Reason)
	}
	return
}

// rewriteFor is the client-side rewrite an agent's work may make: the whole
// rewrite for the person (obo nil) or under a grant that lists the endpoint,
// and otherwise [Client.buildResidualOffline]. catalog says which was made. validate_sql, trace_rewrite and the run's checks all
// take the rewrite from here, so an agent's work reaches the endpoint only
// under the grant (ADR-0270 §SD2) and what they report is what the run sends.
func (inst *Client) rewriteFor(obo *app.OnBehalfOf, sql string, observe func(passreg.ApplyObservation)) (residual string, params map[string]string, catalog bool) {
	catalog = obo == nil || slices.Contains(obo.Destinations, endpointDestination(inst))
	if catalog {
		residual, params = inst.buildResidualObserved(sql, observe)
	} else {
		residual, params = inst.buildResidualOffline(sql, observe)
	}
	return
}

// dispatchFor is [Client.Dispatch] resolved from the rewrite rewriteFor makes
// for obo, so the decision of an agent's run sends no probe its grant does not
// cover either. A nil obo is Dispatch.
func (inst *Client) dispatchFor(obo *app.OnBehalfOf, sql string, affinity string) (dec dispatchDecision) {
	residual, _, _ := inst.rewriteFor(obo, sql, nil)
	dec = inst.dispatchResidual(residual, affinity)
	return
}

// statementBounds refuses a statement validate_sql and trace_rewrite do not
// take: an empty one, or one past the 16 KiB bound.
func statementBounds(stmt string) (err error) {
	switch {
	case strings.TrimSpace(stmt) == "":
		return app.RefuseOperation("the buffer is empty; pass sql")
	case len(stmt) > schemaMaxStatement:
		return app.RefuseOperation("the statement is longer than 16 KiB")
	}
	return
}

// endpointHost is the host:port of an endpoint URL, or the URL itself when
// it does not parse: the one host every grant destination of an endpoint
// is spelled with, here and in the session data service.
func endpointHost(raw string) (host string) { return sessiondata.EndpointHost(raw) }
