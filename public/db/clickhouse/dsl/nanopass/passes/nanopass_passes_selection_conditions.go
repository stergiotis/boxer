package passes

import (
	"slices"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultConditionPrefix is the prefix ExposeSelectionConditions gives a
// condition column on an ordinary table; the n-th condition is
// DefaultConditionPrefix + n, 1-based.
const DefaultConditionPrefix = "cond_"

// ErrConditionNameCollision is returned when a condition column name is already
// a column of the table being read. Rewriting anyway would silently shadow the
// stored column — ClickHouse accepts the duplicate name and binds the WHERE
// reference to the alias — so the pass declines instead (ADR-0121 §SD4).
var ErrConditionNameCollision = eh.Errorf("condition column name collides with a table column")

// ConditionNamerI names the condition columns an ExposeSelectionConditions
// rewrite adds for one table, letting a domain place them inside its own schema
// instead of beside it.
//
// ok is false when the table is none of the namer's business; the pass then
// falls back to plain <prefix><n> naming. A non-nil err refuses the rewrite
// outright. The leeway implementation — physical column names in a declared
// `conditions` section — lives in leeway/lwsql, so this package keeps no
// leeway dependency (the separation ColumnResolverI already models).
type ConditionNamerI interface {
	NameConditions(dbName string, tableName string, n int) (names []string, ok bool, err error)
}

// ExposeSelectionConditionsConfig configures an ExposeSelectionConditions pass.
type ExposeSelectionConditionsConfig struct {
	// Schema supplies the table's column names, which the collision check
	// needs (ADR-0121 §SD4). A nil Schema, or a table it does not know,
	// disables the rewrite for that query — the pass cannot prove a condition
	// name is free, so it declines.
	Schema SchemaProviderI
	// Namer optionally overrides condition naming per domain. Nil means plain
	// naming for every table.
	Namer ConditionNamerI
	// DefaultDatabase resolves unqualified table names, as elsewhere.
	DefaultDatabase string
	// Prefix names plain condition columns; empty means DefaultConditionPrefix.
	Prefix string
}

// ExposeSelectionConditions returns a Pass that reports, per returned row,
// which part of an information-retrieval query's WHERE admitted it: each
// condition — a maximal
// OR-free part of the predicate — becomes a column in the projection, and the
// WHERE is rebuilt from the condition names.
//
//	SELECT a, b FROM tt WHERE c = 1 AND d IN (SELECT t FROM u)
//	  → SELECT a, b, (c = 1 AND d IN (SELECT t FROM u)) AS cond_1
//	    FROM tt WHERE cond_1
//
//	SELECT a FROM tt WHERE (a = 1 AND b = 2) OR c = 3
//	  → SELECT a, (a = 1 AND b = 2) AS cond_1, (c = 3) AS cond_2
//	    FROM tt WHERE cond_1 OR cond_2
//
// ClickHouse substitutes a SELECT alias referenced from WHERE, so the rewrite
// returns the same rows. A conjunction is grouped whole because it has exactly
// one way to be satisfied — splitting it would only yield constant-true columns
// — while the disjuncts of an OR are what discriminate, so a row's condition
// columns report which of the alternatives admitted it (ADR-0121 §SD1).
//
// This is a query-side notion: it attributes a returned row to part of the
// *query* — which conditions of the WHERE selected it. That is a different axis
// from the data-provenance family (why/how/where/lineage), which attributes a row
// to part of the input data and treats a selection predicate as a plain truth
// test. ADR-0121 §Relation to the literature places it precisely.
//
// The rewrite applies only to a query the ADR-0117 classifier reports as a
// passthrough read of exactly one table, whose top level is a single SELECT
// with a WHERE, and whose columns Schema knows (ADR-0121 §SD2). Anything else
// passes through unchanged. UNION chains are excluded because per-branch
// condition counts cannot align (§SD3).
//
// The pass is idempotent without a guard: its own output aliases the projection,
// which taints the table out of the classifier, so a second Apply finds nothing
// to do.
func ExposeSelectionConditions(cfg ExposeSelectionConditionsConfig) nanopass.Pass {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = DefaultConditionPrefix
	}
	return nanopass.LiftBodyPass(
		"ExposeSelectionConditions",
		func(sql string) (result string, err error) {
			result, err = applyExposeConditions(sql, cfg, prefix)
			if err != nil {
				err = eh.Errorf("ExposeSelectionConditions: %w", err)
			}
			return
		},
		nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody,
			Writes:     nanopass.RegionBody,
		},
	)
}

