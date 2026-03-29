//go:build llm_generated_opus46

package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
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
	return func(sql string) (result string, err error) {
		// Phase 1: Split into SET lines and query
		sets, _, query := ParseExtractedQuery(sql, prefix)
		if len(sets) == 0 {
			result = sql
			return
		}

		var accepted []acceptedParam
		var rejectedSets []string

		for _, info := range IterateExtractedParamsFromSets(sets, prefix) {
			if predicate != nil && !predicate(info) {
				// Find the original SET line for this param
				for _, set := range sets {
					if strings.Contains(set, info.FullName) {
						rejectedSets = append(rejectedSets, set)
						break
					}
				}
				continue
			}

			// Build the CTE value — reconstruct cast if present
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

			accepted = append(accepted, acceptedParam{
				info:     info,
				cteValue: cteValue,
			})
		}

		if len(accepted) == 0 {
			// Nothing to inject — return original
			result = sql
			return
		}

		// Phase 3: Replace {param: Type} slots with bare param references in query
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

		// Phase 4: Build CTE definitions and inject into query
		cteQuery, insertErr := insertCTEDefinitions(modifiedQuery, accepted)
		if insertErr != nil {
			err = eh.Errorf("InjectParamsAsCTE: %w", insertErr)
			return
		}

		// Phase 5: Prepend rejected SET lines
		var sb strings.Builder
		for _, set := range rejectedSets {
			sb.WriteString(set)
			sb.WriteString(";\n")
		}
		sb.WriteString(cteQuery)

		result = sb.String()
		return
	}
}

// Move to package level, before InjectParamsAsCTE
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
	var selectStmt *grammar.SelectStmtContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if ss, ok := ctx.(*grammar.SelectStmtContext); ok && selectStmt == nil {
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

	// Check if there's an existing WITH clause
	var withClause *grammar.WithClauseContext
	for i := 0; i < selectStmt.GetChildCount(); i++ {
		if wc, ok := selectStmt.GetChild(i).(*grammar.WithClauseContext); ok {
			withClause = wc
			break
		}
	}

	if withClause != nil {
		// Existing WITH clause — find the ColumnExprListContext and prepend
		var exprList *grammar.ColumnExprListContext
		for i := 0; i < withClause.GetChildCount(); i++ {
			if el, ok := withClause.GetChild(i).(*grammar.ColumnExprListContext); ok {
				exprList = el
				break
			}
		}
		if exprList != nil {
			// Insert before the first token of the existing expression list
			startToken := exprList.GetStart().GetTokenIndex()
			rw.InsertBeforeDefault(startToken, cteText+", ")
		}
	} else {
		// No WITH clause — find ProjectionClauseContext and insert WITH before it
		var projectionClause *grammar.ProjectionClauseContext
		for i := 0; i < selectStmt.GetChildCount(); i++ {
			if pc, ok := selectStmt.GetChild(i).(*grammar.ProjectionClauseContext); ok {
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
