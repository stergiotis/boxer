package tally

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// duSQL is ADR-0198 §7's one-pass du: every directory's recursive size under
// the scope, largest first, by unfolding each file's ancestor prefixes.
func duSQL(loc location, dir string) string {
	having := "1"
	if dir != "" && dir != "." {
		having = fmt.Sprintf("anc = %s OR startsWith(anc, %s)", ladingschema.QuoteLiteral(dir), ladingschema.QuoteLiteral(dir+"/"))
	}
	return fmt.Sprintf(`SELECT anc AS directory, sum(size) AS bytes, count() AS files
FROM fs(%d, %d)
ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
WHERE NOT is_dir AND %s
GROUP BY anc
HAVING %s
ORDER BY bytes DESC
LIMIT 300`, loc.mount.Value(), loc.snap.UnixNano(), scopePredicate(dir), having)
}

// duFilesSQL feeds the treemap: the largest files under the scope, capped —
// the picture is exact below the cap and a top-N approximation above it.
func duFilesSQL(loc location, dir string) string {
	return fmt.Sprintf(`SELECT path, size FROM fs(%d, %d) WHERE NOT is_dir AND %s ORDER BY size DESC LIMIT 4000`,
		loc.mount.Value(), loc.snap.UnixNano(), scopePredicate(dir))
}

// buildDuTree turns (path, size) rows into the nested tree the treemap draws,
// rooted at the scope directory.
func buildDuTree(rootName string, res tableResult) (root *layout.Node, files int) {
	root = &layout.Node{Name: rootName}
	pc, sc := columnIndex(res.headers, "path"), columnIndex(res.headers, "size")
	if pc < 0 || sc < 0 {
		return
	}
	index := map[*layout.Node]map[string]*layout.Node{}
	child := func(parent *layout.Node, name string) *layout.Node {
		m := index[parent]
		if m == nil {
			m = map[string]*layout.Node{}
			index[parent] = m
		}
		if n, ok := m[name]; ok {
			return n
		}
		n := &layout.Node{Name: name}
		m[name] = n
		parent.Children = append(parent.Children, n)
		return n
	}
	for _, row := range res.rows {
		if pc >= len(row) || sc >= len(row) {
			continue
		}
		size, err := strconv.ParseFloat(row[sc], 64)
		if err != nil {
			continue
		}
		segs := strings.Split(row[pc], "/")
		node := root
		for i, seg := range segs {
			if i == len(segs)-1 {
				leaf := child(node, seg)
				leaf.Size = size
				files++
				break
			}
			node = child(node, seg)
		}
	}
	return
}

// The Du tab shares its pane: the table keeps a fixed width at the left, the
// treemap takes what is left, both as tall as the pane minus the header line.
const (
	duTableWidth  float32 = 520
	duHeaderSpace float32 = 40
	duGap         float32 = 12
	duMinTreemapW float32 = 200
	duMinTreemapH float32 = 160
	duFallbackW   float32 = 900
	duFallbackH   float32 = 360
	duProbeSalt   uint64  = 0x0200_d000_0000_0001
)

// renderDu is the Du tab: directory totals as a table, the files as a
// treemap, both for the target pane's directory and both sized to the pane.
func (inst *App) renderDu(sc *storeConn) {
	p := inst.focusPane()
	loc, ok := inst.locationOf(p)
	if !ok {
		c.Label("Pick a mount on the left.").Send()
		return
	}
	// Probe first, before anything is placed, and keep the last good answer:
	// the probe is a frame late and absent on the frame a tab comes back.
	if w, h, okp := c.CapturePaneSize(c.ProbeSeq("tally", "du-pane") ^ duProbeSalt); okp && w > 0 && h > 0 {
		inst.duPaneW, inst.duPaneH = w, h
	}
	paneW, paneH := inst.duPaneW, inst.duPaneH
	if paneW <= 0 {
		paneW = duFallbackW
	}
	if paneH <= 0 {
		paneH = duFallbackH
	}
	dir := p.st.Dir()
	key := loc.key() + "|" + dir
	res, done, derr, busy := inst.duLane.demand(key, func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, duSQL(loc, dir))
	})
	files, fdone, ferr, fbusy := inst.duFilesLane.demand(key, func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, duFilesSQL(loc, dir))
	})
	if busy || fbusy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Summing " + dir + "…").Send()
		}
		return
	}
	if !done || !fdone {
		return
	}
	if derr != nil {
		c.Label("Cannot sum: " + derr.Error()).Send()
		return
	}
	if ferr != nil {
		c.Label("Cannot list files: " + ferr.Error()).Send()
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(fmt.Sprintf("Disk usage%s — %d directories, %d files drawn", scopeNote(dir), len(res.rows), len(files.rows))).Strong().End().Keep()).Selectable(false).Send()
	bodyH := max(paneH-duHeaderSpace, duMinTreemapH)
	treemapW := max(paneW-duTableWidth-duGap, duMinTreemapW)
	for range c.HorizontalTop().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(duTableWidth)
			c.UiSetMaxWidth(duTableWidth)
			inst.duTable.scopeKey = "du-table"
			inst.duTable.resetFor(key)
			inst.duTable.maxHeight = bodyH
			inst.duTable.headers = res.headers
			inst.duTable.rows = res.rows
			inst.duTable.widths = []float32{320, 110, 70}
			if clicked := inst.duTable.render(inst.ids, inst.density); clicked >= 0 {
				if dc := columnIndex(res.headers, "directory"); dc >= 0 && dc < len(res.rows[clicked]) {
					p.st.SetDir(res.rows[clicked][dc])
					p.navigated = true
				}
			}
		}
		c.AddSpace(duGap)
		for range c.Vertical().KeepIter() {
			if inst.duTreeKey != key {
				inst.duTreeKey = key
				root, _ := buildDuTree(dir, files)
				if inst.duTree == nil {
					inst.duTree = treemap.New(inst.ids, "tally-du-treemap", root,
						treemap.WithStatusLine(true), treemap.WithLeafClickSensing(true))
				} else {
					inst.duTree.SetRoot(root)
				}
			}
			if inst.duTree != nil {
				inst.duTree.SetContainerSize(treemapW, bodyH)
				inst.duTree.Render()
			}
		}
	}
}
