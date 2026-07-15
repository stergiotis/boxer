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
}

func sqlHighlight(src string) (out []section) {
	spans := highlight.Highlight(src)
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
