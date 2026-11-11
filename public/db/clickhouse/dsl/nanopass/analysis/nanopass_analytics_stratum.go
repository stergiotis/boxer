package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// QueryStratumE is the ADR-0367 §SD2 stratum of a statement: how much an
// executor has to be able to do to answer it, judged from the text alone.
// Numerically smaller asks for more — combining layers takes the minimum —
// and the zero value is [QueryStratumUnplaced], so a stratum nobody assigned
// can never be mistaken for a simple read.
type QueryStratumE uint8

const (
	// QueryStratumUnplaced — the statement has no read stratum: it writes,
	// it changes a setting, or the grammar could not read it. Which of the
	// three is [Classification.Kind]'s to say.
	QueryStratumUnplaced QueryStratumE = iota
	// QueryStratumDerive — a read whose output is computed: an expression or
	// alias in the projection, an aggregate, a join, a window, ARRAY JOIN,
	// DISTINCT, LIMIT BY, FINAL, SAMPLE, a set operation other than UNION
	// ALL, or a SELECT with no source.
	QueryStratumDerive
	// QueryStratumRestrict — stored rows returned unchanged, but not all of
	// them or not as stored: a column subset, WHERE / PREWHERE, ORDER BY.
	// The predicate and the sort key may be any expression; what they call
	// is [Classification.Functions]'s to say.
	QueryStratumRestrict
	// QueryStratumVerbatim — one source handed over whole: `SELECT *` with
	// at most LIMIT / OFFSET / TOP. Nothing is evaluated per row.
	QueryStratumVerbatim
)

var AllQueryStrata = []QueryStratumE{QueryStratumUnplaced, QueryStratumDerive, QueryStratumRestrict, QueryStratumVerbatim}

func (inst QueryStratumE) String() (s string) {
	switch inst {
	case QueryStratumVerbatim:
		s = "verbatim"
	case QueryStratumRestrict:
		s = "restrict"
	case QueryStratumDerive:
		s = "derive"
	default:
		s = "unplaced"
	}
	return
}

// ClassifyQueryStratum places a parsed read in its stratum (ADR-0367 §SD2).
//
// The rules are [ExtractPassthroughTables]' with two differences, both of
// which follow from asking about the work rather than about the rows. A
// table function is a source like any other here — whether it reaches beyond
// the endpoint is [ClassifyQuerySecurity]'s question — and FINAL, SAMPLE and
// LIMIT BY place a SELECT in [QueryStratumDerive], because each needs more
// than a scan with a predicate. A subquery or non-recursive CTE source is
// followed down, and the statement takes the lowest stratum on the way.
//
// It answers [QueryStratumUnplaced] for the INSERT wrapper. A SET prelude is
// not looked at: a caller that needs "this changes a setting" combines the
// answer with the security class, as [Classify] does.
//
// err is non-nil only for a tree not produced by [nanopass.Parse]; the
// stratum is then the zero value.
func ClassifyQueryStratum(pr *nanopass.ParseResult, defaultDatabase string) (stratum QueryStratumE, err error) {
	if pr == nil || pr.Tree == nil {
		err = eh.Errorf("nil parse result")
		return
	}
	if pr.InsertStmt() != nil {
		return
	}
	scopes, err := nanopass.BuildScopes(pr, defaultDatabase)
	if err != nil {
		err = eh.Errorf("build scopes: %w", err)
		return
	}
	if anyAmbiguousCTE(scopes) {
		// The engine rejects the statement; the strongest read stratum is
		// the closed answer.
		stratum = QueryStratumDerive
		return
	}
	stratum = chainStratum(scopes, topLevelUnionStmt(pr), 0)
	return
}

func chainStratum(members []*nanopass.SelectScope, unionNode *grammar1.SelectUnionStmtContext, depth int) (stratum QueryStratumE) {
	stratum = QueryStratumDerive
	if len(members) == 0 {
		return
	}
	if unionNode != nil && !unionChainIsUnionAll(unionNode) {
		return
	}
	stratum = QueryStratumVerbatim
	for _, member := range members {
		stratum = min(stratum, selectStratum(member, depth))
	}
	return
}

func selectStratum(scope *nanopass.SelectScope, depth int) (stratum QueryStratumE) {
	stratum = QueryStratumDerive
	if depth > maxPassthroughDepth {
		return
	}
	stmt := scope.Node
	if selectHasBlockingClause(scope) || stmt.LimitByClause() != nil || fromHasFinalOrSample(stmt) {
		return
	}
	if len(scope.Tables) != 1 {
		return
	}
	if !projectionIsAllVerbatim(scope) {
		return
	}
	own := QueryStratumVerbatim
	if stmt.WhereClause() != nil || stmt.PrewhereClause() != nil || stmt.OrderByClause() != nil || !projectionIsAllStars(scope) {
		own = QueryStratumRestrict
	}
	ts := &scope.Tables[0]
	switch {
	case ts.IsCTE:
		def, found := scope.ResolveCTE(ts.Table)
		if !found || def.Recursive {
			return
		}
		stratum = min(own, chainStratum(def.Scopes, unionStmtOf(def.Node), depth+1))
	case ts.IsSubquery:
		stratum = min(own, chainStratum(ts.Scopes, unionStmtOf(ts.Node), depth+1))
	default:
		stratum = own
	}
	return
}

