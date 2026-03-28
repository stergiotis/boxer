//go:build llm_generated_opus46

package highlight

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CategoryE classifies a token for highlighting.
type CategoryE int

const (
	CatPlain        CategoryE = iota // unclassified / default
	CatKeyword                       // SQL keywords (SELECT, FROM, WHERE, ...)
	CatOperator                      // logical/comparison operators (AND, OR, NOT, IN, LIKE, BETWEEN)
	CatIdentifier                    // bare identifier (fallback when no semantic info)
	CatTableName                     // table name in FROM/JOIN
	CatTableAlias                    // alias definition in FROM/JOIN (the "o" in "orders AS o")
	CatColumnName                    // column reference
	CatColumnAlias                   // alias definition in SELECT (the "total" in "sum(a) AS total")
	CatCTEName                       // CTE name in WITH definition or FROM reference
	CatFunctionName                  // function/aggregate name
	CatDatabaseName                  // database qualifier (the "db" in "db.table")
	CatTypeName                      // type name in CAST, param slots
	CatStringLit                     // 'string literal'
	CatNumberLit                     // 42, 3.14, 0xFF
	CatPunctuation                   // ( ) [ ] , . ; :
	CatComment                       // -- or /* */
	CatWhitespace                    // spaces, tabs, newlines
	CatParamSlot                     // {name: Type} parameter slot
)

// Span represents a highlighted region of the input SQL.
type Span struct {
	Start    int    // byte offset (inclusive)
	Stop     int    // byte offset (exclusive)
	Text     string // the text of the span
	Category CategoryE
}

// Highlight performs semantic highlighting on a ClickHouse SQL string.
// It first lexes the input for baseline token classification, then parses
// and walks the CST to refine identifiers into semantic categories
// (table names, column names, function names, aliases, etc.).
//
// If parsing fails, lexical-only highlighting is returned.
func Highlight(sql string) (spans []Span) {
	// Phase 1: Lex — produces baseline classification
	spans = lexHighlight(sql)

	// Phase 2: Parse + semantic refinement
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return // lexical-only fallback
	}

	// Build a token index → span index map for fast lookup
	tokenToSpan := buildTokenToSpanMap(pr, spans)

	// Walk CST and refine categories
	semanticRefine(pr, spans, tokenToSpan)

	return
}

// --- Phase 1: Lexical highlighting ---

func lexHighlight(sql string) (spans []Span) {
	lexer := grammar.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()

	spans = make([]Span, 0, 64)
	for {
		tok := lexer.NextToken()
		if tok.GetTokenType() == antlr.TokenEOF {
			break
		}

		start := tok.GetStart()
		stop := tok.GetStop() + 1
		text := tok.GetText()
		cat := classifyTokenType(tok.GetTokenType(), tok.GetChannel())

		spans = append(spans, Span{
			Start:    start,
			Stop:     stop,
			Text:     text,
			Category: cat,
		})
	}

	return
}

