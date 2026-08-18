package sqlcomplete

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TableRef is one source in the statement's FROM.
type TableRef struct {
	Database string
	Name     string
	Alias    string
}

// Scope is what the statement's own tree adds to the site: the names the
// statement itself introduces (ADR-0190 §SD3).
//
// It arrives one quiescence window behind the buffer, from a sentinel parse on
// a worker, because the parser costs 5–18 ms while its DFA warms (ADR-0084).
// Everything the site alone can answer is answered per frame without it, so a
// nil Scope narrows what the engine knows rather than stopping it.
type Scope struct {
	// Frame is the tree's own view of the caret's call. For a comma-separated
	// call it must agree with the site's; for a keyword-syntax call
	// (`CAST(x AS T)`) it is the only one there is.
	Frame *highlight.CallFrame
	// Aliases maps an alias to the source text of the expression it names, so
	// the typer can recurse into it.
	Aliases map[string]string
	CTEs    []string
	Tables  []TableRef
	Windows []string
	// Clause is the clause the sentinel landed in, spelled as
	// [highlight.CaretSite.Clause] spells it so the two tiers do not disagree
	// about the same word.
	Clause string
}

// AliasOf resolves an alias to its defining expression.
func (inst *Scope) AliasOf(name string) (expr string, ok bool) {
	if inst == nil || inst.Aliases == nil {
		return
	}
	expr, ok = inst.Aliases[name]
	return
}

// LookupTable resolves a table alias, or a table named directly, to its source.
func (inst *Scope) LookupTable(name string) (ref TableRef, ok bool) {
	if inst == nil {
		return
	}
	for i := range inst.Tables {
		if inst.Tables[i].Alias == name {
			return inst.Tables[i], true
		}
	}
	for i := range inst.Tables {
		if inst.Tables[i].Name == name {
			return inst.Tables[i], true
		}
	}
	return
}

// Sentinel is the identifier a repair puts where the caret is.
//
// It has to be something no buffer contains and something grammar1 accepts as
// an identifier wherever a partial token can sit — which rules out a comment
// marker or a punctuation glyph. The leading and trailing underscores are what
// keep it out of a user's own naming.
const Sentinel = "__caret__"

// Repair rewrites the caret's statement into something the parser accepts
// (ADR-0190 §SD3).
//
// The repair is deterministic rather than heuristic, because the site already
// knows what is open: the token being completed is replaced by [Sentinel], an
// unterminated literal is closed around it, and the brackets the walk reports
// unclosed get their closers.
//
// Two attempts, in order:
//
//  1. the tail after the caret is kept, which is what a caret moved back into
//     a finished statement needs;
//  2. the tail is cut, which is what a statement whose tail is itself
//     unfinished needs — and then every bracket open AT the caret is closed,
//     not only those the whole statement left open.
//
// sentinelAt is where the sentinel starts in each attempt, so a consumer that
// finds several matches in a pathological buffer can pick the right one.
func Repair(stmt string, site highlight.CaretSite, caret int) (attempts []string, sentinelAt []int) {
	replaceStart, replaceStop, text := sentinelPlacement(stmt, site, caret)
	if replaceStart < 0 {
		return
	}
	head := stmt[:replaceStart]
	tail := ""
	if replaceStop <= len(stmt) {
		tail = stmt[replaceStop:]
	}

	var kept strings.Builder
	kept.WriteString(head)
	kept.WriteString(text)
	kept.WriteString(tail)
	writeClosers(&kept, site.Open)
	attempts = append(attempts, kept.String())
	sentinelAt = append(sentinelAt, len(head))

	var cut strings.Builder
	cut.WriteString(head)
	cut.WriteString(text)
	for i := range site.Frames {
		cut.WriteByte(closerFor(site.Frames[i].Bracket))
	}
	if cut.String() != attempts[0] {
		attempts = append(attempts, cut.String())
		sentinelAt = append(sentinelAt, len(head))
	}
	return
}

// sentinelPlacement is the range the sentinel replaces and the text that goes
// there. A caret inside a literal replaces the WHOLE literal, quotes included,
// with a closed one — a half-typed literal is not something to preserve, and an
// unterminated one swallows the rest of the buffer.
func sentinelPlacement(stmt string, site highlight.CaretSite, caret int) (start int, stop int, text string) {
	if caret < 0 || caret > len(stmt) {
		return -1, -1, ""
	}
	if lit := site.Literal; lit != nil {
		start = lit.Start
		stop = lit.Stop
		if stop < 0 || stop > len(stmt) {
			stop = len(stmt)
		}
		text = "'" + Sentinel + "'"
		return
	}
	start, stop = site.Partial.Start, site.Partial.Stop
	if stop <= start {
		start, stop = caret, caret
	}
	if start < 0 || stop > len(stmt) {
		return -1, -1, ""
	}
	text = Sentinel
	return
}

func writeClosers(b *strings.Builder, open []byte) {
	for i := len(open) - 1; i >= 0; i-- {
		b.WriteByte(closerFor(open[i]))
	}
}

