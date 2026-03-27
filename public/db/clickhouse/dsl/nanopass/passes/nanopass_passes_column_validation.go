//go:build llm_generated_opus46

package passes

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ColumnNameViolation describes a projection column whose resulting name failed validation.
type ColumnNameViolation struct {
	Name       string // the resulting column name (alias or inferred from expression)
	RawName    string // as it appears in SQL (may include quotes)
	Expression string // the full expression text
	IsAlias    bool   // true if the name comes from an explicit alias
	Line       int
	Column     int
}

func (inst *ColumnNameViolation) String() string {
	if inst.IsAlias {
		return fmt.Sprintf("line %d:%d — alias %q on expression %q does not match the required pattern",
			inst.Line, inst.Column, inst.Name, inst.Expression)
	}
	return fmt.Sprintf("line %d:%d — column expression %q produces name %q which does not match the required pattern",
		inst.Line, inst.Column, inst.Expression, inst.Name)
}

// ColumnNameValidationError is returned when column name validation fails.
type ColumnNameValidationError struct {
	Pattern    string
	Violations []ColumnNameViolation
	IsForbid   bool
}

func (inst *ColumnNameValidationError) Error() string {
	var sb strings.Builder

	if inst.IsForbid {
		sb.WriteString(fmt.Sprintf("found %d column name(s) matching forbidden pattern /%s/:\n",
			len(inst.Violations), inst.Pattern))
	} else {
		sb.WriteString(fmt.Sprintf("found %d column name(s) not matching required pattern /%s/:\n",
			len(inst.Violations), inst.Pattern))
	}

	for i, v := range inst.Violations {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("  %s", v.String()))
	}

	return sb.String()
}

// GetColumnNameViolations returns the violations from a ColumnNameValidationError,
// or nil if the error is not a ColumnNameValidationError.
func GetColumnNameViolations(err error) []ColumnNameViolation {
	var cve *ColumnNameValidationError
	if ok := errors.As(err, &cve); ok {
		return cve.Violations
	}
	return nil
}

// ValidateColumnNames returns a Pass that checks all resulting column names
// in the SELECT list match the given regex pattern.
//
// For aliased columns (`a AS x`), the alias name is validated.
// For unaliased columns (`a`, `sum(a)`, `a + b`), the inferred column name is validated:
//   - Bare identifiers: the identifier name (`a`)
//   - Qualified identifiers: the column name without qualifier (`t.a` → `a`)
//   - All other expressions: the full expression text (`sum(a)`, `a + b`)
//
// Star expressions (`*`, `table.*`) and COLUMNS('...') are skipped — their resulting
// names depend on schema and cannot be validated statically.
//
// The pattern is matched against unquoted names (quotes and backticks are stripped).
// All UNION ALL branches, CTE bodies, and subqueries are checked.
//
// This is a validation-only pass — the SQL is returned unchanged if all names match.
func ValidateColumnNames(pattern string) nanopass.Pass {
	return func(sql string) (result string, err error) {
		re, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			err = eh.Errorf("ValidateColumnNames: invalid regex %q: %w", pattern, compileErr)
			return
		}

		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ValidateColumnNames: %w", err)
			return
		}

		violations := collectColumnNameViolations(pr, re, false)

		if len(violations) > 0 {
			err = &ColumnNameValidationError{
				Pattern:    pattern,
				Violations: violations,
				IsForbid:   false,
			}
			return
		}

		result = sql
		return
	}
}

// ValidateColumnNamesExclude returns a Pass that checks no resulting column name
// matches the given "forbidden" regex pattern.
func ValidateColumnNamesExclude(forbiddenPattern string) nanopass.Pass {
	return func(sql string) (result string, err error) {
		re, compileErr := regexp.Compile(forbiddenPattern)
		if compileErr != nil {
			err = eh.Errorf("ValidateColumnNamesExclude: invalid regex %q: %w", forbiddenPattern, compileErr)
			return
		}

		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ValidateColumnNamesExclude: %w", err)
			return
		}

		violations := collectColumnNameViolations(pr, re, true)

		if len(violations) > 0 {
			err = &ColumnNameValidationError{
				Pattern:    forbiddenPattern,
				Violations: violations,
				IsForbid:   true,
			}
			return
		}

		result = sql
		return
	}
}

