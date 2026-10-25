package attentionchrome

import (
	"time"
)

// Jump is "next by weight" (the background note on juggling streams, §2.2,
// after irssi's Alt-A): the most pressing thing first, whatever came first.
// The head of the queue — highest level, then oldest — opens its card, or,
// when it is open, comes to the front; with nothing waiting, the newest
// highlight raises the window that reported it, or the watchbill window for
// a job, and goes. It reports whether
// there was anywhere to go. Call it on the render goroutine.
func (inst *Chrome) Jump() (went bool) {
	now := time.Now()
	inst.read = time.Time{}
	inst.refresh(now)
	if len(inst.entries) > 0 {
		inst.openCard(inst.entries[0].Key)
		inst.listOpen = false
		return true
	}
	for _, n := range inst.notices {
		if !inst.canOpen(n) {
			continue
		}
		inst.openNotice(n)
		inst.read = time.Time{}
		return true
	}
	return false
}