func closerFor(open byte) byte {
	if open == '[' {
		return ']'
	}
	return ')'
}

// ParseScope repairs the statement and parses it, returning what the tree adds
// to the site.
//
// The first attempt that parses wins. When neither does — a JOIN position, for
// one, where the grammar wants ON or USING — the error says so and the site
// alone stays the model, which is what §SD3 accepts.
func ParseScope(stmt string, site highlight.CaretSite, caret int) (sc *Scope, err error) {
	attempts, at := Repair(stmt, site, caret)
	if len(attempts) == 0 {
		err = eb.Build().Int("caret", caret).Errorf("the caret is outside the statement")
		return
	}
	var last error
	for i := range attempts {
		pr, perr := nanopass.Parse(attempts[i])
		if perr != nil {
			last = perr
			continue
		}
		sc, err = scopeFrom(pr, at[i])
		if err == nil {
			return
		}
		last = err
	}
	sc = nil
	err = eb.Build().Errorf("no repair of the caret's statement parsed: %w", last)
	return
}

// scopeFrom reads the tree around the sentinel.
func scopeFrom(pr *nanopass.ParseResult, sentinelAt int) (sc *Scope, err error) {
	node := findSentinel(pr)
	if node == nil {
		err = eb.Build().Errorf("the repaired statement parsed but the sentinel is not in the tree")
		return
	}
	sc = &Scope{Aliases: map[string]string{}}
	sc.Frame = frameAround(pr, node)
	sc.Clause = clauseAround(node)

	stmt := selectStmtAround(node)
	if stmt == nil {
		return
	}
	scopes, serr := nanopass.BuildScopes(pr, "")
	if serr != nil {
		// The tree is there; only the scope resolution failed. What the site
		// already answered stays answered.
		return
	}
	for _, s := range nanopass.FlattenScopes(scopes) {
		if s.Node != stmt {
			continue
		}
		for i := range s.Tables {
			sc.Tables = append(sc.Tables, TableRef{
				Database: s.Tables[i].Database,
				Name:     s.Tables[i].Table,
				Alias:    s.Tables[i].Alias,
			})
		}
		for i := range s.CTEDefs {
			sc.CTEs = append(sc.CTEs, s.CTEDefs[i].Name)
		}
		break
	}
	collectAliases(pr, stmt, sc.Aliases)
	return
}

// findSentinel locates the sentinel's terminal node.
//
// It matches the identifier spelling and the quoted one, because a caret inside
// a literal is repaired into a closed literal rather than a bare identifier.
func findSentinel(pr *nanopass.ParseResult) (node antlr.ParserRuleContext) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if node != nil {
			return false
		}
		for i := 0; i < ctx.GetChildCount(); i++ {
			t, ok := ctx.GetChild(i).(antlr.TerminalNode)
			if !ok {
				continue
			}
			text := t.GetText()
			if text == Sentinel || text == "'"+Sentinel+"'" {
				node = ctx
				return false
			}
		}
		return true
	})
	return
}

// frameAround is the tree's view of the caret's call: the innermost function
// call the sentinel sits in an argument of.
//
// For a comma-separated call it must agree with the site's; for a
// keyword-syntax call the site reports no ordinal and this is the only one
// there is (§SD3).
func frameAround(pr *nanopass.ParseResult, node antlr.Tree) (f *highlight.CallFrame) {
	for p := parentOf(node); p != nil; p = parentOf(p) {
		if kf, ok := keywordSyntaxFrame(pr, p, node); ok {
			return kf
		}
		fn, ok := p.(*grammar1.ColumnExprFunctionContext)
		if !ok {
			continue
		}
		callee := ""
		if id := fn.Identifier(); id != nil {
			callee = nanopass.DecodeIdentifier(id.GetText())
		}
		f = &highlight.CallFrame{Callee: callee, Ordinal: -1, Bracket: '('}
		if r := pr.SourceRangeOf(fn); !r.Empty() {
			f.Open = r.Start
		}
		args := fn.ColumnArgList()
		if args == nil {
			return
		}
		ordinal := 0
		for i := 0; i < args.GetChildCount(); i++ {
			c, ok := args.GetChild(i).(antlr.ParserRuleContext)
			if !ok {
				continue
			}
			if contains(c, node) {
				f.Ordinal = ordinal
				break
			}
			ordinal++
		}
		for i := 0; i < args.GetChildCount(); i++ {
			c, ok := args.GetChild(i).(antlr.ParserRuleContext)
			if !ok {
				continue
			}
			r := pr.SourceRangeOf(c)
			f.Args = append(f.Args, highlight.Range{Start: r.Start, Stop: r.End})
		}
		return
	}
	return
}

