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
	ecosystem string
	module    string
	version   string
	// spdxID is the identifier the category was decided on: the detected
	// identifier for a Go module, the elected branches of the declared
	// expression for a crate (ADR-0246 SD4).
	spdxID string
	// declared is what the component stated. For a Go row it is spdxID; for a
	// crate it is the whole expression, so every automatic election can be
	// checked against the text it was made from.
	declared string
	category CategoryE
}

// Run applies the inbound-license policy to the SBOM at sbomPath and to the
// crates of each `cargo metadata` document in cargoMetadataPaths; either input
// may be empty, not both. When csvPath is non-empty the per-(module, license)
// inventory is also written there. Returns the number of policy violations and
// any invocation error (missing file, malformed input, I/O failure). The
// pre-migration command separated these two failure classes via exit
// codes 1 vs 2; under boxer they collapse to a single non-zero exit
// driven by the returned error, which is behaviour-equivalent for
// `set -e` CI scripts (scripts/ci/license_gate.sh).
func Run(sbomPath string, cargoMetadataPaths []string, csvPath string) (violationCount int, err error) {
	if sbomPath == "" && len(cargoMetadataPaths) == 0 {
		err = eb.Build().Errorf("nothing to gate: pass an SBOM, cargo metadata, or both")
		return
	}
	rows, noLicense, err := goRows(sbomPath)
	if err != nil {
		return
	}
	crateRows, crateUnresolved, err := cargoRows(cargoMetadataPaths)
	if err != nil {
		return
	}
	rows = append(rows, crateRows...)
	noLicense = append(noLicense, crateUnresolved...)

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ecosystem != rows[j].ecosystem {
			return rows[i].ecosystem < rows[j].ecosystem
		}
		if rows[i].module != rows[j].module {
			return rows[i].module < rows[j].module
		}
		if rows[i].version != rows[j].version {
			return rows[i].version < rows[j].version
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
			if v.declared != v.spdxID {
				_, _ = fmt.Fprintf(os.Stderr, "  [%s] %s @ %s -- SPDX:%s (declared: %s)\n", v.category, v.module, v.version, v.spdxID, v.declared)
			} else {
				_, _ = fmt.Fprintf(os.Stderr, "  [%s] %s @ %s -- SPDX:%s\n", v.category, v.module, v.version, v.spdxID)
			}
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

// goRows classifies the components of a CycloneDX SBOM, one row per detected
// identifier (ADR-0004). An empty sbomPath contributes nothing.
func goRows(sbomPath string) (rows []rowT, noLicense []string, err error) {
	noLicense = make([]string, 0, 8)
	if sbomPath == "" {
		return
	}
	bom, err := loadSBOM(sbomPath)
	if err != nil {
		return
	}

	rows = make([]rowT, 0, len(bom.Components)*2)
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
				ecosystem: ecosystemGo,
				module:    c.Name,
				version:   c.Version,
				spdxID:    id,
				declared:  id,
				category:  Categorize(id),
			})
		}
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
	// The ADR-0246 columns are appended, so a reader of the original four keeps
	// reading the same fields.
	err = w.Write([]string{"module", "version", "spdx_id", "category", "ecosystem", "declared"})
	if err != nil {
		err = eb.Build().Errorf("write CSV header: %w", err)
		return
	}
	for _, r := range rows {
		err = w.Write([]string{r.module, r.version, r.spdxID, r.category.String(), r.ecosystem, r.declared})
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
