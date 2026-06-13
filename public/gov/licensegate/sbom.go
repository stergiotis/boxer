package licensegate

import (
	"encoding/json"
	"fmt"
	"os"
)

// Minimal subset of the CycloneDX 1.6 schema needed for license-policy
// evaluation. Field names mirror the JSON keys verbatim.
//
// License information may appear in two places per component:
//   - components[].licenses[]:           asserted (cyclonedx-gomod -assert-licenses)
//   - components[].evidence.licenses[]:  detected (cyclonedx-gomod default)
//
// Both arrays are consumed and deduplicated; see ADR-0004 SD2.

type sbomT struct {
	Components []componentT `json:"components"`
}

type componentT struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Purl     string          `json:"purl"`
	Licenses []licenseEntryT `json:"licenses"`
	Evidence *evidenceT      `json:"evidence,omitempty"`
}

type evidenceT struct {
	Licenses []licenseEntryT `json:"licenses"`
}

type licenseEntryT struct {
	License *licenseObjT `json:"license,omitempty"`
}

type licenseObjT struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func loadSBOM(path string) (b sbomT, err error) {
	f, err := os.Open(path)
	if err != nil {
		err = fmt.Errorf("open SBOM %q: %w", path, err)
		return
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(f)
	err = dec.Decode(&b)
	if err != nil {
		err = fmt.Errorf("decode SBOM %q: %w", path, err)
		return
	}
	return
}

// licenseIDs returns the deduplicated set of SPDX identifiers attached
// to a component, drawing from both the asserted and the evidence
// arrays. Entries lacking an SPDX `id` are skipped (the free-form
// `name` field cannot be reliably classified).
func licenseIDs(c componentT) (ids []string) {
	seen := make(map[string]struct{}, 4)
	collect := func(entries []licenseEntryT) {
		for _, e := range entries {
			if e.License == nil || e.License.ID == "" {
				continue
			}
			_, dup := seen[e.License.ID]
			if dup {
				continue
			}
			seen[e.License.ID] = struct{}{}
			ids = append(ids, e.License.ID)
		}
	}
	collect(c.Licenses)
	if c.Evidence != nil {
		collect(c.Evidence.Licenses)
	}
	return
}
