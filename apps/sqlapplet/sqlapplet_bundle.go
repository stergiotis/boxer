package sqlapplet

import (
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play opens ad-hoc bundles (ADR-0288 §SD4) whose documents are
// applet documents; this package owns their parser and play cannot import
// it, so it hands play the parser when it is linked.
func init() {
	play.SetAppletDocParser(parseBundleDoc)
}

// parseBundleDoc parses a bundle's applet document into what a play window
// applies.
func parseBundleDoc(path string, src []byte) (doc play.AppletDoc, err error) {
	def, err := ParseDocSource("bundle", bundleDocPath(path), src)
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

// bundleDocPath is the path a bundle's document is parsed under: its base
// name made an applet slug — lowercase, every run of other characters one
// hyphen. A bundle's identity is its alias, which may hold capitals and
// underscores; the slug is the applet book's file-name rule and means
// nothing for a bundle, so the alias must not fail it.
func bundleDocPath(path string) (slugged string) {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			hyphen = false
		} else if !hyphen && b.Len() > 0 {
			b.WriteByte('-')
			hyphen = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if slug == "" {
		slug = "bundle"
	}
	return slug + ".md"
}
