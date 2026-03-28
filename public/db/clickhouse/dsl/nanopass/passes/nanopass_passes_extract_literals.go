//go:build llm_generated_opus46

package passes

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ExtractLiteralsConfig configures the literal extraction pass.
type ExtractLiteralsConfig struct {
	// minLength is the minimum character length of a literal to trigger extraction.
	// Literals shorter than this are left inline unless the parent function is whitelisted.
	minLength int

	// funcPolicy maps normalized function/operator names to extraction policy.
	// true = blacklisted (never extract), false = whitelisted (always extract).
	// Functions not in the map follow the minLength threshold.
	funcPolicy map[string]bool

	// prefix is the prefix for generated parameter names. Default: "param".
	prefix string
}

// NewExtractLiteralsConfig creates a config with sensible defaults.
func NewExtractLiteralsConfig(minLength int) (inst *ExtractLiteralsConfig) {
	inst = &ExtractLiteralsConfig{
		minLength:  minLength,
		funcPolicy: make(map[string]bool),
		prefix:     "param",
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

// Whitelist marks a function/operator so its literal arguments are ALWAYS extracted,
// regardless of MinLength. The name is normalized (lowercased, trimmed).
func (inst *ExtractLiteralsConfig) Whitelist(name string) {
	inst.funcPolicy[normalizeFunctionName(name)] = false
}

// Blacklist marks a function/operator so its literal arguments are NEVER extracted.
// Blacklist takes priority over Whitelist — calling Blacklist after Whitelist on the
// same name will override the whitelist entry.
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

// normalizeFunctionName normalizes a function/operator name for policy lookup.
// Lowercases and trims whitespace.
func normalizeFunctionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// extractedParam represents a literal that has been extracted into a query parameter.
type extractedParam struct {
	name       string // param name (e.g. "param_eq_0")
	value      string // literal text as it appears in SQL (e.g. "'hello'")
	typeName   string // ClickHouse type (e.g. "String", "Int64", "Float64")
	contextKey string // dedup key: contextName + argIndex + literalText
}

// literalCandidate represents a literal found during CST walking.
type literalCandidate struct {
	node        *grammar.ColumnExprLiteralContext
	contextName string // function or operator name
	argIndex    int    // argument position
	literalText string // raw SQL text of the literal
	typeName    string // inferred ClickHouse type
	blacklisted bool
	whitelisted bool
}

// ExtractLiterals returns a Pass that extracts long string/number literals
// into SET param_xxx = ... statements, replacing them with {param_xxx: Type}
// parameter slot syntax.
//
// Deduplication: identical literals in the same context (same function/operator
// and argument position) share a single parameter. The parameter name encodes
// the context: param_<func>_<argIndex>.
//
// The pass prepends SET statements to the query. Each SET is on its own line.
func ExtractLiterals(config *ExtractLiteralsConfig) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("ExtractLiterals: %w", err)
			return
		}

		// Phase 1: Collect candidates
		candidates := collectLiteralCandidates(pr, config)
		if len(candidates) == 0 {
			result = sql
			return
		}

		// Phase 2: Filter by length/whitelist/blacklist
		filtered := filterCandidates(candidates, config)
		if len(filtered) == 0 {
			result = sql
			return
		}

		// Phase 3: Assign parameter names with deduplication
		params, paramByNode := assignParamNames(filtered, config.prefix)

		// Phase 4: Rewrite the SQL
		rw := nanopass.NewRewriter(pr)
		for _, cand := range filtered {
			p := paramByNode[cand.node]
			slotText := fmt.Sprintf("{%s: %s}", p.name, p.typeName)
			nanopass.ReplaceNode(rw, cand.node, slotText)
		}

		rewritten := nanopass.GetText(rw)

		// Phase 5: Prepend SET statements
		var sb strings.Builder
		sb.Grow(len(rewritten) + len(params)*40)
		for _, p := range params {
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

// --- Phase 1: Collect candidates ---

func collectLiteralCandidates(pr *nanopass.ParseResult, config *ExtractLiteralsConfig) (candidates []literalCandidate) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		litExpr, ok := ctx.(*grammar.ColumnExprLiteralContext)
		if !ok {
			return true
		}

		// Skip NULL — not useful to parameterize
		litCtx := findLiteralChild(litExpr)
		if litCtx == nil {
			return true
		}
		if litCtx.NULL_SQL() != nil {
			return true
		}

		literalText := nanopass.NodeText(pr, litExpr)
		typeName := inferClickHouseType(litCtx)
		contextName, argIndex := resolveContext(litExpr)
		normalizedCtx := normalizeFunctionName(contextName)

		candidates = append(candidates, literalCandidate{
			node:        litExpr,
			contextName: contextName,
			argIndex:    argIndex,
			literalText: literalText,
			typeName:    typeName,
			blacklisted: config.IsBlacklisted(normalizedCtx),
			whitelisted: config.IsWhitelisted(normalizedCtx),
		})

		return true
	})
	return
}

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
	if lit.NumberLiteral() != nil {
		text := lit.NumberLiteral().GetText()
		if strings.Contains(text, ".") {
			return "Float64"
		}
		if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
			return "UInt64"
		}
		if strings.HasPrefix(text, "-") {
			return "Int64"
		}
		return "Int64"
	}
	return "String"
}

