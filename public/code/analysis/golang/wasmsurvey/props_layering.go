package wasmsurvey

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// This file holds the layering rule of `props verify` (ADR-0394): a library
// package does not import the app platform. The platform is public/keelson
// and public/thestack; the packages allowed to build on it are the platform
// itself, the boxer command (public/app), the governance tools (public/gov),
// the apps and the showcase. Imports are the non-test imports of the native
// build, as for the stability rules: a library's test may drive a host.

// platformPrefixes are the module-relative trees that make up the app
// platform.
var platformPrefixes = []string{"public/keelson", "public/thestack"}

// layeringExemptPrefixes are the module-relative trees allowed to import the
// platform.
var layeringExemptPrefixes = []string{"public/keelson", "public/thestack", "public/app", "public/gov", "apps", "showcase"}

// layeringAllowed lists the library packages that still import the platform,
// each with the deferred fix. An entry is removed when its edge is cut; a new
// edge is not added here but cut, or the package moved up into the platform.
var layeringAllowed = map[string]string{
	"public/code/analysis/sccapplet":                               "an applet over the scc tree widget; deferred: move it under public/thestack",
	"public/science/geo/vectorfield/keelsonfield":                  "the introspection adapter of the vector field; deferred: move it under public/keelson",
	"public/science/geo/vectorfield/keelsonfield/keelsonfieldtest": "the adapter's test host; moves with keelsonfield",
	"public/semistructured/markdown/mdnotebook":                    "runs notebook cells against the introspection engine; deferred: split the engine binding into a keelson adapter",
	"public/semistructured/markdown/obsidian":                      "renders with the design system's web tokens; deferred: move the token source below the platform",
	"public/observability/profiling/pprofhttp":                     "binds through keelson's loopback gate; deferred: move the gate below the platform",
}

// LayeringFinding is one library package importing a platform package.
type LayeringFinding struct {
	ImportPath string
	Imported   string
}

func underAny(rel string, prefixes []string) (b bool) {
	for _, p := range prefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// CheckLayering returns every non-test import from a library package of
// rootModule into the app platform that layeringAllowed does not list,
// sorted by package then import.
func CheckLayering(imports map[string][]string, rootModule string) (findings []LayeringFinding) {
	for pkg, imps := range imports {
		rel := strings.TrimPrefix(pkg, rootModule+"/")
		if rel == pkg || underAny(rel, layeringExemptPrefixes) {
			continue
		}
		if _, ok := layeringAllowed[rel]; ok {
			continue
		}
		for _, imp := range imps {
			irel := strings.TrimPrefix(imp, rootModule+"/")
			if irel != imp && underAny(irel, platformPrefixes) {
				findings = append(findings, LayeringFinding{ImportPath: pkg, Imported: imp})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ImportPath != findings[j].ImportPath {
			return findings[i].ImportPath < findings[j].ImportPath
		}
		return findings[i].Imported < findings[j].Imported
	})
	return
}

// reportLayering prints the layering findings and fails on any.
func reportLayering(imports map[string][]string, rootModule string) (err error) {
	findings := CheckLayering(imports, rootModule)
	for _, f := range findings {
		fmt.Fprintf(os.Stdout, "layering  %s -> %s\n", strings.TrimPrefix(f.ImportPath, rootModule+"/"), strings.TrimPrefix(f.Imported, rootModule+"/"))
	}
	fmt.Fprintf(os.Stdout, "props verify: %d layering violation(s)\n", len(findings))
	if len(findings) > 0 {
		return eb.Build().Int("violations", len(findings)).Errorf("props verify failed: library package(s) import the app platform (ADR-0394); move the dependency below the platform or the package into it")
	}
	return nil
}
