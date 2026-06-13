package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/zeebo/xxh3"
)

// MapClickHouseTypeToCanonicalI is a function that maps a ClickHouse type name
// (e.g. "UInt64", "Array(String)") to a canonical type.
// Return nil if the type cannot be represented.
type MapClickHouseTypeToCanonicalI func(chType string) (ct canonicaltypes.PrimitiveAstNodeI, err error)

// ExtractLiteralsConfig configures the literal extraction pass.
type ExtractLiteralsConfig struct {
	minLength          int
	funcPolicy         map[string]bool
	prefix             string
	minINListSize      int
	useSequentialNames bool
	mapTypeToCanonical MapClickHouseTypeToCanonicalI
}

const ParamPrefixExtracted = "param_x"

// NewExtractLiteralsConfig creates a config with sensible defaults.
func NewExtractLiteralsConfig(minLength int) (inst *ExtractLiteralsConfig) {
	inst = &ExtractLiteralsConfig{
		minLength:     minLength,
		funcPolicy:    make(map[string]bool),
		prefix:        ParamPrefixExtracted,
		minINListSize: 3,
	}
	return
}

func (inst *ExtractLiteralsConfig) SetMinLength(minLength int)     { inst.minLength = minLength }
func (inst *ExtractLiteralsConfig) MinLength() int                 { return inst.minLength }
func (inst *ExtractLiteralsConfig) SetPrefix(prefix string)        { inst.prefix = prefix }
func (inst *ExtractLiteralsConfig) Prefix() string                 { return inst.prefix }
func (inst *ExtractLiteralsConfig) SetMinINListSize(size int)      { inst.minINListSize = size }
func (inst *ExtractLiteralsConfig) MinINListSize() int             { return inst.minINListSize }
func (inst *ExtractLiteralsConfig) SetUseSequentialNames(use bool) { inst.useSequentialNames = use }
func (inst *ExtractLiteralsConfig) UseSequentialNames() bool       { return inst.useSequentialNames }
func (inst *ExtractLiteralsConfig) SetMapTypeToCanonical(fn MapClickHouseTypeToCanonicalI) {
	inst.mapTypeToCanonical = fn
}

func (inst *ExtractLiteralsConfig) Whitelist(name string) {
	inst.funcPolicy[normalizeFunctionName(name)] = false
}
func (inst *ExtractLiteralsConfig) Blacklist(name string) {
	inst.funcPolicy[normalizeFunctionName(name)] = true
}
func (inst *ExtractLiteralsConfig) RemovePolicy(name string) {
	delete(inst.funcPolicy, normalizeFunctionName(name))
}
func (inst *ExtractLiteralsConfig) IsBlacklisted(name string) bool {
	blocked, found := inst.funcPolicy[normalizeFunctionName(name)]
	return found && blocked
}
func (inst *ExtractLiteralsConfig) IsWhitelisted(name string) bool {
	blocked, found := inst.funcPolicy[normalizeFunctionName(name)]
	return found && !blocked
}

func normalizeFunctionName(name string) string {
	return nanopass.NormalizeCallName(strings.TrimSpace(name))
}

// --- Internal types ---

type extractedParam struct {
	name     string
	value    string
	typeName string
	castType canonicaltypes.PrimitiveAstNodeI
	meta     ParamMetadata
}

type literalCandidate struct {
	node        *grammar1.ColumnExprLiteralContext
	castNode    *grammar1.ColumnExprCastContext
	contextName string
	argIndex    int
	literalText string
	typeName    string
	castType    canonicaltypes.PrimitiveAstNodeI
	blacklisted bool
	whitelisted bool
}

type compositeKind int

const (
	compositeKindINTuple compositeKind = iota
	compositeKindArray
	compositeKindTuple
)

func (k compositeKind) contextName() string {
	switch k {
	case compositeKindINTuple:
		return "in"
	case compositeKindArray:
		return "array"
	case compositeKindTuple:
		return "tuple"
	default:
		return "composite"
	}
}

type compositeCandidate struct {
	kind            compositeKind
	containerNode   antlr.ParserRuleContext
	castNode        *grammar1.ColumnExprCastContext
	literalTexts    []string
	perElementTypes []string
	elementType     string
	castType        canonicaltypes.PrimitiveAstNodeI
	blacklisted     bool
	whitelisted     bool
}

// --- ExtractLiterals Pass ---

// ExtractLiterals returns a Pass that walks the body, replaces qualifying
// literals with `{name: Type}` slots, and stores the extracted values into
// env.Params. The accompanying Type info is preserved on each Param entry.
func ExtractLiterals(config *ExtractLiteralsConfig) nanopass.Pass {
	return nanopass.Pass{
		Name:  "ExtractLiterals",
		Apply: extractLiteralsApply(config),
		Properties: nanopass.PassProperties{
			Reads:  nanopass.RegionBody,
			Writes: nanopass.RegionBody | nanopass.RegionParams,
		},
	}
}

