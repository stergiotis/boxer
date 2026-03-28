//go:build llm_generated_opus46

package passes

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"iter"
	"slices"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/scalars"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// Tuple represents a ClickHouse tuple value with optional named slots.
type Tuple struct {
	slotNames  []string
	slotValues []any
}

// NewTuple creates a new Tuple with named slots.
func NewTuple(slotNames []string) (inst *Tuple) {
	inst = &Tuple{
		slotNames:  slotNames,
		slotValues: make([]any, len(slotNames)),
	}
	return
}

// NewUnnamedTuple creates a Tuple with positional-only slots.
func NewUnnamedTuple(values ...any) (inst *Tuple) {
	names := make([]string, len(values))
	for i := range names {
		names[i] = fmt.Sprintf("_%d", i)
	}
	inst = &Tuple{
		slotNames:  names,
		slotValues: make([]any, len(values)),
	}
	copy(inst.slotValues, values)
	return
}

func (inst *Tuple) Len() int {
	return len(inst.slotValues)
}

func (inst *Tuple) SetByName(slotName string, val any) (found bool) {
	idx := slices.Index(inst.slotNames, slotName)
	found = idx != -1
	if found {
		inst.slotValues[idx] = val
	}
	return
}

func (inst *Tuple) SetByIndex(zeroBasedIdx int, val any) (found bool) {
	found = zeroBasedIdx >= 0 && zeroBasedIdx < len(inst.slotValues)
	if found {
		inst.slotValues[zeroBasedIdx] = val
	}
	return
}

func (inst *Tuple) GetByIndex(zeroBasedIdx int) (val any, found bool) {
	found = zeroBasedIdx >= 0 && zeroBasedIdx < len(inst.slotValues)
	if found {
		val = inst.slotValues[zeroBasedIdx]
	}
	return
}

func (inst *Tuple) GetByName(slotName string) (val any, found bool) {
	idx := slices.Index(inst.slotNames, slotName)
	found = idx != -1
	if found {
		val = inst.slotValues[idx]
	}
	return
}

func (inst *Tuple) IterateAll() iter.Seq2[int, any] {
	return func(yield func(int, any) bool) {
		for i, val := range inst.slotValues {
			if !yield(i, val) {
				return
			}
		}
	}
}

func (inst *Tuple) IterateAllWithNames() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for i, val := range inst.slotValues {
			if !yield(inst.slotNames[i], val) {
				return
			}
		}
	}
}

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
		exprList, ok := settingsClause.GetChild(i).(*grammar.SettingExprListContext)
		if !ok {
			continue
		}

		// Iterate SettingExprContext children
		for j := 0; j < exprList.GetChildCount(); j++ {
			expr, ok := exprList.GetChild(j).(*grammar.SettingExprContext)
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

func findSettingsClause(pr *nanopass.ParseResult) *grammar.SettingsClauseContext {
	var result *grammar.SettingsClauseContext
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if sc, ok := ctx.(*grammar.SettingsClauseContext); ok {
			result = sc
			return false
		}
		return true
	})
	return result
}

func extractSettingExpr(expr *grammar.SettingExprContext) (name string, val any, err error) {
	// child[0] = IdentifierContext, child[1] = "=", child[2] = settingValue
	for i := 0; i < expr.GetChildCount(); i++ {
		if ident, ok := expr.GetChild(i).(*grammar.IdentifierContext); ok {
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
	switch c := ctx.(type) {
	case *grammar.SettingLiteralContext:
		return deserializeLiteral(c)

	case *grammar.SettingArrayContext:
		return deserializeSettingArray(c)

	case *grammar.SettingTupleContext:
		return deserializeSettingTuple(c)

	case *grammar.SettingEmptyArrayContext:
		val = make([]any, 0)
		return

	case *grammar.SettingFunctionContext:
		return deserializeSettingFunction(c)

	case *grammar.SettingFunctionEmptyContext:
		// array() → empty slice, tuple() → empty tuple
		funcName := getSettingFunctionName(c)
		switch strings.ToLower(funcName) {
		case "array":
			val = make([]any, 0)
		case "tuple":
			val = NewUnnamedTuple()
		default:
			err = eh.Errorf("unknown setting function: %s", funcName)
		}
		return

	default:
		err = eh.Errorf("unknown setting value type: %T", ctx)
		return
	}
}

func deserializeLiteral(ctx *grammar.SettingLiteralContext) (val any, err error) {
	// SettingLiteral → LiteralContext
	for i := 0; i < ctx.GetChildCount(); i++ {
		if lit, ok := ctx.GetChild(i).(*grammar.LiteralContext); ok {
			return DeserializeLiteralContext(lit)
		}
	}
	err = eh.Errorf("empty SettingLiteral")
	return
}

func DeserializeLiteralContext(lit *grammar.LiteralContext) (val any, err error) {
	if lit.NULL_SQL() != nil {
		val = nil
		return
	}
	if lit.STRING_LITERAL() != nil {
		text := lit.STRING_LITERAL().GetText()
		// Remove surrounding quotes
		if len(text) >= 2 {
			val = text[1 : len(text)-1]
		} else {
			val = ""
		}
		return
	}
	if lit.NumberLiteral() != nil {
		return deserializeNumberLiteral(lit.NumberLiteral().(*grammar.NumberLiteralContext))
	}
	err = eh.Errorf("unrecognized literal")
	return
}

func deserializeNumberLiteral(num *grammar.NumberLiteralContext) (val any, err error) {
	text := num.GetText()

	// Try integer first
	if i, parseErr := strconv.ParseInt(text, 0, 64); parseErr == nil {
		val = i
		return
	}

	// Try float
	if f, parseErr := strconv.ParseFloat(text, 64); parseErr == nil {
		val = f
		return
	}

	// Special values
	lower := strings.ToLower(text)
	switch lower {
	case "inf", "+inf":
		val = math.Inf(1)
		return
	case "-inf":
		val = math.Inf(-1)
		return
	case "nan":
		val = math.NaN()
		return
	}

	err = eh.Errorf("cannot parse number: %s", text)
	return
}

func deserializeSettingArray(ctx *grammar.SettingArrayContext) (val any, err error) {
	var elements []any
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem any
				elem, err = deserializeSettingValue(prc)
				if err != nil {
					return
				}
				elements = append(elements, elem)
			}
		}
	}
	if elements == nil {
		elements = make([]any, 0)
	}
	val = elements
	return
}

