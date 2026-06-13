package env

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
)

// ParamPrefix identifies a SET-line whose key denotes a parameter (e.g.
// `SET param_x = 5;`). Anything else under SET is a session setting.
const ParamPrefix = "param_"

// Extract splits SQL into an Environment and a body.
//
// The Environment owns:
//
//   - The leading `SET key = value;` prelude (split into Params vs.
//     SessionSettings — see SET-line classification rules below).
//   - `{name: Type}` slot occurrences in the body (populating Param.Type).
//   - Read-only views of the inline `... SETTINGS k=v` clause
//     (env.StatementSettings) and trailing `FORMAT FormatName` clause
//     (env.Format). These remain in the body — env.Integrate does not
//     re-emit them, and passes that mutate them rewrite the body's CST.
//
// Round-trip: Integrate(Extract(sql)) is normalising over the prelude only;
// inline SETTINGS / FORMAT pass through verbatim via body.
//
// SET-line classification:
//   - A SET whose key starts with [ParamPrefix] OR whose key matches the
//     name of a `{name: Type}` slot found in the body becomes an env.Params
//     entry.
//   - Everything else becomes an env.SessionSettings entry.
//
// All body parsing is best-effort: if the body does not parse, Extract
// still returns a usable Environment based on the SET prelude alone.
func Extract(sql string) (e *Environment, body string, err error) {
	e = NewEnvironment()

	preludeEntries, body := harvestSetPrelude(sql)
	body = strings.TrimLeft(body, " \t\r\n")

	if body != "" {
		scanBody(body, e)
	}

	for _, entry := range preludeEntries {
		isSlotName := false
		if _, ok := e.Params[entry.name]; ok {
			isSlotName = true
		}
		if isSlotName || strings.HasPrefix(entry.name, ParamPrefix) {
			p := e.Params[entry.name]
			p.Name = entry.name
			p.Raw = entry.raw
			e.Params[entry.name] = p
			continue
		}
		e.SessionSettings[entry.name] = Setting{Name: entry.name, Raw: entry.raw}
	}
	return
}

type preludeEntry struct {
	name string
	raw  string
}

// harvestSetPrelude pulls leading `SET key = value;` lines out of sql,
// returning their (name, raw) pairs and the remaining body. A single line
// may carry several semicolon-separated SET statements; the split is
// quote-aware so a `;` inside a string value never terminates a statement.
// A line that does not parse cleanly in its entirety is left to the body —
// half-harvesting would silently drop or corrupt statements.
func harvestSetPrelude(sql string) (entries []preludeEntry, body string) {
	lines := strings.Split(sql, "\n")
	consumed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			consumed++
			continue
		}
		lineEntries, ok := parseSetLine(trimmed)
		if !ok {
			break
		}
		entries = append(entries, lineEntries...)
		consumed++
	}
	if consumed >= len(lines) {
		return entries, ""
	}
	return entries, strings.Join(lines[consumed:], "\n")
}

// parseSetLine matches one or more `SET key = value;` statements on a
// single line (case-insensitive SET, `=` with or without surrounding
// spaces). ok is false — and no entries are returned — unless the WHOLE
// line consists of SET statements.
func parseSetLine(line string) (entries []preludeEntry, ok bool) {
	// A statement whose string value spans lines (SET a = 'x\ny';) cannot
	// be split line-wise — an unterminated quote on this line means the
	// whole prelude attempt stops here and the statement stays in the
	// body, where the grammar parses it correctly.
	if lineHasUnterminatedQuote(line) {
		return nil, false
	}
	rest := line
	for {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		if len(rest) < 4 || !strings.EqualFold(rest[:3], "SET") || (rest[3] != ' ' && rest[3] != '\t') {
			return nil, false
		}
		stmt := rest[4:]
		end := indexOutsideQuotes(stmt, ';')
		if end >= 0 {
			rest = stmt[end+1:]
			stmt = stmt[:end]
		} else {
			rest = ""
		}
		eqIdx := indexOutsideQuotes(stmt, '=')
		if eqIdx < 0 {
			return nil, false
		}
		name := strings.TrimSpace(stmt[:eqIdx])
		raw := strings.TrimSpace(stmt[eqIdx+1:])
		if name == "" || raw == "" || !plausibleSettingName(name) {
			return nil, false
		}
		entries = append(entries, preludeEntry{name: name, raw: raw})
	}
	if len(entries) == 0 {
		return nil, false
	}
	return entries, true
}

