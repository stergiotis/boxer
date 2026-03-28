//go:build llm_generated_opus46

package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/scalars"
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

// NewExtractLiteralsConfig creates a config with sensible defaults.
func NewExtractLiteralsConfig(minLength int) (inst *ExtractLiteralsConfig) {
	inst = &ExtractLiteralsConfig{
		minLength:     minLength,
		funcPolicy:    make(map[string]bool),
		prefix:        "param",
		minINListSize: 3,
	}
	return
}

// SetMinLength sets the minimum literal length for extraction.
func (inst *ExtractLiteralsConfig) SetMinLength(minLength int) {
	inst.minLength = minLength
}

// MinLength returns the minimum literal length for extraction.
func (inst *ExtractLiteralsConfig) MinLength() int {
	return inst.minLength
}

// SetPrefix sets the parameter name prefix.
func (inst *ExtractLiteralsConfig) SetPrefix(prefix string) {
	inst.prefix = prefix
}

// Prefix returns the parameter name prefix.
func (inst *ExtractLiteralsConfig) Prefix() string {
	return inst.prefix
}

// SetMinINListSize sets the minimum number of literal elements in an IN list
// for the list to be collapsed into a single Array parameter.
// Set to 0 to disable IN-list collapsing.
func (inst *ExtractLiteralsConfig) SetMinINListSize(size int) {
	inst.minINListSize = size
}

// MinINListSize returns the minimum IN-list size for collapsing.
func (inst *ExtractLiteralsConfig) MinINListSize() int {
	return inst.minINListSize
}

// SetUseSequentialNames enables sequential naming (param_eq_<cbor with s=0>, s=1, ...)
// instead of content-hash based naming. Useful for deterministic tests.
func (inst *ExtractLiteralsConfig) SetUseSequentialNames(use bool) {
	inst.useSequentialNames = use
}

// UseSequentialNames returns whether sequential naming is enabled.
func (inst *ExtractLiteralsConfig) UseSequentialNames() bool {
	return inst.useSequentialNames
}

// SetMapTypeToCanonical sets the function used to map ClickHouse type names to canonical types.
// Required for cast-aware type inference. If nil, casts are ignored.
func (inst *ExtractLiteralsConfig) SetMapTypeToCanonical(fn MapClickHouseTypeToCanonicalI) {
	inst.mapTypeToCanonical = fn
}

// Whitelist marks a function/operator so its literal arguments are ALWAYS extracted.
func (inst *ExtractLiteralsConfig) Whitelist(name string) {
	inst.funcPolicy[normalizeFunctionName(name)] = false
}

// Blacklist marks a function/operator so its literal arguments are NEVER extracted.
func (inst *ExtractLiteralsConfig) Blacklist(name string) {
	inst.funcPolicy[normalizeFunctionName(name)] = true
}

// RemovePolicy removes any whitelist/blacklist entry for the given function/operator.
func (inst *ExtractLiteralsConfig) RemovePolicy(name string) {
	delete(inst.funcPolicy, normalizeFunctionName(name))
}

// IsBlacklisted returns true if the function/operator is blacklisted.
func (inst *ExtractLiteralsConfig) IsBlacklisted(name string) bool {
	blocked, found := inst.funcPolicy[normalizeFunctionName(name)]
	return found && blocked
}

// IsWhitelisted returns true if the function/operator is whitelisted.
func (inst *ExtractLiteralsConfig) IsWhitelisted(name string) bool {
	blocked, found := inst.funcPolicy[normalizeFunctionName(name)]
	return found && !blocked
}

func normalizeFunctionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
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
	node        *grammar.ColumnExprLiteralContext
	castNode    *grammar.ColumnExprCastContext
	contextName string
	argIndex    int
	literalText string
	typeName    string
	castType    canonicaltypes.PrimitiveAstNodeI
	blacklisted bool
	whitelisted bool
}

type inListCandidate struct {
	tupleNode    *grammar.ColumnExprTupleContext
	castNode     *grammar.ColumnExprCastContext
	literalTexts []string
	elementType  string
	castType     canonicaltypes.PrimitiveAstNodeI
	blacklisted  bool
	whitelisted  bool
}

// --- ExtractLiterals Pass ---