func classifyTokenType(tokenType int, channel int) CategoryE {
	if channel == antlr.LexerHidden {
		switch tokenType {
		case grammar.ClickHouseLexerWHITESPACE:
			return CatWhitespace
		case grammar.ClickHouseLexerSINGLE_LINE_COMMENT, grammar.ClickHouseLexerMULTI_LINE_COMMENT:
			return CatComment
		default:
			return CatWhitespace
		}
	}

	switch tokenType {
	case grammar.ClickHouseLexerIDENTIFIER:
		return CatIdentifier

	case grammar.ClickHouseLexerSTRING_LITERAL:
		return CatStringLit

	case grammar.ClickHouseLexerDECIMAL_LITERAL,
		grammar.ClickHouseLexerHEXADECIMAL_LITERAL,
		grammar.ClickHouseLexerOCTAL_LITERAL,
		grammar.ClickHouseLexerFLOATING_LITERAL:
		return CatNumberLit

	case grammar.ClickHouseLexerLPAREN,
		grammar.ClickHouseLexerRPAREN,
		grammar.ClickHouseLexerLBRACKET,
		grammar.ClickHouseLexerRBRACKET,
		grammar.ClickHouseLexerLBRACE,
		grammar.ClickHouseLexerRBRACE,
		grammar.ClickHouseLexerCOMMA,
		grammar.ClickHouseLexerDOT,
		grammar.ClickHouseLexerSEMICOLON,
		grammar.ClickHouseLexerCOLON:
		return CatPunctuation

	case grammar.ClickHouseLexerPLUS,
		grammar.ClickHouseLexerDASH,
		grammar.ClickHouseLexerASTERISK,
		grammar.ClickHouseLexerSLASH,
		grammar.ClickHouseLexerPERCENT,
		grammar.ClickHouseLexerEQ_DOUBLE,
		grammar.ClickHouseLexerEQ_SINGLE,
		grammar.ClickHouseLexerNOT_EQ,
		grammar.ClickHouseLexerLE,
		grammar.ClickHouseLexerGE,
		grammar.ClickHouseLexerLT,
		grammar.ClickHouseLexerGT,
		grammar.ClickHouseLexerCONCAT,
		grammar.ClickHouseLexerARROW:
		//grammar.ClickHouseLexerQUESTION:
		return CatPunctuation

	case grammar.ClickHouseLexerAND, grammar.ClickHouseLexerOR, grammar.ClickHouseLexerNOT,
		grammar.ClickHouseLexerIN, grammar.ClickHouseLexerLIKE, grammar.ClickHouseLexerILIKE,
		grammar.ClickHouseLexerBETWEEN, grammar.ClickHouseLexerIS, grammar.ClickHouseLexerGLOBAL:
		return CatOperator

	case grammar.ClickHouseLexerINF, grammar.ClickHouseLexerNAN_SQL:
		return CatNumberLit

	case grammar.ClickHouseLexerNULL_SQL:
		return CatKeyword

	case grammar.ClickHouseLexerJSON_FALSE, grammar.ClickHouseLexerJSON_TRUE:
		return CatNumberLit

	default:
		// All remaining keyword tokens (1-199 range)
		if tokenType >= 1 && tokenType < grammar.ClickHouseLexerIDENTIFIER {
			return CatKeyword
		}
		return CatPlain
	}
}

// --- Phase 2: Semantic refinement ---

func buildTokenToSpanMap(pr *nanopass.ParseResult, spans []Span) (tokenToSpan map[int]int) {
	tokenToSpan = make(map[int]int, len(spans))
	for i, span := range spans {
		// Find the token at this byte offset
		for j := 0; j < pr.TokenStream.Size(); j++ {
			tok := pr.TokenStream.Get(j)
			if tok.GetTokenType() == antlr.TokenEOF {
				break
			}
			if tok.GetStart() == span.Start {
				tokenToSpan[tok.GetTokenIndex()] = i
				break
			}
		}
	}
	return
}