func collectColumnNameViolations(pr *nanopass.ParseResult, re *regexp.Regexp, isForbid bool) (violations []ColumnNameViolation) {
	// Walk the projection clauses in all SELECT statements
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		projClause, ok := ctx.(*grammar.ProjectionClauseContext)
		if !ok {
			return true
		}

		// Find the ColumnExprListContext inside the projection clause
		for i := 0; i < projClause.GetChildCount(); i++ {
			exprList, ok := projClause.GetChild(i).(*grammar.ColumnExprListContext)
			if !ok {
				continue
			}

			// Iterate ColumnsExpr children
			for j := 0; j < exprList.GetChildCount(); j++ {
				child := exprList.GetChild(j)

				switch c := child.(type) {
				case *grammar.ColumnsExprColumnContext:
					v := validateColumnsExprColumn(pr, c, re, isForbid)
					if v != nil {
						violations = append(violations, *v)
					}
				case *grammar.ColumnsExprAsteriskContext:
					// Skip * and table.* — can't validate without schema
				}
			}
		}

		return true // continue to find projection clauses in subqueries
	})
	return
}

func validateColumnsExprColumn(pr *nanopass.ParseResult, colExpr *grammar.ColumnsExprColumnContext, re *regexp.Regexp, isForbid bool) (violation *ColumnNameViolation) {
	if colExpr.GetChildCount() == 0 {
		return
	}

	innerChild := colExpr.GetChild(0)
	innerCtx, ok := innerChild.(antlr.ParserRuleContext)
	if !ok {
		return
	}

	switch c := innerCtx.(type) {
	case *grammar.ColumnExprAliasContext:
		// Has an alias — validate the alias name
		rawAlias, unquotedAlias, aliasLine, aliasCol := extractAliasInfo(c)
		if rawAlias == "" {
			return
		}
		exprText := extractAliasedExpression(pr, c)
		matched := re.MatchString(unquotedAlias)
		if (isForbid && matched) || (!isForbid && !matched) {
			violation = &ColumnNameViolation{
				Name:       unquotedAlias,
				RawName:    rawAlias,
				Expression: exprText,
				IsAlias:    true,
				Line:       aliasLine,
				Column:     aliasCol,
			}
		}

	case *grammar.ColumnExprDynamicContext:
		// COLUMNS('...') — skip, can't validate without schema

	default:
		// No alias — infer the resulting column name from the expression
		inferredName := inferColumnName(pr, innerCtx)
		if inferredName == "" {
			return
		}
		unquoted := unquoteAlias(inferredName)
		exprText := nanopass.NodeText(pr, innerCtx)
		tok := innerCtx.GetStart()

		matched := re.MatchString(unquoted)
		if (isForbid && matched) || (!isForbid && !matched) {
			violation = &ColumnNameViolation{
				Name:       unquoted,
				RawName:    inferredName,
				Expression: exprText,
				IsAlias:    false,
				Line:       tok.GetLine(),
				Column:     tok.GetColumn(),
			}
		}
	}

	return
}

// inferColumnName determines the resulting column name for an unaliased expression.
func inferColumnName(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (name string) {
	switch c := ctx.(type) {
	case *grammar.ColumnExprIdentifierContext:
		// Bare or qualified identifier — extract just the column name
		for i := 0; i < c.GetChildCount(); i++ {
			if colId, ok := c.GetChild(i).(*grammar.ColumnIdentifierContext); ok {
				if colId.NestedIdentifier() != nil {
					name = colId.NestedIdentifier().GetText()
				}
				return
			}
		}

	case *grammar.ColumnExprLiteralContext:
		// Literal — the text is the column name
		name = nanopass.NodeText(pr, c)

	case *grammar.ColumnExprFunctionContext:
		// Function call — full expression is the column name
		name = nanopass.NodeText(pr, c)

	default:
		// Any other expression — full text
		name = nanopass.NodeText(pr, ctx)
	}
	return
}

// --- shared helpers (also used by validateColumnsExprColumn) ---

func extractAliasInfo(ctx *grammar.ColumnExprAliasContext) (raw string, unquoted string, line int, col int) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)

		if ident, ok := child.(*grammar.IdentifierContext); ok {
			tok := ident.GetStart()
			raw = ident.GetText()
			unquoted = unquoteAlias(raw)
			line = tok.GetLine()
			col = tok.GetColumn()
			return
		}

		if alias, ok := child.(*grammar.AliasContext); ok {
			tok := alias.GetStart()
			raw = alias.GetText()
			unquoted = unquoteAlias(raw)
			line = tok.GetLine()
			col = tok.GetColumn()
			return
		}
	}
	return
}

func extractAliasedExpression(pr *nanopass.ParseResult, ctx *grammar.ColumnExprAliasContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if prc.GetRuleIndex() == grammar.ClickHouseParserRULE_columnExpr {
				return nanopass.NodeText(pr, prc)
			}
		}
	}
	return ""
}

func unquoteAlias(s string) string {
	if len(s) < 2 {
		return s
	}
	first := s[0]
	last := s[len(s)-1]

	if (first == '"' && last == '"') || (first == '`' && last == '`') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
