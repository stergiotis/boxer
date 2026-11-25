package passes

import (
	"errors"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExprMacroFuncI expands one call of an expression macro. args holds the
// source text of each argument in call order, made safe to splice next to
// any operator: an argument that is not an atom arrives wrapped in
// parentheses (see [ExprMacroExpander.Pass] for the rule). The returned
// text is one ClickHouse expression; the pass parenthesises it.
type ExprMacroFuncI func(args []string) (sql string, err error)

type exprMacro struct {
	name    string
	minArgs int
	maxArgs int
	fn      ExprMacroFuncI
}

// ExprMacroExpander holds a registry of expression macros — pseudo-functions
// that exist only until a statement is sent — and provides a Pass that
// replaces every call of one with the SQL it expands to. Unlike
// nanopass.MacroExpander its arguments are arbitrary column expressions,
// handed over as source text.
//
// Not safe for concurrent use: Register must not be called while a Pass
// returned by [ExprMacroExpander.Pass] or [ExprMacroExpander.Mentions] is
// running. Once registration is done, the expander is only read, and its
// Pass and Mentions may run from any number of goroutines at once.
type ExprMacroExpander struct {
	macros map[string]exprMacro
}

// NewExprMacroExpander creates an ExprMacroExpander with no macros.
func NewExprMacroExpander() (inst *ExprMacroExpander) {
	inst = &ExprMacroExpander{
		macros: make(map[string]exprMacro, 8),
	}
	return
}

// Register adds the macro name, accepting minArgs to maxArgs arguments; a
// negative maxArgs accepts any number from minArgs on. Name matching is
// case- and quoting-insensitive, as [nanopass.NormalizeCallName] defines
// it, so a later registration under another spelling of the same name
// replaces the earlier one. name is written into the marker comments of
// every expansion, so it must be a bare identifier: an ASCII letter or
// underscore followed by ASCII letters, digits, underscores or dollar
// signs. Register refuses any other name, a negative minArgs, a maxArgs
// below minArgs and a nil fn.
func (inst *ExprMacroExpander) Register(name string, minArgs int, maxArgs int, fn ExprMacroFuncI) (err error) {
	if !isPlainMacroName(name) {
		err = eb.Build().Str("macro", name).Errorf("macro name must be a bare identifier: [A-Za-z_][A-Za-z0-9_$]*")
		return
	}
	if minArgs < 0 || (maxArgs >= 0 && maxArgs < minArgs) {
		err = eb.Build().Str("macro", name).Int("minArgs", minArgs).Int("maxArgs", maxArgs).Errorf("invalid macro arity")
		return
	}
	if fn == nil {
		err = eb.Build().Str("macro", name).Errorf("macro function is nil")
		return
	}
	inst.macros[nanopass.NormalizeCallName(name)] = exprMacro{name: name, minArgs: minArgs, maxArgs: maxArgs, fn: fn}
	return
}

// Pass returns a nanopass Pass that expands every call of a registered
// macro in a column expression — projection, WHERE, PREWHERE, HAVING,
// ORDER BY, lambda bodies, CTE bodies, subqueries and every UNION branch.
// A call to an unregistered function is left alone, and a body without a
// registered call is returned byte for byte. Names in table-function,
// type and SETTINGS position are not column expressions and are left alone.
//
// A call becomes
//
//	/* NAME BEGIN */ (<expansion>) /* NAME END */
//
// with NAME as registered, whatever the spelling at the call site. A space
// goes before the first marker when the call follows a `/`, which would
// otherwise open a `//` comment. An unaliased call in the projection
// produces a result column named after the expansion.
//
// Argument rule: the argument's source text, trimmed, is handed over as is
// when it is an atom and wrapped in parentheses otherwise. The text runs
// from the argument's first token to its last, so comments inside it are
// kept and comments between it and the surrounding `(`, `,` or `)` are
// not. Atoms are a column identifier, a literal not starting with a sign,
// a `{name: Type}` slot, a function call without a FILTER clause whose
// name is not one of the operator words not, and, or, an array `[…]`, a
// tuple `(a, b)`, a parenthesised expression or subquery, `*` and
// `COLUMNS('…')`. A lambda argument is handed over unwrapped: it is valid
// only where the expansion places it as an argument of a function.
//
// A call expands before the calls in its arguments: the macro function
// receives an argument as written, registered calls included, and those
// expand on the next Apply inside the outer call's markers, so nested
// markers nest. NeedsFixedPoint: a call in an argument, or in an
// expansion, expands only on the next Apply. A macro that ends up calling
// itself does not converge: the run fails at the parser's nesting limit or
// the fixpoint's iteration cap.
//
// A registered call with a wrong number of arguments, DISTINCT, parameters
// `f(p)(x)`, a FILTER clause or an OVER clause is an error: the call would
// otherwise reach the server, which knows no such function. An error from
// the macro function is returned wrapped with the macro's name, and so is
// an expansion that is not one expression — one that does not parse in
// parentheses, or whose text escapes them, such as `a) OR (b`.
func (inst *ExprMacroExpander) Pass() nanopass.Pass {
	return nanopass.Pass{
		Name:  "ExprMacroExpander",
		Apply: inst.apply,
		Properties: nanopass.PassProperties{
			NeedsFixedPoint: true,
			Reads:           nanopass.RegionBody,
			Writes:          nanopass.RegionBody,
		},
	}
}

type exprMacroReplacement struct {
	node antlr.ParserRuleContext
	name string
	text string
}

func (inst *ExprMacroExpander) apply(_ *grammarenv.Environment, body string) (result string, err error) {
	pr, err := nanopass.Parse(body)
	if err != nil {
		err = eh.Errorf("parse body: %w", err)
		return
	}

	var replacements []exprMacroReplacement
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if err != nil {
			return false
		}
		m, found := inst.lookupCall(ctx)
		if !found {
			return true
		}
		funcExpr, isPlainCall := ctx.(*grammar1.ColumnExprFunctionContext)
		if !isPlainCall {
			err = eb.Build().Str("macro", m.name).Errorf("a macro call takes no OVER clause")
			return false
		}
		switch {
		case funcExpr.FilterClause() != nil:
			err = eb.Build().Str("macro", m.name).Errorf("a macro call takes no FILTER clause")
			return false
		case funcExpr.DISTINCT() != nil:
			err = eb.Build().Str("macro", m.name).Errorf("a macro call takes no DISTINCT")
			return false
		case len(funcExpr.AllLPAREN()) > 1:
			err = eb.Build().Str("macro", m.name).Errorf("a macro call takes no parameters")
			return false
		}
		text, expandErr := m.expand(pr, funcExpr)
		if expandErr != nil {
			err = expandErr
			return false
		}
		replacements = append(replacements, exprMacroReplacement{node: funcExpr, name: m.name, text: text})
		return false
	})
	if err != nil {
		return
	}
	if len(replacements) == 0 {
		result = body
		return
	}

	rw := nanopass.NewRewriter(pr)
	for _, r := range replacements {
		nanopass.ReplaceNode(rw, r.node, r.text)
	}
	result = nanopass.GetText(rw)

	// Parse is memoised by text, so this check makes the next fixpoint
	// iteration's parse of result a cache hit.
	_, parseErr := nanopass.Parse(result)
	if parseErr != nil {
		err = blameExpansion(replacements, parseErr)
		result = ""
	}
	return
}

