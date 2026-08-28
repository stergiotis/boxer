package waveform

import (
	"context"
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/audio/track"
)

// BuildTaskKind is the task kind under which a peaks build is reported.
const BuildTaskKind = "audio.peaks"

// buildTaskPollInterval is how often the reporter samples the build; the
// task primitive gates publication itself, so this only bounds staleness.
const buildTaskPollInterval = 200 * time.Millisecond

// SpawnBuildTask reports a track's background peaks build (ADR-0208 SD4) as
// a keelson background task (ADR-0038), so it shows in the task monitor with
// its progress and ETA and can be cancelled from there. track itself stays
// bus-free: the host that has a task API attaches it here.
//
// The reporter polls [track.Track.BuildProgress] until the build completes
// (task Done), fails (task Error), or the task is cancelled — which cancels
// the build through [track.Track.CancelBuild], leaving the partial pyramid
// drawable. A build that is already complete spawns nothing and returns a
// nil handle.
func SpawnBuildTask(ctx context.Context, tasks task.TaskApiI, tr *track.Track, title string) (h task.HandleI, err error) {
	if tasks == nil || tr == nil {
		return nil, eh.New("nil task api or track")
	}
	bp := tr.BuildProgress()
	if bp.Complete || bp.Err != nil {
		return nil, nil
	}
	if title == "" {
		title = "audio peaks"
	}
	h, err = tasks.Spawn(ctx, task.SpawnOpts{
		Kind:        BuildTaskKind,
		Title:       title,
		Cancellable: true,
		EstimatedMs: bp.EtaMs,
	})
	if err != nil {
		return nil, eh.Errorf("unable to spawn the peaks build task: %w", err)
	}
	go reportBuild(h, tr)
	return h, nil
}

func reportBuild(h task.HandleI, tr *track.Track) {
	ticker := time.NewTicker(buildTaskPollInterval)
	defer ticker.Stop()
	for {
		bp := tr.BuildProgress()
		switch {
		case bp.Err != nil:
			_ = h.Error(bp.Err, "peaks build failed")
			return
		case bp.Complete:
			h.Report(task.ProgressReport{Current: uint64(bp.TotalFrames), Total: uint64(bp.TotalFrames), Unit: task.UnitItems})
			_ = h.Done(nil)
			return
		}
		h.Report(task.ProgressReport{
			Current: uint64(max(bp.BuiltFrames, 0)),
			Total:   uint64(max(bp.TotalFrames, 0)),
			Unit:    task.UnitItems,
			Note:    fmt.Sprintf("%d of %d frames", bp.BuiltFrames, bp.TotalFrames),
		})
		select {
		case <-h.Ctx().Done():
			// A cancel from the bus (or the host's context) stops the build;
			// the pyramid keeps what it has.
			tr.CancelBuild()
			return
		case <-ticker.C:
		}
	}
}
