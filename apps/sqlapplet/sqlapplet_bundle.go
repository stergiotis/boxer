package sqlapplet

import (
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play opens ad-hoc bundles (ADR-0288 (proposed) §SD4) whose documents are
// applet documents; this package owns their parser and play cannot import
// it, so it hands play the parser when it is linked.
func init() {
	play.SetAppletDocParser(parseBundleDoc)
}

// parseBundleDoc parses a bundle's applet document into what a play window
// applies.
func parseBundleDoc(path string, src []byte) (doc play.AppletDoc, err error) {
	def, err := ParseDocSource("bundle", path, src)
	if err != nil {
		return
	}
	if def == nil {
		// A document with no sql fence is a prose page: nothing to run.
		return doc, eb.Build().Str("path", path).Errorf("a bundle's document has no sql fence")
	}
	doc = play.AppletDoc{Title: def.Title, Sql: def.SQL, BandsSql: def.BandsSQL, Datasets: def.Datasets,
		Introspection: def.Endpoint == EndpointIntrospection, Preamble: []byte(def.Preamble),
		Runnable: def.Class == analysis.QuerySecurityRead}
	if len(def.Tabs) > 0 {
		doc.Tab = def.Tabs[0].ID
	}
	return
}