// plausibleSettingName reports whether name can be a SET key: a bare
// identifier-shaped name (keywords included — the grammar's identifier
// rule tolerates them) or a quoted spelling. Garbage like `0` or names
// with spaces reject the line so it stays in the body and fails loudly
// through the parser instead of round-tripping as invalid SET output.
func plausibleSettingName(name string) bool {
	c := name[0]
	if c == '"' || c == '`' {
		return len(name) >= 2 && name[len(name)-1] == c
	}
	if !(c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// lineHasUnterminatedQuote reports whether a quoted region opened on this
// line is still open at its end.
func lineHasUnterminatedQuote(s string) bool {
	for i := 0; i < len(s); {
		ch := s[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			q := ch
			i++
			closed := false
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == q {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return true
			}
			continue
		}
		i++
	}
	return false
}

// indexOutsideQuotes returns the index of the first occurrence of c in s
// that is not inside a single-quoted string, double-quoted identifier, or
// backquoted identifier (backslash escapes and doubled closing quotes
// respected). Returns -1 if none.
func indexOutsideQuotes(s string, c byte) int {
	for i := 0; i < len(s); {
		ch := s[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			q := ch
			i++
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == q {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if ch == c {
			return i
		}
		i++
	}
	return -1
}

// scanBody parses body and populates env.Params Type from slot occurrences,
// env.StatementSettings from the inline SETTINGS clause, and env.Format
// from the FORMAT clause. The body is not rewritten — these are read-only
// observations that consumers may use to inform their behaviour.
func scanBody(body string, e *Environment) {
	input := antlr.NewInputStream(body)
	lexer := grammar1.NewClickHouseLexer(input)
	// Best-effort scan: diagnostics are not surfaced, but the default
	// listeners print to stderr — drop them on the lexer too.
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)
	parser.RemoveErrorListeners()
	tree := parser.QueryStmt()

	walkCST(tree, func(ctx antlr.ParserRuleContext) bool {
		if slot, ok := ctx.(*grammar1.ParamSlotContext); ok {
			name, typ := splitParamSlotText(slot.GetText())
			if name != "" {
				p := e.Params[name]
				p.Name = name
				if typ != "" {
					p.Type = typ
				}
				e.Params[name] = p
			}
			return true
		}
		if sc, ok := ctx.(*grammar1.SettingsClauseContext); ok {
			collectSettingsClause(sc, e)
			return false
		}
		return true
	})

	if root, ok := tree.(antlr.ParserRuleContext); ok {
		hasFormatToken := false
		for i := 0; i < root.GetChildCount(); i++ {
			child := root.GetChild(i)
			if tn, isTerm := child.(antlr.TerminalNode); isTerm {
				if tn.GetSymbol().GetTokenType() == grammar1.ClickHouseParserGrammar1FORMAT {
					hasFormatToken = true
				}
				continue
			}
			if ioc, isIOC := child.(*grammar1.IdentifierOrNullContext); isIOC && hasFormatToken {
				e.Format = strings.TrimSpace(ioc.GetText())
				return
			}
		}
	}
}

// collectSettingsClause walks a SettingsClauseContext and populates
// env.StatementSettings with each k=v entry. Values are kept as raw text.
func collectSettingsClause(sc *grammar1.SettingsClauseContext, e *Environment) {
	for i := 0; i < sc.GetChildCount(); i++ {
		exprList, ok := sc.GetChild(i).(*grammar1.SettingExprListContext)
		if !ok {
			continue
		}
		for j := 0; j < exprList.GetChildCount(); j++ {
			expr, ok := exprList.GetChild(j).(*grammar1.SettingExprContext)
			if !ok {
				continue
			}
			name, raw := splitSettingExpr(expr)
			if name == "" {
				continue
			}
			e.StatementSettings[name] = Setting{Name: name, Raw: raw}
		}
	}
}

// splitSettingExpr extracts (name, raw value text) from a SettingExpr.
func splitSettingExpr(expr *grammar1.SettingExprContext) (name string, raw string) {
	for i := 0; i < expr.GetChildCount(); i++ {
		if ident, ok := expr.GetChild(i).(*grammar1.IdentifierContext); ok {
			name = ident.GetText()
			break
		}
	}
	for i := 0; i < expr.GetChildCount(); i++ {
		child := expr.GetChild(i)
		if prc, ok := child.(antlr.ParserRuleContext); ok {
			if _, isIdent := prc.(*grammar1.IdentifierContext); isIdent {
				continue
			}
			raw = prc.GetText()
			return
		}
	}
	return
}

// walkCST does a depth-first walk over node, invoking fn for each
// ParserRuleContext. Returning false from fn skips the subtree.
func walkCST(node antlr.Tree, fn func(antlr.ParserRuleContext) bool) {
	if node == nil {
		return
	}
	if ctx, ok := node.(antlr.ParserRuleContext); ok {
		if !fn(ctx) {
			return
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		child := node.GetChild(i)
		if child == nil {
			continue
		}
		walkCST(child, fn)
	}
}

// splitParamSlotText takes the textual form of a param slot — e.g.
// `{a:UInt64}` — and returns name and type.
func splitParamSlotText(s string) (name, typ string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return
	}
	inner := s[1 : len(s)-1]
	colon := strings.IndexByte(inner, ':')
	if colon < 0 {
		return
	}
	name = strings.TrimSpace(inner[:colon])
	typ = strings.TrimSpace(inner[colon+1:])
	return
}
