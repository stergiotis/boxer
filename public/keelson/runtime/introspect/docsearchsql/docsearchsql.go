// Package docsearchsql is the nanopass pass that expands the
// docsearch('<query>') table-function macro into the documentation
// search over the section-grained corpora (ADR-0164 §SD5): a UNION of
// keelson('helpsections'), keelson('adrsections') and the executing
// server's own system.documentation, scored with the same field
// weights as the embedded tier (help/search) so a query means the same
// thing on both. Write
//
//	SELECT * FROM docsearch('deduplicate argMax') ORDER BY score DESC LIMIT 20
//
// and the pass splices the query's pattern battery — compiled exactly
// like the GUI search boxes compile it: whitespace-separated tokens,
// case-insensitive, an invalid regex degrading to a quoted literal —
// into multiMatchAllIndices calls. Every pattern must hit a section
// for it to qualify (the §SD2 RequireAll mode; the any-mode spelling
// for generated batteries arrives with text2regex, ADR-0139).
//
// The expansion emits keelson(...) references and leaves them for the
// downstream keelsonsql pass, so it inherits that macro's transport
// story: temporary tables in-process, url() against an external
// server. system.documentation resolves on either engine —
// clickhouse-local ships its own (the `system` database is
// placement-neutral, see play's dispatch policy) — and reflects
// whichever binary executes, which is the honest answer to "what does
// THIS server document".
//
// Result columns: source ('help'|'adr'|'chdoc'), ref (the canonical
// docref string, help/docref), kind, title, heading, score, context.
// No ORDER BY or LIMIT is baked in — the macro sits in table position,
// and those belong to the enclosing statement.
package docsearchsql

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FuncName is the table-function name the macro uses.
const FuncName = "docsearch"

// ExpandPass rewrites every docsearch('<query>') table-function call
// into its search subquery. A statement without the macro passes
// through byte-identical; expansion output contains no docsearch call,
// which is what makes the pass idempotent. A malformed call (wrong
// arity, a non-literal argument, an empty query) errors at expansion
// so it never reaches the server (the LW_ID / descriptiveStatistics
// precedent).
var ExpandPass = nanopass.LiftBodyPass("DocsearchExpand",
	expand,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	})

func expand(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eb.Build().Errorf("docsearchsql: parse: %w", err)
		return
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		result = sql
		return
	}
	rw := nanopass.NewRewriter(pr)
	for _, fn := range calls {
		query, argErr := queryArg(fn)
		if argErr != nil {
			err = argErr
			return
		}
		sub, subErr := expansionSQL(query)
		if subErr != nil {
			err = subErr
			return
		}
		nanopass.ReplaceNode(rw, fn, sub)
	}
	result = nanopass.GetText(rw)
	return
}

// findCalls returns every docsearch(...) table-function call in pr, in
// document order. Only the table-function position counts — a scalar
// docsearch('x') in a SELECT list is not this macro (mirroring
// keelsonsql.findCalls' single match predicate rationale).
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

// queryArg extracts the single quoted query-string argument.
func queryArg(fn *grammar1.TableFunctionExprContext) (query string, err error) {
	al := fn.TableArgList()
	if al == nil {
		err = eh.Errorf("docsearchsql: docsearch() needs exactly one quoted query argument")
		return
	}
	args := al.AllTableArgExpr()
	if len(args) != 1 {
		err = eh.Errorf("docsearchsql: docsearch() takes exactly one argument, got %d", len(args))
		return
	}
	lit := args[0].Literal()
	if lit == nil {
		err = eh.Errorf("docsearchsql: docsearch() argument must be a quoted query string")
		return
	}
	query, err = marshalling.UnescapeString(lit.GetText())
	if err != nil {
		err = eb.Build().Errorf("docsearchsql: query literal: %w", err)
		return
	}
	return
}

// arm parameterises one UNION branch. Column expressions are SQL
// fragments against the arm's table; docLevel gates the title tier
// (§SD3: a title hit belongs to the doc-level row, not to every
// section of the doc).
type arm struct {
	source   string
	table    string
	ref      string
	kind     string
	title    string
	heading  string
	body     string
	docLevel string
}

