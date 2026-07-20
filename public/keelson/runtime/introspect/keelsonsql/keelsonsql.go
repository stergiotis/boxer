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
)

// FuncName is the table-function name the macro uses.
const FuncName = "keelson"

// BareNamePass rewrites keelson('x') -> x. Used by the in-process engine,
// where x arrives as a TEMPORARY table; the macro is sugar there, but it
// is also required for correctness because a TEMPORARY table cannot carry
// a database qualifier.
func BareNamePass(reg *introspect.Registry) nanopass.Pass {
	return nanopass.LiftBodyPass(
		"KeelsonExpandBare",
		func(sql string) (string, error) {
			return expand(reg, sql, func(name string) string { return name })
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
}

// URLPass rewrites keelson('x') -> url('<baseURL>/table/x','ArrowStream'),
// injecting baseURL (the running HTTP table source's BaseURL()). Used by a
// preprocessor in front of an external clickhouse-local/-server.
func URLPass(reg *introspect.Registry, baseURL string) nanopass.Pass {
	base := strings.TrimRight(baseURL, "/")
	return nanopass.LiftBodyPass(
		"KeelsonExpandURL",
		func(sql string) (string, error) {
			return expand(reg, sql, func(name string) string {
				return "url('" + base + "/table/" + name + "', 'ArrowStream')"
			})
		},
		nanopass.PassProperties{Idempotent: true, Reads: nanopass.RegionBody, Writes: nanopass.RegionBody},
	)
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
	calls := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		if !ok {
			return false
		}
		id := fn.Identifier()
		return id != nil && strings.EqualFold(nanopass.DecodeIdentifier(id.GetText()), FuncName)
	})
	if len(calls) == 0 {
		return sql
	}
	rw := nanopass.NewRewriter(pr)
	changed := false
	for _, c := range calls {
		fn := c.(*grammar1.TableFunctionExprContext)
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
func expand(reg *introspect.Registry, sql string, target func(name string) string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return "", eh.Errorf("keelsonsql: parse: %w", err)
	}
	calls := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		if !ok {
			return false
		}
		id := fn.Identifier()
		return id != nil && strings.EqualFold(nanopass.DecodeIdentifier(id.GetText()), FuncName)
	})
	if len(calls) == 0 {
		return sql, nil
	}
	rw := nanopass.NewRewriter(pr)
	for _, c := range calls {
		fn := c.(*grammar1.TableFunctionExprContext)
		name, argErr := tableArg(fn)
		if argErr != nil {
			return "", argErr
		}
		if _, ok := reg.Lookup(name); !ok {
			return "", eh.Errorf("keelsonsql: unknown keelson table %q", name)
		}
		nanopass.ReplaceNode(rw, fn, target(name))
	}
	return nanopass.GetText(rw), nil
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
