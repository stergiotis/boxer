//go:build llm_generated_opus47

package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// InjectParamsAsCTE returns a Pass that takes the output of ExtractLiterals
// (SET lines + query with {param: Type} slots) and converts selected parameters
// into WITH clause (CTE) definitions.
//
// The predicate controls which params become CTE values. Params accepted by the
// predicate are injected as CTE definitions and their {param: Type} slots are
// replaced with bare param references. Params rejected by the predicate are left
// as {param: Type} slots, with their SET lines preserved in the output.
//
// For params with cast types, the CTE value includes the cast:
//
//	WITH 1::UInt64 AS param_x_eq_<meta>, ... SELECT ...
//
// Existing WITH clauses in the query are preserved — new definitions are prepended.
//
// If mapCanonicalToClickHouse is nil, cast types are not reconstructed and bare
// values are used in the CTE definitions.
func InjectParamsAsCTE(
	prefix string,
	predicate func(info ExtractedParamInfo) bool,
	mapCanonicalToClickHouse func(ct canonicaltypes.PrimitiveAstNodeI) (string, error),
) nanopass.Pass {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}

	ctParser := canonicaltypes.NewParser()
	return nanopass.Pass{
		Name: "InjectParamsAsCTE",
		Apply: func(e *env.Environment, body string) (result string, err error) {
			if e == nil || len(e.Params) == 0 {
				result = body
				return
			}

			// Build the input as `SET line; ... ; body` so the existing logic
			// (which iterates SET text) can be reused without redesign.
			var preludeBuilder strings.Builder
			for name, p := range e.Params {
				if p.Raw == "" {
					continue
				}
				if _, _, parseErr := ParseParamName(name, prefix); parseErr != nil {
					continue
				}
				preludeBuilder.WriteString("SET ")
				preludeBuilder.WriteString(name)
				preludeBuilder.WriteString(" = ")
				preludeBuilder.WriteString(p.Raw)
				preludeBuilder.WriteString(";\n")
			}
			prelude := preludeBuilder.String()
			if prelude == "" {
				result = body
				return
			}
			full := prelude + body
			sets, _, query := ParseExtractedQuery(full, prefix)
			if len(sets) == 0 {
				result = body
				return
			}

			var accepted []acceptedParam
			rejectedNames := make(map[string]bool, len(sets))

			for _, info := range IterateExtractedParamsFromSets(sets, prefix) {
				if predicate != nil && !predicate(info) {
					rejectedNames[info.FullName] = true
					continue
				}
				cteValue := info.LiteralSQL
				if info.Metadata.CastTypeCanonical != "" && mapCanonicalToClickHouse != nil {
					var ct canonicaltypes.PrimitiveAstNodeI
					ct, err = ctParser.ParsePrimitiveTypeAst(info.Metadata.CastTypeCanonical)
					if err != nil {
						err = eb.Build().Str("info", info.String()).Errorf("error parsing canonical type (cast): %w", err)
						return
					}
					chType, mapErr := mapCanonicalToClickHouse(ct)
					if mapErr == nil && chType != "" {
						cteValue = info.LiteralSQL + "::" + chType
					}
				}
				accepted = append(accepted, acceptedParam{info: info, cteValue: cteValue})
			}

			if len(accepted) == 0 {
				result = body
				return
			}

			modifiedQuery := query
			for _, ap := range accepted {
				slotPrefix := "{" + ap.info.FullName + ":"
				for {
					idx := strings.Index(modifiedQuery, slotPrefix)
					if idx < 0 {
						break
					}
					endIdx := strings.Index(modifiedQuery[idx:], "}")
					if endIdx < 0 {
						break
					}
					endIdx += idx
					modifiedQuery = modifiedQuery[:idx] + ap.info.FullName + modifiedQuery[endIdx+1:]
				}
			}

			cteQuery, insertErr := insertCTEDefinitions(modifiedQuery, accepted)
			if insertErr != nil {
				err = eh.Errorf("InjectParamsAsCTE: %w", insertErr)
				return
			}

			// Remove accepted-and-injected params from env so Integrate
			// won't re-emit them as SET lines. Rejected params stay in env.
			for _, ap := range accepted {
				delete(e.Params, ap.info.FullName)
			}
			result = cteQuery
			return
		},
		Properties: nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody | nanopass.RegionParams,
			Writes:     nanopass.RegionBody | nanopass.RegionParams,
		},
	}
}

type acceptedParam struct {
	info     ExtractedParamInfo
	cteValue string
}