func extractLiteralsApply(config *ExtractLiteralsConfig) nanopass.ApplyFunc {
	return func(e *env.Environment, sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ExtractLiterals: %w", err)
			return
		}

		compositeCandidates := collectCompositeCandidates(pr, config)
		excludeNodes := make(map[*grammar1.ColumnExprLiteralContext]bool)
		for _, cc := range compositeCandidates {
			for _, litNode := range collectLiteralNodesInComposite(cc.containerNode) {
				excludeNodes[litNode] = true
			}
		}

		candidates := collectLiteralCandidates(pr, config, excludeNodes)
		filtered := filterCandidates(candidates, config)
		filteredComposites := filterCompositeCandidates(compositeCandidates, config)

		if len(filtered) == 0 && len(filteredComposites) == 0 {
			result = sql
			return
		}

		var allParams []extractedParam
		rw := nanopass.NewRewriter(pr)

		if len(filtered) > 0 {
			params, paramByNode := assignParamNames(filtered, config)
			allParams = append(allParams, params...)
			for _, cand := range filtered {
				p := paramByNode[cand.node]
				slotText := fmt.Sprintf("{%s: %s}", p.name, p.typeName)
				if cand.castNode != nil {
					nanopass.ReplaceNode(rw, cand.castNode, slotText)
				} else {
					nanopass.ReplaceNode(rw, cand.node, slotText)
				}
			}
		}

		if len(filteredComposites) > 0 {
			compositeParams := assignCompositeParamNames(filteredComposites, config)
			allParams = append(allParams, compositeParams...)
			for i, cc := range filteredComposites {
				p := &compositeParams[i]
				slotText := fmt.Sprintf("{%s: %s}", p.name, p.typeName)
				if cc.castNode != nil {
					nanopass.ReplaceNode(rw, cc.castNode, slotText)
				} else {
					nanopass.ReplaceNode(rw, cc.containerNode, slotText)
				}
			}
		}

		result = nanopass.GetText(rw)

		if e != nil {
			for _, p := range allParams {
				existing := e.Params[p.name]
				existing.Name = p.name
				existing.Raw = p.value
				if existing.Type == "" {
					existing.Type = p.typeName
				}
				e.Params[p.name] = existing
			}
		}
		return
	}
}

// --- Phase 1a: Composite (IN-tuple, array, tuple) collection ---

// collectCompositeCandidates walks the CST for literal-only composites worth
// collapsing into a single composite parameter. It recognises two shapes:
//
//   - The IN-tuple syntactic form (`x IN (1, 2, 3)`), preserved for callers
//     that have not run CanonicalizeConstructors.
//   - The function-call form `array(...)` and `tuple(...)`, which is the
//     canonical shape produced by `CanonicalizeConstructors(ConstructorFormFunction)`.
//
// Standalone `[...]` and `(...)` literals are intentionally NOT handled here:
// CanonicalizeConstructors rewrites them into the function form first.
func collectCompositeCandidates(pr *nanopass.ParseResult, config *ExtractLiteralsConfig) (candidates []compositeCandidate) {
	if config.minINListSize <= 0 {
		return
	}

	inTuples := collectINTupleSet(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch container := ctx.(type) {
		case *grammar1.ColumnExprTupleContext:
			if !inTuples[container] {
				return true
			}
			if cand, ok := buildCompositeCandidate(pr, container, compositeKindINTuple, config); ok {
				candidates = append(candidates, cand)
			}
		case *grammar1.ColumnExprFunctionContext:
			kind, isComposite := classifyCompositeFunction(container)
			if !isComposite {
				return true
			}
			if cand, ok := buildCompositeFunctionCandidate(pr, container, kind, config); ok {
				candidates = append(candidates, cand)
			}
		}
		return true
	})
	return
}

// classifyCompositeFunction returns the composite kind for `array(...)` and
// `tuple(...)` function calls.
func classifyCompositeFunction(funcCtx *grammar1.ColumnExprFunctionContext) (kind compositeKind, ok bool) {
	ident := funcCtx.Identifier()
	if ident == nil {
		return
	}
	switch normalizeFunctionName(ident.GetText()) {
	case "array":
		return compositeKindArray, true
	case "tuple":
		return compositeKindTuple, true
	}
	return
}