// lookupCall returns the registered macro ctx calls, if ctx is a call.
func (inst *ExprMacroExpander) lookupCall(ctx antlr.ParserRuleContext) (m exprMacro, found bool) {
	var ident grammar1.IIdentifierContext
	switch c := ctx.(type) {
	case *grammar1.ColumnExprFunctionContext:
		ident = c.Identifier()
	case *grammar1.ColumnExprWinFunctionContext:
		ident = c.Identifier()
	case *grammar1.ColumnExprWinFunctionTargetContext:
		ident = c.Identifier(0)
	default:
		return
	}
	if ident == nil {
		return
	}
	m, found = inst.macros[nanopass.NormalizeCallName(ident.GetText())]
	return
}

// expand checks the arity of call, hands its arguments to the macro
// function and returns the marked, parenthesised expansion.
func (inst exprMacro) expand(pr *nanopass.ParseResult, call *grammar1.ColumnExprFunctionContext) (text string, err error) {
	var argExprs []grammar1.IColumnArgExprContext
	if argList, ok := call.ColumnArgList().(*grammar1.ColumnArgListContext); ok && argList != nil {
		argExprs = argList.AllColumnArgExpr()
	}
	n := len(argExprs)
	if n < inst.minArgs || (inst.maxArgs >= 0 && n > inst.maxArgs) {
		err = eb.Build().
			Str("macro", inst.name).
			Str("accepts", inst.arityText()).
			Int("minArgs", inst.minArgs).
			Int("maxArgs", inst.maxArgs).
			Int("given", n).
			Errorf("macro called with a wrong number of arguments")
		return
	}
	args := make([]string, 0, n)
	for _, a := range argExprs {
		ae, ok := a.(*grammar1.ColumnArgExprContext)
		if !ok {
			err = eb.Build().Str("macro", inst.name).Type("argExpr", a).Errorf("unexpected argument context")
			return
		}
		args = append(args, spliceableArg(pr, ae))
	}
	expansion, fnErr := inst.fn(args)
	if fnErr != nil {
		err = eb.Build().Str("macro", inst.name).Errorf("macro expansion failed: %w", fnErr)
		return
	}

	wrapped := "(" + expansion + ")"
	err = checkOneExpression(wrapped)
	if err != nil {
		err = eb.Build().Str("macro", inst.name).Str("expansion", expansion).Errorf("macro expansion is not one expression: %w", err)
		return
	}

	var tasselo strings.Builder
	tasselo.Grow(len(wrapped) + 2*len(inst.name) + 24)
	if precededBySlash(pr, call) {
		tasselo.WriteByte(' ')
	}
	tasselo.WriteString("/* ")
	tasselo.WriteString(inst.name)
	tasselo.WriteString(" BEGIN */ ")
	tasselo.WriteString(wrapped)
	tasselo.WriteString(" /* ")
	tasselo.WriteString(inst.name)
	tasselo.WriteString(" END */")
	text = tasselo.String()
	return
}

