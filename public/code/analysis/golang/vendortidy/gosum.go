package vendortidy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// SumIndex is the union of the go.sum lines a workspace carries, by module
// version; a version's /go.mod line is filed under the version itself.
type SumIndex struct {
	lines map[module.Version][]string
}

// LoadSums reads go.work.sum and the go.sum of every used module.
func (inst *Workspace) LoadSums() (idx SumIndex, err error) {
	idx.lines = make(map[module.Version][]string, 1024)
	files := make([]string, 0, len(inst.modules)+1)
	files = append(files, filepath.Join(inst.root, "go.work.sum"))
	for _, m := range inst.modules {
		files = append(files, filepath.Join(m.Dir, "go.sum"))
	}
	for _, f := range files {
		var b []byte
		b, err = os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
			continue
		}
		if err != nil {
			err = eb.Build().Str("path", f).Errorf("read checksums: %w", err)
			return
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			mv, ok := sumLineVersion(line)
			if !ok {
				continue
			}
			if !slices.Contains(idx.lines[mv], line) {
				idx.lines[mv] = append(idx.lines[mv], line)
			}
		}
	}
	return
}

func sumLineVersion(line string) (mv module.Version, ok bool) {
	f := strings.Fields(line)
	if len(f) != 3 {
		return
	}
	return module.Version{Path: f[0], Version: strings.TrimSuffix(f[1], "/go.mod")}, true
}

// MergeGoSum returns a go.sum for the result (ADR-0384 §SD4): lines of a
// module the old go.mod required and the result does not are dropped; a
// module whose version changed or that was added loses its old lines and
// gains the workspace's lines for the new version; every other line is kept.
// missing lists the changed versions the workspace holds no module checksum
// for.
func MergeGoSum(oldSum []byte, oldGoMod []byte, res Result, idx SumIndex) (out []byte, missing []module.Version, err error) {
	var mf *modfile.File
	mf, err = modfile.Parse("go.mod", oldGoMod, nil)
	if err != nil {
		err = eb.Build().Errorf("parse go.mod: %w", err)
		return
	}
	prev := make(map[string]string, len(mf.Require))
	for _, r := range mf.Require {
		prev[r.Mod.Path] = r.Mod.Version
	}
	replaced := make(map[string]bool, len(res.Requires))
	now := make(map[string]bool, len(res.Requires))
	for _, r := range res.Requires {
		now[r.Path] = true
		if prev[r.Path] != r.Version {
			replaced[r.Path] = true
		}
	}
	set := make(map[string]struct{}, 256)
	for line := range strings.SplitSeq(string(oldSum), "\n") {
		mv, ok := sumLineVersion(line)
		if !ok {
			continue
		}
		_, wasRequired := prev[mv.Path]
		if replaced[mv.Path] || (wasRequired && !now[mv.Path]) {
			continue
		}
		set[line] = struct{}{}
	}
	for _, r := range res.Requires {
		if !replaced[r.Path] {
			continue
		}
		mv := module.Version{Path: r.Path, Version: r.Version}
		hasZip := false
		for _, l := range idx.lines[mv] {
			set[l] = struct{}{}
			if !strings.Contains(l, "/go.mod ") {
				hasZip = true
			}
		}
		if !hasZip {
			missing = append(missing, mv)
		}
	}
	lines := sortSumLines(set)
	if len(lines) > 0 {
		out = []byte(strings.Join(lines, "\n") + "\n")
	}
	return
}

// MissingSums lists, for every requirement, the checksum lines the workspace
// carries that go.sum lacks. A go.sum that lacks none is complete for the
// result; extra lines are allowed (ADR-0384 §SD4).
func MissingSums(sum []byte, res Result, idx SumIndex) (missing []string) {
	have := make(map[string]struct{}, 256)
	for line := range strings.SplitSeq(string(sum), "\n") {
		have[strings.TrimSpace(line)] = struct{}{}
	}
	for _, r := range res.Requires {
		for _, l := range idx.lines[module.Version{Path: r.Path, Version: r.Version}] {
			if _, ok := have[l]; !ok {
				missing = append(missing, l)
			}
		}
	}
	return
}

// sortSumLines orders go.sum lines as cmd/go writes them: by module path,
// then by version, a version's /go.mod line after its module line.
func sortSumLines(set map[string]struct{}) (lines []string) {
	byKey := make(map[module.Version][]string, len(set))
	keys := make([]module.Version, 0, len(set))
	for l := range set {
		f := strings.Fields(l)
		k := module.Version{Path: f[0], Version: f[1]}
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], l)
	}
	module.Sort(keys)
	lines = make([]string, 0, len(set))
	for _, k := range keys {
		ls := byKey[k]
		slices.Sort(ls)
		lines = append(lines, ls...)
	}
	return
}