func buildCompositeFunctionCandidate(pr *nanopass.ParseResult, funcCtx *grammar1.ColumnExprFunctionContext, kind compositeKind, config *ExtractLiteralsConfig) (cand compositeCandidate, ok bool) {
	contextName := kind.contextName()
	if config.IsBlacklisted(contextName) {
		return
	}

	argList := funcCtx.ColumnArgList()
	if argList == nil {
		return
	}
	literalTexts, elementType, perElementTypes, allLiterals := extractLiteralsFromArgList(pr, argList.(*grammar1.ColumnArgListContext))
	if !allLiterals || len(literalTexts) < config.minINListSize {
		return
	}

	var castNode *grammar1.ColumnExprCastContext
	var castType canonicaltypes.PrimitiveAstNodeI
	if config.mapTypeToCanonical != nil {
		if castCtx, isCast := funcCtx.GetParent().(*grammar1.ColumnExprCastContext); isCast {
			castTypeText := extractCastTypeText(castCtx)
			if castTypeText != "" {
				ct, mapErr := config.mapTypeToCanonical(castTypeText)
				if mapErr == nil && ct != nil {
					castNode = castCtx
					castType = ct
				}
			}
		}
	}

	cand = compositeCandidate{
		kind:            kind,
		containerNode:   funcCtx,
		castNode:        castNode,
		literalTexts:    literalTexts,
		perElementTypes: perElementTypes,
		elementType:     elementType,
		castType:        castType,
		whitelisted:     config.IsWhitelisted(contextName),
	}
	ok = true
	return
}

func extractLiteralsFromArgList(pr *nanopass.ParseResult, argList *grammar1.ColumnArgListContext) (texts []string, elementType string, perElementTypes []string, allLiterals bool) {
	texts = make([]string, 0, argList.GetChildCount())
	perElementTypes = make([]string, 0, argList.GetChildCount())
	allLiterals = true
	for i := 0; i < argList.GetChildCount(); i++ {
		argExpr, ok := argList.GetChild(i).(*grammar1.ColumnArgExprContext)
		if !ok {
			continue
		}
		if argExpr.GetChildCount() == 0 {
			allLiterals = false
			return
		}
		litExpr, ok := argExpr.GetChild(0).(*grammar1.ColumnExprLiteralContext)
		if !ok {
			allLiterals = false
			return
		}
		litCtx := findLiteralChild(litExpr)
		if litCtx == nil || litCtx.NULL_SQL() != nil {
			allLiterals = false
			return
		}
		thisType := inferClickHouseType(litCtx)
		if elementType == "" {
			elementType = thisType
		} else if elementType != thisType {
			elementType = "String"
		}
		perElementTypes = append(perElementTypes, thisType)
		texts = append(texts, nanopass.NodeText(pr, litExpr))
	}
	if len(texts) == 0 {
		allLiterals = false
	}
	return
}

func collectINTupleSet(pr *nanopass.ParseResult) (set map[*grammar1.ColumnExprTupleContext]bool) {
	set = make(map[*grammar1.ColumnExprTupleContext]bool)
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		prec3, ok := ctx.(*grammar1.ColumnExprPrecedence3Context)
		if !ok || !isINExpression(prec3) {
			return true
		}
		if t := findTupleInPrecedence3(prec3); t != nil {
			set[t] = true
		}
		return true
	})
	return
}

// buildCompositeCandidate handles the IN-tuple syntactic form. The container
// here is always a ColumnExprTupleContext that appears as the right operand of
// an IN expression.
func buildCompositeCandidate(pr *nanopass.ParseResult, container antlr.ParserRuleContext, kind compositeKind, config *ExtractLiteralsConfig) (cand compositeCandidate, ok bool) {
	contextName := kind.contextName()
	if config.IsBlacklisted(contextName) {
		return
	}

	literalTexts, elementType, perElementTypes, allLiterals := extractListLiteralsFromContainer(pr, container)
	if !allLiterals || len(literalTexts) < config.minINListSize {
		return
	}

	var castNode *grammar1.ColumnExprCastContext
	var castType canonicaltypes.PrimitiveAstNodeI
	if config.mapTypeToCanonical != nil {
		castWrapper := findINTupleCastWrapper(container)
		if castWrapper != nil {
			castTypeText := extractCastTypeText(castWrapper)
			if castTypeText != "" {
				ct, mapErr := config.mapTypeToCanonical(castTypeText)
				if mapErr == nil && ct != nil {
					castNode = castWrapper
					castType = ct
				}
			}
		}
	}

	cand = compositeCandidate{
		kind:            kind,
		containerNode:   container,
		castNode:        castNode,
		literalTexts:    literalTexts,
		perElementTypes: perElementTypes,
		elementType:     elementType,
		castType:        castType,
		whitelisted:     config.IsWhitelisted(contextName),
	}
	ok = true
	return
}

// findINTupleCastWrapper returns the cast wrapping an IN expression like
// `(x IN (1,2,3))::Array(UInt64)`. The cast wraps the enclosing precedence3
// expression, not the tuple itself.
func findINTupleCastWrapper(container antlr.ParserRuleContext) *grammar1.ColumnExprCastContext {
	parent := container.GetParent()
	if parent == nil {
		return nil
	}
	prec3, ok := parent.(*grammar1.ColumnExprPrecedence3Context)
	if !ok {
		return nil
	}
	castCtx, isCast := prec3.GetParent().(*grammar1.ColumnExprCastContext)
	if !isCast {
		return nil
	}
	return castCtx
}

