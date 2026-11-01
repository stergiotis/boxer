package appcenter

import (
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// The Running tab's actions on the windows the filter shows: Close all
// (ADR-0348 §SD4), which closes them, this one among them when shown, and
// Pause and Resume (ADR-0354 §SD4), which leave this one out. They come from
// the host the process boots, not from a bus subject: no manifest can ask to
// close or pause windows. They are the person's controls only, never
// operations (ADR-0348 §SD5, ADR-0354 §SD3).

// HostI is the window host as the Running tab acts on it.
// *windowhost.Inst satisfies it.
type HostI interface {
	// Close takes the window through its closing edge at the end of the
	// frame: workingset saved, Unmount, drain.
	Close(key windowhost.WindowKeyT, reason string)
	// Pause and Resume ask for the window to be paused or resumed at the
	// next frame (ADR-0354); from names where the person asked.
	Pause(key windowhost.WindowKeyT, from string) (ok bool)
	Resume(key windowhost.WindowKeyT, from string) (ok bool)
}

// closeReason lands in the stopped lifecycle row of a window closed here.
const closeReason = "closed from the app center"

var (
	hostMu sync.RWMutex
	host   HostI
)

// SetHost hands the Running tab the host it closes windows through. Host
// boot calls it; without it the tab shows no Close all.
func SetHost(h HostI) {
	hostMu.Lock()
	host = h
	hostMu.Unlock()
}

func hostOf() (h HostI) {
	hostMu.RLock()
	defer hostMu.RUnlock()
	return host
}

// closable is what Close all would close of the shown rows: every shown
// window not already closing, in the order shown, with this one last, so the
// others' closes are asked for before the window asking for them goes.
func (inst *App) closable(v *runningView, shown []int64) (keys []uint64) {
	self := false
	for _, i := range shown {
		row := &v.rows[i]
		switch {
		case inst.closing[row.key]:
		case row.key == inst.selfKey && inst.selfKey != 0:
			self = true
		default:
			keys = append(keys, row.key)
		}
	}
	if self {
		keys = append(keys, inst.selfKey)
	}
	return
}

// armCloseAll asks for the confirmation of closing keys. The confirmation
// holds the keys and the filter they were shown under, so neither a re-read
// that adds a window nor typing in the filter widens what it closes.
func (inst *App) armCloseAll(keys []uint64) {
	inst.closeArmed = slices.Clone(keys)
	inst.closeArmedFilter = inst.runFilter
}

func (inst *App) disarmCloseAll() {
	inst.closeArmed, inst.closeArmedFilter = nil, ""
}

// closeAll closes the armed windows; their rows say so until a read no
// longer lists them.
func (inst *App) closeAll(h HostI) {
	keys := inst.closeArmed
	inst.disarmCloseAll()
	if inst.closing == nil {
		inst.closing = make(map[uint64]bool, len(keys))
	}
	for _, key := range keys {
		h.Close(windowhost.WindowKeyT(key), closeReason)
		inst.closing[key] = true
	}
	inst.markDirty()
}

// pruneClosing forgets the windows a read no longer lists, drops them from
// an armed confirmation, and drops the confirmation once the filter moved or
// nothing it named is left.
func (inst *App) pruneClosing(v *runningView) {
	if len(inst.closing) == 0 && inst.closeArmed == nil {
		return
	}
	open := make(map[uint64]bool, len(v.rows))
	for i := range v.rows {
		open[v.rows[i].key] = true
	}
	for key := range inst.closing {
		if !open[key] {
			delete(inst.closing, key)
		}
	}
	if inst.closeArmed == nil {
		return
	}
	if inst.closeArmedFilter != inst.runFilter {
		inst.disarmCloseAll()
		return
	}
	inst.closeArmed = slices.DeleteFunc(inst.closeArmed, func(key uint64) bool { return !open[key] })
	if len(inst.closeArmed) == 0 {
		inst.disarmCloseAll()
	}
}

// rowPaused says whether the window is paused as the tab shows it: as the
// person last asked, until a read agrees, else as the read says.
func (inst *App) rowPaused(row *runningRow) bool { return pausedAs(inst.pauseWant, row) }

func pausedAs(want map[uint64]bool, row *runningRow) bool {
	if w, ok := want[row.key]; ok {
		return w
	}
	return row.pausedSince != ""
}

// pausable is what Pause would pause of the shown rows, and resumable what
// Resume would resume: this window never, nor one closing.
func (inst *App) pausable(v *runningView, shown []int64) (pause []uint64, resume []uint64) {
	for _, i := range shown {
		row := &v.rows[i]
		if row.key == inst.selfKey || inst.closing[row.key] {
			continue
		}
		if inst.rowPaused(row) {
			resume = append(resume, row.key)
		} else {
			pause = append(pause, row.key)
		}
	}
	return
}

// setPaused asks the host to pause or resume keys, from the Running tab,
// and shows them so until a read agrees.
func (inst *App) setPaused(h HostI, keys []uint64, paused bool) {
	if inst.pauseWant == nil {
		inst.pauseWant = make(map[uint64]bool, len(keys))
	}
	for _, key := range keys {
		var ok bool
		if paused {
			ok = h.Pause(windowhost.WindowKeyT(key), windowhost.PauseFromRunningTab)
		} else {
			ok = h.Resume(windowhost.WindowKeyT(key), windowhost.PauseFromRunningTab)
		}
		if ok {
			inst.pauseWant[key] = paused
		}
	}
	inst.markDirty()
}

// prunePauses forgets what the person asked once a read agrees, or no
// longer lists the window.
func (inst *App) prunePauses(v *runningView) {
	if len(inst.pauseWant) == 0 {
		return
	}
	read := make(map[uint64]bool, len(v.rows))
	for i := range v.rows {
		read[v.rows[i].key] = v.rows[i].pausedSince != ""
	}
	for key, want := range inst.pauseWant {
		if got, ok := read[key]; !ok || got == want {
			delete(inst.pauseWant, key)
		}
	}
}
