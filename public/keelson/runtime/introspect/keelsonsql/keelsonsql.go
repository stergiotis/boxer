// Package keelsonsql is a nanopass pass that expands the keelson('<table>')
// table-function macro into a concrete table source (ADR-0094 §SD4). It
// gives queries a stable, transport-agnostic surface — write
//
//	SELECT name FROM keelson('env')
//
// and the pass rewrites keelson('env') to either a bare TEMPORARY-table
// reference (for the in-process engine, which feeds Arrow via the chlocal
// broker's InputTables) or a url('<live-base>/table/env','ArrowStream')
// reference (for an external clickhouse-local/-server reached over HTTP).
// The url() engine and the instance's address never appear in user
// queries, so the transport can evolve behind the macro and the live
// bound port is injected at expansion time rather than hard-coded.
package keelsonsql

import (
	"crypto/sha256" //boxer:lint disable=CS009 reason="names a TEMPORARY table after a call's resolved arguments; not a security boundary"
	"encoding/hex"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FuncName is the table-function name the macro uses.
const FuncName = "keelson"

// References reports the table names sql addresses through the
// keelson('<name>') table-function macro, in first-appearance order and
// deduplicated. It states a fact about the SQL and attaches no meaning to
// it: a non-empty result says the statement reaches into the
// introspection plane's namespace, not that it should execute anywhere in
// particular. Callers that route on it own that policy.
//
// Total and best-effort, so there is no error a caller would have to act
// on: unparseable SQL and macro-free SQL both return nil, and a malformed
// call (wrong arity, an argument that is neither a quoted literal nor a
// bare identifier) is skipped rather than reported — the same statement
// surfaces a precise error when it executes. Only the table-function
// position counts, so a scalar keelson('env') in a SELECT list and a
// qualified keelson.env table reference are both absent from the result.
func References(sql string) (names []string) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return nil
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(calls))
	names = make([]string, 0, len(calls))
	for _, fn := range calls {
		call, argErr := parseCall(fn)
		if argErr != nil {
			continue
		}
		name := call.Name
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return
}

