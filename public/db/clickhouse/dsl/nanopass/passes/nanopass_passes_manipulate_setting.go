//go:build llm_generated_opus46

package passes

import (
	"strings"

	"slices"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// --- ReadSettings: CST → Go ---

// ReadSettings parses a SQL query and returns all settings as a map of Go values.
// Value types: int64, float64, string (unquoted), nil (NULL), []any (array), *Tuple (tuple).
// Returns an empty map if the query has no SETTINGS clause.
func ReadSettings(sql string) (settings map[string]any, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("ReadSettings: %w", err)
		return
	}

	settings = make(map[string]any)

	settingsClause := findSettingsClause(pr)
	if settingsClause == nil {
		return
	}

	// Find SettingExprListContext
	for i := 0; i < settingsClause.GetChildCount(); i++ {
		exprList, ok := settingsClause.GetChild(i).(*grammar1.SettingExprListContext)
		if !ok {
			continue
		}

		// Iterate SettingExprContext children
		for j := 0; j < exprList.GetChildCount(); j++ {
			expr, ok := exprList.GetChild(j).(*grammar1.SettingExprContext)
			if !ok {
				continue
			}

			name, val, extractErr := extractSettingExpr(expr)
			if extractErr != nil {
				err = eh.Errorf("ReadSettings: setting %q: %w", name, extractErr)
				return
			}
			settings[name] = val
		}
	}
	return
}

func findSettingsClause(pr *nanopass.ParseResult) *grammar1.SettingsClauseContext {
	var result *grammar1.SettingsClauseContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if sc, ok := ctx.(*grammar1.SettingsClauseContext); ok {
			result = sc
			return false
		}
		return true
	})
	return result
}

func extractSettingExpr(expr *grammar1.SettingExprContext) (name string, val any, err error) {
	// child[0] = IdentifierContext, child[1] = "=", child[2] = settingValue
	for i := 0; i < expr.GetChildCount(); i++ {
		if ident, ok := expr.GetChild(i).(*grammar1.IdentifierContext); ok {
			name = ident.GetText()
			break
		}
	}

	// Find the setting value (last child that is a settingValue node)
	for i := 0; i < expr.GetChildCount(); i++ {
		child := expr.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				val, err = deserializeSettingValue(prc)
				return
			}
		}
	}

	err = eh.Errorf("no value found for setting %q", name)
	return
}

func deserializeSettingValue(ctx antlr.ParserRuleContext) (val any, err error) {
	lit, litErr := deserializeSettingValueTyped(ctx)
	if litErr != nil {
		err = litErr
		return
	}
	return lit.ToAny()
}

func deserializeSettingValueTyped(ctx antlr.ParserRuleContext) (val marshalling.TypedLiteral, err error) {
	switch c := ctx.(type) {
	case *grammar1.SettingLiteralContext:
		return deserializeLiteral(c)

	case *grammar1.SettingArrayContext:
		return deserializeSettingArray(c)

	case *grammar1.SettingTupleContext:
		return deserializeSettingTuple(c)

	case *grammar1.SettingEmptyArrayContext:
		val = marshalling.NewHeterogeneousArray()
		return

	case *grammar1.SettingFunctionContext:
		return deserializeSettingFunction(c)

	case *grammar1.SettingFunctionEmptyContext:
		funcName := getSettingFunctionName(c)
		switch strings.ToLower(funcName) {
		case "array":
			val = marshalling.NewHeterogeneousArray()
		case "tuple":
			val = marshalling.NewTupleTyped()
		default:
			err = eh.Errorf("unknown setting function: %s", funcName)
		}
		return

	default:
		err = eh.Errorf("unknown setting value type: %T", ctx)
		return
	}
}

func deserializeLiteral(ctx *grammar1.SettingLiteralContext) (val marshalling.TypedLiteral, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if lit, ok := ctx.GetChild(i).(*grammar1.LiteralContext); ok {
			return marshalling.UnmarshalScalarLiteral(lit.GetText())
		}
	}
	err = eh.Errorf("empty SettingLiteral")
	return
}

func DeserializeLiteralContext(lit *grammar1.LiteralContext) (val marshalling.TypedLiteral, err error) {
	return marshalling.UnmarshalScalarLiteral(lit.GetText())
}

func deserializeSettingArray(ctx *grammar1.SettingArrayContext) (val marshalling.TypedLiteral, err error) {
	elems := make([]marshalling.TypedLiteral, 0)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem marshalling.TypedLiteral
				elem, err = deserializeSettingValueTyped(prc)
				if err != nil {
					return
				}
				elems = append(elems, elem)
			}
		}
	}
	het := marshalling.NewHeterogeneousArray(elems...)
	if hom, ok := het.TryHomogeneous(); ok {
		val = hom
	} else {
		val = het
	}
	return
}

func deserializeSettingTuple(ctx *grammar1.SettingTupleContext) (val marshalling.TypedLiteral, err error) {
	elems := make([]marshalling.TypedLiteral, 0)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem marshalling.TypedLiteral
				elem, err = deserializeSettingValueTyped(prc)
				if err != nil {
					return
				}
				elems = append(elems, elem)
			}
		}
	}
	val = marshalling.NewTupleTyped(elems...)
	return
}