func isINExpression(prec3 *grammar1.ColumnExprPrecedence3Context) bool {
	for i := 0; i < prec3.GetChildCount(); i++ {
		if term, ok := prec3.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerIN {
				return true
			}
		}
	}
	return false
}

// findTupleInPrecedence3 returns the tuple serving as the IN candidate list
// — the RIGHT operand, i.e. the tuple after the IN token. A constant row
// tuple on the LEFT ((1,2) IN (…)) is a value, not a list.
func findTupleInPrecedence3(prec3 *grammar1.ColumnExprPrecedence3Context) *grammar1.ColumnExprTupleContext {
	seenIN := false
	for i := 0; i < prec3.GetChildCount(); i++ {
		child := prec3.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerIN {
				seenIN = true
			}
			continue
		}
		if !seenIN {
			continue
		}
		if tuple, ok := child.(*grammar1.ColumnExprTupleContext); ok {
			return tuple
		}
	}
	return nil
}

func extractListLiteralsFromContainer(pr *nanopass.ParseResult, container antlr.ParserRuleContext) (texts []string, elementType string, perElementTypes []string, allLiterals bool) {
	exprList := findExprListChild(container)
	if exprList == nil {
		return nil, "", nil, false
	}
	texts = make([]string, 0, exprList.GetChildCount())
	perElementTypes = make([]string, 0, exprList.GetChildCount())
	allLiterals = true
	for i := 0; i < exprList.GetChildCount(); i++ {
		colsExpr, ok := exprList.GetChild(i).(*grammar1.ColumnsExprColumnContext)
		if !ok {
			continue
		}
		if colsExpr.GetChildCount() == 0 {
			allLiterals = false
			return
		}
		litExpr, ok := colsExpr.GetChild(0).(*grammar1.ColumnExprLiteralContext)
		if !ok {
			allLiterals = false
			return
		}
		litCtx := findLiteralChild(litExpr)
		if litCtx == nil || litCtx.NULL_SQL() != nil {
			allLiterals = false
			return
		}
		thisType := inferClickHouseType(litCtx)
		if elementType == "" {
			elementType = thisType
		} else if elementType != thisType {
			elementType = "String"
		}
		perElementTypes = append(perElementTypes, thisType)
		texts = append(texts, nanopass.NodeText(pr, litExpr))
	}
	if len(texts) == 0 {
		allLiterals = false
	}
	return
}

func findExprListChild(container antlr.Tree) *grammar1.ColumnExprListContext {
	for i := 0; i < container.GetChildCount(); i++ {
		if el, ok := container.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			return el
		}
	}
	return nil
}

func collectLiteralNodesInComposite(container antlr.Tree) (nodes []*grammar1.ColumnExprLiteralContext) {
	nanopass.WalkCST(container, func(ctx antlr.ParserRuleContext) bool {
		if litExpr, ok := ctx.(*grammar1.ColumnExprLiteralContext); ok {
			nodes = append(nodes, litExpr)
		}
		return true
	})
	return
}

// --- Phase 1b: Individual literal collection ---

func collectLiteralCandidates(pr *nanopass.ParseResult, config *ExtractLiteralsConfig, excludeNodes map[*grammar1.ColumnExprLiteralContext]bool) (candidates []literalCandidate) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		litExpr, ok := ctx.(*grammar1.ColumnExprLiteralContext)
		if !ok {
			return true
		}
		if excludeNodes[litExpr] {
			return true
		}
		litCtx := findLiteralChild(litExpr)
		if litCtx == nil || litCtx.NULL_SQL() != nil {
			return true
		}

		literalText := nanopass.NodeText(pr, litExpr)
		typeName := inferClickHouseType(litCtx)

		var castNode *grammar1.ColumnExprCastContext
		var castType canonicaltypes.PrimitiveAstNodeI
		if castCtx, isCast := litExpr.GetParent().(*grammar1.ColumnExprCastContext); isCast && config.mapTypeToCanonical != nil {
			castTypeText := extractCastTypeText(castCtx)
			if castTypeText != "" {
				ct, mapErr := config.mapTypeToCanonical(castTypeText)
				if mapErr == nil && ct != nil {
					castNode = castCtx
					castType = ct
					typeName = castTypeText
				}
			}
		}

		var contextName string
		var argIndex int
		if castNode != nil {
			contextName, argIndex = resolveContextFromParent(castNode)
		} else {
			contextName, argIndex = resolveContext(litExpr)
		}

		normalizedCtx := normalizeFunctionName(contextName)
		candidates = append(candidates, literalCandidate{
			node: litExpr, castNode: castNode, contextName: contextName, argIndex: argIndex,
			literalText: literalText, typeName: typeName, castType: castType,
			blacklisted: config.IsBlacklisted(normalizedCtx), whitelisted: config.IsWhitelisted(normalizedCtx),
		})
		return true
	})
	return
}

// --- Cast type extraction ---

