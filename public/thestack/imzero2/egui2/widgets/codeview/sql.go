package codeview

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// sqlColors is the per-category palette for the ClickHouse-DSL highlighter
// (VS Code dark+ inspired). Retained holders are interned at init() and
// reused across frames.
var sqlColors [highlight.CatParamSlot + 1]color.Color

// sqlSpec is the highlighter spec consumed by build / buildLines.
var sqlSpec highlighterSpec

// sqlLexSpec is the lex-only spec behind BuildSqlLex (ADR-0130 editor path).
var sqlLexSpec highlighterSpec

func init() {
	defaultColor := internRgb(212, 212, 212) // light gray
	blue := internRgb(86, 156, 214)
	teal := internRgb(78, 201, 176)
	lightBlue := internRgb(156, 220, 254)
	yellow := internRgb(220, 220, 170)
	orange := internRgb(206, 145, 120)
	green := internRgb(181, 206, 168)
	dimGreen := internRgb(106, 153, 85)

	sqlColors[highlight.CatPlain] = defaultColor
	sqlColors[highlight.CatKeyword] = blue
	sqlColors[highlight.CatOperator] = blue
	sqlColors[highlight.CatIdentifier] = defaultColor
	sqlColors[highlight.CatTableName] = teal
	sqlColors[highlight.CatTableAlias] = teal
	sqlColors[highlight.CatColumnName] = lightBlue
	sqlColors[highlight.CatColumnAlias] = lightBlue
	sqlColors[highlight.CatCTEName] = teal
	sqlColors[highlight.CatFunctionName] = yellow
	sqlColors[highlight.CatDatabaseName] = teal
	sqlColors[highlight.CatTypeName] = teal
	sqlColors[highlight.CatStringLit] = orange
	sqlColors[highlight.CatNumberLit] = green
	sqlColors[highlight.CatPunctuation] = defaultColor
	sqlColors[highlight.CatComment] = dimGreen
	sqlColors[highlight.CatWhitespace] = defaultColor
	sqlColors[highlight.CatParamSlot] = yellow

	sqlSpec = highlighterSpec{
		highlight:   sqlHighlight,
		gutterColor: defaultColor,
		plainColor:  defaultColor,
	}
	sqlLexSpec = highlighterSpec{
		highlight:   sqlLexHighlight,
		gutterColor: defaultColor,
		plainColor:  defaultColor,
	}
}

func sqlHighlight(src string) (out []section) {
	return sqlSpansToSections(highlight.Highlight(src))
}

// sqlLexHighlight is the lex-only variant: token classification plus the
// function-name lookahead, no parse. Same palette, so a buffer upgraded
// later by the semantic tier only changes identifier colors.
func sqlLexHighlight(src string) (out []section) {
	return sqlSpansToSections(highlight.HighlightLex(src))
}

func sqlSpansToSections(spans []highlight.Span) (out []section) {
	out = make([]section, len(spans))
	for i, s := range spans {
		out[i] = section{
			start: uint32(s.Start),
			stop:  uint32(s.Stop),
			col:   sqlColors[s.Category],
		}
	}
	return
}

// BuildSql highlights SQL and returns a retained CodeViewJob. Every call
// re-tokenises — and SQL is the expensive one: highlight.Highlight runs a full
// nanopass.Parse plus a CST walk, so this is ~129 µs for a one-line query and
// ~3.5 ms for a three-line CTE. Use it for one-shot work, or when you already
// hold a cheaper key than the SQL text. Use [PrepareSql] otherwise.
func BuildSql(sql string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(sqlSpec, sql)
}

// PrepareSql highlights SQL through the package memo: the same statement
// prepared again returns the same retained holder without re-parsing
// (ADR-0125). Prefer this anywhere the same SQL is shown across frames.
func PrepareSql(sql string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return memo.prepare(memoKey{lang: langSQL, src: sql}, func() job {
		return build(sqlSpec, sql)
	})
}

// BuildSqlLex highlights SQL at the lexical tier only (no parse): keywords,
// literals, comments, operators, and peek-ahead function names — ~26 µs for a
// 180 B statement vs ~5.7 ms for the full semantic build. This is the
// per-keystroke span source for the ADR-0130 editor path
// (TextEdit.HighlightJob), which is why it is deliberately uncached: editor
// content is new on every keystroke, so the ADR-0125 memo would only churn.
//
// The job's text must equal the editor buffer byte-for-byte for the Rust-side
// reconcile to line up, so — unlike the read-only builders — no tab expansion
// is applied.
func BuildSqlLex(sql string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return build(sqlLexSpec, sql)
}

// BuildSqlFromSpans serializes highlighter spans somebody else already
// computed into a retained CodeViewJob with the SQL palette. This is the
// render-thread half of the ADR-0130 L2 split: a background goroutine runs
// the expensive highlight.Highlight (pure Go, no c.* calls), and the render
// thread pays only this serialization. `sql` must be the exact text the
// spans describe — like BuildSqlLex, no tab expansion is applied.
func BuildSqlFromSpans(sql string, spans []highlight.Span) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	return buildFromSections(sql, sqlSpansToSections(spans))
}