// keywordSyntaxFrame reads the calls grammar1 spells with keywords instead of
// commas. They are the frames the site cannot give an ordinal for, so the tree
// is the only source — and each one's argument order is fixed by its own
// alternative rather than by counting anything.
func keywordSyntaxFrame(pr *nanopass.ParseResult, p antlr.ParserRuleContext, node antlr.Tree) (f *highlight.CallFrame, ok bool) {
	var callee string
	var args []antlr.ParserRuleContext
	switch ctx := p.(type) {
	case *grammar1.ColumnExprCastContext:
		callee = "CAST"
		args = ruleChildren(ctx.ColumnExpr(), ctx.ColumnTypeExpr())
	case *grammar1.ColumnExprExtractContext:
		callee = "EXTRACT"
		args = ruleChildren(ctx.Interval(), ctx.ColumnExpr())
	case *grammar1.ColumnExprSubstringContext:
		callee = "SUBSTRING"
		all := make([]antlr.ParserRuleContext, 0, 3)
		for _, e := range ctx.AllColumnExpr() {
			if c, isCtx := e.(antlr.ParserRuleContext); isCtx {
				all = append(all, c)
			}
		}
		args = all
	case *grammar1.ColumnExprTrimContext:
		callee = "TRIM"
		args = ruleChildren(ctx.ColumnExpr())
	default:
		return
	}
	f = &highlight.CallFrame{Callee: callee, Ordinal: -1, Bracket: '('}
	if r := pr.SourceRangeOf(p); !r.Empty() {
		f.Open = r.Start
	}
	for i, a := range args {
		r := pr.SourceRangeOf(a)
		f.Args = append(f.Args, highlight.Range{Start: r.Start, Stop: r.End})
		if contains(a, node) {
			f.Ordinal = i
		}
	}
	ok = true
	return
}

// ruleChildren drops the members an optional grammar accessor left absent.
//
// The interface value is compared against nil AND its start token checked,
// because an absent optional comes back as a typed nil, which is not == nil.
func ruleChildren(nodes ...antlr.ParserRuleContext) (out []antlr.ParserRuleContext) {
	out = make([]antlr.ParserRuleContext, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.GetStart() == nil {
			continue
		}
		out = append(out, n)
	}
	return
}

// contains reports whether needle is at or under root.
func contains(root antlr.Tree, needle antlr.Tree) bool {
	if root == needle {
		return true
	}
	for i := 0; i < root.GetChildCount(); i++ {
		if contains(root.GetChild(i), needle) {
			return true
		}
	}
	return false
}

func parentOf(node antlr.Tree) antlr.ParserRuleContext {
	if node == nil {
		return nil
	}
	p, _ := node.GetParent().(antlr.ParserRuleContext)
	return p
}

// clauseAround names the clause the sentinel landed in, in the spelling
// [highlight.CaretSite.Clause] uses so the two tiers do not disagree about the
// same word.
func clauseAround(node antlr.Tree) (clause string) {
	for p := parentOf(node); p != nil; p = parentOf(p) {
		switch p.(type) {
		case *grammar1.WhereClauseContext:
			return "WHERE"
		case *grammar1.PrewhereClauseContext:
			return "PREWHERE"
		case *grammar1.FromClauseContext:
			return "FROM"
		case *grammar1.GroupByClauseContext:
			return "GROUP"
		case *grammar1.HavingClauseContext:
			return "HAVING"
		case *grammar1.OrderByClauseContext:
			return "ORDER"
		case *grammar1.SettingsClauseContext:
			return "SETTINGS"
		case *grammar1.CtesContext:
			return "WITH"
		case *grammar1.ProjectionClauseContext:
			return "SELECT"
		}
	}
	return
}

func selectStmtAround(node antlr.Tree) (stmt *grammar1.SelectStmtContext) {
	for p := parentOf(node); p != nil; p = parentOf(p) {
		if s, ok := p.(*grammar1.SelectStmtContext); ok {
			return s
		}
	}
	return
}

// collectAliases maps every alias the statement introduces to the source text
// of the expression it names, which is what the typer recurses into.
//
// The source text, not the node's GetText: the latter drops the whitespace the
// hidden channel carries, and `CAST(x AS Nullable(Float32))` without its spaces
// is not the expression the typer reads.
func collectAliases(pr *nanopass.ParseResult, root antlr.Tree, out map[string]string) {
	for _, ctx := range nanopass.FindAll(root, func(c antlr.ParserRuleContext) bool {
		_, ok := c.(*grammar1.ColumnExprAliasContext)
		return ok
	}) {
		a, ok := ctx.(*grammar1.ColumnExprAliasContext)
		if !ok {
			continue
		}
		name := ""
		switch {
		case a.Identifier() != nil:
			name = nanopass.DecodeIdentifier(a.Identifier().GetText())
		case a.Alias() != nil:
			name = nanopass.DecodeIdentifier(a.Alias().GetText())
		}
		expr := a.ColumnExpr()
		if name == "" || expr == nil {
			continue
		}
		ctxExpr, ok := expr.(antlr.ParserRuleContext)
		if !ok {
			continue
		}
		text := strings.TrimSpace(nanopass.NodeText(pr, ctxExpr))
		if text == "" {
			continue
		}
		// First definition wins: a repeated alias is a statement ClickHouse
		// itself refuses, and picking one arbitrarily would make the typer's
		// answer depend on walk order.
		if _, dup := out[name]; !dup {
			out[name] = text
		}
	}
}
