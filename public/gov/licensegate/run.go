//go:build llm_generated_opus47

package licensegate

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const selfModulePurlPrefix = "pkg:golang/github.com/stergiotis/boxer"

type rowT struct {
	module   string
	version  string
	spdxID   string
	category CategoryE
}

// Run applies the inbound-license policy to the SBOM at sbomPath. When
// csvPath is non-empty the per-(module, license) inventory is also
// written there. Returns the number of policy violations and any
// invocation error (missing file, malformed SBOM, I/O failure). The
// pre-migration command separated these two failure classes via exit
// codes 1 vs 2; under boxer they collapse to a single non-zero exit
// driven by the returned error, which is behaviour-equivalent for
// `set -e` CI scripts (scripts/ci/license_gate.sh).
func Run(sbomPath, csvPath string) (violationCount int, err error) {
	bom, err := loadSBOM(sbomPath)
	if err != nil {
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
		_, _ = fmt.Fprintln(os.Stderr, "")
		_, _ = fmt.Fprintf(os.Stderr, "=== POLICY VIOLATIONS (%d) ===\n", len(violations))
		for _, v := range violations {
			_, _ = fmt.Fprintf(os.Stderr, "  [%s] %s @ %s -- SPDX:%s\n", v.category, v.module, v.version, v.spdxID)
		}
	}

	if len(noLicense) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, "")
		_, _ = fmt.Fprintf(os.Stderr, "=== unresolved licenses (%d) -- review manually ===\n", len(noLicense))
		for _, m := range noLicense {
			_, _ = fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
	}

	violationCount = len(violations)
	return
}

func isSelfModule(purl string) (b bool) {
	b = strings.HasPrefix(purl, selfModulePurlPrefix)
	return
}

func writeCSV(path string, rows []rowT) (err error) {
	f, err := os.Create(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("create CSV: %w", err)
		return
	}
	defer func() {
		cerr := f.Close()
		if err == nil && cerr != nil {
			err = eb.Build().Str("path", path).Errorf("close CSV: %w", cerr)
		}
	}()
	w := csv.NewWriter(f)
	err = w.Write([]string{"module", "version", "spdx_id", "category"})
	if err != nil {
		err = eb.Build().Errorf("write CSV header: %w", err)
		return
	}
	for _, r := range rows {
		err = w.Write([]string{r.module, r.version, r.spdxID, r.category.String()})
		if err != nil {
			err = eb.Build().Str("module", r.module).Errorf("write CSV row: %w", err)
			return
		}
	}
	w.Flush()
	err = w.Error()
	if err != nil {
		err = eb.Build().Errorf("flush CSV: %w", err)
		return
	}
	return
}