// ExtractLiterals returns a Pass that extracts long string/number literals
// into SET param_xxx = ... statements, replacing them with {param_xxx: Type}
// parameter slot syntax.
//
// Parameter names encode structured metadata (arg index, content hash or sequential
// index, and optional canonical cast type) as hex-encoded CBOR in the name suffix.
// Format: <prefix>_<context>_<hex(cbor(ParamMetadata))>
//
// When a literal has an explicit cast (e.g. 1::UInt64), the cast type is mapped
// to a canonical type and stored in the parameter metadata. The entire cast expression
// is replaced, and the ClickHouse type from the cast is used in the parameter slot.
func ExtractLiterals(config *ExtractLiteralsConfig) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ExtractLiterals: %w", err)
			return
		}

		// Phase 1a: Collect IN-list candidates
		inListCandidates := collectINListCandidates(pr, config)

		// Build exclusion set for literals inside collapsed IN lists
		inListNodes := make(map[*grammar.ColumnExprLiteralContext]bool)
		for _, ilc := range inListCandidates {
			for _, litNode := range collectLiteralNodesInTuple(ilc.tupleNode) {
				inListNodes[litNode] = true
			}
		}

		// Phase 1b: Collect individual literal candidates
		candidates := collectLiteralCandidates(pr, config, inListNodes)

		// Phase 2: Filter
		filtered := filterCandidates(candidates, config)
		filteredINLists := filterINListCandidates(inListCandidates, config)

		if len(filtered) == 0 && len(filteredINLists) == 0 {
			result = sql
			return
		}

		// Phase 3: Assign parameter names
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

		if len(filteredINLists) > 0 {
			inParams := assignINListParamNames(filteredINLists, config)
			allParams = append(allParams, inParams...)

			for i, ilc := range filteredINLists {
				p := &inParams[i]
				slotText := fmt.Sprintf("{%s: %s}", p.name, p.typeName)
				if ilc.castNode != nil {
					nanopass.ReplaceNode(rw, ilc.castNode, slotText)
				} else {
					nanopass.ReplaceNode(rw, ilc.tupleNode, slotText)
				}
			}
		}

		rewritten := nanopass.GetText(rw)

		// Phase 5: Prepend SET statements
		var sb strings.Builder
		sb.Grow(len(rewritten) + len(allParams)*50)
		for _, p := range allParams {
			sb.WriteString("SET ")
			sb.WriteString(p.name)
			sb.WriteString(" = ")
			sb.WriteString(p.value)
			sb.WriteString(";\n")
		}
		sb.WriteString(rewritten)

		result = sb.String()
		return
	}
}

// --- Phase 1a: IN-list collection ---

func collectINListCandidates(pr *nanopass.ParseResult, config *ExtractLiteralsConfig) (candidates []inListCandidate) {
	if config.minINListSize <= 0 {
		return
	}

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		prec3, ok := ctx.(*grammar.ColumnExprPrecedence3Context)
		if !ok {
			return true
		}

		if !isINExpression(prec3) {
			return true
		}

		if config.IsBlacklisted("in") {
			return true
		}

		tupleNode := findTupleInPrecedence3(prec3)
		if tupleNode == nil {
			return true
		}

		// Check if the tuple (or prec3) is inside a cast
		var castNode *grammar.ColumnExprCastContext
		var castType canonicaltypes.PrimitiveAstNodeI
		if castCtx, ok := prec3.GetParent().(*grammar.ColumnExprCastContext); ok && config.mapTypeToCanonical != nil {
			castTypeText := extractCastTypeText(castCtx)
			if castTypeText != "" {
				ct, mapErr := config.mapTypeToCanonical(castTypeText)
				if mapErr == nil && ct != nil {
					castNode = castCtx
					castType = ct
				}
			}
		}

		literalTexts, elementType, allLiterals := extractTupleLiterals(pr, tupleNode)
		if !allLiterals {
			return true
		}

		if len(literalTexts) < config.minINListSize {
			return true
		}

		candidates = append(candidates, inListCandidate{
			tupleNode:    tupleNode,
			castNode:     castNode,
			literalTexts: literalTexts,
			elementType:  elementType,
			castType:     castType,
			whitelisted:  config.IsWhitelisted("in"),
		})

		return true
	})
	return
}

func isINExpression(prec3 *grammar.ColumnExprPrecedence3Context) bool {
	for i := 0; i < prec3.GetChildCount(); i++ {
		if term, ok := prec3.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerIN {
				return true
			}
		}
	}
	return false
}

