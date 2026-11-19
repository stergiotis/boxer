package vendortidy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/mod/modfile"
)

// WorkspaceModule is a module the workspace uses.
type WorkspaceModule struct {
	File *modfile.File
	Path string
	Dir  string
}

// Workspace is a go.work, the modules it uses and its vendor tree.
type Workspace struct {
	vendor  *Vendor
	root    string
	goRoot  string
	modules []WorkspaceModule
}

// LoadWorkspace reads go.work, the go.mod of every used module and
// vendor/modules.txt beside go.work. goRoot is the GOROOT whose src tree
// decides which imports are standard library.
func LoadWorkspace(goWorkPath string, goRoot string) (ws *Workspace, err error) {
	var b []byte
	b, err = os.ReadFile(goWorkPath)
	if err != nil {
		err = eb.Build().Str("path", goWorkPath).Errorf("read go.work: %w", err)
		return
	}
	var wf *modfile.WorkFile
	wf, err = modfile.ParseWork(goWorkPath, b, nil)
	if err != nil {
		err = eb.Build().Str("path", goWorkPath).Errorf("parse go.work: %w", err)
		return
	}
	var root string
	root, err = filepath.Abs(filepath.Dir(goWorkPath))
	if err != nil {
		err = eb.Build().Str("path", goWorkPath).Errorf("resolve workspace root: %w", err)
		return
	}
	ws = &Workspace{root: root, goRoot: goRoot, modules: make([]WorkspaceModule, 0, len(wf.Use))}
	for _, u := range wf.Use {
		dir := u.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		var mf *modfile.File
		mf, err = readModFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return
		}
		if mf.Module == nil {
			err = eb.Build().Str("dir", dir).Errorf("go.mod has no module line")
			return
		}
		ws.modules = append(ws.modules, WorkspaceModule{File: mf, Path: mf.Module.Mod.Path, Dir: dir})
	}
	// Longest path first, so that a module nested under another one's path
	// wins the prefix match.
	slices.SortFunc(ws.modules, func(a, b WorkspaceModule) int { return len(b.Path) - len(a.Path) })
	ws.vendor, err = ParseModulesTxt(filepath.Join(root, "vendor"))
	return
}

func readModFile(p string) (mf *modfile.File, err error) {
	var b []byte
	b, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("read go.mod: %w", err)
		return
	}
	mf, err = modfile.Parse(p, b, nil)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("parse go.mod: %w", err)
	}
	return
}

// Root returns the directory holding go.work.
func (inst *Workspace) Root() string {
	return inst.root
}

// Vendor returns the parsed vendor/modules.txt.
func (inst *Workspace) Vendor() *Vendor {
	return inst.vendor
}

// ModuleAt returns the used module whose directory is dir.
func (inst *Workspace) ModuleAt(dir string) (m WorkspaceModule, ok bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	for _, c := range inst.modules {
		if c.Dir == abs {
			return c, true
		}
	}
	return
}

type pkgRef struct {
	importPath string
	dir        string
	module     string
}

// resolve maps an import path to the directory and module providing it: a
// used module's source first, the vendor tree second.
func (inst *Workspace) resolve(importPath string) (ref pkgRef, ok bool) {
	for _, m := range inst.modules {
		rest, found := strings.CutPrefix(importPath, m.Path)
		if !found || (rest != "" && rest[0] != '/') {
			continue
		}
		d := filepath.Join(m.Dir, filepath.FromSlash(strings.TrimPrefix(rest, "/")))
		if hasGoFiles(d) {
			return pkgRef{importPath: importPath, dir: d, module: m.Path}, true
		}
	}
	if mp, found := inst.vendor.ModuleOfPackage(importPath); found {
		return pkgRef{importPath: importPath, dir: filepath.Join(inst.vendor.dir, filepath.FromSlash(importPath)), module: mp}, true
	}
	return
}

// isStd reports whether an import path names a standard-library package.
func (inst *Workspace) isStd(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	if strings.Contains(first, ".") {
		return false
	}
	st, err := os.Stat(filepath.Join(inst.goRoot, "src", filepath.FromSlash(importPath)))
	return err == nil && st.IsDir()
}