func semanticRefine(pr *nanopass.ParseResult, spans []Span, tokenToSpan map[int]int) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {

		// --- Function names ---
		case *grammar.ColumnExprFunctionContext:
			if ident := c.Identifier(); ident != nil {
				refineIdentifier(ident, spans, tokenToSpan, CatFunctionName)
			}

		// --- Window function names ---
		case *grammar.ColumnExprWinFunctionContext:
			if ident := c.Identifier(); ident != nil {
				refineIdentifier(ident, spans, tokenToSpan, CatFunctionName)
			}

		// --- Table identifiers in FROM/JOIN ---
		case *grammar.TableIdentifierContext:
			if isTableContext(c) {
				if c.Identifier() != nil {
					refineIdentifier(c.Identifier(), spans, tokenToSpan, CatTableName)
				}
				if c.DatabaseIdentifier() != nil {
					dbIdent := c.DatabaseIdentifier().(*grammar.DatabaseIdentifierContext)
					if dbIdent.Identifier() != nil {
						refineIdentifier(dbIdent.Identifier(), spans, tokenToSpan, CatDatabaseName)
					}
				}
			}

		// --- Table aliases ---
		case *grammar.TableExprAliasContext:
			for i := 0; i < c.GetChildCount(); i++ {
				child := c.GetChild(i)
				if ident, ok := child.(*grammar.IdentifierContext); ok {
					refineIdentifier(ident, spans, tokenToSpan, CatTableAlias)
				}
				if alias, ok := child.(*grammar.AliasContext); ok {
					refineLeafTokens(alias, spans, tokenToSpan, CatTableAlias)
				}
			}

		// --- Column aliases ---
		case *grammar.ColumnExprAliasContext:
			for i := 0; i < c.GetChildCount(); i++ {
				child := c.GetChild(i)
				if ident, ok := child.(*grammar.IdentifierContext); ok {
					refineIdentifier(ident, spans, tokenToSpan, CatColumnAlias)
				}
				if alias, ok := child.(*grammar.AliasContext); ok {
					refineLeafTokens(alias, spans, tokenToSpan, CatColumnAlias)
				}
			}

		// --- CTE names ---
		case *grammar.NamedQueryContext:
			if c.Identifier() != nil {
				refineIdentifier(c.Identifier(), spans, tokenToSpan, CatCTEName)
			}

		// --- Column identifiers ---
		case *grammar.ColumnIdentifierContext:
			if ni := c.NestedIdentifier(); ni != nil {
				refineNestedIdentifier(ni.(*grammar.NestedIdentifierContext), spans, tokenToSpan)
			}

		// --- Parameter slots {name: Type} ---
		case *grammar.ColumnExprParamSlotContext:
			refineAllTokens(c, spans, tokenToSpan, CatParamSlot)
		}

		return true
	})

	// Second pass: refine CTE references in FROM
	refineCTEReferences(pr, spans, tokenToSpan)
}

func refineIdentifier(ident antlr.RuleNode, spans []Span, tokenToSpan map[int]int, cat CategoryE) {
	identCtx, ok := ident.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	refineLeafTokens(identCtx, spans, tokenToSpan, cat)
}

func refineLeafTokens(ctx antlr.ParserRuleContext, spans []Span, tokenToSpan map[int]int, cat CategoryE) {
	start := ctx.GetStart()
	stop := ctx.GetStop()
	if start == nil || stop == nil {
		return
	}
	for idx := start.GetTokenIndex(); idx <= stop.GetTokenIndex(); idx++ {
		if spanIdx, ok := tokenToSpan[idx]; ok {
			prev := spans[spanIdx].Category
			if prev == CatIdentifier ||
				prev == CatKeyword ||
				prev == CatPlain ||
				prev == CatTableName {
				spans[spanIdx].Category = cat
			}
		}
	}
}

func refineAllTokens(ctx antlr.ParserRuleContext, spans []Span, tokenToSpan map[int]int, cat CategoryE) {
	start := ctx.GetStart()
	stop := ctx.GetStop()
	if start == nil || stop == nil {
		return
	}
	for idx := start.GetTokenIndex(); idx <= stop.GetTokenIndex(); idx++ {
		if spanIdx, ok := tokenToSpan[idx]; ok {
			spans[spanIdx].Category = cat
		}
	}
}

func refineNestedIdentifier(ni *grammar.NestedIdentifierContext, spans []Span, tokenToSpan map[int]int) {
	// NestedIdentifier can be:
	// - identifier (bare column)
	// - identifier DOT identifier (table.column)
	children := make([]antlr.ParserRuleContext, 0, 2)
	for i := 0; i < ni.GetChildCount(); i++ {
		if ident, ok := ni.GetChild(i).(*grammar.IdentifierContext); ok {
			children = append(children, ident)
		}
	}

	if len(children) == 1 {
		// Bare column
		refineLeafTokens(children[0], spans, tokenToSpan, CatColumnName)
	} else if len(children) == 2 {
		// table.column — first is qualifier, second is column
		refineLeafTokens(children[0], spans, tokenToSpan, CatTableName)
		refineLeafTokens(children[1], spans, tokenToSpan, CatColumnName)
	}
}