func findTupleInPrecedence3(prec3 *grammar.ColumnExprPrecedence3Context) *grammar.ColumnExprTupleContext {
	for i := 0; i < prec3.GetChildCount(); i++ {
		if tuple, ok := prec3.GetChild(i).(*grammar.ColumnExprTupleContext); ok {
			return tuple
		}
	}
	return nil
}

func extractTupleLiterals(pr *nanopass.ParseResult, tuple *grammar.ColumnExprTupleContext) (texts []string, elementType string, allLiterals bool) {
	var exprList *grammar.ColumnExprListContext
	for i := 0; i < tuple.GetChildCount(); i++ {
		if el, ok := tuple.GetChild(i).(*grammar.ColumnExprListContext); ok {
			exprList = el
			break
		}
	}
	if exprList == nil {
		return nil, "", false
	}

	texts = make([]string, 0, exprList.GetChildCount())
	allLiterals = true
	elementType = ""

	for i := 0; i < exprList.GetChildCount(); i++ {
		colsExpr, ok := exprList.GetChild(i).(*grammar.ColumnsExprColumnContext)
		if !ok {
			continue
		}

		if colsExpr.GetChildCount() == 0 {
			allLiterals = false
			return
		}

		litExpr, ok := colsExpr.GetChild(0).(*grammar.ColumnExprLiteralContext)
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

		texts = append(texts, nanopass.NodeText(pr, litExpr))
	}

	if len(texts) == 0 {
		allLiterals = false
	}
	return
}

func collectLiteralNodesInTuple(tuple *grammar.ColumnExprTupleContext) (nodes []*grammar.ColumnExprLiteralContext) {
	nanopass.WalkCST(tuple, func(ctx antlr.ParserRuleContext) bool {
		if litExpr, ok := ctx.(*grammar.ColumnExprLiteralContext); ok {
			nodes = append(nodes, litExpr)
		}
		return true
	})
	return
}

// --- Phase 1b: Individual literal collection ---

func collectLiteralCandidates(pr *nanopass.ParseResult, config *ExtractLiteralsConfig, excludeNodes map[*grammar.ColumnExprLiteralContext]bool) (candidates []literalCandidate) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		litExpr, ok := ctx.(*grammar.ColumnExprLiteralContext)
		if !ok {
			return true
		}

		if excludeNodes[litExpr] {
			return true
		}

		litCtx := findLiteralChild(litExpr)
		if litCtx == nil {
			return true
		}
		if litCtx.NULL_SQL() != nil {
			return true
		}

		literalText := nanopass.NodeText(pr, litExpr)
		typeName := inferClickHouseType(litCtx)

		// Check for cast context
		var castNode *grammar.ColumnExprCastContext
		var castType canonicaltypes.PrimitiveAstNodeI
		if castCtx, isCast := litExpr.GetParent().(*grammar.ColumnExprCastContext); isCast && config.mapTypeToCanonical != nil {
			castTypeText := extractCastTypeText(castCtx)
			if castTypeText != "" {
				ct, mapErr := config.mapTypeToCanonical(castTypeText)
				if mapErr == nil && ct != nil {
					castNode = castCtx
					castType = ct
					typeName = castTypeText // use the cast type for the {param: Type} slot
				}
			}
		}

		// Resolve context — use cast's parent if inside a cast
		var contextName string
		var argIndex int
		if castNode != nil {
			contextName, argIndex = resolveContextFromParent(castNode)
		} else {
			contextName, argIndex = resolveContext(litExpr)
		}

		normalizedCtx := normalizeFunctionName(contextName)

		candidates = append(candidates, literalCandidate{
			node:        litExpr,
			castNode:    castNode,
			contextName: contextName,
			argIndex:    argIndex,
			literalText: literalText,
			typeName:    typeName,
			castType:    castType,
			blacklisted: config.IsBlacklisted(normalizedCtx),
			whitelisted: config.IsWhitelisted(normalizedCtx),
		})

		return true
	})
	return
}

// --- Cast type extraction ---

func extractCastTypeText(castCtx *grammar.ColumnExprCastContext) string {
	for i := 0; i < castCtx.GetChildCount(); i++ {
		child := castCtx.GetChild(i)
		switch c := child.(type) {
		case *grammar.ColumnTypeExprSimpleContext:
			return c.GetText()
		case *grammar.ColumnTypeExprComplexContext:
			return c.GetText()
		}
	}
	return ""
}

