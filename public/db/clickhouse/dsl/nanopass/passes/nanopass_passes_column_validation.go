//go:build llm_generated_opus46

package passes

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ColumnNameViolation describes a projection column whose resulting name failed validation.
type ColumnNameViolation struct {
	Name       string // the resulting column name (alias or inferred from expression)
	RawName    string // as it appears in SQL (may include quotes)
	Expression string // the full expression text
	IsAlias    bool   // true if the name comes from an explicit alias
	Line       int
	Column     int
	Err        error
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
			err = eb.Build().Str("pattern", pattern).Errorf("invalid column name regex: %w", compileErr)
			return
		}

		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ValidateColumnNames: %w", err)
			return
		}

		violations := collectColumnNameViolations(pr, ValidatorFromRegexp(re))

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
func ValidatorFromRegexp(pattern *regexp.Regexp) ColumnNameValidator {
	return func(unquotedColName string, isAlias bool) (err error) {
		if !pattern.MatchString(unquotedColName) {
			err = fmt.Errorf("invalid column name: %q (alias=%v)", unquotedColName, isAlias)
		}
		return
	}
}

type ColumnNameValidator func(unquotedColName string, isAlias bool) (err error)

func collectColumnNameViolations(pr *nanopass.ParseResult, validator ColumnNameValidator) (violations []ColumnNameViolation) {
	// Walk the projection clauses in all SELECT statements
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		projClause, ok := ctx.(*grammar1.ProjectionClauseContext)
		if !ok {
			return true
		}

		// Find the ColumnExprListContext inside the projection clause
		for i := 0; i < projClause.GetChildCount(); i++ {
			exprList, ok := projClause.GetChild(i).(*grammar1.ColumnExprListContext)
			if !ok {
				continue
			}

			// Iterate ColumnsExpr children
			for j := 0; j < exprList.GetChildCount(); j++ {
				child := exprList.GetChild(j)

				switch c := child.(type) {
				case *grammar1.ColumnsExprColumnContext:
					v := validateColumnsExprColumn(pr, c, validator)
					if v != nil {
						violations = append(violations, *v)
					}
				case *grammar1.ColumnsExprAsteriskContext:
					// Skip * and table.* — can't validate without schema
				}
			}
		}

		return true // continue to find projection clauses in subqueries
	})
	return
}

func validateColumnsExprColumn(pr *nanopass.ParseResult, colExpr *grammar1.ColumnsExprColumnContext, validator ColumnNameValidator) (violation *ColumnNameViolation) {
	if colExpr.GetChildCount() == 0 {
		return
	}

	innerChild := colExpr.GetChild(0)
	innerCtx, ok := innerChild.(antlr.ParserRuleContext)
	if !ok {
		return
	}

	switch c := innerCtx.(type) {
	case *grammar1.ColumnExprAliasContext:
		// Has an alias — validate the alias name
		rawAlias, unquotedAlias, aliasLine, aliasCol := extractAliasInfo(c)
		if rawAlias == "" {
			return
		}
		exprText := extractAliasedExpression(pr, c)
		e := validator(unquotedAlias, true)
		if e != nil {
			violation = &ColumnNameViolation{
				Name:       unquotedAlias,
				RawName:    rawAlias,
				Expression: exprText,
				IsAlias:    true,
				Line:       aliasLine,
				Column:     aliasCol,
				Err:        e,
			}
		}

	case *grammar1.ColumnExprDynamicContext:
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

		e := validator(unquoted, false)
		if e != nil {
			violation = &ColumnNameViolation{
				Name:       unquoted,
				RawName:    inferredName,
				Expression: exprText,
				IsAlias:    false,
				Line:       tok.GetLine(),
				Column:     tok.GetColumn(),
				Err:        e,
			}
		}
	}

	return
}

// inferColumnName determines the resulting column name for an unaliased expression.
func inferColumnName(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (name string) {
	switch c := ctx.(type) {
	case *grammar1.ColumnExprIdentifierContext:
		// Bare or qualified identifier — extract just the column name
		for i := 0; i < c.GetChildCount(); i++ {
			if colId, ok := c.GetChild(i).(*grammar1.ColumnIdentifierContext); ok {
				if colId.NestedIdentifier() != nil {
					name = colId.NestedIdentifier().GetText()
				}
				return
			}
		}

	case *grammar1.ColumnExprLiteralContext:
		// Literal — the text is the column name
		name = nanopass.NodeText(pr, c)

	case *grammar1.ColumnExprFunctionContext:
		// Function call — full expression is the column name
		name = nanopass.NodeText(pr, c)

	default:
		// Any other expression — full text
		name = nanopass.NodeText(pr, ctx)
	}
	return
}

// --- shared helpers (also used by validateColumnsExprColumn) ---

func extractAliasInfo(ctx *grammar1.ColumnExprAliasContext) (raw string, unquoted string, line int, col int) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)

		if ident, ok := child.(*grammar1.IdentifierContext); ok {
			tok := ident.GetStart()
			raw = ident.GetText()
			unquoted = unquoteAlias(raw)
			line = tok.GetLine()
			col = tok.GetColumn()
			return
		}

		if alias, ok := child.(*grammar1.AliasContext); ok {
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

func extractAliasedExpression(pr *nanopass.ParseResult, ctx *grammar1.ColumnExprAliasContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if prc.GetRuleIndex() == grammar1.ClickHouseParserGrammar1RULE_columnExpr {
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
