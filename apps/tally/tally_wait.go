package tally

import (
	"errors"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
)

// configureLanes makes every store-bound lane's run a cancellable background
// task (ADR-0038) under a kind of its own, so the host's task monitor names
// what tally is waiting for. The connection is left out: it is the app coming
// up, not work the reader asked for.
func (inst *App) configureLanes() {
	inst.mounts.Configure(inst.tasks, "tally-mounts", "tally: list the mounts")
	inst.resultLane.Configure(inst.tasks, "tally-results", "tally: run the results query")
	inst.preview.Configure(inst.tasks, "tally-preview", "tally: load a preview")
	inst.info.Configure(inst.tasks, "tally-info", "tally: read an entry")
	inst.history.Configure(inst.tasks, "tally-history", "tally: read a path's history")
	inst.diff.Configure(inst.tasks, "tally-diff", "tally: compare two snapshots")
	inst.findLane.Configure(inst.tasks, "tally-find", "tally: search the store")
	inst.duLane.Configure(inst.tasks, "tally-du", "tally: sum a directory")
	inst.duFilesLane.Configure(inst.tasks, "tally-du-files", "tally: list a directory's largest files")
	inst.problemsLane.Configure(inst.tasks, "tally-problems", "tally: read a snapshot's problems")
	inst.auditLane.Configure(inst.tasks, "tally-audit", "tally: audit every block")
	inst.components.Configure(inst.tasks, "tally-components", "tally: look up registered kinds")
}

// waiting draws a lane's wait as the standard job row with Cancel, and
// reports whether it drew: nothing for a lane that is not running. id is the
// lane's name, which is unique in the app and so makes the Cancel's id.
func (inst *App) waiting(job bgjobrow.JobI, id string, note string) (running bool) {
	return bgjobrow.Render(job, bgjobrow.Input{
		Note:     note,
		CancelId: inst.ids.PrepareStr("cancel-" + id),
	})
}

// laneFailed says why a lane has nothing to show. A cancelled run is the
// reader's own doing and not an error, so it says so and offers the way back:
// a lane keeps a cancelled key answered, and only a changed key or an
// invalidation runs it again.
func (inst *App) laneFailed(job interface{ Invalidate() }, id string, what string, err error) {
	if !errors.Is(err, bgjob.ErrCancelled) {
		c.Label(what + " failed: " + err.Error()).Send()
		return
	}
	for range c.Horizontal().KeepIter() {
		c.Label(what + " was cancelled.").Selectable(false).Send()
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.Button(inst.ids.PrepareStr("again-"+id), c.Atoms().Text("Run again").Keep()).
			Small().SendResp().HasPrimaryClicked() {
			job.Invalidate()
		}
	}
}