func extractCastTypeText(castCtx *grammar1.ColumnExprCastContext) string {
	for i := 0; i < castCtx.GetChildCount(); i++ {
		child := castCtx.GetChild(i)
		switch c := child.(type) {
		case *grammar1.ColumnTypeExprSimpleContext:
			return c.GetText()
		case *grammar1.ColumnTypeExprComplexContext:
			return c.GetText()
		}
	}
	return ""
}

// --- Helpers ---

func findLiteralChild(litExpr *grammar1.ColumnExprLiteralContext) *grammar1.LiteralContext {
	for i := 0; i < litExpr.GetChildCount(); i++ {
		if lit, ok := litExpr.GetChild(i).(*grammar1.LiteralContext); ok {
			return lit
		}
	}
	return nil
}

func inferClickHouseType(lit *grammar1.LiteralContext) string {
	if lit.STRING_LITERAL() != nil {
		return "String"
	}
	if lit.NULL_SQL() != nil {
		return "String"
	}
	if lit.NumberLiteral() != nil {
		text := lit.NumberLiteral().GetText()
		scalar, parseErr := marshalling.UnmarshalScalarLiteral(text)
		if parseErr != nil {
			return "Int64"
		}
		if scalar.IsNull() {
			return "String"
		}
		if scalar.ScalarType != nil {
			switch scalar.ScalarType.String() {
			case "u64":
				return "UInt64"
			case "i64":
				return "Int64"
			case "f64":
				return "Float64"
			case "b":
				return "Bool"
			}
		}
		return "Int64"
	}
	return "String"
}

// --- Context resolution ---

func resolveContext(litExpr *grammar1.ColumnExprLiteralContext) (contextName string, argIndex int) {
	parent := litExpr.GetParent()
	if parent == nil {
		return "expr", 0
	}
	return resolveContextFromNodeWithChild(parent, litExpr)
}

func resolveContextFromParent(node antlr.ParserRuleContext) (contextName string, argIndex int) {
	parent := node.GetParent()
	if parent == nil {
		return "expr", 0
	}
	return resolveContextFromNodeWithChild(parent, node)
}

func resolveContextFromNodeWithChild(parent antlr.Tree, child antlr.ParserRuleContext) (contextName string, argIndex int) {
	switch p := parent.(type) {
	case *grammar1.ColumnArgExprContext:
		return resolveFuncArgContext(p)
	case *grammar1.ColumnExprPrecedence1Context:
		return resolveOperatorContextWithChild(p, child)
	case *grammar1.ColumnExprPrecedence2Context:
		return resolveOperatorContextWithChild(p, child)
	case *grammar1.ColumnExprPrecedence3Context:
		return resolveOperatorContextWithChild(p, child)
	case *grammar1.ColumnsExprColumnContext:
		return resolveColumnsExprContextGeneric(p)
	case *grammar1.ColumnExprBetweenContext:
		return resolveBetweenContextWithChild(p, child)
	default:
		return "expr", 0
	}
}

func resolveFuncArgContext(argExpr *grammar1.ColumnArgExprContext) (contextName string, argIndex int) {
	argList := argExpr.GetParent()
	if argList == nil {
		return "func", 0
	}
	argIndex = 0
	for i := 0; i < argList.GetChildCount(); i++ {
		child := argList.GetChild(i)
		if child == argExpr {
			break
		}
		if _, isArg := child.(*grammar1.ColumnArgExprContext); isArg {
			argIndex++
		}
	}
	funcParent := argList.(antlr.RuleNode).GetParent()
	if funcParent == nil {
		return "func", argIndex
	}
	switch fp := funcParent.(type) {
	case *grammar1.ColumnExprFunctionContext:
		if fp.Identifier() != nil {
			contextName = normalizeFunctionName(fp.Identifier().GetText())
		} else {
			contextName = "func"
		}
	case *grammar1.ColumnExprWinFunctionContext:
		if fp.Identifier() != nil {
			contextName = normalizeFunctionName(fp.Identifier().GetText())
		} else {
			contextName = "winfunc"
		}
	default:
		contextName = "func"
	}
	return
}

