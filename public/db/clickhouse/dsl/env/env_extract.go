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
//     SessionSettings — see SET-line classification rules below), and any
//     comments interleaved with it (env.PreludeComments).
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

	preludeEntries, comments, off := bodyStart(sql)
	body = sql[off:]
	e.PreludeComments = joinComments(sql, comments)

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

// BodyOffset returns the byte offset at which [Extract]'s body begins within
// sql, so that `sql[BodyOffset(sql):] == body`.
//
// A byte range recorded against the body — everything a nanopass pass sees,
// since [Pass.Run] hands passes the extracted body — maps back into the
// original SQL by adding this offset. Callers that slice the user's buffer
// with pass-recorded ranges need it.
//
// Costs the prelude scan only, not the CST walk Extract additionally runs.
func BodyOffset(sql string) int {
	_, _, off := bodyStart(sql)
	return off
}

// bodyStart is the single definition of where the body begins: the SET
// prelude is consumed whole lines at a time, so what remains is a
// byte-identical suffix of sql, and the leading whitespace skip only moves
// the start further right.
//
// Extract slices at this offset rather than trimming its own copy, which is
// what makes "the body is the suffix starting at BodyOffset" a definition
// instead of a promise two functions have to keep independently. The two used
// to spell the cutset separately; widening one of them to unicode whitespace
// would have skewed every pass-recorded range by the difference, silently.
func bodyStart(sql string) (entries []preludeEntry, comments []commentSpan, off int) {
	entries, comments, end := harvestSetPrelude(sql)
	return entries, comments, len(sql) - len(strings.TrimLeft(sql[end:], " \t\r\n"))
}

type preludeEntry struct {
	name string
	raw  string
}

// commentSpan is one comment's byte range in the SQL it was scanned from.
type commentSpan struct {
	start int
	end   int
}

// harvestSetPrelude pulls the `SET key = value;` prelude out of sql,
// returning its (name, raw) pairs, the comments interleaved with it, and the
// byte offset just past the last line carrying a SET.
//
// The prelude is the longest leading run of lines holding nothing but blanks,
// comments and SET statements, truncated to end at the last line carrying a
// SET (ADR-0006, 2026-08-15 Update). Both halves of that rule are
// load-bearing. Spanning comments is what keeps a `-- play: …` directive — or
// any header comment — from ending the prelude before it starts, which used to
// collapse BodyOffset to zero and strip a buffer's parameter bindings off its
// own run. Truncating at the last SET is what keeps a comment above a bare
// SELECT *in* the body, so a documented query does not shift every
// pass-recorded range by the length of its own header.
//
// A single line may carry several semicolon-separated SET statements; the
// split is quote-aware so a `;` inside a string value never terminates a
// statement. A line that does not parse cleanly in its entirety is left to the
// body — half-harvesting would silently drop or corrupt statements.
func harvestSetPrelude(sql string) (entries []preludeEntry, comments []commentSpan, end int) {
	// Comments are held back until a SET line commits them: the ones trailing
	// the last SET belong to the body, and until the next line is read there
	// is no telling which side of that edge they are on.
	var pending []commentSpan
	inBlock := false
	for pos := 0; pos <= len(sql); {
		lineEnd, next := len(sql), len(sql)+1
		if nl := strings.IndexByte(sql[pos:], '\n'); nl >= 0 {
			lineEnd, next = pos+nl, pos+nl+1
		}
		code, lineComments, stillInBlock, openQuote := scanPreludeLine(sql[pos:lineEnd], pos, inBlock)
		if openQuote {
			break
		}
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			pending = append(pending, lineComments...)
			inBlock = stillInBlock
			pos = next
			continue
		}
		lineEntries, ok := parseSetLine(trimmed)
		if !ok {
			break
		}
		comments = append(comments, pending...)
		comments = append(comments, lineComments...)
		pending = pending[:0]
		entries = append(entries, lineEntries...)
		inBlock = stillInBlock
		end = min(next, len(sql))
		pos = next
	}
	return entries, comments, end
}

// scanPreludeLine splits one candidate prelude line into the code left once
// its comments are removed and the byte ranges of the comments removed, base
// being the line's own offset within the SQL so those ranges index it.
//
// Quotes and comments are tracked in one left-to-right walk, which is the only
// way to get both right: a `--` inside a string literal does not open a
// comment, and an apostrophe inside a comment does not open a string. The
// comment forms are grammar1's — `--`, `//` and `/* … */` — deliberately not
// ClickHouse's `#`, which the lexer every downstream consumer uses does not
// take either.
//
// inBlock carries a `/* … */` opened on an earlier line and stillInBlock
// reports the state at this line's end, so a block comment may span the
// prelude. A closed block comment consumed mid-line leaves a space behind
// rather than nothing: it separates tokens in ClickHouse, and dropping it
// would silently splice `1/*c*/2` into the value `12`.
//
// openQuote reports a quoted region still open at the line's end. A SET whose
// string value spans lines cannot be split line-wise, so the caller ends the
// prelude there and the statement stays in the body, where the grammar parses
// it correctly.
func scanPreludeLine(line string, base int, inBlock bool) (code string, comments []commentSpan, stillInBlock, openQuote bool) {
	var b strings.Builder
	i := 0
	if inBlock {
		j := strings.Index(line, "*/")
		if j < 0 {
			return "", []commentSpan{{start: base, end: base + len(line)}}, true, false
		}
		comments = append(comments, commentSpan{start: base, end: base + j + 2})
		b.WriteByte(' ')
		i = j + 2
	}
	for i < len(line) {
		ch := line[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			start := i
			i++
			closed := false
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					i += 2
					continue
				}
				if line[i] == ch {
					if i+1 < len(line) && line[i+1] == ch {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			b.WriteString(line[start:i])
			if !closed {
				return b.String(), comments, false, true
			}
			continue
		}
		if i+1 < len(line) && (ch == '-' && line[i+1] == '-' || ch == '/' && line[i+1] == '/') {
			comments = append(comments, commentSpan{start: base + i, end: base + len(line)})
			return b.String(), comments, false, false
		}
		if i+1 < len(line) && ch == '/' && line[i+1] == '*' {
			j := strings.Index(line[i+2:], "*/")
			if j < 0 {
				comments = append(comments, commentSpan{start: base + i, end: base + len(line)})
				return b.String(), comments, true, false
			}
			comments = append(comments, commentSpan{start: base + i, end: base + i + 2 + j + 2})
			b.WriteByte(' ')
			i += 2 + j + 2
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return b.String(), comments, false, false
}

// joinComments renders the harvested comment spans as the text
// [Environment.Integrate] re-emits: each span on its own line, verbatim.
//
// A block comment spanning n lines arrives as n spans — one per line, each
// stopping at its line's end — so joining them with the newlines this puts
// back reproduces it byte for byte.
func joinComments(sql string, comments []commentSpan) string {
	if len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range comments {
		b.WriteString(sql[c.start:c.end])
		b.WriteByte('\n')
	}
	return b.String()
}

// parseSetLine matches one or more `SET key = value;` statements on a
// single line (case-insensitive SET, `=` with or without surrounding
// spaces). ok is false — and no entries are returned — unless the WHOLE
// line consists of SET statements.
//
// Takes the line with its comments already removed by [scanPreludeLine],
// which is also where the unterminated-quote rule lives: every quoted region
// here is closed.
func parseSetLine(line string) (entries []preludeEntry, ok bool) {
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