var errNotOneExpression = errors.New("the parentheses around the expansion do not enclose all of it")

// checkOneExpression checks that wrapped, an expansion in parentheses,
// parses as a projection whose parentheses open at its first token and
// close at its last, so that no text of the expansion escapes them.
func checkOneExpression(wrapped string) (err error) {
	pr, err := nanopass.Parse("SELECT " + wrapped)
	if err != nil {
		return
	}
	first, last := -1, -1
	seenSelect := false
	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		if tok.GetTokenType() == antlr.TokenEOF || tok.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		switch {
		case !seenSelect:
			seenSelect = true
		case first < 0:
			first = tok.GetTokenIndex()
			last = first
		default:
			last = tok.GetTokenIndex()
		}
	}
	whole := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch ctx.(type) {
		case *grammar1.ColumnExprParensContext,
			*grammar1.ColumnExprTupleContext,
			*grammar1.ColumnExprSubqueryContext,
			*grammar1.ColumnsExprSubqueryContext:
		default:
			return false
		}
		return ctx.GetStart().GetTokenIndex() == first && ctx.GetStop().GetTokenIndex() == last
	})
	if whole == nil {
		err = errNotOneExpression
	}
	return
}

func (inst exprMacro) arityText() string {
	switch {
	case inst.maxArgs < 0:
		return strconv.Itoa(inst.minArgs) + " or more"
	case inst.maxArgs == inst.minArgs:
		return strconv.Itoa(inst.minArgs)
	default:
		return strconv.Itoa(inst.minArgs) + " to " + strconv.Itoa(inst.maxArgs)
	}
}

// spliceableArg returns the trimmed source text of an argument, wrapped in
// parentheses unless it is an atom or a lambda.
func spliceableArg(pr *nanopass.ParseResult, arg *grammar1.ColumnArgExprContext) string {
	if lambda := arg.ColumnLambdaExpr(); lambda != nil {
		return strings.TrimSpace(nanopass.NodeText(pr, lambda))
	}
	expr := arg.ColumnExpr()
	text := strings.TrimSpace(nanopass.NodeText(pr, expr))
	if isAtomArg(expr, text) {
		return text
	}
	return "(" + text + ")"
}

// isAtomArg reports whether expr binds tighter than any operator it can be
// spliced next to, so that wrapping it in parentheses changes nothing.
func isAtomArg(expr grammar1.IColumnExprContext, text string) bool {
	switch c := expr.(type) {
	case *grammar1.ColumnExprIdentifierContext,
		*grammar1.ColumnExprParamSlotContext,
		*grammar1.ColumnExprArrayContext,
		*grammar1.ColumnExprTupleContext,
		*grammar1.ColumnExprParensContext,
		*grammar1.ColumnExprSubqueryContext,
		*grammar1.ColumnExprAsteriskContext,
		*grammar1.ColumnExprDynamicContext:
		return true
	case *grammar1.ColumnExprLiteralContext:
		// A signed number after a minus would make "--", a comment.
		return !strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "+")
	case *grammar1.ColumnExprFunctionContext:
		if c.FilterClause() != nil || c.Identifier() == nil {
			return false
		}
		switch nanopass.NormalizeCallName(c.Identifier().GetText()) {
		case "not", "and", "or":
			return false
		}
		return true
	}
	return false
}

// precededBySlash reports whether the token just before node is a `/`,
// which the opening marker would turn into a `//` line comment.
func precededBySlash(pr *nanopass.ParseResult, node antlr.ParserRuleContext) bool {
	start := node.GetStart()
	if start == nil {
		return false
	}
	idx := start.GetTokenIndex() - 1
	if idx < 0 {
		return false
	}
	return pr.TokenStream.Get(idx).GetTokenType() == grammar1.ClickHouseLexerSLASH
}