func resolveOperatorContextWithChild(parent antlr.ParserRuleContext, targetChild antlr.ParserRuleContext) (contextName string, argIndex int) {
	opName := "op"
	litIdx := -1
	exprIdx := 0
	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if child == targetChild {
			litIdx = exprIdx
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tok := term.GetSymbol()
			switch tok.GetTokenType() {
			case grammar1.ClickHouseLexerEQ_SINGLE, grammar1.ClickHouseLexerEQ_DOUBLE:
				opName = "eq"
			case grammar1.ClickHouseLexerNOT_EQ:
				opName = "neq"
			case grammar1.ClickHouseLexerLT:
				opName = "lt"
			case grammar1.ClickHouseLexerGT:
				opName = "gt"
			case grammar1.ClickHouseLexerLE:
				opName = "le"
			case grammar1.ClickHouseLexerGE:
				opName = "ge"
			case grammar1.ClickHouseLexerPLUS:
				opName = "plus"
			case grammar1.ClickHouseLexerDASH:
				opName = "minus"
			case grammar1.ClickHouseLexerASTERISK:
				opName = "mul"
			case grammar1.ClickHouseLexerSLASH:
				opName = "div"
			case grammar1.ClickHouseLexerPERCENT:
				opName = "mod"
			case grammar1.ClickHouseLexerCONCAT:
				opName = "concat"
			case grammar1.ClickHouseLexerLIKE:
				opName = "like"
			case grammar1.ClickHouseLexerILIKE:
				opName = "ilike"
			case grammar1.ClickHouseLexerIN:
				opName = "in"
			}
		}
		if _, isExpr := child.(antlr.ParserRuleContext); isExpr {
			exprIdx++
		}
	}
	if litIdx < 0 {
		litIdx = 0
	}
	return opName, litIdx
}

func resolveColumnsExprContextGeneric(parent *grammar1.ColumnsExprColumnContext) (contextName string, argIndex int) {
	gp := parent.GetParent()
	if gp == nil {
		return "select", 0
	}
	if exprList, ok := gp.(*grammar1.ColumnExprListContext); ok {
		ggp := exprList.GetParent()
		if _, isParen := ggp.(*grammar1.ColumnExprPrecedence3Context); isParen {
			argIndex = 0
			for i := 0; i < exprList.GetChildCount(); i++ {
				child := exprList.GetChild(i)
				if child == parent {
					break
				}
				if _, isCol := child.(*grammar1.ColumnsExprColumnContext); isCol {
					argIndex++
				}
			}
			return "in", argIndex
		}
	}
	if exprList, ok := gp.(*grammar1.ColumnExprListContext); ok {
		argIndex = 0
		for i := 0; i < exprList.GetChildCount(); i++ {
			child := exprList.GetChild(i)
			if child == parent {
				break
			}
			if _, isCol := child.(*grammar1.ColumnsExprColumnContext); isCol {
				argIndex++
			}
		}
	}
	return "select", argIndex
}

func resolveBetweenContextWithChild(parent *grammar1.ColumnExprBetweenContext, targetChild antlr.ParserRuleContext) (contextName string, argIndex int) {
	contextName = "between"
	argIndex = 0
	exprIdx := 0
	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if child == targetChild {
			argIndex = exprIdx
			break
		}
		if _, isExpr := child.(antlr.ParserRuleContext); isExpr {
			exprIdx++
		}
	}
	return
}

// --- Phase 2: Filter ---

func filterCandidates(candidates []literalCandidate, config *ExtractLiteralsConfig) (filtered []literalCandidate) {
	filtered = make([]literalCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.blacklisted {
			continue
		}
		if c.whitelisted || len(c.literalText) >= config.minLength {
			filtered = append(filtered, c)
		}
	}
	return
}

func filterCompositeCandidates(candidates []compositeCandidate, config *ExtractLiteralsConfig) (filtered []compositeCandidate) {
	filtered = make([]compositeCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.blacklisted {
			continue
		}
		filtered = append(filtered, c)
	}
	return
}

// --- Phase 3: Assign names ---

func literalHash(literalText string) uint64 {
	h := xxh3.HashString128(literalText)
	return h.Lo
}

func assignParamNames(candidates []literalCandidate, config *ExtractLiteralsConfig) (params []extractedParam, paramByNode map[*grammar1.ColumnExprLiteralContext]*extractedParam) {
	paramByNode = make(map[*grammar1.ColumnExprLiteralContext]*extractedParam, len(candidates))
	type dedupKey struct {
		contextName string
		argIndex    int
		literalText string
		castCanon   string
	}
	dedupMap := make(map[dedupKey]*extractedParam)
	usedNames := make(map[string]bool)
	params = make([]extractedParam, 0, len(candidates))
	seqCounter := uint32(0)

	for i := range candidates {
		c := &candidates[i]
		castCanon := ""
		if c.castType != nil {
			castCanon = c.castType.String()
		}
		key := dedupKey{contextName: c.contextName, argIndex: c.argIndex, literalText: c.literalText, castCanon: castCanon}
		if existing, found := dedupMap[key]; found {
			paramByNode[c.node] = existing
			continue
		}
		meta := ParamMetadata{ArgIndex: uint32(c.argIndex), CastTypeCanonical: castCanon}
		if config.useSequentialNames {
			meta.IsSequential = true
			meta.SequentialIndex = seqCounter
			seqCounter++
		} else {
			meta.ContentHash = literalHash(c.literalText)
		}
		name, buildErr := BuildParamName(config.prefix, c.contextName, &meta)
		if buildErr != nil {
			continue
		}
		if usedNames[name] {
			meta.HashCollisionCounter = 2
			for {
				name, buildErr = BuildParamName(config.prefix, c.contextName, &meta)
				if buildErr != nil {
					break
				}
				if !usedNames[name] {
					break
				}
				meta.HashCollisionCounter++
			}
		}
		p := extractedParam{name: name, value: c.literalText, typeName: c.typeName, castType: c.castType, meta: meta}
		params = append(params, p)
		paramPtr := &params[len(params)-1]
		dedupMap[key] = paramPtr
		usedNames[name] = true
		paramByNode[c.node] = paramPtr
	}
	return
}

