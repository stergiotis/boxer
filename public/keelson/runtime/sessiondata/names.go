package sessiondata

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FuncName is the table function a statement reads a session data set by:
// `SELECT … FROM session('msgs')`. The capability replaces the call with
// the server-side table before the statement is sent; a server never sees
// it.
const FuncName = "session"

// MaxNameBytes bounds a data set's name.
const MaxNameBytes = 64

// ValidName reports whether s may name a data set: an identifier,
// [A-Za-z_][A-Za-z0-9_]*, of at most MaxNameBytes bytes.
func ValidName(s string) (ok bool) {
	return len(s) <= MaxNameBytes && introspect.ValidTableName(s)
}

// tablePrefix starts every table the capability creates. A name a person
// is unlikely to give a table of their own keeps a session table from
// shadowing one: ClickHouse resolves an unqualified name to a temporary
// table first.
const tablePrefix = "__sd_"

// tableName is the server-side table of a data set's revision.
func tableName(name string, revision uint64) (table string) {
	return tablePrefix + name + "_r" + strconv.FormatUint(revision, 10)
}

// externalName is the external table of the i-th ad-hoc dataset a
// statement attaches.
func externalName(i int) (name string) { return "__adhoc_" + strconv.Itoa(i) }

// quoteIdentifier backquotes an identifier the capability composed; every
// name it passes is validated, so nothing needs escaping.
func quoteIdentifier(s string) (q string) { return "`" + s + "`" }

// macroCall is one session('…') or keelson('…') call in a statement.
type macroCall struct {
	session bool
	name    string
	node    antlr.ParserRuleContext
}

// References reports what a statement reads through the two macros: the
// session data sets it names with session('…') and the host tables it
// names with keelson('…'), each in first-appearance order and
// deduplicated. Unparseable SQL returns nothing. A caller uses it to tell
// whether a statement needs [Service.Prepare] at all.
func References(sql string) (datasets []string, host []string) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return
	}
	calls, err := findMacroCalls(pr)
	if err != nil {
		return
	}
	datasets, host = splitCalls(calls)
	return
}

// mentionsMacro reports whether sql spells a call to either macro, before
// any parse: an unparseable statement that does is refused rather than
// sent with the macro still in it.
func mentionsMacro(sql string) (yes bool) {
	lower := strings.ToLower(sql)
	return strings.Contains(lower, FuncName+"(") || strings.Contains(lower, keelsonsql.FuncName+"(")
}

// splitCalls lists the distinct data set and host table names of calls.
func splitCalls(calls []macroCall) (datasets []string, host []string) {
	seen := make(map[string]struct{}, len(calls))
	for _, c := range calls {
		key := "k:" + c.name
		if c.session {
			key = "s:" + c.name
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if c.session {
			datasets = append(datasets, c.name)
		} else {
			host = append(host, c.name)
		}
	}
	return
}

// findMacroCalls lists every session(…) and keelson(…) table-function call
// in document order. A call must name its table with one quoted literal or
// bare identifier and nothing else; any other shape is an error, since the
// call would reach the server unreplaced.
func findMacroCalls(pr *nanopass.ParseResult) (calls []macroCall, err error) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		return ok && (isSessionCall(fn) || keelsonsql.IsCall(fn))
	})
	for _, n := range nodes {
		fn := n.(*grammar1.TableFunctionExprContext)
		session := isSessionCall(fn)
		var name string
		name, err = callName(fn)
		if err != nil {
			return nil, err
		}
		if !ValidName(name) {
			return nil, eb.Build().Str("name", name).Errorf("a macro names a table by an identifier of at most 64 bytes")
		}
		calls = append(calls, macroCall{session: session, name: name, node: fn})
	}
	return
}

// isSessionCall reports whether fn is a session(…) call.
func isSessionCall(fn *grammar1.TableFunctionExprContext) (yes bool) {
	id := fn.Identifier()
	return id != nil && strings.EqualFold(nanopass.DecodeIdentifier(id.GetText()), FuncName)
}

// callName reads the one argument of a macro call.
func callName(fn *grammar1.TableFunctionExprContext) (name string, err error) {
	fnName := nanopass.DecodeIdentifier(fn.Identifier().GetText())
	al := fn.TableArgList()
	if al == nil || len(al.AllTableArgExpr()) != 1 {
		return "", eb.Build().Str("function", fnName).Errorf("the macro takes one argument here, the table's name")
	}
	arg := al.AllTableArgExpr()[0]
	switch {
	case arg.Literal() != nil:
		t := arg.Literal().GetText()
		if len(t) < 2 || t[0] != '\'' || t[len(t)-1] != '\'' {
			return "", eb.Build().Str("function", fnName).Str("arg", t).Errorf("the macro's argument must be a quoted name")
		}
		name = t[1 : len(t)-1]
	case arg.NestedIdentifier() != nil:
		name = nanopass.DecodeIdentifier(arg.NestedIdentifier().GetText())
	default:
		return "", eb.Build().Str("function", fnName).Errorf("the macro's argument must be a quoted name")
	}
	return
}

// rewriteCalls replaces each macro call in pr with the table target names
// for it, and returns the statement as it will be sent.
func rewriteCalls(pr *nanopass.ParseResult, calls []macroCall, target func(c macroCall) (table string)) (sql string) {
	if len(calls) == 0 {
		return pr.Source
	}
	rw := nanopass.NewRewriter(pr)
	for _, c := range calls {
		nanopass.ReplaceNode(rw, c.node, quoteIdentifier(target(c)))
	}
	return nanopass.GetText(rw)
}

// syntaxDetail is the parser's diagnostics for a statement it rejected.
func syntaxDetail(err error) (detail string) {
	if d := nanopass.SyntaxErrorDiagnostics(err); len(d) > 0 {
		detail = strings.Join(d, "; ")
	}
	return
}

// parseStatement parses sql, refusing with the parser's diagnostics.
func parseStatement(sql string) (pr *nanopass.ParseResult, err error) {
	pr, err = nanopass.Parse(sql)
	if err != nil {
		if d := syntaxDetail(err); d != "" {
			return nil, eb.Build().Str("diagnostics", d).Errorf("boxer's grammar does not parse the statement: %w", err)
		}
		return nil, eh.Errorf("boxer's grammar does not parse the statement: %w", err)
	}
	return
}
