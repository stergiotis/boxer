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

// Extract splits SQL into an Environment and a body. The Environment owns the
// leading SET-line prelude and a scan of `{name: Type}` slots in the body. The
// body is the remainder of the input, unparsed and not byte-identical to the
// input (leading SET lines are removed; whitespace between them is collapsed).
//
// SET-line classification:
//   - A SET whose key starts with [ParamPrefix] OR whose key matches the name
//     of a `{name: Type}` slot found in the body becomes an env.Params entry.
//   - Everything else becomes an env.SessionSettings entry.
//
// v1 limitations:
//   - Inline `... SETTINGS k=v` and `... FORMAT FormatName` clauses are NOT
//     stripped from body. Env.StatementSettings and Env.Format remain empty
//     unless a pass writes them.
//   - Param.Value is not deserialised here. Callers that need the Go value
//     must combine Param.Raw with Param.Type using a deserialiser of their
//     choice.
//
// Slot-scan is best-effort: if the body does not parse, Extract still
// returns a usable Environment based on the prelude alone.
func Extract(sql string) (e *Environment, body string, err error) {
	e = NewEnvironment()

	// First, harvest the prelude into a per-line collection without
	// classifying yet — we need to see body slots before deciding.
	preludeEntries, body := harvestSetPrelude(sql)
	body = strings.TrimLeft(body, " \t\r\n")

	if body != "" {
		scanParamSlots(body, e)
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
// returning their (name, raw) pairs and the remaining body. Classification
// into Params vs SessionSettings is done by the caller after the body has
// been scanned for slots.
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

// scanParamSlots parses the body and records the type of every {name: Type}
// slot it finds, merging into e.Params. Parse failures are silently ignored
// (best-effort scan).
func scanParamSlots(body string, e *Environment) {
	input := antlr.NewInputStream(body)
	lexer := grammar1.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)
	// Suppress error listener — best-effort parse.
	parser.RemoveErrorListeners()
	tree := parser.QueryStmt()
	walkCST(tree, func(ctx antlr.ParserRuleContext) bool {
		slot, ok := ctx.(*grammar1.ParamSlotContext)
		if !ok {
			return true
		}
		name, typ := splitParamSlotText(slot.GetText())
		if name == "" {
			return true
		}
		p := e.Params[name]
		p.Name = name
		if typ != "" {
			p.Type = typ
		}
		e.Params[name] = p
		return true
	})
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