// --- Helpers ---

func findLiteralChild(litExpr *grammar.ColumnExprLiteralContext) *grammar.LiteralContext {
	for i := 0; i < litExpr.GetChildCount(); i++ {
		if lit, ok := litExpr.GetChild(i).(*grammar.LiteralContext); ok {
			return lit
		}
	}
	return nil
}

func inferClickHouseType(lit *grammar.LiteralContext) string {
	if lit.STRING_LITERAL() != nil {
		return "String"
	}
	if lit.NULL_SQL() != nil {
		return "Nullable(Nothing)"
	}
	if lit.NumberLiteral() != nil {
		text := lit.NumberLiteral().GetText()
		scalar, err := scalars.UnmarshalScalarLiteral(text)
		if err != nil {
			return "Int64"
		}
		if scalar.Type != nil {
			switch scalar.Type.String() {
			case "u64":
				return "UInt64"
			case "i64":
				return "Int64"
			case "f64":
				return "Float64"
			}
		}
		return "Int64"
	}
	return "String"
}

// --- Context resolution ---

func resolveContext(litExpr *grammar.ColumnExprLiteralContext) (contextName string, argIndex int) {
	parent := litExpr.GetParent()
	if parent == nil {
		return "expr", 0
	}
	return resolveContextFromNode(parent)
}

func resolveContextFromParent(node antlr.ParserRuleContext) (contextName string, argIndex int) {
	parent := node.GetParent()
	if parent == nil {
		return "expr", 0
	}
	return resolveContextFromNode(parent)
}

func resolveContextFromNode(parent antlr.Tree) (contextName string, argIndex int) {
	switch p := parent.(type) {
	case *grammar.ColumnArgExprContext:
		return resolveFuncArgContext(p)
	case *grammar.ColumnExprPrecedence1Context:
		return resolveOperatorContextGeneric(p)
	case *grammar.ColumnExprPrecedence2Context:
		return resolveOperatorContextGeneric(p)
	case *grammar.ColumnExprPrecedence3Context:
		return resolveOperatorContextGeneric(p)
	case *grammar.ColumnsExprColumnContext:
		return resolveColumnsExprContextGeneric(p)
	case *grammar.ColumnExprBetweenContext:
		return resolveBetweenContextGeneric(p)
	default:
		return "expr", 0
	}
}

func resolveFuncArgContext(argExpr *grammar.ColumnArgExprContext) (contextName string, argIndex int) {
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
		if _, isArg := child.(*grammar.ColumnArgExprContext); isArg {
			argIndex++
		}
	}

	funcParent := argList.(antlr.RuleNode).GetParent()
	if funcParent == nil {
		return "func", argIndex
	}

	switch fp := funcParent.(type) {
	case *grammar.ColumnExprFunctionContext:
		if fp.Identifier() != nil {
			contextName = normalizeFunctionName(fp.Identifier().GetText())
		} else {
			contextName = "func"
		}
	case *grammar.ColumnExprWinFunctionContext:
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

func resolveOperatorContextGeneric(parent antlr.ParserRuleContext) (contextName string, argIndex int) {
	opName := "op"

	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tok := term.GetSymbol()
			switch tok.GetTokenType() {
			case grammar.ClickHouseLexerEQ_SINGLE, grammar.ClickHouseLexerEQ_DOUBLE:
				opName = "eq"
			case grammar.ClickHouseLexerNOT_EQ:
				opName = "neq"
			case grammar.ClickHouseLexerLT:
				opName = "lt"
			case grammar.ClickHouseLexerGT:
				opName = "gt"
			case grammar.ClickHouseLexerLE:
				opName = "le"
			case grammar.ClickHouseLexerGE:
				opName = "ge"
			case grammar.ClickHouseLexerPLUS:
				opName = "plus"
			case grammar.ClickHouseLexerDASH:
				opName = "minus"
			case grammar.ClickHouseLexerASTERISK:
				opName = "mul"
			case grammar.ClickHouseLexerSLASH:
				opName = "div"
			case grammar.ClickHouseLexerPERCENT:
				opName = "mod"
			case grammar.ClickHouseLexerCONCAT:
				opName = "concat"
			case grammar.ClickHouseLexerLIKE:
				opName = "like"
			case grammar.ClickHouseLexerILIKE:
				opName = "ilike"
			case grammar.ClickHouseLexerIN:
				opName = "in"
			}
		}
	}

	// Determine arg index: count expr children before the target
	// For now, assume the literal/cast is the last expr child (right operand = index 1)
	exprCount := 0
	for i := 0; i < parent.GetChildCount(); i++ {
		if _, isExpr := parent.GetChild(i).(antlr.ParserRuleContext); isExpr {
			exprCount++
		}
	}
	if exprCount >= 2 {
		argIndex = 1
	}

	return opName, argIndex
}

