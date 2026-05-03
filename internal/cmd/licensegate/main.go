// Command licensegate applies the boxer inbound-license policy to a
// CycloneDX SBOM produced by cyclonedx-gomod and emits a CSV inventory.
//
// Usage:
//
//	go run ./internal/cmd/licensegate -sbom sbom.json [-csv third_party_licenses.csv]
//
// Exit codes:
//
//	0 — no policy violations
//	1 — at least one forbidden or restricted dependency found
//	2 — invocation error (missing flags, malformed SBOM, I/O failure)
//
// See doc/adr/0004-license-gate-cyclonedx.md for the policy rationale.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

const selfModulePurlPrefix = "pkg:golang/github.com/stergiotis/boxer"

type rowT struct {
	module   string
	version  string
	spdxID   string
	category CategoryE
}

func main() {
	sbomPath := flag.String("sbom", "", "path to CycloneDX JSON SBOM (required)")
	csvPath := flag.String("csv", "", "output CSV inventory path (optional; written only if non-empty)")
	flag.Parse()
	if *sbomPath == "" {
		fmt.Fprintln(os.Stderr, "ERROR: -sbom is required")
		flag.Usage()
		os.Exit(2)
	}
	os.Exit(run(*sbomPath, *csvPath))
}

func run(sbomPath, csvPath string) (exitCode int) {
	bom, err := loadSBOM(sbomPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		exitCode = 2
		return
	}

	rows := make([]rowT, 0, len(bom.Components)*2)
	noLicense := make([]string, 0, 8)
	for _, c := range bom.Components {
		if isSelfModule(c.Purl) {
			continue
		}
		var ids []string
		elected, hasElection := ElectedLicense(c.Name)
		if hasElection {
			ids = []string{elected}
		} else {
			ids = licenseIDs(c)
		}
		if len(ids) == 0 {
			label := c.Name
			if c.Version != "" {
				label = c.Name + "@" + c.Version
			}
			noLicense = append(noLicense, label)
			continue
		}
		for _, id := range ids {
			rows = append(rows, rowT{
				module:   c.Name,
				version:  c.Version,
				spdxID:   id,
				category: Categorize(id),
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].module != rows[j].module {
			return rows[i].module < rows[j].module
		}
		return rows[i].spdxID < rows[j].spdxID
	})
	sort.Strings(noLicense)

	if csvPath != "" {
		err = writeCSV(csvPath, rows)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: write CSV: %v\n", err)
			exitCode = 2
			return
		}
	}

	violations := make([]rowT, 0, 4)
	for _, r := range rows {
		if r.category.IsViolation() {
			violations = append(violations, r)
		}
	}

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "=== POLICY VIOLATIONS (%d) ===\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  [%s] %s @ %s -- SPDX:%s\n", v.category, v.module, v.version, v.spdxID)
		}
	}

	if len(noLicense) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "=== unresolved licenses (%d) -- review manually ===\n", len(noLicense))
		for _, m := range noLicense {
			fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
	}

	if len(violations) > 0 {
		exitCode = 1
		return
	}
	return
}

func isSelfModule(purl string) (b bool) {
	b = strings.HasPrefix(purl, selfModulePurlPrefix)
	return
}

func writeCSV(path string, rows []rowT) (err error) {
	f, err := os.Create(path)
	if err != nil {
		err = fmt.Errorf("create CSV %q: %w", path, err)
		return
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			err = fmt.Errorf("close CSV %q: %w", path, cerr)
		}
	}()
	w := csv.NewWriter(f)
	err = w.Write([]string{"module", "version", "spdx_id", "category"})
	if err != nil {
		err = fmt.Errorf("write CSV header: %w", err)
		return
	}
	for _, r := range rows {
		err = w.Write([]string{r.module, r.version, r.spdxID, r.category.String()})
		if err != nil {
			err = fmt.Errorf("write CSV row %q: %w", r.module, err)
			return
		}
	}
	w.Flush()
	err = w.Error()
	if err != nil {
		err = fmt.Errorf("flush CSV: %w", err)
		return
	}
	return
}