func assignCompositeParamNames(candidates []compositeCandidate, config *ExtractLiteralsConfig) (params []extractedParam) {
	usedNames := make(map[string]bool)
	params = make([]extractedParam, 0, len(candidates))
	seqCounter := uint32(0)
	for _, c := range candidates {
		value := formatCompositeValue(&c)
		typeName := defaultCompositeTypeName(&c)
		if c.castNode != nil {
			if castTypeText := extractCastTypeText(c.castNode); castTypeText != "" {
				typeName = castTypeText
			}
		}
		castCanon := ""
		if c.castType != nil {
			castCanon = c.castType.String()
		}
		contextName := c.kind.contextName()
		meta := ParamMetadata{ArgIndex: 0, CastTypeCanonical: castCanon}
		if config.useSequentialNames {
			meta.IsSequential = true
			meta.SequentialIndex = seqCounter
			seqCounter++
		} else {
			meta.ContentHash = literalHash(value)
		}
		name, buildErr := BuildParamName(config.prefix, contextName, &meta)
		if buildErr != nil {
			continue
		}
		if usedNames[name] {
			meta.HashCollisionCounter = 2
			for {
				name, buildErr = BuildParamName(config.prefix, contextName, &meta)
				if buildErr != nil {
					break
				}
				if !usedNames[name] {
					break
				}
				meta.HashCollisionCounter++
			}
		}
		p := extractedParam{name: name, value: value, typeName: typeName, castType: c.castType, meta: meta}
		params = append(params, p)
		usedNames[name] = true
	}
	return
}

// formatCompositeValue serializes the captured literal texts back into SQL form.
// IN-tuples and arrays use brackets so they bind to ClickHouse Array(...) parameters;
// standalone tuples use parentheses so they bind to Tuple(...) parameters.
func formatCompositeValue(c *compositeCandidate) string {
	joined := strings.Join(c.literalTexts, ", ")
	if c.kind == compositeKindTuple {
		return "(" + joined + ")"
	}
	return "[" + joined + "]"
}

// defaultCompositeTypeName returns the ClickHouse parameter type for the candidate
// when no explicit cast wrapper is present.
func defaultCompositeTypeName(c *compositeCandidate) string {
	if c.kind == compositeKindTuple {
		if len(c.perElementTypes) == 0 {
			return fmt.Sprintf("Tuple(%s)", c.elementType)
		}
		return "Tuple(" + strings.Join(c.perElementTypes, ", ") + ")"
	}
	return fmt.Sprintf("Array(%s)", c.elementType)
}

func sanitizeName(name string) string {
	var sb strings.Builder
	sb.Grow(len(name))
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		} else if c >= 'A' && c <= 'Z' {
			sb.WriteRune(c - 'A' + 'a')
		}
	}
	s := sb.String()
	if s == "" {
		return "v"
	}
	return s
}

// --- Analysis ---

func AnalyzeExtractions(sql string, config *ExtractLiteralsConfig) (extractions []ExtractionInfo, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("AnalyzeExtractions: %w", err)
		return
	}
	compositeCandidates := collectCompositeCandidates(pr, config)
	excludeNodes := make(map[*grammar1.ColumnExprLiteralContext]bool)
	for _, cc := range compositeCandidates {
		for _, litNode := range collectLiteralNodesInComposite(cc.containerNode) {
			excludeNodes[litNode] = true
		}
	}
	candidates := collectLiteralCandidates(pr, config, excludeNodes)
	filtered := filterCandidates(candidates, config)
	filteredComposites := filterCompositeCandidates(compositeCandidates, config)
	extractions = make([]ExtractionInfo, 0, len(filtered)+len(filteredComposites))
	if len(filtered) > 0 {
		_, paramByNode := assignParamNames(filtered, config)
		seen := make(map[string]bool)
		for _, c := range filtered {
			p := paramByNode[c.node]
			if seen[p.name] {
				continue
			}
			seen[p.name] = true
			extractions = append(extractions, ExtractionInfo{
				ParamName: p.name, Value: p.value, TypeName: p.typeName,
				ContextName: c.contextName, ArgIndex: c.argIndex,
				Line: c.node.GetStart().GetLine(), Column: c.node.GetStart().GetColumn(),
				CastType: c.castType,
			})
		}
	}
	if len(filteredComposites) > 0 {
		compositeParams := assignCompositeParamNames(filteredComposites, config)
		for i, cc := range filteredComposites {
			p := &compositeParams[i]
			extractions = append(extractions, ExtractionInfo{
				ParamName: p.name, Value: p.value, TypeName: p.typeName,
				ContextName: cc.kind.contextName(), ArgIndex: 0,
				Line: cc.containerNode.GetStart().GetLine(), Column: cc.containerNode.GetStart().GetColumn(),
				CastType: cc.castType,
			})
		}
	}
	return
}