// resolveContext determines the function/operator context and argument position
// of a literal expression.
func resolveContext(litExpr *grammar.ColumnExprLiteralContext) (contextName string, argIndex int) {
	parent := litExpr.GetParent()
	if parent == nil {
		return "expr", 0
	}

	switch p := parent.(type) {
	case *grammar.ColumnArgExprContext:
		return resolveFuncArgContext(p)

	case *grammar.ColumnExprPrecedence1Context:
		return resolveOperatorContext(p, litExpr)

	case *grammar.ColumnExprPrecedence2Context:
		return resolveOperatorContext(p, litExpr)

	case *grammar.ColumnExprPrecedence3Context:
		return resolveOperatorContext(p, litExpr)

	case *grammar.ColumnsExprColumnContext:
		return resolveColumnsExprContext(p, litExpr)

	case *grammar.ColumnExprBetweenContext:
		return resolveBetweenContext(p, litExpr)

	default:
		return "expr", 0
	}
}

func resolveFuncArgContext(argExpr *grammar.ColumnArgExprContext) (contextName string, argIndex int) {
	argList := argExpr.GetParent()
	if argList == nil {
		return "func", 0
	}

	// Find the argument index (skipping commas)
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

	// Find the function name — argList's parent should be ColumnExprFunctionContext
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

func resolveOperatorContext(parent antlr.ParserRuleContext, litExpr *grammar.ColumnExprLiteralContext) (contextName string, argIndex int) {
	opName := "op"
	litIdx := -1

	exprIdx := 0
	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if child == litExpr {
			litIdx = exprIdx
		}
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
		if _, isExpr := child.(antlr.ParserRuleContext); isExpr {
			exprIdx++
		}
	}

	if litIdx < 0 {
		litIdx = 0
	}
	return opName, litIdx
}

func resolveColumnsExprContext(parent *grammar.ColumnsExprColumnContext, litExpr *grammar.ColumnExprLiteralContext) (contextName string, argIndex int) {
	gp := parent.GetParent()
	if gp == nil {
		return "select", 0
	}

	// If grandparent is ColumnExprListContext whose parent is Precedence3 (IN), it's an IN list
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

	// Otherwise it's a SELECT list item
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

func resolveBetweenContext(parent *grammar.ColumnExprBetweenContext, litExpr *grammar.ColumnExprLiteralContext) (contextName string, argIndex int) {
	contextName = "between"
	argIndex = 0
	exprIdx := 0
	for i := 0; i < parent.GetChildCount(); i++ {
		child := parent.GetChild(i)
		if child == litExpr {
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

// --- Phase 3: Assign names with deduplication ---

func assignParamNames(candidates []literalCandidate, prefix string) (params []extractedParam, paramByNode map[*grammar.ColumnExprLiteralContext]*extractedParam) {
	paramByNode = make(map[*grammar.ColumnExprLiteralContext]*extractedParam, len(candidates))

	type dedupKey struct {
		contextName string
		argIndex    int
		literalText string
	}
	dedupMap := make(map[dedupKey]*extractedParam)

	// Track base names to detect collisions
	baseNameUsed := make(map[string]*extractedParam)

	params = make([]extractedParam, 0, len(candidates))

	for i := range candidates {
		c := &candidates[i]
		key := dedupKey{
			contextName: c.contextName,
			argIndex:    c.argIndex,
			literalText: c.literalText,
		}

		if existing, found := dedupMap[key]; found {
			paramByNode[c.node] = existing
			continue
		}

		baseName := fmt.Sprintf("%s_%s_%d", prefix, sanitizeName(c.contextName), c.argIndex)
		finalName := baseName

		if prev, used := baseNameUsed[baseName]; used {
			if prev.value != c.literalText {
				counter := 2
				for {
					finalName = fmt.Sprintf("%s_%d", baseName, counter)
					if _, exists := baseNameUsed[finalName]; !exists {
						break
					}
					counter++
				}
			}
		}

		p := extractedParam{
			name:       finalName,
			value:      c.literalText,
			typeName:   c.typeName,
			contextKey: fmt.Sprintf("%s_%d_%s", c.contextName, c.argIndex, c.literalText),
		}

		params = append(params, p)
		paramPtr := &params[len(params)-1]
		dedupMap[key] = paramPtr
		baseNameUsed[finalName] = paramPtr
		paramByNode[c.node] = paramPtr
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
// without modifying the SQL. Useful for dry-run / preview.
func AnalyzeExtractions(sql string, config *ExtractLiteralsConfig) (extractions []ExtractionInfo, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("AnalyzeExtractions: %w", err)
		return
	}

	candidates := collectLiteralCandidates(pr, config)
	filtered := filterCandidates(candidates, config)
	if len(filtered) == 0 {
		return
	}

	_, paramByNode := assignParamNames(filtered, config.prefix)

	seen := make(map[string]bool)
	extractions = make([]ExtractionInfo, 0, len(filtered))
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
		})
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
}

func (inst *ExtractionInfo) String() string {
	return fmt.Sprintf("SET %s = %s; -- %s arg %d at line %d:%d (type %s)",
		inst.ParamName, inst.Value, inst.ContextName, inst.ArgIndex, inst.Line, inst.Column, inst.TypeName)
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

// CountExtractableParams returns the number of unique parameters that would be
// extracted from the query.
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
// literals inlined.
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