func resolveColumnsExprContextGeneric(parent *grammar.ColumnsExprColumnContext) (contextName string, argIndex int) {
	gp := parent.GetParent()
	if gp == nil {
		return "select", 0
	}

	if exprList, ok := gp.(*grammar.ColumnExprListContext); ok {
		ggp := exprList.GetParent()
		if _, isParen := ggp.(*grammar.ColumnExprPrecedence3Context); isParen {
			argIndex = 0
			for i := 0; i < exprList.GetChildCount(); i++ {
				child := exprList.GetChild(i)
				if child == parent {
					break
				}
				if _, isCol := child.(*grammar.ColumnsExprColumnContext); isCol {
					argIndex++
				}
			}
			return "in", argIndex
		}
	}

	if exprList, ok := gp.(*grammar.ColumnExprListContext); ok {
		argIndex = 0
		for i := 0; i < exprList.GetChildCount(); i++ {
			child := exprList.GetChild(i)
			if child == parent {
				break
			}
			if _, isCol := child.(*grammar.ColumnsExprColumnContext); isCol {
				argIndex++
			}
		}
	}
	return "select", argIndex
}

func resolveBetweenContextGeneric(parent *grammar.ColumnExprBetweenContext) (contextName string, argIndex int) {
	contextName = "between"
	// BETWEEN has 3 expr children: value, low, high
	// We can't easily determine which one is the literal without the original node ref
	// Default to argIndex 1 (low bound)
	argIndex = 1
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

func filterINListCandidates(candidates []inListCandidate, config *ExtractLiteralsConfig) (filtered []inListCandidate) {
	filtered = make([]inListCandidate, 0, len(candidates))
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

func assignParamNames(candidates []literalCandidate, config *ExtractLiteralsConfig) (params []extractedParam, paramByNode map[*grammar.ColumnExprLiteralContext]*extractedParam) {
	paramByNode = make(map[*grammar.ColumnExprLiteralContext]*extractedParam, len(candidates))

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

		key := dedupKey{
			contextName: c.contextName,
			argIndex:    c.argIndex,
			literalText: c.literalText,
			castCanon:   castCanon,
		}

		if existing, found := dedupMap[key]; found {
			paramByNode[c.node] = existing
			continue
		}

		meta := ParamMetadata{
			ArgIndex:          uint32(c.argIndex),
			CastTypeCanonical: castCanon,
		}

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

		// Handle name collisions
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

		p := extractedParam{
			name:     name,
			value:    c.literalText,
			typeName: c.typeName,
			castType: c.castType,
			meta:     meta,
		}

		params = append(params, p)
		paramPtr := &params[len(params)-1]
		dedupMap[key] = paramPtr
		usedNames[name] = true
		paramByNode[c.node] = paramPtr
	}

	return
}

func assignINListParamNames(candidates []inListCandidate, config *ExtractLiteralsConfig) (params []extractedParam) {
	usedNames := make(map[string]bool)
	params = make([]extractedParam, 0, len(candidates))
	seqCounter := uint32(0)

	for _, c := range candidates {
		arrayValue := "[" + strings.Join(c.literalTexts, ", ") + "]"

		castCanon := ""
		if c.castType != nil {
			castCanon = c.castType.String()
		}

		typeName := fmt.Sprintf("Array(%s)", c.elementType)
		if c.castType != nil {
			// Use the cast type text for the slot
			castTypeText := ""
			if c.castNode != nil {
				castTypeText = extractCastTypeText(c.castNode)
			}
			if castTypeText != "" {
				typeName = castTypeText
			}
		}

		meta := ParamMetadata{
			ArgIndex:          0,
			CastTypeCanonical: castCanon,
		}

		if config.useSequentialNames {
			meta.IsSequential = true
			meta.SequentialIndex = seqCounter
			seqCounter++
		} else {
			meta.ContentHash = literalHash(arrayValue)
		}

		name, buildErr := BuildParamName(config.prefix, "in", &meta)
		if buildErr != nil {
			continue
		}

		if usedNames[name] {
			meta.HashCollisionCounter = 2
			for {
				name, buildErr = BuildParamName(config.prefix, "in", &meta)
				if buildErr != nil {
					break
				}
				if !usedNames[name] {
					break
				}
				meta.HashCollisionCounter++
			}
		}

		p := extractedParam{
			name:     name,
			value:    arrayValue,
			typeName: typeName,
			castType: c.castType,
			meta:     meta,
		}

		params = append(params, p)
		usedNames[name] = true
	}

	return
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

// AnalyzeExtractions returns the parameter extractions that would be performed
// without modifying the SQL.
func AnalyzeExtractions(sql string, config *ExtractLiteralsConfig) (extractions []ExtractionInfo, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("AnalyzeExtractions: %w", err)
		return
	}

	inListCandidates := collectINListCandidates(pr, config)
	inListNodes := make(map[*grammar.ColumnExprLiteralContext]bool)
	for _, ilc := range inListCandidates {
		for _, litNode := range collectLiteralNodesInTuple(ilc.tupleNode) {
			inListNodes[litNode] = true
		}
	}

	candidates := collectLiteralCandidates(pr, config, inListNodes)
	filtered := filterCandidates(candidates, config)
	filteredINLists := filterINListCandidates(inListCandidates, config)

	extractions = make([]ExtractionInfo, 0, len(filtered)+len(filteredINLists))

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
				ParamName:   p.name,
				Value:       p.value,
				TypeName:    p.typeName,
				ContextName: c.contextName,
				ArgIndex:    c.argIndex,
				Line:        c.node.GetStart().GetLine(),
				Column:      c.node.GetStart().GetColumn(),
				CastType:    c.castType,
			})
		}
	}

	if len(filteredINLists) > 0 {
		inParams := assignINListParamNames(filteredINLists, config)
		for i, ilc := range filteredINLists {
			p := &inParams[i]
			extractions = append(extractions, ExtractionInfo{
				ParamName:   p.name,
				Value:       p.value,
				TypeName:    p.typeName,
				ContextName: "in",
				ArgIndex:    0,
				Line:        ilc.tupleNode.GetStart().GetLine(),
				Column:      ilc.tupleNode.GetStart().GetColumn(),
				CastType:    ilc.castType,
			})
		}
	}

	return
}

