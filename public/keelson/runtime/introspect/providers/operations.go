package providers

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableAppOperations names the operations table; it equals
// agent.TableOperations, kept a literal so this package need not import the
// agent service.
const TableAppOperations = "app_operations"

// operationsProvider exposes every registered operations catalog as
// keelson.app_operations (ADR-0269 §SD2): what each app lets a caller do,
// before any window is open. A catalog withdrawn at registration appears as
// one row with an empty operation and the diagnostic.
type operationsProvider struct{}

func (operationsProvider) Name() string                         { return TableAppOperations }
func (operationsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (operationsProvider) Schema() *arrow.Schema                { return operationsTable(nil, nil).Schema() }

func (operationsProvider) Snapshot(ctx context.Context, proj introspect.Projection) (arrow.RecordBatch, error) {
	regs := app.AllRegistrations()
	vs := appops.Views(regs)
	return operationsTable(vs, linkExamplesOf(regs)).Build(proj, len(vs)), nil
}

// linkExamplesOf is, in the order appops.Views lists operations, each
// operation's codec examples spelled as links (ADR-0409, proposed).
func linkExamplesOf(regs []app.Registration) (out [][]string) {
	for _, r := range regs {
		if r.Manifest.Operations == nil {
			continue
		}
		for _, o := range r.Manifest.Operations.Operations {
			out = append(out, applink.Examples(r.Manifest, o))
		}
	}
	return
}

func operationsTable(vs []appops.View, examples [][]string) *introspect.Table {
	v := func(i int) *appops.View { return &vs[i] }
	return introspect.NewTable().
		String("app_id", func(i int) string { return string(v(i).App) }).
		String("app_display", func(i int) string { return v(i).AppDisplay }).
		String("operation", func(i int) string { return v(i).Name }).
		Int32("version", func(i int) int32 { return int32(v(i).Version) }).
		String("summary", func(i int) string { return v(i).Summary }).
		// query, external_read or command (ADR-0269 §SD1).
		String("class", func(i int) string { return v(i).Class.String() }).
		// none, view, document, run or consequential (§SD5): what a grant's
		// mode is checked against.
		String("effect", func(i int) string { return v(i).Effect.String() }).
		StringList("reads", func(i int) []string { return v(i).Reads }).
		StringList("writes", func(i int) []string { return v(i).Writes }).
		StringList("refs", func(i int) []string { return v(i).Refs }).
		StringList("follows", func(i int) []string { return v(i).Follows }).
		// agents is false unless the app declared the operation for agents.
		Bool("agents", func(i int) bool { return v(i).Agents }).
		Bool("untrusted", func(i int) bool { return v(i).Untrusted }).
		// The UI gesture that does the same; empty when there is none.
		String("gesture", func(i int) string { return v(i).Gesture }).
		// The grant destination that covers a consequential call without a
		// confirmation, e.g. publish:<bundle prefix>; empty when none.
		String("consent", func(i int) string { return v(i).Consent }).
		String("rebuild", func(i int) string { return v(i).Rebuild }).
		String("fork", func(i int) string { return v(i).Fork }).
		Bool("async", func(i int) bool { return v(i).Async }).
		String("dry_run", func(i int) string { return v(i).DryRun }).
		String("args_schema", func(i int) string { return v(i).ArgsSchema }).
		String("result_schema", func(i int) string { return v(i).ResultSchema }).
		// The operation takes boxer: links, the command an authorless link
		// calls instead of a run, and the codec's examples as links
		// (ADR-0409, proposed).
		Bool("links", func(i int) bool { return v(i).Links }).
		String("link_stage", func(i int) string { return v(i).LinkStage }).
		StringList("link_examples", func(i int) []string {
			if i < len(examples) {
				return examples[i]
			}
			return nil
		})
}
