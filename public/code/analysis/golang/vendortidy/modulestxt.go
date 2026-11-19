package vendortidy

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// VendoredModule is one module entry of vendor/modules.txt.
type VendoredModule struct {
	Path    string
	Version string
	// Replacement is the text after "=>", empty when the module is not replaced.
	Replacement string
	GoVersion   string
	Explicit    bool
}

// Vendor is a parsed vendor/modules.txt. A workspace vendor lists a module
// once for each version some workspace module requires; packages are listed
// under the highest of them, the version that is built (ADR-0384 §SD2).
type Vendor struct {
	dir        string
	selected   map[string]VendoredModule
	explicit   map[module.Version]bool
	packageMod map[string]string
	providing  map[string]bool
}

// ParseModulesTxt reads <vendorDir>/modules.txt.
func ParseModulesTxt(vendorDir string) (v *Vendor, err error) {
	p := filepath.Join(vendorDir, "modules.txt")
	var b []byte
	b, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("read modules.txt: %w", err)
		return
	}
	v = &Vendor{
		dir:        vendorDir,
		selected:   make(map[string]VendoredModule, 256),
		explicit:   make(map[module.Version]bool, 256),
		packageMod: make(map[string]string, 2048),
		providing:  make(map[string]bool, 256),
	}
	all := make([]VendoredModule, 0, 256)
	cur := -1
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "## "):
			if cur < 0 {
				continue
			}
			for a := range strings.SplitSeq(line[3:], ";") {
				a = strings.TrimSpace(a)
				if a == "explicit" {
					all[cur].Explicit = true
				} else if gv, ok := strings.CutPrefix(a, "go "); ok {
					all[cur].GoVersion = gv
				}
			}
		case strings.HasPrefix(line, "# "):
			cur = -1
			f := strings.Fields(line[2:])
			if len(f) == 0 {
				continue
			}
			m := VendoredModule{Path: f[0]}
			rest := f[1:]
			if len(rest) > 0 && rest[0] != "=>" {
				m.Version = rest[0]
				rest = rest[1:]
			}
			if len(rest) > 0 && rest[0] == "=>" {
				m.Replacement = strings.Join(rest[1:], " ")
			}
			all = append(all, m)
			cur = len(all) - 1
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			if cur >= 0 {
				v.packageMod[line] = all[cur].Path
				v.providing[all[cur].Path] = true
			}
		}
	}
	err = sc.Err()
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("scan modules.txt: %w", err)
		return
	}
	for _, m := range all {
		if m.Explicit {
			v.explicit[module.Version{Path: m.Path, Version: m.Version}] = true
		}
		prev, ok := v.selected[m.Path]
		if !ok || semver.Compare(m.Version, prev.Version) > 0 {
			v.selected[m.Path] = m
		}
	}
	return
}

// Selected returns the version of a module the vendor tree builds.
func (inst *Vendor) Selected(modulePath string) (m VendoredModule, ok bool) {
	m, ok = inst.selected[modulePath]
	return
}

// IsExplicit reports whether modules.txt marks the module version explicit,
// the condition Go's vendor check puts on every require of a workspace module.
func (inst *Vendor) IsExplicit(mv module.Version) bool {
	return inst.explicit[mv]
}

// ModuleOfPackage returns the module providing a vendored package.
func (inst *Vendor) ModuleOfPackage(importPath string) (modulePath string, ok bool) {
	modulePath, ok = inst.packageMod[importPath]
	return
}

// IsProvidingPackages reports whether any package of the module is vendored.
func (inst *Vendor) IsProvidingPackages(modulePath string) bool {
	return inst.providing[modulePath]
}