// ExtractionInfo describes a single literal extraction for preview/analysis.
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

// ParseExtractedQuery splits the output of ExtractLiterals into SET lines and the query.
func ParseExtractedQuery(extracted string) (sets []string, query string) {
	lines := strings.Split(extracted, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "SET ") {
			sets = append(sets, strings.TrimSuffix(line, ";"))
		} else {
			query = strings.Join(lines[i:], "\n")
			break
		}
	}
	return
}

// CountExtractableParams returns the number of unique parameters that would be extracted.
func CountExtractableParams(sql string, config *ExtractLiteralsConfig) (count int, err error) {
	extractions, err := AnalyzeExtractions(sql, config)
	if err != nil {
		return
	}
	count = len(extractions)
	return
}

// InjectParams is the inverse of ExtractLiterals — it takes SET param = value
// lines and a query with {param: Type} slots and produces a single query with
// literals inlined. Does NOT reconstruct casts — use InjectParamsWithCasts for that.
func InjectParams(sets []string, query string) (result string, err error) {
	paramMap := make(map[string]string, len(sets))
	for _, set := range sets {
		parts := strings.SplitN(set, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "SET ")
		name = strings.TrimSpace(name)
		value := strings.TrimSpace(parts[1])
		paramMap[name] = value
	}

	result = query
	for name, value := range paramMap {
		prefix := "{" + name + ":"
		for {
			idx := strings.Index(result, prefix)
			if idx < 0 {
				break
			}
			endIdx := strings.Index(result[idx:], "}")
			if endIdx < 0 {
				break
			}
			endIdx += idx
			result = result[:idx] + value + result[endIdx+1:]
		}
	}
	return
}
