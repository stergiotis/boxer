package harness

import (
	"slices"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// A coordinator's tools are declared where it lives (the chat's are a Go
// registry in that package) and served as one introspection table for the
// host (ADR-0315, update of 2026-10-13): keelson('agent_tools'), one row
// per tool a coordinator may offer its model, in the order the model sees
// them. The per-window operation tools a coordinator mints from the apps'
// catalogs are not rows: they are keelson('app_operations') under another
// name. An owner publishes its set once, at start.

// TableTools is keelson('agent_tools').
const TableTools = "agent_tools"

// ToolRow is one published tool.
type ToolRow struct {
	App  string
	Name string
	// Group says when the tool is offered, in the owner's words.
	Group       string
	Description string
	// Schema is the parameter schema the model sees, as JSON.
	Schema string
	// Budget bounds the bytes of the result the model reads; zero for
	// none.
	Budget       int
	ParallelSafe bool
}

var (
	toolsMu sync.Mutex
	tools   = map[string][]ToolRow{}
)

// PublishTools sets an app's tools, in the order its model sees them; a
// second publication replaces the first.
func PublishTools(app string, rows []ToolRow) {
	rows = slices.Clone(rows)
	for i := range rows {
		rows[i].App = app
	}
	toolsMu.Lock()
	defer toolsMu.Unlock()
	tools[app] = rows
}

// PublishedTools is the table's rows, by app, each app's in its order.
func PublishedTools() (rows []ToolRow) {
	toolsMu.Lock()
	defer toolsMu.Unlock()
	apps := make([]string, 0, len(tools))
	for a := range tools {
		apps = append(apps, a)
	}
	slices.Sort(apps)
	for _, a := range apps {
		rows = append(rows, tools[a]...)
	}
	return
}

type toolsProvider struct{}

func (toolsProvider) Name() string                         { return TableTools }
func (toolsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (toolsProvider) Schema() *arrow.Schema                { return toolsTable(nil).Schema() }
func (toolsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	rows := PublishedTools()
	rec = toolsTable(rows).Build(proj, len(rows))
	return
}

func toolsTable(rows []ToolRow) *introspect.Table {
	r := func(i int) *ToolRow { return &rows[i] }
	ord := func(i int) int32 {
		n := int32(0)
		for j := i - 1; j >= 0 && rows[j].App == rows[i].App; j-- {
			n++
		}
		return n
	}
	return introspect.NewTable().
		String("app", func(i int) string { return r(i).App }).
		// The tool's place in the list the model sees, from zero.
		Int32("ordinal", ord).
		String("name", func(i int) string { return r(i).Name }).
		String("group", func(i int) string { return r(i).Group }).
		String("description", func(i int) string { return r(i).Description }).
		String("schema", func(i int) string { return r(i).Schema }).
		Int64("budget", func(i int) int64 { return int64(r(i).Budget) }).
		Bool("parallel_safe", func(i int) bool { return r(i).ParallelSafe })
}