// BareNamePass rewrites keelson('x') -> x. Used by the in-process engine,
// where x arrives as a TEMPORARY table; the macro is sugar there, but it
// is also required for correctness because a TEMPORARY table cannot carry
// a database qualifier.
func BareNamePass(reg *introspect.Registry) nanopass.Pass {
	return nanopass.LiftBodyPass(
		"KeelsonExpandBare",
		func(sql string) (string, error) {
			return expand(reg, sql, func(name string, _ introspect.Provider) string { return name })
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
}

// URLPass rewrites keelson('x') -> url('<baseURL>/table/x','ArrowStream'),
// injecting baseURL (the running HTTP table source's BaseURL()). Used by a
// preprocessor in front of an external clickhouse-local/-server. A sealed
// dataset (introspect.EncryptedDatasetI) additionally carries its explicit
// structure as the third url() argument, so clickhouse applies the publish
// gate's bounded-type mapping rather than its own Arrow inference, and the
// /table endpoint serves the plaintext by opening the record (ADR-0240
// §SD2/§SD3).
func URLPass(reg *introspect.Registry, baseURL string) nanopass.Pass {
	target := urlTarget(baseURL)
	return nanopass.LiftBodyPass(
		"KeelsonExpandURL",
		func(sql string) (string, error) {
			return expand(reg, sql, target)
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
}

// SplitPass rewrites keelson('x') -> x for an ordinary provider and ->
// url('<baseURL>/table/x', …) for a sealed dataset. It is the in-process
// engine's rewrite once it also serves the HTTP endpoint (ADR-0253 §SD5):
// an ordinary table arrives as a TEMPORARY table, projected in-process,
// while a sealed dataset keeps the one route that decrypts it — the
// loopback plane, by handle (ADR-0145 §SD2) — and is never snapshotted.
func SplitPass(reg *introspect.Registry, baseURL string) nanopass.Pass {
	url := urlTarget(baseURL)
	return nanopass.LiftBodyPass(
		"KeelsonExpandSplit",
		func(sql string) (string, error) {
			return expand(reg, sql, func(name string, p introspect.Provider) string {
				if _, isEnc := p.(introspect.EncryptedDatasetI); isEnc {
					return url(name, p)
				}
				return name
			})
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
}

// urlTarget is the url() spelling of a table on the HTTP source at
// baseURL. A sealed dataset additionally carries its explicit structure as
// the third argument (see URLPass).
func urlTarget(baseURL string) func(name string, p introspect.Provider) string {
	base := strings.TrimRight(baseURL, "/")
	return func(name string, p introspect.Provider) string {
		u := "url('" + base + "/table/" + name + "', 'ArrowStream'"
		if enc, isEnc := p.(introspect.EncryptedDatasetI); isEnc {
			u += ", " + sqlQuoteLiteral(enc.Structure())
		}
		return u + ")"
	}
}

// sqlQuoteLiteral single-quotes a ClickHouse string literal, escaping
// backslashes and single quotes (the structure string carries 'UTC').
func sqlQuoteLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

// RewriteAliases rewrites every reference to a bound alias — the
// `keelson('<alias>')` call and the bare, unqualified table name `<alias>`
// alike — to `keelson('<handle>')`, leaving every other keelson(...) call
// and all other SQL untouched (ADR-0240 §SD7, from ADR-0134 §SD4). It is
// the client-side indirection that lets a buffer name a stable alias while
// an instance binds it to an ephemeral dataset handle. Both spellings are
// rewritten because the placement wall (ADR-0145) inspects both, so an
// alias must reach the wall under neither. Unlike expand, an unbound or
// unknown name is not an error: it passes through for the downstream
// (server-side) keelson pass to resolve or reject. Best-effort — a parse
// failure returns the input unchanged, since the same SQL will surface a
// clear error when it executes. The handles come from a validated binding
// map, so no quoting or escaping is needed.
func RewriteAliases(sql string, bindings map[string]string) (result string) {
	if len(bindings) == 0 {
		return sql
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return sql
	}
	rw := nanopass.NewRewriter(pr)
	changed := false
	for _, fn := range findCalls(pr) {
		call, argErr := parseCall(fn)
		if argErr != nil {
			continue // leave a malformed call for the server to reject
		}
		handle, ok := bindings[call.Name]
		if !ok {
			continue // unbound names pass through untouched
		}
		// The name argument alone, so a call's named arguments survive.
		nanopass.ReplaceNode(rw, call.nameNode, "'"+handle+"'")
		changed = true
	}
	// Bare names: a plain, unqualified relation in any FROM/JOIN — not a
	// CTE, a subquery or a table function. The node is the identifier
	// alone, so `items AS i` keeps its alias.
	if scopes, sErr := nanopass.BuildScopes(pr, ""); sErr == nil {
		for _, scope := range nanopass.FlattenScopes(scopes) {
			for _, ts := range scope.Tables {
				if ts.IsCTE || ts.IsSubquery || ts.IsFunction || ts.Database != "" || ts.Node == nil {
					continue
				}
				handle, ok := bindings[ts.Table]
				if !ok {
					continue
				}
				nanopass.ReplaceNode(rw, ts.Node, FuncName+"('"+handle+"')")
				changed = true
			}
		}
	}
	if !changed {
		return sql
	}
	return nanopass.GetText(rw)
}

// RewriteToBare runs BareNamePass over sql.
func RewriteToBare(reg *introspect.Registry, sql string) (string, error) {
	return BareNamePass(reg).Run(sql)
}

// RewriteToURL runs URLPass over sql.
func RewriteToURL(reg *introspect.Registry, baseURL, sql string) (string, error) {
	return URLPass(reg, baseURL).Run(sql)
}

// RewriteSplit runs SplitPass over sql.
func RewriteSplit(reg *introspect.Registry, baseURL, sql string) (string, error) {
	return SplitPass(reg, baseURL).Run(sql)
}

// expand finds every keelson('x') table function in sql and replaces it
// with target(x). An unknown or malformed table name is an error: the
// name is validated against reg (so it can never reach a url() path or a
// table identifier unless it is a registered, identifier-clean name).
func expand(reg *introspect.Registry, sql string, target func(name string, p introspect.Provider) string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return "", eh.Errorf("keelsonsql: parse: %w", err)
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return sql, nil
	}
	rw := nanopass.NewRewriter(pr)
	for _, fn := range calls {
		call, argErr := parseCall(fn)
		if argErr != nil {
			return "", argErr
		}
		p, ok := reg.Lookup(call.Name)
		if !ok {
			return "", eb.Build().Str("name", call.Name).Errorf("keelsonsql: unknown keelson table")
		}
		if len(call.Args) > 0 {
			// Arguments are resolved against the query's parameters, which
			// only ExpandWithArgs is given (ADR-0290 §SD2).
			return "", eb.Build().Str("name", call.Name).Errorf("keelsonsql: keelson() with named arguments is resolved by the introspection engine, not on this path")
		}
		nanopass.ReplaceNode(rw, fn, target(call.Name, p))
	}
	return nanopass.GetText(rw), nil
}

// ArgCall is one keelson() call with named arguments, resolved for a run:
// the provider's table, the TEMPORARY table the statement now reads in its
// place, and the arguments' value texts by name.
type ArgCall struct {
	Table string
	Temp  string
	Raw   map[string]string
}

// ExpandWithArgs is the in-process engine's rewrite (ADR-0290 §SD2): every
// keelson() call becomes a bare TEMPORARY-table reference, as BareNamePass
// does, except a sealed dataset, which becomes url() against sealedBaseURL
// when it is non-empty (SplitPass). A call with named arguments takes each
// value from its literal or, for a `{slot:Type}` placeholder, from params by
// bare name, a `SET param_<name>` prelude binding as param_<name> does; the
// arguments are checked and typed against the provider's declaration, and
// the call is renamed to a TEMPORARY table of its own, so
// two calls with different values are two tables. Those calls are returned
// for the engine to snapshot; calls without arguments are not, since the
// engine finds them by name as before.
func ExpandWithArgs(reg *introspect.Registry, sealedBaseURL, sql string, params map[string]string) (result string, argCalls []ArgCall, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return "", nil, eh.Errorf("keelsonsql: parse: %w", err)
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return sql, nil, nil
	}
	// A `SET param_<name>` prelude binds as a request's param_<name> does
	// (NewConstScope); it stays in the statement for the engine, which
	// binds it too.
	scope, err := NewConstScope(pr, params)
	if err != nil {
		return "", nil, err
	}
	var url func(string, introspect.Provider) string
	if sealedBaseURL != "" {
		url = urlTarget(sealedBaseURL)
	}
	rw := nanopass.NewRewriter(pr)
	seen := make(map[string]struct{})
	for _, fn := range calls {
		call, argErr := parseCall(fn)
		if argErr != nil {
			return "", nil, argErr
		}
		p, ok := reg.Lookup(call.Name)
		if !ok {
			return "", nil, eb.Build().Str("name", call.Name).Errorf("keelsonsql: unknown keelson table")
		}
		_, sealed := p.(introspect.EncryptedDatasetI)
		if len(call.Args) == 0 {
			if sealed && url != nil {
				nanopass.ReplaceNode(rw, fn, url(call.Name, p))
			} else {
				nanopass.ReplaceNode(rw, fn, call.Name)
			}
			continue
		}
		if sealed {
			return "", nil, eb.Build().Str("name", call.Name).Errorf("keelsonsql: a sealed dataset takes no arguments")
		}
		var raw map[string]string
		raw, err = call.values(scope)
		if err != nil {
			return "", nil, err
		}
		ap, takes := p.(introspect.ArgsProviderI)
		if !takes {
			return "", nil, eb.Build().Str("name", call.Name).Errorf("keelsonsql: the keelson table takes no arguments")
		}
		var args introspect.Args
		args, err = introspect.ResolveArgs(ap.Args(), raw)
		if err != nil {
			return "", nil, eb.Build().Str("name", call.Name).Errorf("keelsonsql: %w", err)
		}
		temp := argTableName(call.Name, args.Key())
		nanopass.ReplaceNode(rw, fn, temp)
		if _, dup := seen[temp]; !dup {
			seen[temp] = struct{}{}
			argCalls = append(argCalls, ArgCall{Table: call.Name, Temp: temp, Raw: raw})
		}
	}
	return nanopass.GetText(rw), argCalls, nil
}

// argTableName is the TEMPORARY table a call with arguments is read from:
// the provider's name and a digest of the resolved values, within the
// 64-byte identifier limit.
func argTableName(name string, key string) (temp string) {
	sum := sha256.Sum256([]byte(name + "\x00" + key))
	suffix := "__a" + hex.EncodeToString(sum[:8])
	if len(name)+len(suffix) > 64 {
		name = name[:64-len(suffix)]
	}
	return name + suffix
}

// findCalls returns every keelson(...) table-function call in pr, in
// document order. The match predicate lives here alone so the fact
// extraction (References) and the rewrites (RewriteAliases, expand) can
// never drift apart about what counts as a macro call — a scalar
// keelson('env') in a SELECT list is not a TableFunctionExpr and so is
// invisible to all three.
func findCalls(pr *nanopass.ParseResult) (calls []*grammar1.TableFunctionExprContext) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		return ok && IsCall(fn)
	})
	calls = make([]*grammar1.TableFunctionExprContext, 0, len(nodes))
	for _, n := range nodes {
		calls = append(calls, n.(*grammar1.TableFunctionExprContext))
	}
	return
}

// callArg is one named argument of a keelson() call: its name and the
// constant expression that is its value — a literal, a `{slot:Type}`
// parameter or a WITH constant — evaluated when the call is resolved.
type callArg struct {
	key  string
	expr grammar1.IColumnExprContext
}

// call is a parsed keelson(...) call: the table name, the node that spells
// it, and the named arguments in source order.
type call struct {
	Name     string
	Args     []callArg
	nameNode antlr.ParserRuleContext
}

// values resolves the arguments' value texts in scope: a literal as
// written, a placeholder from the parameters by bare name and decoded as
// ClickHouse reads a parameter's value, a WITH constant as its value.
func (inst call) values(scope *ConstScope) (raw map[string]string, err error) {
	raw = make(map[string]string, len(inst.Args))
	for _, a := range inst.Args {
		var c Constant
		c, err = EvalConstant(a.expr, scope)
		if err == nil {
			raw[a.key], err = c.Text()
		}
		if err != nil {
			return nil, eb.Build().Str("name", inst.Name).Str("arg", a.key).Errorf("%w", err)
		}
	}
	return
}

// parseCall reads a keelson(...) call: the table name first — a quoted
// literal (keelson('env')) or a bare identifier (keelson(env)) — then any
// number of named arguments `key = value`, where value is a literal or a
// `{slot:Type}` placeholder (ADR-0290 §SD1). A key may appear once.
func parseCall(fn *grammar1.TableFunctionExprContext) (c call, err error) {
	al := fn.TableArgList()
	if al == nil {
		return c, eh.Errorf("keelsonsql: keelson() needs a table-name argument")
	}
	args := al.AllTableArgExpr()
	if len(args) == 0 {
		return c, eh.Errorf("keelsonsql: keelson() needs a table-name argument")
	}
	first := args[0]
	c.nameNode = first
	switch {
	case first.Literal() != nil:
		t := first.Literal().GetText()
		if len(t) < 2 || t[0] != '\'' || t[len(t)-1] != '\'' {
			return c, eb.Build().Str("arg", t).Errorf("keelsonsql: keelson() argument must be a quoted table name")
		}
		c.Name = t[1 : len(t)-1]
	case first.NestedIdentifier() != nil:
		c.Name = nanopass.DecodeIdentifier(first.NestedIdentifier().GetText())
	default:
		return c, eh.Errorf("keelsonsql: unsupported keelson() argument (use keelson('table'))")
	}
	keys := make(map[string]struct{}, len(args)-1)
	for _, a := range args[1:] {
		var ca callArg
		ca, err = parseNamedArg(a)
		if err != nil {
			return c, eb.Build().Str("name", c.Name).Errorf("%w", err)
		}
		if _, dup := keys[ca.key]; dup {
			return c, eb.Build().Str("name", c.Name).Str("arg", ca.key).Errorf("keelsonsql: keelson() names an argument twice")
		}
		keys[ca.key] = struct{}{}
		c.Args = append(c.Args, ca)
	}
	return
}

// parseNamedArg reads `key = value`; the value is evaluated as a constant
// when the call is resolved (call.values).
func parseNamedArg(a grammar1.ITableArgExprContext) (ca callArg, err error) {
	eq, ok := a.ColumnExpr().(*grammar1.ColumnExprPrecedence3Context)
	if !ok || eq.EQ_SINGLE() == nil || len(eq.AllColumnExpr()) != 2 {
		return ca, eb.Build().Str("arg", a.GetText()).Errorf("keelsonsql: keelson() takes named arguments after the table name, as key = value")
	}
	sides := eq.AllColumnExpr()
	id, ok := sides[0].(*grammar1.ColumnExprIdentifierContext)
	if !ok {
		return ca, eb.Build().Str("arg", a.GetText()).Errorf("keelsonsql: a keelson() argument's name must be an identifier")
	}
	ca.key = nanopass.DecodeIdentifier(id.GetText())
	if !introspect.ValidTableName(ca.key) {
		return ca, eb.Build().Str("arg", ca.key).Errorf("keelsonsql: a keelson() argument's name must be an identifier")
	}
	ca.expr = sides[1]
	return
}

// IsCall reports whether fn is a keelson(...) table-function call.
func IsCall(fn *grammar1.TableFunctionExprContext) (yes bool) {
	id := fn.Identifier()
	return id != nil && strings.EqualFold(nanopass.DecodeIdentifier(id.GetText()), FuncName)
}

// ResolveCall resolves one keelson(...) call for a run without rewriting
// anything: the registered provider it names and its arguments' value
// texts, each evaluated as a constant in scope. It is what a
// reader that answers the call itself — rather than handing SQL to
// ClickHouse — needs (ADR-0290 §SD3). Arguments are checked against the
// provider's declaration; a sealed dataset is refused, since only the
// loopback source can open one.
func ResolveCall(reg *introspect.Registry, fn *grammar1.TableFunctionExprContext, scope *ConstScope) (p introspect.Provider, raw map[string]string, err error) {
	if !IsCall(fn) {
		return nil, nil, eb.Build().Str("fn", fn.GetText()).Errorf("keelsonsql: not a keelson() call")
	}
	c, err := parseCall(fn)
	if err != nil {
		return nil, nil, err
	}
	p, ok := reg.Lookup(c.Name)
	if !ok {
		return nil, nil, eb.Build().Str("name", c.Name).Errorf("keelsonsql: unknown keelson table")
	}
	if _, sealed := p.(introspect.EncryptedDatasetI); sealed {
		return nil, nil, eb.Build().Str("name", c.Name).Errorf("keelsonsql: a sealed dataset is read through the introspection source, not here")
	}
	raw, err = c.values(scope)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		raw = nil
	}
	if ap, takes := p.(introspect.ArgsProviderI); takes {
		if _, err = introspect.ResolveArgs(ap.Args(), raw); err != nil {
			return nil, nil, eb.Build().Str("name", c.Name).Errorf("keelsonsql: %w", err)
		}
	} else if len(raw) > 0 {
		return nil, nil, eb.Build().Str("name", c.Name).Errorf("keelsonsql: the keelson table takes no arguments")
	}
	return
}