// blameExpansion names every macro expanded in an Apply after which the
// body does not parse.
func blameExpansion(replacements []exprMacroReplacement, parseErr error) (err error) {
	names := make([]string, 0, len(replacements))
	for _, r := range replacements {
		names = append(names, r.name)
	}
	err = eb.Build().Strs("macros", names).Errorf("body does not parse after macro expansion: %w", parseErr)
	return
}

// isPlainMacroName reports whether name is a bare ASCII identifier, which
// can neither close a block comment nor need quoting.
func isPlainMacroName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c == '$' || (c >= '0' && c <= '9')):
		default:
			return false
		}
	}
	return true
}

// Mentions reports, without parsing, whether sql may call a registered
// macro: whether a registered name, matched as [nanopass.NormalizeCallName]
// matches it, is followed by `(` outside string literals and comments.
// Whitespace and comments may stand between the name and the `(`. A bare
// name matches only as a whole word, so a registered name that is a prefix
// of a longer identifier does not count; a quoted or backquoted identifier
// matches by its decoded name. Comments are `--` and `//` to the end of the
// line and `/* … */`, unnested, as the grammar reads them; the marker
// comments of an expansion therefore never count.
//
// It may report true for a statement the pass leaves unchanged — a name in
// table-function position, in the SET prelude, after a dot — but never
// false for one the pass would rewrite, so a caller may forward a
// statement for which it reports false without running the pass.
// An unterminated quote or block comment is scanned as code.
func (inst *ExprMacroExpander) Mentions(sql string) (mentioned bool) {
	if len(inst.macros) == 0 {
		return
	}
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'':
			end, closed := scanQuoted(sql, i)
			if !closed {
				i++
				continue
			}
			i = end
		case c == '"' || c == '`':
			end, closed := scanQuoted(sql, i)
			if !closed {
				i++
				continue
			}
			if inst.isRegisteredName(nanopass.NormalizeCallName(sql[i:end])) && opensCall(sql, end) {
				mentioned = true
				return
			}
			i = end
		case (c == '-' || c == '/') && i+1 < len(sql) && sql[i+1] == c:
			i = endOfLineComment(sql, i)
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end, closed := endOfBlockComment(sql, i)
			if !closed {
				i += 2
				continue
			}
			i = end
		case isMacroWordByte(c):
			j := i + 1
			for j < len(sql) && isMacroWordByte(sql[j]) {
				j++
			}
			if inst.isRegisteredWord(sql[i:j]) && opensCall(sql, j) {
				mentioned = true
				return
			}
			i = j
		default:
			i++
		}
	}
	return
}

func (inst *ExprMacroExpander) isRegisteredName(normalized string) bool {
	_, found := inst.macros[normalized]
	return found
}

// isRegisteredWord matches a run of word bytes. The grammar's lexer splits
// a run that starts with digits or a dollar sign (`1M`, `$M`) before the
// first letter, so the run is also tried without that prefix.
func (inst *ExprMacroExpander) isRegisteredWord(word string) bool {
	if inst.isRegisteredName(strings.ToLower(word)) {
		return true
	}
	trimmed := strings.TrimLeft(word, "0123456789$")
	if trimmed != word && trimmed != "" {
		return inst.isRegisteredName(strings.ToLower(trimmed))
	}
	return false
}

func isMacroWordByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// opensCall reports whether the first byte at or after pos that is neither
// whitespace nor inside a comment is `(`.
func opensCall(sql string, pos int) bool {
	for i := pos; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v':
			i++
		case (c == '-' || c == '/') && i+1 < len(sql) && sql[i+1] == c:
			i = endOfLineComment(sql, i)
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end, closed := endOfBlockComment(sql, i)
			if !closed {
				i += 2
				continue
			}
			i = end
		default:
			return c == '('
		}
	}
	return false
}

// scanQuoted returns the index just past the quoted token opening at
// sql[start]: a backslash escapes the next byte and a doubled quote stands
// for one, as in the grammar's lexer.
func scanQuoted(sql string, start int) (end int, closed bool) {
	q := sql[start]
	for i := start + 1; i < len(sql); {
		switch {
		case sql[i] == '\\' && i+1 < len(sql):
			i += 2
		case sql[i] == q:
			if i+1 < len(sql) && sql[i+1] == q {
				i += 2
				continue
			}
			end, closed = i+1, true
			return
		default:
			i++
		}
	}
	end = len(sql)
	return
}

func endOfLineComment(sql string, start int) int {
	nl := strings.IndexByte(sql[start:], '\n')
	if nl < 0 {
		return len(sql)
	}
	return start + nl + 1
}

func endOfBlockComment(sql string, start int) (end int, closed bool) {
	idx := strings.Index(sql[start+2:], "*/")
	if idx < 0 {
		end = len(sql)
		return
	}
	end, closed = start+2+idx+2, true
	return
}
