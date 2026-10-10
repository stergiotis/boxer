// Package sccapplet writes a repository's code volume and cyclomatic
// complexity, as scc counts them, into a SQL applet document (ADR-0132) whose
// buffer carries its rows as literals: `SELECT … FROM values(…)`. A browser
// tab answers that statement in process (ADR-0290 §SD3), so the document
// draws as a treemap where no database and no table exist — beside a
// published demo page, for one (ADR-0299).
//
// One row per directory. Its value is the lines of code in the files directly
// inside it, as the treemap's node contract counts a node without its
// children; its colour is the complexity per 100 lines of everything under
// it, so a directory that holds only directories still says how dense its
// code is. Generated files and tests are left out, by the predicates the repo
// code exploration app applies by default ([scctree.IsGenerated],
// [scctree.IsTest]).
//
// The document is a snapshot of one tree. [Compose] is deterministic: the
// same scan and options give the same bytes, and the revision it is given is
// the only provenance it states.
package sccapplet

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
)

// Options shape the document.
type Options struct {
	// Depth folds every directory deeper than this many levels into its
	// ancestor at that depth; 0 folds nothing. Folding keeps every line and
	// shortens the statement a tab parses, at the cost of the folded
	// directories' own cells.
	Depth int
	// Revision names the tree the scan read — a commit id — and is stated in
	// the document. Empty states none.
	Revision string
}

// Stats describe what a document holds.
type Stats struct {
	Dirs       int
	Files      int
	Generated  int
	Tests      int
	Code       int64
	Complexity int64
}

// Scan runs scc over the git worktree that contains dir, with the flags the
// repo code exploration app uses.
func Scan(ctx context.Context, dir string) (groups []scctree.SccGroup, err error) {
	root, err := scctree.RepoRootAt(dir)
	if err != nil {
		err = eh.Errorf("sccapplet: %w", err)
		return
	}
	groups, err = scctree.RunSccContext(ctx, root)
	if err != nil {
		err = eh.Errorf("sccapplet: scc: %w", err)
	}
	return
}

type dirNode struct {
	code, complexity int64
}

// rootID is the id of the row for the repository's top directory.
const rootID = "repo"

// Compose writes the applet document for a scan.
func Compose(groups []scctree.SccGroup, opts Options) (doc []byte, st Stats, err error) {
	if opts.Depth < 0 {
		err = eh.Errorf("sccapplet: depth must not be negative")
		return
	}
	nodes := map[string]*dirNode{".": {}}
	for _, g := range groups {
		for i := range g.Files {
			f := &g.Files[i]
			if scctree.IsGenerated(f) {
				st.Generated++
				continue
			}
			if scctree.IsTest(f) {
				st.Tests++
				continue
			}
			dir := foldDir(path.Dir(path.Clean(strings.TrimPrefix(f.Location, "./"))), opts.Depth)
			n := ensureDir(nodes, dir)
			n.code += f.Code
			n.complexity += f.Complexity
			st.Files++
			st.Code += f.Code
			st.Complexity += f.Complexity
		}
	}
	dirs := make([]string, 0, len(nodes))
	for d := range nodes {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	subCode := make(map[string]int64, len(dirs))
	subCx := make(map[string]int64, len(dirs))
	for _, d := range dirs {
		n := nodes[d]
		for a := d; ; a = path.Dir(a) {
			subCode[a] += n.code
			subCx[a] += n.complexity
			if a == "." {
				break
			}
		}
	}
	rows := make([]string, 0, len(dirs))
	for _, d := range dirs {
		id, parent, label := d, path.Dir(d), path.Base(d)
		switch {
		case d == ".":
			id, parent, label = rootID, "", rootID
		case parent == ".":
			parent = rootID
		}
		density := 0.0
		if subCode[d] > 0 {
			density = float64(subCx[d]) * 100 / float64(subCode[d])
		}
		rows = append(rows, "("+quote(id)+","+quote(parent)+","+quote(label)+","+
			strconv.FormatInt(nodes[d].code, 10)+","+strconv.FormatFloat(density, 'f', 1, 64)+")")
	}
	st.Dirs = len(rows)

	var b strings.Builder
	b.WriteString("---\ntype: reference\naudience: end-user\nstatus: draft\n")
	b.WriteString("title: \"Repository complexity map\"\n")
	b.WriteString("summary: \"Lines of code and cyclomatic complexity per directory, as a snapshot\"\n")
	b.WriteString("icon: \"🗺\"\ntabs: [treemap, table, detail@side]\n---\n\n")
	b.WriteString("# Repository complexity map\n\n")
	if opts.Revision != "" {
		fmt.Fprintf(&b, "A snapshot of the repository at `%s`: ", opts.Revision)
	} else {
		b.WriteString("A snapshot of a repository: ")
	}
	b.WriteString("one cell per directory, its **area** the lines of code in the files directly " +
		"inside it, its **colour** the cyclomatic complexity per 100 lines of everything under it. ")
	if opts.Depth > 0 {
		fmt.Fprintf(&b, "Directories deeper than %d levels are folded into their ancestor at that depth. ", opts.Depth)
	}
	fmt.Fprintf(&b, "Counted by scc; generated files (%d) and tests (%d) are left out, as the repo code "+
		"exploration app leaves them out by default. %d files, %d lines of code, complexity %d in total.\n\n",
		st.Generated, st.Tests, st.Files, st.Code, st.Complexity)
	b.WriteString("The rows are literal, so the statement needs no table and no server: a browser tab " +
		"answers it in process. It describes the tree it was taken from and does not follow it.\n\n")
	b.WriteString("```sql\nSELECT *, 'lines' AS unit, 'complexity / 100 lines' AS color_unit\n")
	b.WriteString("FROM values('id String, parent String, label String, value UInt64, color Float64',\n")
	b.WriteString(strings.Join(rows, ",\n"))
	b.WriteString(")\n```\n")
	doc = []byte(b.String())
	return
}

// foldDir cuts dir to its first depth segments; depth 0 keeps it whole.
func foldDir(dir string, depth int) string {
	if depth == 0 || dir == "." {
		return dir
	}
	if parts := strings.Split(dir, "/"); len(parts) > depth {
		return strings.Join(parts[:depth], "/")
	}
	return dir
}

// ensureDir returns dir's node, creating it and every ancestor, so each row's
// parent is a row.
func ensureDir(nodes map[string]*dirNode, dir string) (n *dirNode) {
	n = nodes[dir]
	if n != nil {
		return
	}
	n = &dirNode{}
	nodes[dir] = n
	for a := path.Dir(dir); nodes[a] == nil; a = path.Dir(a) {
		nodes[a] = &dirNode{}
	}
	return
}

// quote writes s as a ClickHouse string literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}