func refineCTEReferences(pr *nanopass.ParseResult, spans []Span, tokenToSpan map[int]int) {
	// Collect CTE names from NamedQueryContext nodes
	cteNames := make(map[string]bool)
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if nq, ok := ctx.(*grammar.NamedQueryContext); ok {
			if nq.Identifier() != nil {
				cteNames[nq.Identifier().GetText()] = true
			}
		}
		return true
	})

	if len(cteNames) == 0 {
		return
	}

	// Walk all TableIdentifier nodes and refine CTE references
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		tid, ok := ctx.(*grammar.TableIdentifierContext)
		if !ok {
			return true
		}
		if !isTableContext(tid) {
			return true
		}
		if tid.DatabaseIdentifier() != nil {
			return true // qualified names can't be CTE references
		}
		if tid.Identifier() != nil {
			name := tid.Identifier().GetText()
			if cteNames[name] {
				refineIdentifier(tid.Identifier(), spans, tokenToSpan, CatCTEName)
			}
		}
		return true
	})
}

func scopeAll(scopes []*nanopass.SelectScope) (all []*nanopass.SelectScope) {
	for _, s := range scopes {
		all = append(all, s.AllScopes()...)
	}
	return
}

func isTableContext(tid *grammar.TableIdentifierContext) bool {
	// Walk up to check if this TableIdentifier is inside a TableExpr (FROM/JOIN)
	// rather than inside a ColumnIdentifier (column qualifier)
	parent := tid.GetParent()
	if parent == nil {
		return false
	}
	switch parent.(type) {
	case *grammar.TableExprIdentifierContext:
		return true
	case *grammar.TableExprAliasContext:
		return true
	default:
		// Could be nested — check grandparent
		if gp := parent.(antlr.Tree); gp != nil {
			if gpCtx, ok := gp.(antlr.ParserRuleContext); ok {
				switch gpCtx.(type) {
				case *grammar.TableExprIdentifierContext:
					return true
				case *grammar.TableExprAliasContext:
					return true
				}
			}
		}
		return false
	}
}

// --- Renderers ---

// RenderANSI renders highlighted spans as ANSI terminal output.
func RenderANSI(spans []Span) string {
	var sb strings.Builder
	sb.Grow(256)

	for _, span := range spans {
		code := ansiCode(span.Category)
		if code != "" {
			sb.WriteString(code)
			sb.WriteString(span.Text)
			sb.WriteString("\033[0m")
		} else {
			sb.WriteString(span.Text)
		}
	}

	return sb.String()
}

func ansiCode(cat CategoryE) string {
	switch cat {
	case CatKeyword:
		return "\033[1;34m" // bold blue
	case CatOperator:
		return "\033[34m" // blue
	case CatTableName:
		return "\033[1;33m" // bold yellow
	case CatTableAlias:
		return "\033[33m" // yellow
	case CatColumnName:
		return "\033[36m" // cyan
	case CatColumnAlias:
		return "\033[1;36m" // bold cyan
	case CatCTEName:
		return "\033[1;35m" // bold magenta
	case CatFunctionName:
		return "\033[32m" // green
	case CatDatabaseName:
		return "\033[33m" // yellow
	case CatTypeName:
		return "\033[35m" // magenta
	case CatStringLit:
		return "\033[31m" // red
	case CatNumberLit:
		return "\033[91m" // bright red
	case CatComment:
		return "\033[90m" // dark gray
	case CatParamSlot:
		return "\033[95m" // bright magenta
	case CatPunctuation:
		return "" // default color
	case CatWhitespace:
		return ""
	default:
		return ""
	}
}

// RenderHTML renders highlighted spans as an HTML fragment with CSS classes.
// The output is a <code> block. Use HighlightCSS() for the corresponding stylesheet.
func RenderHTML(spans []Span) string {
	var sb strings.Builder
	sb.Grow(512)
	sb.WriteString(`<code class="hl-sql">`)

	for _, span := range spans {
		cls := htmlClass(span.Category)
		escaped := htmlEscape(span.Text)
		if cls != "" {
			sb.WriteString(`<span class="`)
			sb.WriteString(cls)
			sb.WriteString(`">`)
			sb.WriteString(escaped)
			sb.WriteString(`</span>`)
		} else {
			sb.WriteString(escaped)
		}
	}

	sb.WriteString(`</code>`)
	return sb.String()
}