var arms = []arm{
	{
		source:   "help",
		table:    "keelson('helpsections')",
		ref:      "ref",
		kind:     "doc_type",
		title:    "doc_title",
		heading:  "heading",
		body:     "`body@text/markdown`",
		docLevel: "section = ''",
	},
	{
		source:   "adr",
		table:    "keelson('adrsections')",
		ref:      "ref",
		kind:     "status",
		title:    "title",
		heading:  "heading",
		body:     "`body@text/markdown`",
		docLevel: "section = ''",
	},
	{
		// One row per documented entity; every row is its own doc, so
		// the title tier (the function NAME) is always in play and
		// there is no heading tier.
		source:   "chdoc",
		table:    "system.documentation",
		ref:      "concat('chdoc://', name)",
		kind:     "toString(type)",
		title:    "name",
		heading:  "''",
		body:     "description",
		docLevel: "1",
	},
}

// expansionSQL renders the parenthesized UNION subquery for query.
// The battery is compiled here — not on the server — so the same
// degradation rules apply as in the GUI boxes, and the spliced
// patterns are guaranteed-valid RE2 (which is the subset hyperscan's
// multiMatch* accepts; see search.PatternT.EffectiveSource).
func expansionSQL(query string) (sub string, err error) {
	battery := search.ParseQuery(query)
	if battery.IsZero() {
		err = eh.Errorf("docsearchsql: docsearch() query is empty")
		return
	}
	pats := make([]string, len(battery.Patterns))
	for i := range battery.Patterns {
		pats[i] = battery.Patterns[i].EffectiveSource()
	}
	patsLit, err := marshalling.MarshalGoValueToSQL(pats)
	if err != nil {
		err = eb.Build().Errorf("docsearchsql: splice patterns: %w", err)
		return
	}
	ctxLit, err := marshalling.MarshalGoValueToSQL(contextPattern(&battery.Patterns[0]))
	if err != nil {
		err = eb.Build().Errorf("docsearchsql: splice context pattern: %w", err)
		return
	}
	n := strconv.Itoa(len(pats))

	var b strings.Builder
	b.WriteString("(")
	for i := range arms {
		a := &arms[i]
		if i > 0 {
			b.WriteString(" UNION ALL ")
		}
		// Weight per pattern k: the strongest tier it hit (§SD3 —
		// tiers do not add up for one pattern). w stays visible to the
		// outer WHERE so RequireAll can demand every pattern hit.
		b.WriteString("SELECT source, ref, kind, title, heading, score, context FROM (")
		b.WriteString("SELECT '" + a.source + "' AS source, " + a.ref + " AS ref, " + a.kind + " AS kind, ")
		b.WriteString(a.title + " AS title, " + a.heading + " AS heading, ")
		b.WriteString("arrayMap(k -> multiIf(")
		b.WriteString("(" + a.docLevel + ") AND has(multiMatchAllIndices(" + a.title + ", " + patsLit + "), k), 8, ")
		b.WriteString("(" + a.heading + ") != '' AND has(multiMatchAllIndices(" + a.heading + ", " + patsLit + "), k), 4, ")
		b.WriteString("has(multiMatchAllIndices(" + a.body + ", " + patsLit + "), k), 1, 0), ")
		b.WriteString("range(1, " + n + " + 1)) AS w, ")
		b.WriteString("arraySum(w) AS score, ")
		b.WriteString("extract(" + a.body + ", " + ctxLit + ") AS context ")
		b.WriteString("FROM " + a.table)
		b.WriteString(") WHERE score > 0 AND NOT has(w, 0)")
	}
	b.WriteString(")")
	sub = b.String()
	return
}

// contextPattern composes the snippet extractor for the first battery
// pattern: up to 40 bytes before the first match and 60 after, in one
// capturing group. The group wrapper matters — extract() returns the
// first capture group when the pattern has any, so wrapping the whole
// snippet makes any groups inside the user's own pattern ordinal 2+
// and harmless. A pattern that only hit the title yields an empty
// context, which the consumer renders as nothing.
func contextPattern(p *search.PatternT) (pat string) {
	pat = "(?is)(.{0,40}(?:" + strings.TrimPrefix(p.EffectiveSource(), "(?i)") + ").{0,60})"
	return
}
