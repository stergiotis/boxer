package appcenter

import (
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// The Running page's Raise and Close (ADR-0348 §SD4). They come from the
// host the process boots, not from a bus subject: windowhost.raise refuses
// an app another app's window, and no manifest can ask to close windows.
// They are the person's controls only, never operations (§SD5).

// HostI is the window host as the Running page acts on it.
// *windowhost.Inst satisfies it.
type HostI interface {
	// Raise brings the window to the front on the next frame.
	Raise(key windowhost.WindowKeyT) (err error)
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

// SetHost hands the Running page the host it raises and closes windows
// through. Host boot calls it; without it the page shows no actions.
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

// raiseWindow brings key's window to the front.
func (inst *App) raiseWindow(h HostI, key uint64) {
	inst.actionNote = ""
	if err := h.Raise(windowhost.WindowKeyT(key)); err != nil {
		inst.actionNote = err.Error()
	}
}

// armClose asks for the confirmation of closing key's window. Armed by key,
// so a re-read that reorders the rows does not move it to another window.
func (inst *App) armClose(key uint64) {
	inst.closeArmed = key
}

// closeWindow closes key's window; the row says so until a read no longer
// lists it.
func (inst *App) closeWindow(h HostI, key uint64) {
	inst.closeArmed = 0
	h.Close(windowhost.WindowKeyT(key), closeReason)
	if inst.closing == nil {
		inst.closing = make(map[uint64]bool, 1)
	}
	inst.closing[key] = true
	inst.markDirty()
}

// pruneClosing forgets the windows a read no longer lists, and a
// confirmation armed for one.
func (inst *App) pruneClosing(v *runningView) {
	if len(inst.closing) == 0 && inst.closeArmed == 0 {
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
	if inst.closeArmed != 0 && !open[inst.closeArmed] {
		inst.closeArmed = 0
	}
}