// projectionIsAllStars reports whether the projection is stars only, with no
// EXCEPT: the source's columns as they are, none dropped.
func projectionIsAllStars(scope *nanopass.SelectScope) (ok bool) {
	pc, isPC := scope.Node.ProjectionClause().(*grammar1.ProjectionClauseContext)
	if !isPC || pc.ProjectionExceptClause() != nil {
		return
	}
	cel, isCEL := pc.ColumnExprList().(*grammar1.ColumnExprListContext)
	if !isCEL {
		return
	}
	items := cel.AllColumnsExpr()
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		if _, star := item.(*grammar1.ColumnsExprAsteriskContext); !star {
			return
		}
	}
	ok = true
	return
}

// fromHasFinalOrSample reports a FINAL or SAMPLE anywhere under the SELECT's
// FROM. It also sees one inside a subquery source, which only lowers a
// stratum the subquery would have lowered itself.
func fromHasFinalOrSample(stmt *grammar1.SelectStmtContext) (found bool) {
	from := stmt.FromClause()
	if from == nil {
		return
	}
	hits := nanopass.FindAll(from, func(ctx antlr.ParserRuleContext) bool {
		jt, ok := ctx.(*grammar1.JoinExprTableContext)
		return ok && (jt.FINAL() != nil || jt.SampleClause() != nil)
	})
	found = len(hits) > 0
	return
}

// Classification is everything the text of one statement shows, gathered
// from a single parse (ADR-0367 §SD2). The zero value is the closed answer on
// every axis: unknown kind, mutating, unplaced.
type Classification struct {
	// Parsed reports whether the grammar read the statement — for a wrapper
	// that Introspects, the statement it wraps. When it did not, Kind comes
	// from the leading keyword and, unless Introspects, every other field
	// is its zero value.
	Parsed bool
	// Introspects marks a statement that reports on a statement or a named
	// object instead of returning rows: EXPLAIN, DESCRIBE, EXISTS. Around a
	// statement the grammar reads, the other fields are that statement's —
	// what a server has to understand to answer — except Passthrough, since
	// no row of a table is returned. About a named object, the statement is
	// a read with no stratum.
	Introspects bool
	Kind        KindE
	Security    QuerySecurityClassE
	Stratum     QueryStratumE
	// Witnesses are the constructs that lowered Security below read.
	Witnesses []SecurityWitness
	// Passthrough lists the base tables returned 1:1 (ADR-0117). A
	// statement in [QueryStratumRestrict] or above over a table function
	// has none.
	Passthrough []TableRef
	// Functions are the functions the statement calls, in source order.
	Functions []FunctionRef
}

// Classify reads sql once and answers every text-level question about it:
// kind, security class, stratum, passthrough tables and the functions it
// calls. It is total — no input errors and none is rejected — and each axis
// fails closed on its own, as the function that owns it documents.
//
// A statement whose security class is mutating has no read stratum, whatever
// its shape: a non-`param_*` SET ahead of a `SELECT *` is a change of state
// first.
func Classify(sql string, defaultDatabase string) (c Classification) {
	pr, err := nanopass.Parse(sql)
	c.Kind = statementKind(sql, pr, err)
	if err != nil {
		if c.Kind != KindReadOnly {
			return
		}
		inner, catalogue, ok := introspected(sql)
		switch {
		case !ok:
		case catalogue:
			c.Introspects, c.Security = true, QuerySecurityRead
		default:
			// The wrapped statement has to be one the grammar reads: an
			// introspection wrapped in another is left unread.
			w := Classify(inner, defaultDatabase)
			if w.Parsed && !w.Introspects && w.Kind == KindReadOnly {
				c = w
				c.Introspects, c.Passthrough = true, nil
			}
		}
		return
	}
	c.Parsed = true
	c.Functions = ExtractFunctions(pr)
	c.Security, c.Witnesses, err = ClassifyQuerySecurity(pr)
	if err != nil || c.Security == QuerySecurityMutating {
		c.Security = QuerySecurityMutating
		return
	}
	stratum, err := ClassifyQueryStratum(pr, defaultDatabase)
	if err != nil {
		return
	}
	c.Stratum = stratum
	if stratum >= QueryStratumRestrict {
		// err leaves the list empty, which is the closed answer.
		c.Passthrough, _ = ExtractPassthroughTables(pr, defaultDatabase)
	}
	return
}