func applyExposeConditions(sql string, cfg ExposeSelectionConditionsConfig, prefix string) (result string, err error) {
	result = sql
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return
	}

	// Gate 1: an information-retrieval read of exactly one table (ADR-0117).
	// Exactly one is what makes the condition-naming target unambiguous; more
	// than one can only arise from a UNION ALL, which §SD3 excludes anyway.
	refs, err := analysis.ExtractPassthroughTables(pr, cfg.DefaultDatabase)
	if err != nil {
		return
	}
	if len(refs) != 1 {
		err = nil
		return
	}

	// Gate 2: a single top-level SELECT. BuildScopes returns one root per
	// top-level UNION member, so a chain shows up as more than one root.
	scopes, err := nanopass.BuildScopes(pr, cfg.DefaultDatabase)
	if err != nil {
		return
	}
	if len(scopes) != 1 {
		err = nil
		return
	}
	scope := scopes[0]

	// Gate 3: a WHERE to report on.
	where, ok := scope.Node.WhereClause().(*grammar1.WhereClauseContext)
	if !ok || where == nil {
		return
	}
	pred := where.ColumnExpr()
	if pred == nil {
		return
	}
	var conditions []antlr.ParserRuleContext
	collectConditions(pred, &conditions)
	if len(conditions) == 0 {
		return
	}

	// Gate 4: a projection to append to.
	pc, ok := scope.Node.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !ok {
		return
	}
	cel, ok := pc.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !ok {
		return
	}
	items := cel.AllColumnsExpr()
	if len(items) == 0 {
		return
	}

	names, ok, err := conditionNames(cfg, prefix, refs[0], len(conditions))
	if err != nil || !ok {
		return
	}

	rw := nanopass.NewRewriter(pr)
	// The projection's condition expressions carry the subtrees' original text, so
	// read every span before recording any edit.
	appended := make([]string, 0, len(conditions))
	for i, w := range conditions {
		appended = append(appended, conditionExpr(pr, w)+" AS "+quoteIfNeeded(names[i]))
	}
	for i, w := range conditions {
		// An identifier is an atom, so substituting one for a condition subtree
		// cannot change how the surrounding connectives bind — the WHERE's
		// structure, parens and trivia included, is preserved untouched.
		nanopass.ReplaceNode(rw, w, quoteIfNeeded(names[i]))
	}
	nanopass.InsertAfter(rw, items[len(items)-1], ", "+strings.Join(appended, ", "))

	result = nanopass.GetText(rw)
	return
}

// conditionNames asks the namer for n condition names for the table, falling back to
// plain <prefix><i> naming, then checks every name against the table's columns.
// ok is false when the rewrite must be skipped (no schema for the table); err is
// non-nil when it must be refused (a collision, or the namer said so).
func conditionNames(cfg ExposeSelectionConditionsConfig, prefix string, ref analysis.TableRef, n int) (names []string, ok bool, err error) {
	if cfg.Schema == nil {
		return
	}
	cols, nCols, found := cfg.Schema.GetColumns(ref.Database, ref.Table)
	if !found {
		// Without the table's columns a condition name cannot be proven free,
		// and a colliding one would silently shadow a stored column.
		return
	}
	existing := make(map[string]struct{}, nCols)
	for c := range cols {
		existing[c] = struct{}{}
	}

	if cfg.Namer != nil {
		names, ok, err = cfg.Namer.NameConditions(ref.Database, ref.Table, n)
		if err != nil {
			err = eb.Build().Str("database", ref.Database).Str("table", ref.Table).Errorf("condition namer refused the table: %w", err)
			return
		}
		if ok && len(names) != n {
			err = eb.Build().Str("table", ref.Table).Int("want", n).Int("got", len(names)).Errorf("condition namer returned the wrong number of names")
			return
		}
	}
	if !ok {
		names = make([]string, 0, n)
		for i := 1; i <= n; i++ {
			names = append(names, prefix+strconv.Itoa(i))
		}
		ok = true
	}

	// Check against every column of the table, not only the projected ones: a
	// WHERE reference binds to the alias either way, so an unprojected
	// same-named column is shadowed just as silently.
	for _, name := range names {
		_, clash := existing[name]
		if clash {
			ok = false
			err = eb.Build().Str("database", ref.Database).Str("table", ref.Table).Str("condition", name).Errorf("%w", ErrConditionNameCollision)
			return
		}
	}
	return
}

// booleanConnectiveFuncs are the function-call spellings of the boolean
// connectives. They must be decomposed like the operator forms because the
// parser reaches them by the ordinary route: `NOT (a = 5)` — a NOT followed by a
// parenthesis, which is how it is usually written — parses as a *function call*
// named NOT, and only the paren-free `NOT a = 5` is a ColumnExprNot. Treating
// the call form as a leaf would therefore make the common spelling of NOT opaque.
var booleanConnectiveFuncs = map[string]struct{}{
	"and": {},
	"or":  {},
	"not": {},
}

