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
		name, argErr := tableArg(fn)
		if argErr != nil {
			continue
		}
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
// preprocessor in front of an external clickhouse-local/-server. An
// ad-hoc dataset (introspect.EncryptedDatasetI) additionally carries its
// explicit structure as the third url() argument, so clickhouse applies
// the controlled bounded-type mapping (ADR-0134 SD1) rather than its own
// Arrow inference, and the /table endpoint streams the decrypt
// (ADR-0134 §SD3, revised).
func URLPass(reg *introspect.Registry, baseURL string) nanopass.Pass {
	base := strings.TrimRight(baseURL, "/")
	return nanopass.LiftBodyPass(
		"KeelsonExpandURL",
		func(sql string) (string, error) {
			return expand(reg, sql, func(name string, p introspect.Provider) string {
				u := "url('" + base + "/table/" + name + "', 'ArrowStream'"
				if enc, isEnc := p.(introspect.EncryptedDatasetI); isEnc {
					u += ", " + sqlQuoteLiteral(enc.Structure())
				}
				return u + ")"
			})
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
}

// sqlQuoteLiteral single-quotes a ClickHouse string literal, escaping
// backslashes and single quotes (the structure string carries 'UTC').
func sqlQuoteLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

// RewriteAliases rewrites keelson('<alias>') to keelson('<handle>') for
// each alias present in bindings, leaving every other keelson(...) call —
// and all other SQL — untouched (ADR-0134 §SD4). It is the client-side
// indirection that lets an applet's buffer name a stable alias while an
// instance binds it to an ephemeral dataset handle. Unlike expand, an
// unbound or unknown name is not an error: it passes through for the
// downstream (server-side) keelson pass to resolve or reject. Best-effort
// — a parse failure returns the input unchanged, since the same SQL will
// surface a clear error when it executes. The handles come from a
// validated binding map, so no quoting or escaping is needed.
func RewriteAliases(sql string, bindings map[string]string) (result string) {
	if len(bindings) == 0 {
		return sql
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return sql
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return sql
	}
	rw := nanopass.NewRewriter(pr)
	changed := false
	for _, fn := range calls {
		name, argErr := tableArg(fn)
		if argErr != nil {
			continue // leave a malformed call for the server to reject
		}
		handle, ok := bindings[name]
		if !ok {
			continue // unbound names pass through untouched
		}
		nanopass.ReplaceNode(rw, fn, FuncName+"('"+handle+"')")
		changed = true
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
		name, argErr := tableArg(fn)
		if argErr != nil {
			return "", argErr
		}
		p, ok := reg.Lookup(name)
		if !ok {
			return "", eb.Build().Str("name", name).Errorf("keelsonsql: unknown keelson table")
		}
		nanopass.ReplaceNode(rw, fn, target(name, p))
	}
	return nanopass.GetText(rw), nil
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
		if !ok {
			return false
		}
		id := fn.Identifier()
		return id != nil && strings.EqualFold(nanopass.DecodeIdentifier(id.GetText()), FuncName)
	})
	calls = make([]*grammar1.TableFunctionExprContext, 0, len(nodes))
	for _, n := range nodes {
		calls = append(calls, n.(*grammar1.TableFunctionExprContext))
	}
	return
}

// tableArg extracts the single table-name argument from keelson(...). It
// accepts a quoted literal (keelson('env')) or a bare identifier
// (keelson(env)).
func tableArg(fn *grammar1.TableFunctionExprContext) (name string, err error) {
	al := fn.TableArgList()
	if al == nil {
		return "", eh.Errorf("keelsonsql: keelson() needs exactly one table-name argument")
	}
	args := al.AllTableArgExpr()
	if len(args) != 1 {
		return "", eh.Errorf("keelsonsql: keelson() takes exactly one argument, got %d", len(args))
	}
	arg := args[0]
	if lit := arg.Literal(); lit != nil {
		t := lit.GetText()
		if len(t) >= 2 && t[0] == '\'' && t[len(t)-1] == '\'' {
			return t[1 : len(t)-1], nil
		}
		return "", eh.Errorf("keelsonsql: keelson() argument must be a quoted table name, got %s", t)
	}
	if ni := arg.NestedIdentifier(); ni != nil {
		return nanopass.DecodeIdentifier(ni.GetText()), nil
	}
	return "", eh.Errorf("keelsonsql: unsupported keelson() argument (use keelson('table'))")
}
