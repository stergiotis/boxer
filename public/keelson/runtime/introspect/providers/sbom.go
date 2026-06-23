package providers

import (
	"encoding/json"
	"os"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// SbomPath points at a CycloneDX SBOM JSON to expose as keelson.sbom
// (ADR-0094 §SD8). Empty (the default) yields a zero-row table — the
// SBOM is a build artefact that may be absent in a dev run, so the
// table degrades to empty rather than failing the query.
var SbomPath = env.NewString(env.Spec{
	Name:        "KEELSON_INTROSPECT_SBOM_PATH",
	Description: "path to a CycloneDX SBOM JSON exposed as the keelson.sbom introspection table; empty disables the table",
	Category:    env.CategorySystem,
})

// cyclonedxDoc is the minimal subset of CycloneDX 1.x that keelson.sbom
// reads. A bespoke struct keeps the sbom provider free of any dependency
// on the licensegate package's private SBOM types.
type cyclonedxDoc struct {
	Components []struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Purl     string `json:"purl"`
		Licenses []struct {
			License struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"license"`
		} `json:"licenses"`
	} `json:"components"`
}

type sbomComponent struct {
	name     string
	version  string
	purl     string
	licenses []string
}

// loadSbomComponents reads and parses the SBOM at SbomPath. Any failure
// (unset path, unreadable file, malformed JSON) yields no components —
// the table is best-effort, never a query error.
func loadSbomComponents() (comps []sbomComponent) {
	path, _ := SbomPath.Lookup()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc cyclonedxDoc
	if json.Unmarshal(data, &doc) != nil {
		return
	}
	comps = make([]sbomComponent, 0, len(doc.Components))
	for _, c := range doc.Components {
		lic := make([]string, 0, len(c.Licenses))
		for _, l := range c.Licenses {
			id := l.License.ID
			if id == "" {
				id = l.License.Name
			}
			if id != "" {
				lic = append(lic, id)
			}
		}
		comps = append(comps, sbomComponent{name: c.Name, version: c.Version, purl: c.Purl, licenses: lic})
	}
	return
}

// sbomProvider exposes the build SBOM as keelson.sbom.
type sbomProvider struct{}

func (sbomProvider) Name() string                        { return "sbom" }
func (sbomProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (sbomProvider) Schema() *arrow.Schema               { return sbomTable(nil).Schema() }

func (sbomProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	comps := loadSbomComponents()
	return sbomTable(comps).Build(proj, len(comps)), nil
}

func sbomTable(comps []sbomComponent) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return comps[i].name }).
		String("version", func(i int) string { return comps[i].version }).
		String("purl", func(i int) string { return comps[i].purl }).
		StringList("licenses", func(i int) []string { return comps[i].licenses })
}
