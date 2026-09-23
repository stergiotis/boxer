package promptbook

import (
	"math"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableName is the introspection table of every registered prompt
// document (ADR-0254 §SD4): what a model may be asked to do in this
// process, one row per document, a failed one included with its error.
// keelson('llm_calls').purpose is `book/slug`, so the two join exactly.
const TableName = "llm_prompts"

// RegisterIntrospect registers keelson('llm_prompts') on reg.
func RegisterIntrospect(reg *introspect.Registry) (err error) {
	return reg.Register(promptsProvider{})
}

// promptsProvider is live: a book can register until every package's init
// has run, and the walk over embedded documents is cheap.
type promptsProvider struct{}

func (promptsProvider) Name() string                         { return TableName }
func (promptsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (promptsProvider) Schema() *arrow.Schema                { return promptsTable(nil).Schema() }

func (promptsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	docs := AllDocuments()
	rec = promptsTable(docs).Build(proj, len(docs))
	return
}

func promptsTable(rows []Document) *introspect.Table {
	return introspect.NewTable().
		String("book", func(i int) string { return rows[i].BookId }).
		String("path", func(i int) string { return rows[i].Path }).
		String("slug", func(i int) string { return rows[i].Def.Slug }).
		String("purpose", func(i int) string {
			if rows[i].Err != nil {
				return ""
			}
			return rows[i].Def.Purpose()
		}).
		String("title", func(i int) string { return rows[i].Def.Title }).
		String("summary", func(i int) string { return rows[i].Def.Summary }).
		String("icon", func(i int) string { return rows[i].Def.Icon }).
		String("scope", func(i int) string {
			if rows[i].Err != nil {
				return ""
			}
			return rows[i].Def.Scope.Name()
		}).
		Float64("temperature", func(i int) float64 {
			if t := rows[i].Def.Temperature; t != nil {
				return float64(*t)
			}
			return math.NaN()
		}).
		Int64("max_tokens", func(i int) int64 { return int64(rows[i].Def.MaxTokens) }).
		String("system", func(i int) string { return rows[i].Def.System }).
		String("parse_error", func(i int) string {
			if rows[i].Err != nil {
				return rows[i].Err.Error()
			}
			return ""
		})
}
