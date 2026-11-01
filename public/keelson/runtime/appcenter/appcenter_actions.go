package appcenter

import (
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// The Running tab's one action, Close all (ADR-0348 §SD4): it closes the
// windows the filter shows, this one among them when shown. It comes from
// the host the process boots, not from a bus subject: no manifest can ask to
// close windows. It is the person's control only, never an operation (§SD5).

// HostI is the window host as the Running tab acts on it.
// *windowhost.Inst satisfies it.
type HostI interface {
	// Close takes the window through its closing edge at the end of the
	// frame: workingset saved, Unmount, drain.
	Close(key windowhost.WindowKeyT, reason string)
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
