package externaledit

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// waitFor is how long one wait for a revision is held.
const waitFor = 25 * time.Second

// Editing is one window's text edited in another window: it hands the text
// over, opens the editor and collects the saved revisions. Its methods run
// on the render goroutine; the bus calls run off it. The zero value is idle.
type Editing struct {
	bus  app.BusI
	id   string
	base string

	giving  chan given
	opening chan error
	revs    chan Revision
	stop    chan struct{}

	last    *Revision
	pending *Revision
	status  string
}

type given struct {
	id  string
	err error
}

// Start hands text over under name and opens the host's editor on it.
func (inst *Editing) Start(bus app.BusI, name string, text string) {
	if inst.Active() || bus == nil {
		return
	}
	ch := make(chan given, 1)
	inst.bus, inst.giving, inst.base = bus, ch, text
	inst.last, inst.pending = nil, nil
	inst.status = "handing over…"
	go func() {
		id, err := Give(bus, name, []byte(text))
		ch <- given{id: id, err: err}
	}()
}

// Active says a text is handed over or being handed over.
func (inst *Editing) Active() bool { return inst.giving != nil || inst.id != "" }

// Busy says the editor has not opened yet, or a revision may arrive soon:
// the caller keeps drawing frames while it is.
func (inst *Editing) Busy() bool {
	return inst.giving != nil || inst.opening != nil || inst.revs != nil
}

// Status is a short line for the person: where the edit stands, or why it
// ended.
func (inst *Editing) Status() string { return inst.status }

// Last is the latest revision received, if any.
func (inst *Editing) Last() *Revision { return inst.last }

// Conflict says a revision waits because the text was edited here since it
// was handed over or last taken.
func (inst *Editing) Conflict() bool { return inst.pending != nil }

// Poll collects what arrived and answers the text to put in place of
// current, if a revision is to be taken. A revision is taken only while
// current is still the text last handed over or taken; otherwise it waits
// as a conflict for Take or Keep.
func (inst *Editing) Poll(current string) (text string, replace bool) {
	if inst.giving != nil {
		select {
		case g := <-inst.giving:
			inst.giving = nil
			if g.err != nil {
				inst.status = "not handed over: " + g.err.Error()
				break
			}
			inst.id = g.id
			inst.status = "editing elsewhere; each save comes back here"
			inst.open(g.id)
			inst.wait(g.id)
		default:
		}
	}
	if inst.opening != nil {
		select {
		case err := <-inst.opening:
			inst.opening = nil
			if err != nil {
				inst.status = "the editor did not open: " + err.Error()
			}
		default:
		}
	}
	for inst.revs != nil {
		select {
		case rev := <-inst.revs:
			if rev.Closed {
				inst.status = "the edit ended"
				if rev.Reason != "" {
					inst.status += ": " + rev.Reason
				}
				inst.revs, inst.id, inst.stop = nil, "", nil
				return
			}
			inst.last = &rev
			if current == inst.base {
				inst.pending = nil
				inst.base = string(rev.Text)
				text, replace = inst.base, true
				current = inst.base
			} else {
				inst.pending = &rev
			}
		default:
			return
		}
	}
	return
}

// Take answers the waiting revision's text and makes it the base.
func (inst *Editing) Take() (text string, ok bool) {
	if inst.pending == nil {
		return "", false
	}
	inst.base = string(inst.pending.Text)
	inst.pending = nil
	return inst.base, true
}

// Keep drops the waiting revision; current becomes the base, so the next
// save is taken over it.
func (inst *Editing) Keep(current string) {
	inst.base = current
	inst.pending = nil
}

// End stops editing; the service removes the file.
func (inst *Editing) End() {
	if inst.giving != nil {
		ch, bus := inst.giving, inst.bus
		go func() {
			if g := <-ch; g.err == nil {
				_ = End(bus, g.id)
			}
		}()
		inst.giving = nil
		inst.status = "stopped"
	}
	if inst.stop != nil {
		close(inst.stop)
		inst.stop = nil
	}
	if inst.id != "" && inst.bus != nil {
		id, bus := inst.id, inst.bus
		go func() { _ = End(bus, id) }()
		inst.status = "stopped; the editor's file was removed"
	}
	inst.id, inst.revs, inst.pending, inst.opening = "", nil, nil, nil
}

func (inst *Editing) open(id string) {
	ch := make(chan error, 1)
	inst.opening = ch
	bus := inst.bus
	go func() { ch <- Open(bus, id) }()
}

func (inst *Editing) wait(id string) {
	revs := make(chan Revision, 4)
	stop := make(chan struct{})
	inst.revs, inst.stop = revs, stop
	bus := inst.bus
	go func() {
		var seq uint64
		for {
			select {
			case <-stop:
				return
			default:
			}
			rev, err := Next(bus, id, seq, waitFor)
			if err != nil {
				rev = Revision{Closed: true, Reason: err.Error()}
			}
			if rev.Seq > seq || rev.Closed {
				seq = rev.Seq
				select {
				case revs <- rev:
				case <-stop:
					return
				}
			}
			if rev.Closed {
				return
			}
		}
	}()
}