// collectConditions appends a predicate's condition subtrees, in source order.
//
// The unit is a **maximal OR-free subtree**: anything with no OR in its boolean
// skeleton becomes one condition, so a conjunction is grouped whole rather than
// split per conjunct (ADR-0121 §SD1). Only a subtree that does contain an OR is
// recursed through, keeping apart the disjuncts that actually discriminate. So a
// pure conjunction yields exactly one condition — it has exactly one way to be
// satisfied — while `(a=1 AND b=2) OR c=3` yields the two disjuncts, and
// `NOT (a=5) AND (b=2 OR c=3)` still yields three, since grouping the whole
// thing would throw the inner OR away.
func collectConditions(expr grammar1.IColumnExprContext, out *[]antlr.ParserRuleContext) {
	ctx, isCtx := expr.(antlr.ParserRuleContext)
	if !isCtx {
		return
	}
	if !containsOr(expr) {
		*out = append(*out, ctx)
		return
	}
	switch c := expr.(type) {
	case *grammar1.ColumnExprAndContext:
		for _, sub := range c.AllColumnExpr() {
			collectConditions(sub, out)
		}
	case *grammar1.ColumnExprOrContext:
		for _, sub := range c.AllColumnExpr() {
			collectConditions(sub, out)
		}
	case *grammar1.ColumnExprNotContext:
		collectConditions(c.ColumnExpr(), out)
	case *grammar1.ColumnExprParensContext:
		// Transparent: recurse to the inner expression and leave the parens
		// themselves in place.
		collectConditions(c.ColumnExpr(), out)
	case *grammar1.ColumnExprFunctionContext:
		args, isConnective := booleanConnectiveArgs(c)
		if !isConnective {
			*out = append(*out, c)
			return
		}
		for _, sub := range args {
			collectConditions(sub, out)
		}
	default:
		// containsOr only reports ORs it reached through connectives, so a
		// non-connective never lands here.
		*out = append(*out, ctx)
	}
}

// containsOr reports whether an OR appears in a predicate's own boolean
// skeleton — that is, reachable from expr through connectives alone.
//
// Descending no further than a leaf is the point, not a shortcut: an OR inside
// `d IN (SELECT t FROM u WHERE x OR y)` belongs to the subquery's structure, not
// to this predicate's, and counting it would split a conjunction that ought to
// group. It mirrors collectConditions' descent exactly, so the two agree on what
// "structure" means.
func containsOr(expr grammar1.IColumnExprContext) (found bool) {
	switch c := expr.(type) {
	case *grammar1.ColumnExprOrContext:
		found = true
	case *grammar1.ColumnExprAndContext:
		found = slices.ContainsFunc(c.AllColumnExpr(), containsOr)
	case *grammar1.ColumnExprNotContext:
		found = containsOr(c.ColumnExpr())
	case *grammar1.ColumnExprParensContext:
		found = containsOr(c.ColumnExpr())
	case *grammar1.ColumnExprFunctionContext:
		args, isConnective := booleanConnectiveArgs(c)
		if !isConnective {
			return
		}
		if nanopass.NormalizeCallName(c.Identifier().GetText()) == "or" {
			found = true
			return
		}
		found = slices.ContainsFunc(args, containsOr)
	}
	return
}

// booleanConnectiveArgs reports the argument expressions of a function call that
// spells a boolean connective. isConnective is false for any other call — and
// for a parametric one (`f(p)(x)`) or one carrying DISTINCT, neither of which a
// connective ever is, so an unexpected shape stays a leaf.
func booleanConnectiveArgs(c *grammar1.ColumnExprFunctionContext) (args []grammar1.IColumnExprContext, isConnective bool) {
	id := c.Identifier()
	if id == nil {
		return
	}
	_, known := booleanConnectiveFuncs[nanopass.NormalizeCallName(id.GetText())]
	if !known {
		return
	}
	if c.ColumnExprList() != nil || c.DISTINCT() != nil {
		return
	}
	argList, ok := c.ColumnArgList().(*grammar1.ColumnArgListContext)
	if !ok {
		return
	}
	all := argList.AllColumnArgExpr()
	args = make([]grammar1.IColumnExprContext, 0, len(all))
	for _, a := range all {
		ac, isArg := a.(*grammar1.ColumnArgExprContext)
		if !isArg {
			return nil, false
		}
		// A lambda argument has no columnExpr; a connective never takes one,
		// so an arg without one means this is not the shape we assumed.
		sub := ac.ColumnExpr()
		if sub == nil {
			return nil, false
		}
		args = append(args, sub)
	}
	if len(args) == 0 {
		return nil, false
	}
	isConnective = true
	return
}

// conditionExpr renders a condition subtree as the parenthesised expression the
// projection needs, so nothing in it can bind into the following AS. A condition
// that is itself a parenthesised group already carries its own parentheses —
// wrapping it again would emit `((a = 1 AND b = 2)) AS cond_1`.
func conditionExpr(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (expr string) {
	expr = nanopass.NodeText(pr, node)
	_, isParens := node.(*grammar1.ColumnExprParensContext)
	if isParens {
		return
	}
	expr = "(" + expr + ")"
	return
}

// quoteIfNeeded double-quotes a condition name that is not a bare SQL identifier —
// a leeway physical name carries colons and must be quoted, while a plain
// `cond_1` reads better unquoted.
func quoteIfNeeded(name string) (out string) {
	if isBareIdentifier(name) {
		out = name
		return
	}
	out = nanopass.QuoteIdentifier(name)
	return
}

func isBareIdentifier(name string) (bare bool) {
	if name == "" {
		return
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return
		}
	}
	bare = true
	return
}
