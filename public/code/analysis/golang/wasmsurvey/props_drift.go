package wasmsurvey

import (
	"bytes"
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// DriftKindE is how one import path differs between the committed table and
// the declarations on disk.
type DriftKindE uint8

const (
	// DriftMissing — a declaration exists, the table has no row for it.
	DriftMissing DriftKindE = iota
	// DriftExtra — the table has a row whose declaration is gone (a deleted
	// or renamed package).
	DriftExtra
	// DriftChanged — both exist and disagree on at least one property.
	DriftChanged
)

func (inst DriftKindE) String() (s string) {
	switch inst {
	case DriftMissing:
		return "missing"
	case DriftExtra:
		return "extra"
	case DriftChanged:
		return "changed"
	}
	return "unknown"
}

// Drift is one disagreement between the generated table and the declarations.
type Drift struct {
	ImportPath string
	Kind       DriftKindE
	// Table and Source are the two sides. For DriftMissing the Table side is
	// the zero value; for DriftExtra the Source side is.
	Table  packageprops.Props
	Source packageprops.Props
}

// TrackedPropsFiles returns the repo-relative paths of every git-tracked
// package_props.go at or below root.
//
// Tracked, not on-disk, and that is the whole point. The table is a committed
// artifact, so the honest comparison is against committed declarations: a
// package_props.go that exists only in someone's working tree is not part of
// the repository yet, and the commit that adds it is the commit that should
// carry its table row.
//
// Tracked *paths* with working-tree content, deliberately — not the content at
// HEAD. Reading HEAD would make the table impossible to regenerate and commit
// in one go: every regen would encode the previous commit's declarations and
// the gate would sit one commit behind forever. Editing a tracked declaration
// and regenerating in the same commit is the intended workflow, and this is
// what makes it pass.
//
// This also makes the check usable on a shared worktree, where another
// session's in-flight package would otherwise report as drift and train
// everyone to ignore the result. ADR-0080's 2026-07-02 entry deferred the
// table regen for exactly that contamination and waited for "a settled tree";
// scoping the comparison is what makes the wait unnecessary.
func TrackedPropsFiles(ctx context.Context, root string) (paths []string, err error) {
	cmd, err := extbin.Git.Command(ctx, extbin.Opts{Dir: root}, "ls-files", "-z", "--", "*/"+propsFileName, propsFileName)
	if err != nil {
		err = eh.Errorf("props drift: resolve git: %w", err)
		return
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err = cmd.Run(); err != nil {
		err = eb.Build().Str("root", root).Errorf("props drift: git ls-files: %w", err)
		return
	}
	for p := range strings.SplitSeq(out.String(), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return
}

// HarvestTracked reads the declarations of every git-tracked package_props.go
// under root, in import-path order. It is [HarvestProps] restricted to what is
// committed — see [TrackedPropsFiles] for why that restriction is the correct
// comparison rather than a concession.
func HarvestTracked(ctx context.Context, root string, rootModule string) (rows []HarvestRow, err error) {
	paths, err := TrackedPropsFiles(ctx, root)
	if err != nil {
		return
	}
	for _, rel := range paths {
		fields, e := parsePropsFile(filepath.Join(root, rel))
		if e != nil {
			continue // unparseable: same tolerance HarvestProps applies
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		importPath := rootModule
		if dir != "." {
			importPath += "/" + dir
		}
		rows = append(rows, HarvestRow{
			ImportPath:       importPath,
			WASMWASI:         parseStateToken(fields["WASMWASI"]),
			WASMJS:           parseStateToken(fields["WASMJS"]),
			WASMFreestanding: parseStateToken(fields["WASMFreestanding"]),
			Kind:             parseKindToken(fields["Kind"]),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ImportPath < rows[j].ImportPath })
	return
}

// DriftAgainstTable compares a committed table against harvested declarations
// and returns every disagreement, in import-path order.
//
// Both directions matter and they fail differently. A missing row silently
// under-reports every query over the table — no error, no null, just a short
// answer. An extra row reports a package that no longer exists. Neither is
// visible to a reader of the query result, which is why this is a gate rather
// than a report.
func DriftAgainstTable(table packageprops.Table, declared []HarvestRow) (drifts []Drift) {
	inTable := make(map[string]packageprops.Props, len(table))
	for _, e := range table {
		inTable[e.ImportPath] = e.Props
	}
	seen := make(map[string]struct{}, len(declared))
	for _, d := range declared {
		seen[d.ImportPath] = struct{}{}
		props := d.Props()
		t, ok := inTable[d.ImportPath]
		switch {
		case !ok:
			drifts = append(drifts, Drift{ImportPath: d.ImportPath, Kind: DriftMissing, Source: props})
		case t != props:
			drifts = append(drifts, Drift{ImportPath: d.ImportPath, Kind: DriftChanged, Table: t, Source: props})
		}
	}
	for _, e := range table {
		if _, ok := seen[e.ImportPath]; !ok {
			drifts = append(drifts, Drift{ImportPath: e.ImportPath, Kind: DriftExtra, Table: e.Props})
		}
	}
	sort.Slice(drifts, func(i, j int) bool { return drifts[i].ImportPath < drifts[j].ImportPath })
	return
}
