package notebookdemo

import (
	"strconv"
	"time"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/apps/notebooklet"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ownDocument is a notebook of the host's own: no book lists it and no
// launcher shows it. Its records make their own rows, so any ClickHouse
// answers them, and it stores nothing, so it has no stored reading: its
// pane runs when the window opens.
const ownDocument = `---
title: Squares
---

# Squares

The squares of the first ten numbers add up to [?](val:total.sum_sq).
The host wrote this notebook in its own code; the pane reads it live.

` + "```sql" + `
SELECT number AS n, number * number AS sq FROM numbers(10)
` + "```" + `

^squares

` + "```sql" + `
SELECT sum(sq) AS sum_sq, max(sq) AS largest FROM squares
` + "```" + `

^total

` + "```sql table" + `
SELECT n, sq AS square FROM squares
` + "```" + `
`

// The two panes' keys: the second half of their composed identities.
const (
	keyRegistered = "report"
	keyOwn        = "squares"
)

// paneEntry is one embedded pane and what the host says about it.
type paneEntry struct {
	key   string
	label string
	how   string
	pane  *notebook.EmbeddedPane
	err   string
}

// App is one window: the host's own row, then the pane it shows.
type App struct {
	ids     *c.WidgetIdStack
	panes   []*paneEntry
	shown   int
	clicked bool
	// run stands in for the server in a test; nil is the environment's.
	run mdnotebook.RunFunc
}

var _ app.AppI = (*App)(nil)
var _ app.OperationsAppI = (*App)(nil)

func newApp() (inst *App) {
	return &App{ids: c.NewWidgetIdStack()}
}

func (inst *App) Manifest() (m app.Manifest) { return manifest }

// Operations serves the catalog for this window.
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// build makes the two panes, once. A pane that cannot be built is shown
// with why; the other still is.
func (inst *App) build() {
	if inst.panes != nil {
		return
	}
	registered := &paneEntry{key: keyRegistered, label: "The report sample, by its slug",
		how: "notebooklet.Embed(\"report\", …): a notebook of a registered book, the same document the launcher lists as a notebooklet. It shows what it stores until Run live."}
	registered.pane, registered.err = embedOrErr(notebooklet.Embed(keyRegistered, notebook.EmbedConfig{Host: ManifestId, Key: keyRegistered, Run: inst.run}))
	own := &paneEntry{key: keyOwn, label: "A notebook of the host's own",
		how: "notebook.NewEmbedded(text, …): a document this app wrote, which no launcher lists. It stores nothing, so it runs when the window opens."}
	own.pane, own.err = embedOrErr(notebook.NewEmbedded([]byte(ownDocument), notebook.EmbedConfig{Host: ManifestId, Key: keyOwn, RunOnMount: true, Run: inst.run}))
	inst.panes = []*paneEntry{registered, own}
}

func embedOrErr(p *notebook.EmbeddedPane, err error) (*notebook.EmbeddedPane, string) {
	if err != nil {
		return nil, err.Error()
	}
	return p, ""
}

// Mount mounts every pane with the host's context: the panes' runs are
// tasks of this window's.
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.build()
	for _, e := range inst.panes {
		if e.pane == nil {
			continue
		}
		if mErr := e.pane.Mount(ctx); mErr != nil {
			e.err = mErr.Error()
		}
	}
	return nil
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	for _, e := range inst.panes {
		if e.pane != nil {
			_ = e.pane.Unmount(ctx)
		}
	}
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.build()
	c.Label("This window is a host. The row below is its own; the notebook under the line is a pane it embeds, " +
		"drawn by the same constructor the notebook window and a notebooklet use, under this app's capabilities.").Wrap().Send()
	for range c.HorizontalTop().KeepIter() {
		for i, e := range inst.panes {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.RadioButton(inst.ids.PrepareStr("pane"), c.Atoms().Text(e.label).Keep(), inst.shown == i).SendRespVal(&inst.clicked).HasPrimaryClicked() {
					inst.shown = i
				}
			}
		}
	}
	e := inst.panes[inst.shown]
	c.Label(e.how).Wrap().Send()
	if e.pane != nil {
		inst.renderHostRead(e)
	}
	c.Separator().Send()
	if e.pane == nil {
		c.Label("The pane could not be built: " + e.err).Wrap().Send()
		return nil
	}
	return e.pane.Frame(ctx)
}

// renderHostRead draws what the host reads of the pane: its identity and
// its state, as a host would to react to a run.
func (inst *App) renderHostRead(e *paneEntry) {
	st := e.pane.State()
	for range c.Grid(inst.ids.PrepareStr("hostRead")).NumColumns(2).Striped(true).KeepIter() {
		row := func(k, v string) {
			c.Label(k).Send()
			c.Label(v).Send()
			c.EndRow()
		}
		row("runs attributed to", e.pane.Identity())
		row("reading", st.Reading)
		if !st.RunAt.IsZero() {
			row("computed", st.RunAt.Format(time.DateTime)+" · "+strconv.Itoa(st.Records)+" records in "+strconv.FormatInt(st.TookMs, 10)+" ms")
		}
		if st.LastErr != "" {
			row("last run", st.LastErr)
		}
	}
}