func htmlClass(cat CategoryE) string {
	switch cat {
	case CatKeyword:
		return "hl-kw"
	case CatOperator:
		return "hl-op"
	case CatTableName:
		return "hl-tbl"
	case CatTableAlias:
		return "hl-ta"
	case CatColumnName:
		return "hl-col"
	case CatColumnAlias:
		return "hl-ca"
	case CatCTEName:
		return "hl-cte"
	case CatFunctionName:
		return "hl-fn"
	case CatDatabaseName:
		return "hl-db"
	case CatTypeName:
		return "hl-ty"
	case CatStringLit:
		return "hl-str"
	case CatNumberLit:
		return "hl-num"
	case CatComment:
		return "hl-cmt"
	case CatParamSlot:
		return "hl-ps"
	case CatPunctuation:
		return "hl-pn"
	default:
		return ""
	}
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// HighlightCSS returns a CSS stylesheet for the HTML highlighter.
// Provides both a light theme and a dark theme via prefers-color-scheme.
func HighlightCSS() string {
	return `.hl-sql {
  font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 14px;
  line-height: 1.5;
  white-space: pre-wrap;
  tab-size: 4;
}

/* Light theme */
.hl-kw  { color: #0033b3; font-weight: bold; }
.hl-op  { color: #0033b3; }
.hl-tbl { color: #986801; font-weight: bold; }
.hl-ta  { color: #986801; font-style: italic; }
.hl-col { color: #005f87; }
.hl-ca  { color: #005f87; font-weight: bold; }
.hl-cte { color: #8700af; font-weight: bold; }
.hl-fn  { color: #067d17; }
.hl-db  { color: #986801; }
.hl-ty  { color: #8700af; }
.hl-str { color: #a31515; }
.hl-num { color: #1750eb; }
.hl-cmt { color: #8c8c8c; font-style: italic; }
.hl-ps  { color: #af00d7; }
.hl-pn  { color: #383a42; }

/* Dark theme */
@media (prefers-color-scheme: dark) {
  .hl-kw  { color: #569cd6; font-weight: bold; }
  .hl-op  { color: #569cd6; }
  .hl-tbl { color: #dcdcaa; font-weight: bold; }
  .hl-ta  { color: #dcdcaa; font-style: italic; }
  .hl-col { color: #9cdcfe; }
  .hl-ca  { color: #9cdcfe; font-weight: bold; }
  .hl-cte { color: #c586c0; font-weight: bold; }
  .hl-fn  { color: #4ec9b0; }
  .hl-db  { color: #dcdcaa; }
  .hl-ty  { color: #c586c0; }
  .hl-str { color: #ce9178; }
  .hl-num { color: #b5cea8; }
  .hl-cmt { color: #6a9955; font-style: italic; }
  .hl-ps  { color: #d4a0ff; }
  .hl-pn  { color: #d4d4d4; }
}
`
}

// CategoryName returns a human-readable name for a category.
func CategoryName(cat CategoryE) string {
	switch cat {
	case CatPlain:
		return "plain"
	case CatKeyword:
		return "keyword"
	case CatOperator:
		return "operator"
	case CatIdentifier:
		return "identifier"
	case CatTableName:
		return "table"
	case CatTableAlias:
		return "table_alias"
	case CatColumnName:
		return "column"
	case CatColumnAlias:
		return "column_alias"
	case CatCTEName:
		return "cte"
	case CatFunctionName:
		return "function"
	case CatDatabaseName:
		return "database"
	case CatTypeName:
		return "type"
	case CatStringLit:
		return "string"
	case CatNumberLit:
		return "number"
	case CatPunctuation:
		return "punctuation"
	case CatComment:
		return "comment"
	case CatWhitespace:
		return "whitespace"
	case CatParamSlot:
		return "param_slot"
	default:
		return fmt.Sprintf("unknown(%d)", int(cat))
	}
}
