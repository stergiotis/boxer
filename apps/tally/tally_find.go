package tally

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// findScopeE is where a search looks.
type findScopeE uint8

const (
	findScopeDir   findScopeE = iota // the target pane's directory subtree
	findScopeMount                   // the target pane's whole snapshot
	findScopeAll                     // every visible mount, newest snapshot each
)

func (s findScopeE) String() string {
	switch s {
	case findScopeMount:
		return "this snapshot"
	case findScopeAll:
		return "all mounts"
	}
	return "this directory"
}

// findState is the Find tab's knobs. The text fields are stable pointers
// the TextEdits bind to.
type findState struct {
	pattern string
	ext     string
	minSize string
	needle  string
	scope   findScopeE
	armed   string // the query key the Search button armed; "" = nothing yet
	table   stringTable
}

// relation spells the entries relation for a scope: one pinned snapshot, or
// every visible mount's newest one.
func relation(fn string, loc location, all bool) string {
	if all {
		return fn + "('*')"
	}
	return fmt.Sprintf("%s(%d, %d)", fn, loc.mount.Value(), loc.snap.UnixNano())
}

// findSQL is the name search: files matched by a RE2 pattern over the path,
// an extension and a minimum size, largest first.
func findSQL(loc location, scope findScopeE, dir, pattern, ext string, minSize int64) string {
	all := scope == findScopeAll
	pred := "1"
	if scope == findScopeDir {
		pred = scopePredicate(dir)
	}
	return fmt.Sprintf(`SELECT mount, path, size, mtime, ext, content, lower(hex(content_hash)) AS hash
FROM %s
WHERE NOT is_dir
  AND %s
  AND (%s = '' OR match(path, %s))
  AND (%s = '' OR ext = %s)
  AND size >= %d
ORDER BY size DESC
LIMIT 500`,
		relation("fs", loc, all), pred,
		ladingschema.QuoteLiteral(pattern), ladingschema.QuoteLiteral(pattern),
		ladingschema.QuoteLiteral(ext), ladingschema.QuoteLiteral(ext),
		minSize)
}

// grepSQL is the content search with exact line numbers (ADR-0198 §7): only
// files the store marked text take part, so a line never straddles a block.
func grepSQL(loc location, scope findScopeE, dir, needle string) string {
	all := scope == findScopeAll
	pred := "1"
	if scope == findScopeDir {
		pred = scopePredicate(dir)
	}
	q := ladingschema.QuoteLiteral(needle)
	return fmt.Sprintf(`SELECT mount, path, line0 + i - 1 AS lineno, line
FROM %s
ARRAY JOIN splitByChar('\n', data) AS line, arrayEnumerate(splitByChar('\n', data)) AS i
WHERE (mount, path) IN (SELECT mount, path FROM %s WHERE text AND %s)
  AND match(data, %s)
  AND match(line, %s)
ORDER BY mount, path, lineno
LIMIT 1000`,
		relation("fsdata", loc, all), relation("fs", loc, all), pred, q, q)
}

// renderFind is the Find tab: the knobs, a Search button, and the results
// as a table whose rows travel the target pane.
func (inst *App) renderFind(sc *storeConn) {
	p := inst.focusPane()
	loc, ok := inst.locationOf(p)
	if !ok {
		c.Label("Pick a mount on the left.").Send()
		return
	}
	f := &inst.find
	for range c.HorizontalTop().KeepIter() {
		c.Label(icons.PhMagnifyingGlass).Selectable(false).Send()
		c.TextEdit(inst.ids.PrepareStr("find-pattern"), f.pattern, false).HintText("path pattern (RE2)").DesiredWidth(200).SendRespVal(&f.pattern)
		c.TextEdit(inst.ids.PrepareStr("find-ext"), f.ext, false).HintText(".ext").DesiredWidth(70).SendRespVal(&f.ext)
		c.TextEdit(inst.ids.PrepareStr("find-min"), f.minSize, false).HintText("min bytes").DesiredWidth(90).SendRespVal(&f.minSize)
		c.TextEdit(inst.ids.PrepareStr("find-needle"), f.needle, false).HintText("content (RE2, text files)").DesiredWidth(220).SendRespVal(&f.needle)
		c.AddSpace(styletokens.GapInline(inst.density))
		for _, s := range []findScopeE{findScopeDir, findScopeMount, findScopeAll} {
			if c.Button(inst.ids.PrepareSeq(0x3000+uint64(s)), c.Atoms().Text(s.String()).Keep()).
				Selected(f.scope == s).SendResp().HasPrimaryClicked() {
				f.scope = s
			}
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.Button(inst.ids.PrepareStr("find-go"), c.Atoms().Text("Search").Keep()).SendResp().HasPrimaryClicked() {
			f.armed = inst.findKey(loc, p.st.Dir())
		}
	}
	if f.armed == "" {
		c.Label("Name and content search over the store — Search runs it; results are capped.").Selectable(false).Send()
		return
	}
	sql := f.armedSQL(loc, p.st.Dir())
	res, done, ferr, busy := inst.findLane.demand(f.armed, func(ctx context.Context) (tableResult, error) {
		return runTable(ctx, sc.exec, sql)
	})
	if busy {
		c.RequestRepaint()
		for range c.HorizontalTop().KeepIter() {
			c.Spinner().Send()
			c.Label("Searching…").Send()
		}
		return
	}
	if !done {
		return
	}
	if ferr != nil {
		c.Label("Search failed: " + ferr.Error()).Send()
		return
	}
	c.Label(fmt.Sprintf("%d result(s) in %s", len(res.rows), f.scope.String())).Selectable(false).Send()
	if len(res.rows) == 0 {
		return
	}
	f.table.scopeKey = "find-table"
	f.table.resetFor(f.armed)
	f.table.headers = res.headers
	f.table.rows = res.rows
	f.table.tone = nil
	f.table.widths = []float32{160, 420, 100, 170, 70, 90, 300}
	if clicked := f.table.render(inst.ids, inst.density); clicked >= 0 {
		inst.travelToRow(p, res, clicked)
	}
}

// findKey is what the Search button arms: the knobs and the place, so a
// repeated search with nothing changed is a cache hit and any change re-runs.
func (inst *App) findKey(loc location, dir string) string {
	f := &inst.find
	return strings.Join([]string{loc.key(), dir, strconv.Itoa(int(f.scope)), f.pattern, f.ext, f.minSize, f.needle}, "\x00")
}

func (f *findState) armedSQL(loc location, dir string) string {
	if strings.TrimSpace(f.needle) != "" {
		return grepSQL(loc, f.scope, dir, strings.TrimSpace(f.needle))
	}
	minSize, _ := strconv.ParseInt(strings.TrimSpace(f.minSize), 10, 64)
	return findSQL(loc, f.scope, dir, strings.TrimSpace(f.pattern), strings.TrimSpace(f.ext), minSize)
}

// travelToRow puts the pane on the row's path, switching mount first when
// the row names another one (a wildcard search does).
func (inst *App) travelToRow(p *pane, res tableResult, row int) {
	pc := columnIndex(res.headers, "path")
	if pc < 0 || row < 0 || row >= len(res.rows) || pc >= len(res.rows[row]) {
		return
	}
	if mc := columnIndex(res.headers, "mount"); mc >= 0 && mc < len(res.rows[row]) {
		if v, err := strconv.ParseUint(res.rows[row][mc], 10, 64); err == nil {
			if id := identifier.TaggedId(v); id != p.mount && id.IsValid() {
				inst.selectMount(p, id)
			}
		}
	}
	inst.travelTo(p, res.rows[row][pc])
}