type ExtractionInfo struct {
	ParamName   string
	Value       string
	TypeName    string
	ContextName string
	ArgIndex    int
	Line        int
	Column      int
	CastType    canonicaltypes.PrimitiveAstNodeI
}

func (inst *ExtractionInfo) String() string {
	castStr := ""
	if inst.CastType != nil {
		castStr = fmt.Sprintf(" cast=%s", inst.CastType.String())
	}
	return fmt.Sprintf("SET %s = %s; -- %s arg %d at line %d:%d (type %s%s)",
		inst.ParamName, inst.Value, inst.ContextName, inst.ArgIndex, inst.Line, inst.Column, inst.TypeName, castStr)
}

// --- Convenience ---

func ParseExtractedQuery(extracted string, prefix string) (setsExtracted []string, sets []string, query string) {
	lines := strings.Split(extracted, "\n")
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "SET ") {
			parts := strings.SplitN(line, " = ", 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimPrefix(parts[0], "SET ")
			name = strings.TrimSpace(name)
			_, _, parseErr := ParseParamName(name, prefix)
			if parseErr == nil {
				setsExtracted = append(setsExtracted, strings.TrimSuffix(line, ";"))
			} else {
				sets = append(sets, strings.TrimSuffix(line, ";"))
			}
		} else {
			query = strings.Join(lines[i:], "\n")
			break
		}
	}
	return
}

func CountExtractableParams(sql string, config *ExtractLiteralsConfig) (count int, err error) {
	extractions, err := AnalyzeExtractions(sql, config)
	if err != nil {
		return
	}
	count = len(extractions)
	return
}

func InjectParams(sets []string, prefix string, query string) (result string, err error) {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}
	paramMap := make(map[string]string, len(sets))
	for _, set := range sets {
		parts := strings.SplitN(set, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "SET ")
		name = strings.TrimSpace(name)
		value := strings.TrimSpace(parts[1])
		_, _, parseErr := ParseParamName(name, prefix)
		if parseErr == nil {
			paramMap[name] = value
		}
	}
	result = query
	for name, value := range paramMap {
		result = replaceParamSlots(result, name, value)
	}
	return
}

// replaceParamSlots replaces every `{name: Type}` slot in text with
// replacement. The scan is quote-aware — slot-shaped text inside string
// literals or quoted identifiers is left alone.
func replaceParamSlots(text string, name string, replacement string) string {
	slotPrefix := "{" + name + ":"
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		c := text[i]
		if c == '\'' || c == '"' || c == '`' {
			end := skipQuotedToken(text, i)
			b.WriteString(text[i:end])
			i = end
			continue
		}
		if c == '{' && strings.HasPrefix(text[i:], slotPrefix) {
			if close := strings.IndexByte(text[i:], '}'); close >= 0 {
				b.WriteString(replacement)
				i += close + 1
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// skipQuotedToken returns the index just past the quoted token starting at
// text[start]. Handles backslash escapes and doubled closing quotes; an
// unterminated quote consumes the rest of the string.
func skipQuotedToken(text string, start int) int {
	q := text[start]
	i := start + 1
	for i < len(text) {
		switch {
		case text[i] == '\\' && i+1 < len(text):
			i += 2
		case text[i] == q:
			if i+1 < len(text) && text[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		default:
			i++
		}
	}
	return i
}

func InjectParamsWithCasts(sets []string, query string, prefix string, mapCanonicalToClickHouse func(canonical string) (string, error)) (result string, err error) {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}
	type paramEntry struct {
		value         string
		castCanonical string
	}
	paramMap := make(map[string]paramEntry, len(sets))
	for _, set := range sets {
		line := strings.TrimPrefix(set, "SET ")
		line = strings.TrimSpace(line)
		eqIdx := strings.Index(line, " = ")
		if eqIdx < 0 {
			continue
		}
		name := line[:eqIdx]
		value := line[eqIdx+3:]
		castCanonical := ""
		_, meta, parseErr := ParseParamName(name, prefix)
		if parseErr == nil {
			castCanonical = meta.CastTypeCanonical
		}
		paramMap[name] = paramEntry{value: value, castCanonical: castCanonical}
	}
	result = query
	for name, entry := range paramMap {
		replacement := entry.value
		if entry.castCanonical != "" && mapCanonicalToClickHouse != nil {
			chType, mapErr := mapCanonicalToClickHouse(entry.castCanonical)
			if mapErr == nil && chType != "" {
				replacement = entry.value + "::" + chType
			}
		}
		result = replaceParamSlots(result, name, replacement)
	}
	return
}
