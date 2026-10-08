package play

// The checked constructor of an ad-hoc bundle (ADR-0288 §SD2):
// a producer states what the bundle is — its SQL in a pane's shape, the
// panes, the datasets by local name, where the rows came from — and gets a
// document composed the one way, parsed by the applet parser before it is
// published, and provenance the service records as data. It lives in play
// rather than sqlapplet because play's publish_result is a producer and
// sqlapplet imports play; the parser it checks with is sqlapplet's, which
// sqlapplet installs (SetAppletDocParser).

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// BundleSpec is a bundle as its producer states it.
type BundleSpec struct {
	// Alias is the bundle's alias, without a double underscore.
	Alias string
	// Title and Summary head the document; the alias and a generic line
	// when empty.
	Title   string
	Summary string
	// Sql is the bundle's own statement over keelson('<local name>'), in
	// the shape the panes in Tabs draw; SELECT * of the first dataset when
	// empty. It may read only the bundle's datasets.
	Sql string
	// Tabs are the panes the bundle opens on, as play's tab registry names
	// them; table when empty.
	Tabs []string
	// Prose is markdown the document carries after its SQL, for the person
	// who opens it. It may not hold a code fence.
	Prose string
	// Datasets are the bundle's datasets by local name.
	Datasets []adhocdata.BundleDatasetInput
	// SourceSql and InputHandles are where the rows came from: the
	// statement as the producer ran it, and the datasets it read.
	SourceSql    string
	InputHandles []string
	// KeepAfterClose and OnBehalfOf are the publish's, as
	// adhocdata.BundlePublishInput has them.
	KeepAfterClose bool
	OnBehalfOf     *app.OnBehalfOf
}

// ComposeBundleDoc writes spec's applet document and checks it with the
// installed parser: it parses, it reads its datasets under the local names
// the bundle carries and no others, and it asks for the introspection
// endpoint, where they resolve.
func ComposeBundleDoc(spec BundleSpec) (doc []byte, err error) {
	if len(spec.Datasets) == 0 {
		return nil, eb.Build().Str("bundle", spec.Alias).Errorf("a bundle carries one or more datasets")
	}
	locals := make([]string, 0, len(spec.Datasets))
	for _, d := range spec.Datasets {
		if !validDatasetIdentifier(d.LocalName) {
			return nil, eb.Build().Str("bundle", spec.Alias).Str("localName", d.LocalName).Errorf("a local name is a bare identifier")
		}
		locals = append(locals, d.LocalName)
	}
	if strings.Contains(spec.Prose, "```") {
		return nil, eb.Build().Str("bundle", spec.Alias).Errorf("a bundle's prose holds no code fence: the first sql fence is its buffer")
	}
	title := spec.Title
	if title == "" {
		title = spec.Alias
	}
	summary := spec.Summary
	if summary == "" {
		summary = "an ad-hoc bundle"
	}
	sql := strings.TrimSpace(spec.Sql)
	if sql == "" {
		sql = "SELECT * FROM keelson('" + locals[0] + "')"
	}
	tabs := spec.Tabs
	if len(tabs) == 0 {
		tabs = []string{"table"}
	}
	for _, tab := range tabs {
		if !validDatasetIdentifier(tab) {
			return nil, eb.Build().Str("bundle", spec.Alias).Str("tab", tab).Errorf("a pane is named by its registry slug")
		}
	}

	var b strings.Builder
	b.WriteString("---\ntype: reference\nstatus: draft\ntitle: ")
	b.WriteString(yamlQuote(title))
	b.WriteString("\nsummary: ")
	b.WriteString(yamlQuote(summary))
	b.WriteString("\nendpoint: introspection\ndatasets: [")
	b.WriteString(strings.Join(locals, ", "))
	b.WriteString("]\ntabs: [")
	b.WriteString(strings.Join(tabs, ", "))
	b.WriteString("]\n---\n\n# ")
	b.WriteString(strings.ReplaceAll(title, "\n", " "))
	b.WriteString("\n\n```sql\n")
	b.WriteString(sql)
	b.WriteString("\n```\n")
	if prose := strings.TrimSpace(spec.Prose); prose != "" {
		b.WriteString("\n")
		b.WriteString(prose)
		b.WriteString("\n")
	}
	doc = []byte(b.String())

	parsed, err := parseAppletDoc(spec.Alias+".md", doc)
	if err != nil {
		return nil, eh.Errorf("the bundle's document does not parse: %w", err)
	}
	switch {
	case !slices.Equal(parsed.Datasets, locals):
		return nil, eb.Build().Str("bundle", spec.Alias).Errorf("the document's datasets are not the bundle's")
	case !parsed.Introspection:
		return nil, eb.Build().Str("bundle", spec.Alias).Errorf("the document does not ask for the introspection endpoint")
	}
	for _, name := range keelsonsql.References(parsed.Sql) {
		if !slices.Contains(locals, name) {
			return nil, eb.Build().Str("bundle", spec.Alias).Str("table", name).Errorf("the bundle's SQL reads keelson('%s'), which the bundle does not carry", name) //boxer:lint disable=CS013 reason="the refusal reaches the model as text and names the table to fix"
		}
	}
	return doc, nil
}

// PublishBundle composes and checks spec's document, then publishes the
// bundle with its provenance (ADR-0288 §SD2, §SD5). It is a bus
// round trip: call it off the render goroutine.
func PublishBundle(bus app.BusI, spec BundleSpec) (res adhocdata.BundleResult, err error) {
	doc, err := ComposeBundleDoc(spec)
	if err != nil {
		return
	}
	return adhocdata.PublishBundleRequest(bus, adhocdata.BundlePublishInput{
		Alias: spec.Alias, Document: doc, Datasets: spec.Datasets,
		KeepAfterClose: spec.KeepAfterClose, OnBehalfOf: spec.OnBehalfOf,
		Provenance: adhocdata.BundleProvenance{SourceSql: spec.SourceSql, InputHandles: spec.InputHandles},
	})
}

// yamlQuote quotes s as a YAML double-quoted scalar, escaping what a
// double-quoted scalar may not hold as it is.
func yamlQuote(s string) (q string) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			b.WriteString(`\x`)
			b.WriteByte("0123456789abcdef"[r>>4])
			b.WriteByte("0123456789abcdef"[r&0xf])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
