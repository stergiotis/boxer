package notebookdemo

import (
	"strconv"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The catalog: what the host reads of its panes, which one it shows, and a
// run of one. The panes' own buttons act directly; the host's catalog is
// its own, and does not serve the notebook window's operations.

const (
	resPanes = "panes"

	opGetPanes = "get_panes"
	opShowPane = "show_pane"
	opRunPane  = "run_pane"
)

// PaneState is one embedded pane as the host reads it.
type PaneState struct {
	Key      string             `desc:"the pane's key in this host: report, the registered sample by its slug; squares, a document of the host's own"`
	Identity string             `json:",omitzero" desc:"what the pane's runs are attributed to in the server's query log, <host>#<key>"`
	Shown    bool               `json:",omitzero" desc:"the window shows this pane"`
	Error    string             `json:",omitzero" desc:"why the pane could not be built"`
	Live     notebook.LiveState `desc:"the pane's reading and its last run"`
}

// Panes is get_panes' result.
type Panes struct {
	Panes []PaneState `desc:"the host's panes, in the order the window offers them"`
}

// PaneArgs names a pane.
type PaneArgs struct {
	Key string `desc:"report or squares"`
}

type snap struct{ panes []PaneState }

var ops = func() (s *appops.Set[*App, snap]) {
	s = appops.NewSet(func(inst *App) (sn snap) {
		inst.build()
		for i, e := range inst.panes {
			ps := PaneState{Key: e.key, Shown: i == inst.shown, Error: e.err}
			if e.pane != nil {
				ps.Identity, ps.Live = e.pane.Identity(), e.pane.State()
			}
			sn.panes = append(sn.panes, ps)
		}
		return
	})
	s.Resource(resPanes, "the panes, which one is shown and their readings", func(inst *App) any {
		inst.build()
		key := ""
		for i, e := range inst.panes {
			if i == inst.shown {
				key += "*"
			}
			key += e.key
			if e.pane != nil {
				started, ended := e.pane.Runs()
				key += ":" + e.pane.State().Reading + ":" + itoa(started) + ":" + itoa(ended)
			}
			key += ";"
		}
		return key
	})
	appops.Query(s, app.OperationSpec{Name: opGetPanes, Version: 1,
		Summary: "the embedded panes as the host reads them: identity, reading, last run",
		Reads:   []string{resPanes}, Agents: true},
		func(sn snap, in appops.None) (out Panes, err error) { return Panes{Panes: sn.panes}, nil })
	appops.Command(s, app.OperationSpec{Name: opShowPane, Version: 1,
		Summary: "show one of the panes under the host's row",
		Effect:  app.OperationEffectView, Writes: []string{resPanes}, Agents: true, Gesture: "the radio buttons of the host's row"},
		func(inst *App, call app.OperationCall, in PaneArgs) (appops.None, error) {
			i, err := inst.find(in.Key)
			if err == nil {
				inst.shown = i
			}
			return appops.None{}, err
		})
	appops.Command(s, app.OperationSpec{Name: opRunPane, Version: 1,
		Summary: "compute a pane's records on the server now, read-only, and show the notebook over them; get_panes says when it answered",
		Effect:  app.OperationEffectRun, Reads: []string{resPanes}, Writes: []string{resPanes}, Agents: true, Gesture: "Run live, in the pane"},
		func(inst *App, call app.OperationCall, in PaneArgs) (appops.None, error) {
			i, err := inst.find(in.Key)
			if err != nil {
				return appops.None{}, err
			}
			e := inst.panes[i]
			if e.pane == nil {
				return appops.None{}, app.RefuseOperation("the pane could not be built: " + e.err)
			}
			return appops.None{}, e.pane.Run()
		})
	return
}()

func (inst *App) find(key string) (i int, err error) {
	inst.build()
	for i, e := range inst.panes {
		if e.key == key {
			return i, nil
		}
	}
	return 0, app.RefuseOperation("a pane is report or squares")
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }
