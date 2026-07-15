package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/packageprops"
)

// packageCapsProvider exposes each linked package's capability verdict
// (ADR-0120) as keelson.package_capabilities — what privileged operations this
// binary's code can reach, and which packages bring them in.
//
// The rows come from the packageprops runtime registry, not the whole-repo
// static table: every package_props.go self-registers from init, so the registry
// is exactly what is compiled into *this* binary. That makes the table answer
// "what can this process do?" rather than "what does the repository contain",
// which is the question the rest of the keelson tables answer about themselves.
//
// Static: the verdicts are compiled-in declarations, and the registry is fully
// populated once init has run.
//
// Read caps_direct first. caps_reachable is the closure — nearly every package
// reaches nearly everything through the standard library — so as a positive
// claim it says little; its value is in the negative, where an absent capability
// proves the package cannot reach it by any path:
//
//	SELECT import_path FROM keelson('package_capabilities')
//	WHERE surveyed AND NOT has(caps_reachable, 'network')
//
// For one row per package × capability, arrayJoin the column.
//
// Under the boxer_disable_packagecaps build tag the table stays registered with
// this schema but yields no rows (ADR-0120 SD9) — see packagecaps_disabled.go.
type packageCapsProvider struct{}

func (packageCapsProvider) Name() string                         { return "package_capabilities" }
func (packageCapsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (packageCapsProvider) Schema() *arrow.Schema                { return packageCapsTable(nil).Schema() }

func (packageCapsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := packageCapsRows()
	return packageCapsTable(rows).Build(proj, len(rows)), nil
}

func packageCapsTable(rows packageprops.Table) *introspect.Table {
	return introspect.NewTable().
		String("import_path", func(i int) string { return rows[i].ImportPath }).
		// surveyed distinguishes "the capability survey ran and found nothing"
		// from "no verdict recorded". Without it an empty caps_direct would be
		// ambiguous, and callers would read an unsurveyed package as safe.
		Bool("surveyed", func(i int) bool { return rows[i].Props.CapsDirect.Surveyed() }).
		Bool("safe", func(i int) bool { return rows[i].Props.CapsDirect.Safe() }).
		StringList("caps_direct", func(i int) []string { return rows[i].Props.CapsDirect.Names() }).
		StringList("caps_reachable", func(i int) []string { return rows[i].Props.CapsReachable.Names() })
}