func deserializeSettingFunction(ctx *grammar1.SettingFunctionContext) (val marshalling.TypedLiteral, err error) {
	funcName := ""
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			funcName = strings.ToLower(ident.GetText())
			break
		}
	}

	elems := make([]marshalling.TypedLiteral, 0)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem marshalling.TypedLiteral
				elem, err = deserializeSettingValueTyped(prc)
				if err != nil {
					return
				}
				elems = append(elems, elem)
			}
		}
	}

	switch funcName {
	case "array":
		het := marshalling.NewHeterogeneousArray(elems...)
		if hom, ok := het.TryHomogeneous(); ok {
			val = hom
		} else {
			val = het
		}
	case "tuple":
		val = marshalling.NewTupleTyped(elems...)
	default:
		err = eh.Errorf("unknown setting function: %s", funcName)
	}
	return
}
func getSettingFunctionName(ctx antlr.ParserRuleContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			return ident.GetText()
		}
	}
	return ""
}

// --- WriteSettings: Go → SQL ---

// WriteSettings returns a Pass that replaces the SETTINGS clause with the given values.
// If the query has no SETTINGS clause, one is added.
// If settings is empty, the SETTINGS clause is removed.
func WriteSettings(settings map[string]any) nanopass.Pass {
	return func(sql string) (result string, err error) {
		// First remove existing SETTINGS, then add new ones
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("WriteSettings: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		settingsClause := findSettingsClause(pr)

		if len(settings) == 0 {
			// Remove existing SETTINGS clause
			if settingsClause != nil {
				start := settingsClause.GetStart().GetTokenIndex()
				stop := settingsClause.GetStop().GetTokenIndex()
				// Include preceding whitespace
				if start > 0 {
					prevTok := pr.TokenStream.Get(start - 1)
					if prevTok.GetTokenType() == grammar1.ClickHouseLexerWHITESPACE {
						start = prevTok.GetTokenIndex()
					}
				}
				rw.DeleteDefault(start, stop)
			}
			result = nanopass.GetText(rw)
			return
		}

		// Serialize the new settings
		settingsSQL, serErr := serializeSettingsMap(settings)
		if serErr != nil {
			err = eh.Errorf("WriteSettings: %w", serErr)
			return
		}

		if settingsClause != nil {
			// Replace existing SETTINGS clause
			nanopass.ReplaceNode(rw, settingsClause, "SETTINGS "+settingsSQL)
		} else {
			// Add new SETTINGS clause — find anchor
			selectStmt := findOutermostSelectStmt(pr)
			if selectStmt == nil {
				err = eh.Errorf("WriteSettings: no SELECT statement found")
				return
			}
			anchor := findLastSelectStmtClause(selectStmt)
			if anchor == nil {
				err = eh.Errorf("WriteSettings: no clause found to anchor SETTINGS")
				return
			}
			nanopass.InsertAfter(rw, anchor, " SETTINGS "+settingsSQL)
		}

		result = nanopass.GetText(rw)
		return
	}
}

// ModifySettings returns a Pass that reads existing settings, applies a modifier function,
// and writes the result back. This enables atomic read-modify-write.
func ModifySettings(modifier func(settings map[string]any) error) nanopass.Pass {
	return func(sql string) (result string, err error) {
		settings, err := ReadSettings(sql)
		if err != nil {
			return
		}

		err = modifier(settings)
		if err != nil {
			err = eh.Errorf("ModifySettings: %w", err)
			return
		}

		result, err = WriteSettings(settings)(sql)
		return
	}
}

// --- Serialization: Go → SQL string ---

func serializeSettingsMap(settings map[string]any) (sql string, err error) {
	// Sort keys for deterministic output
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var valSQL string
		valSQL, err = marshalling.MarshalGoValueToSQL(settings[k])
		if err != nil {
			err = eh.Errorf("setting %q: %w", k, err)
			return
		}
		parts = append(parts, k+" = "+valSQL)
	}
	sql = strings.Join(parts, ", ")
	return
}

// findOutermostSelectStmt finds the first (outermost) selectStmt in the parse tree.
func findOutermostSelectStmt(pr *nanopass.ParseResult) *grammar1.SelectStmtContext {
	node := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar1.SelectStmtContext)
		return ok
	})
	if node == nil {
		return nil
	}
	return node.(*grammar1.SelectStmtContext)
}

// findLastSelectStmtClause returns the last clause present in the selectStmt.
func findLastSelectStmtClause(stmt *grammar1.SelectStmtContext) antlr.ParserRuleContext {
	if stmt.LimitClause() != nil {
		return stmt.LimitClause().(antlr.ParserRuleContext)
	}
	if stmt.LimitByClause() != nil {
		return stmt.LimitByClause().(antlr.ParserRuleContext)
	}
	if stmt.OrderByClause() != nil {
		return stmt.OrderByClause().(antlr.ParserRuleContext)
	}
	if stmt.HavingClause() != nil {
		return stmt.HavingClause().(antlr.ParserRuleContext)
	}
	if stmt.GroupByClause() != nil {
		return stmt.GroupByClause().(antlr.ParserRuleContext)
	}
	if stmt.WhereClause() != nil {
		return stmt.WhereClause().(antlr.ParserRuleContext)
	}
	if stmt.PrewhereClause() != nil {
		return stmt.PrewhereClause().(antlr.ParserRuleContext)
	}
	if stmt.QualifyClause() != nil {
		return stmt.QualifyClause().(antlr.ParserRuleContext)
	}
	if stmt.WindowClause() != nil {
		return stmt.WindowClause().(antlr.ParserRuleContext)
	}
	if stmt.ArrayJoinClause() != nil {
		return stmt.ArrayJoinClause().(antlr.ParserRuleContext)
	}
	if stmt.FromClause() != nil {
		return stmt.FromClause().(antlr.ParserRuleContext)
	}
	if stmt.ProjectionClause() != nil {
		return stmt.ProjectionClause().(antlr.ParserRuleContext)
	}
	return nil
}