// insertCTEDefinitions parses the query and injects CTE definitions into the WITH clause.
// If the query has no WITH clause, one is created. If it has an existing WITH clause,
// the new definitions are prepended to the existing list.
func insertCTEDefinitions(query string, accepted []acceptedParam) (result string, err error) {
	// Build the CTE definition text: "value AS name, value2 AS name2"
	cteParts := make([]string, 0, len(accepted))
	for _, ap := range accepted {
		cteParts = append(cteParts, fmt.Sprintf("%s AS %s", ap.cteValue, ap.info.FullName))
	}
	cteText := strings.Join(cteParts, ", ")

	// Parse the query to find the insertion point
	pr, parseErr := nanopass.Parse(query)
	if parseErr != nil {
		// If query doesn't parse (e.g., has remaining {param: Type} slots),
		// fall back to string-level insertion
		result, err = insertCTEDefinitionsStringLevel(query, cteText)
		return
	}

	// Find the first SelectStmtContext
	var selectStmt *grammar1.SelectStmtContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if ss, ok := ctx.(*grammar1.SelectStmtContext); ok && selectStmt == nil {
			selectStmt = ss
			return false
		}
		return true
	})

	if selectStmt == nil {
		// No SELECT statement found — fall back to string-level
		result, err = insertCTEDefinitionsStringLevel(query, cteText)
		return
	}

	rw := nanopass.NewRewriter(pr)

	// Find an existing WITH source for the query. Top-level WITH may live in
	// the query-level `ctes?` rule (preferred by the parser) or in this
	// selectStmt's `withClause?` (used when nested inside a subquery). Both
	// rules emit a sequence of withItem children; we just need the first item.
	firstItem := findFirstWithItem(pr.Tree, selectStmt)

	if firstItem != nil {
		startToken := firstItem.GetStart().GetTokenIndex()
		rw.InsertBeforeDefault(startToken, cteText+", ")
	} else {
		// No WITH clause — find ProjectionClauseContext and insert WITH before it
		var projectionClause *grammar1.ProjectionClauseContext
		for i := 0; i < selectStmt.GetChildCount(); i++ {
			if pc, ok := selectStmt.GetChild(i).(*grammar1.ProjectionClauseContext); ok {
				projectionClause = pc
				break
			}
		}
		if projectionClause != nil {
			startToken := projectionClause.GetStart().GetTokenIndex()
			rw.InsertBeforeDefault(startToken, "WITH "+cteText+" ")
		}
	}

	result = nanopass.GetText(rw)
	return
}

// findFirstWithItem returns the first withItem under either the query-level
// ctes? or the given selectStmt's withClause?, whichever holds the existing
// WITH for this query. Returns nil if neither rule produced a withItem.
func findFirstWithItem(tree antlr.ParserRuleContext, selectStmt *grammar1.SelectStmtContext) grammar1.IWithItemContext {
	// query-level ctes (preferred at the top level)
	if qs, ok := tree.(*grammar1.QueryStmtContext); ok {
		for i := 0; i < qs.GetChildCount(); i++ {
			q, ok := qs.GetChild(i).(*grammar1.QueryContext)
			if !ok {
				continue
			}
			for j := 0; j < q.GetChildCount(); j++ {
				ctes, ok := q.GetChild(j).(*grammar1.CtesContext)
				if !ok {
					continue
				}
				if wi := firstWithItemIn(ctes); wi != nil {
					return wi
				}
			}
		}
	}
	// selectStmt-level withClause (used for nested SELECTs)
	for i := 0; i < selectStmt.GetChildCount(); i++ {
		wc, ok := selectStmt.GetChild(i).(*grammar1.WithClauseContext)
		if !ok {
			continue
		}
		if wi := firstWithItemIn(wc); wi != nil {
			return wi
		}
	}
	return nil
}

func firstWithItemIn(parent antlr.ParserRuleContext) grammar1.IWithItemContext {
	for i := 0; i < parent.GetChildCount(); i++ {
		if wi, ok := parent.GetChild(i).(grammar1.IWithItemContext); ok {
			return wi
		}
	}
	return nil
}

// insertCTEDefinitionsStringLevel is a fallback for when CST parsing fails
// (e.g., because the query still has {param: Type} slots that aren't valid SQL).
func insertCTEDefinitionsStringLevel(query string, cteText string) (result string, err error) {
	trimmed := strings.TrimSpace(query)

	// Check for existing WITH clause (case-insensitive)
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "WITH ") {
		// Find the position after "WITH " and insert before existing definitions
		// We need to handle "WITH\n" and "WITH  " etc.
		withEnd := 0
		for i := 4; i < len(trimmed); i++ {
			if trimmed[i] != ' ' && trimmed[i] != '\t' && trimmed[i] != '\n' && trimmed[i] != '\r' {
				withEnd = i
				break
			}
		}
		if withEnd > 0 {
			result = trimmed[:withEnd] + cteText + ", " + trimmed[withEnd:]
			return
		}
	}

	// Check for SELECT (case-insensitive)
	if strings.HasPrefix(upper, "SELECT") {
		result = "WITH " + cteText + " " + trimmed
		return
	}

	// Unknown structure — prepend WITH
	result = "WITH " + cteText + "\n" + trimmed
	return
}