func deserializeSettingTuple(ctx *grammar.SettingTupleContext) (val any, err error) {
	var elements []any
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem any
				elem, err = deserializeSettingValue(prc)
				if err != nil {
					return
				}
				elements = append(elements, elem)
			}
		}
	}
	val = NewUnnamedTuple(elements...)
	return
}

func deserializeSettingFunction(ctx *grammar.SettingFunctionContext) (val any, err error) {
	funcName := ""
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
			funcName = strings.ToLower(ident.GetText())
			break
		}
	}

	var elements []any
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(prc) {
				var elem any
				elem, err = deserializeSettingValue(prc)
				if err != nil {
					return
				}
				elements = append(elements, elem)
			}
		}
	}

	switch funcName {
	case "array":
		if elements == nil {
			elements = make([]any, 0)
		}
		val = elements
	case "tuple":
		val = NewUnnamedTuple(elements...)
	default:
		err = eh.Errorf("unknown setting function: %s", funcName)
	}
	return
}

func getSettingFunctionName(ctx antlr.ParserRuleContext) string {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
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
					if prevTok.GetTokenType() == grammar.ClickHouseLexerWHITESPACE {
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
		valSQL, err = SerializeSettingValue(settings[k])
		if err != nil {
			err = eh.Errorf("setting %q: %w", k, err)
			return
		}
		parts = append(parts, k+" = "+valSQL)
	}
	sql = strings.Join(parts, ", ")
	return
}

// SerializeSettingValue converts a Go value to its SQL representation.
// Supported types: int64, int, float64, string, nil, bool, []any, *Tuple.
func SerializeSettingValue(val any) (sql string, err error) {
	if val == nil {
		sql = "NULL"
		return
	}

	switch v := val.(type) {
	case int64:
		sql, err = scalars.MarshalScalarLiteral(scalars.Literal{
			Type:   ctabb.I64,
			IntVal: v,
		})
	case int:
		sql, err = scalars.MarshalScalarLiteral(scalars.Literal{
			Type:   ctabb.I64,
			IntVal: int64(v),
		})
	case uint64:
		sql, err = scalars.MarshalScalarLiteral(scalars.Literal{
			Type:    ctabb.U64,
			UintVal: v,
		})
	case float64:
		sql, err = scalars.MarshalScalarLiteral(scalars.Literal{
			Type:     ctabb.F64,
			FloatVal: v,
		})
	case string:
		sql = scalars.EscapeString(v)
	case bool:
		if v {
			sql = "1"
		} else {
			sql = "0"
		}
	case []any:
		sql, err = serializeArray(v)
	case *Tuple:
		sql, err = serializeTuple(v)
	default:
		err = eh.Errorf("unsupported setting value type: %T", val)
	}
	return
}

func serializeArray(arr []any) (sql string, err error) {
	if len(arr) == 0 {
		sql = "[]"
		return
	}
	parts := make([]string, 0, len(arr))
	for _, elem := range arr {
		var elemSQL string
		elemSQL, err = SerializeSettingValue(elem)
		if err != nil {
			return
		}
		parts = append(parts, elemSQL)
	}
	sql = "[" + strings.Join(parts, ", ") + "]"
	return
}

func serializeTuple(t *Tuple) (sql string, err error) {
	if t.Len() == 0 {
		sql = "tuple()"
		return
	}
	parts := make([]string, 0, t.Len())
	for _, val := range t.IterateAll() {
		var elemSQL string
		elemSQL, err = SerializeSettingValue(val)
		if err != nil {
			return
		}
		parts = append(parts, elemSQL)
	}
	sql = "(" + strings.Join(parts, ", ") + ")"
	return
}

// findOutermostSelectStmt finds the first (outermost) selectStmt in the parse tree.
func findOutermostSelectStmt(pr *nanopass.ParseResult) *grammar.SelectStmtContext {
	node := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, ok := ctx.(*grammar.SelectStmtContext)
		return ok
	})
	if node == nil {
		return nil
	}
	return node.(*grammar.SelectStmtContext)
}

// findLastSelectStmtClause returns the last clause present in the selectStmt.
func findLastSelectStmtClause(stmt *grammar.SelectStmtContext) antlr.ParserRuleContext {
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
