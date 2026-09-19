package licensegate

import (
	"encoding/json/v2"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The Rust half of the gate reads `cargo metadata --locked --format-version 1`
// rather than an SBOM (ADR-0246 SD1): the same document gov cargo-licenses
// reads, carrying each crate's own SPDX expression unflattened.

const (
	ecosystemGo    = "go"
	ecosystemCargo = "cargo"
)

// cargoMetadataT is the subset of `cargo metadata --format-version 1` the gate
// reads.
type cargoMetadataT struct {
	Packages []cargoPackageT `json:"packages"`
}

type cargoPackageT struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	License     string `json:"license"`
	LicenseFile string `json:"license_file"`
	// Source is null for a crate that lives in the workspace or is reached by
	// path, which is how boxer's own crates and its vendored path dependencies
	// appear. Only a crate with a source came from outside.
	Source string `json:"source"`
}

func loadCargoMetadata(path string) (md cargoMetadataT, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read cargo metadata: %w", err)
		return
	}
	err = json.Unmarshal(raw, &md)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("decode cargo metadata: %w", err)
		return
	}
	return
}

// cargoRows classifies the registry crates of one or more metadata documents.
// The four crate trees share much of their dependency graph, so a crate is
// classified once per name and version however many trees resolve it. A crate
// whose declaration is absent, unreadable or outside the policy map goes to
// unresolved rather than rows (ADR-0246 SD7).
func cargoRows(paths []string) (rows []rowT, unresolved []string, err error) {
	seen := make(map[string]struct{}, 512)
	rows = make([]rowT, 0, 512)
	unresolved = make([]string, 0, 8)
	for _, path := range paths {
		var md cargoMetadataT
		md, err = loadCargoMetadata(path)
		if err != nil {
			return
		}
		for _, p := range md.Packages {
			if p.Source == "" {
				continue
			}
			key := p.Name + "@" + p.Version
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}

			declared := strings.TrimSpace(p.License)
			if declared == "" {
				reason := "no license declared"
				if p.LicenseFile != "" {
					reason = "license-file only, no SPDX expression"
				}
				unresolved = append(unresolved, key+" (cargo: "+reason+")")
				continue
			}
			category, elected, evalErr := EvaluateExpressionE(declared)
			if evalErr != nil {
				unresolved = append(unresolved, key+" (cargo: unreadable expression "+declared+")")
				continue
			}
			if category == CategoryUnknown {
				unresolved = append(unresolved, key+" (cargo: "+declared+")")
			}
			rows = append(rows, rowT{
				ecosystem: ecosystemCargo,
				module:    p.Name,
				version:   p.Version,
				spdxID:    elected,
				declared:  declared,
				category:  category,
			})
		}
	}
	return
}
