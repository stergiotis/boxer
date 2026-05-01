//go:build llm_generated_opus47

package env

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
)

// ParamPrefix identifies a SET-line whose key denotes a parameter (e.g.
// `SET param_x = 5;`). Anything else under SET is a session setting.
const ParamPrefix = "param_"

// Extract splits SQL into an Environment and a body. The Environment owns:
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
// returning their (name, raw) pairs and the remaining body.
func harvestSetPrelude(sql string) (entries []preludeEntry, body string) {
	lines := strings.Split(sql, "\n")
	consumed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			consumed++
			continue
		}
		name, raw, ok := parseSetLine(trimmed)
		if !ok {
			break
		}
		entries = append(entries, preludeEntry{name: name, raw: raw})
		consumed++
	}
	if consumed >= len(lines) {
		return entries, ""
	}
	return entries, strings.Join(lines[consumed:], "\n")
}

// parseSetLine matches `SET key = value;`. Returns name and raw value; ok
// false if the line is not a SET statement.
func parseSetLine(line string) (name string, raw string, ok bool) {
	const prefix = "SET "
	if !strings.HasPrefix(line, prefix) && !strings.HasPrefix(line, "set ") {
		return
	}
	rest := line[len(prefix):]
	eqIdx := strings.Index(rest, " = ")
	if eqIdx < 0 {
		return
	}
	name = strings.TrimSpace(rest[:eqIdx])
	raw = strings.TrimSpace(rest[eqIdx+3:])
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if name == "" {
		return
	}
	ok = true
	return
}

// scanBody parses body and populates env.Params Type from slot occurrences,
// env.StatementSettings from the inline SETTINGS clause, and env.Format
// from the FORMAT clause. The body is not rewritten — these are read-only
// observations that consumers may use to inform their behaviour.
func scanBody(body string, e *Environment) {
	input := antlr.NewInputStream(body)
	lexer := grammar1.NewClickHouseLexer(input)
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
		walkCST(child.(antlr.Tree), fn)
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
