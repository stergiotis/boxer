package task

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// ProducerCaps returns the SubjectFilter set an app needs to spawn tasks.
// task.> with Both direction covers publishing created/progress/done/error
// and subscribing to the per-task cancel inbox. Add to Manifest.Caps.
func ProducerCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{
			Pattern:   PatternAll,
			Direction: app.CapDirectionBoth,
			Reason:    "task: publish lifecycle + receive cancel",
		},
	}
	return
}

// ObserverCaps returns the SubjectFilter set a passive observer needs to
// watch every task on the bus (e.g., a status panel or audit supervisor).
func ObserverCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{
			Pattern:   PatternAll,
			Direction: app.CapDirectionSub,
			Reason:    "task: observe lifecycle",
		},
	}
	return
}

// CancelerCaps returns the SubjectFilter set a consumer needs to request
// cancellation of any task — narrower than ObserverCaps for apps that
// issue cancels but never read progress.
func CancelerCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{
			Pattern:   PatternCancelAll,
			Direction: app.CapDirectionPub,
			Reason:    "task: request cancellation",
		},
	}
	return
}
