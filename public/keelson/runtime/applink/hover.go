package applink

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostwake"
)

// Hovers answers a markdown surface's link hover (ADR-0409 (proposed)
// §SD8) from what the host said of each link: Text runs on the render
// goroutine once per claimed link each frame and never blocks, so it
// returns the last answer and, for a link not yet asked about or asked
// about too long ago, asks the host off the render goroutine and wakes a
// pass when the answer lands. A minted link's reading can change — its
// task ends — so an answer is kept for a while, not for good.
type Hovers struct {
	bus    app.BusI
	author Author
	spawn  func(f func(ctx context.Context)) (started bool)

	mu      sync.Mutex
	entries map[string]*hoverEntry
	order   []string
}

type hoverEntry struct {
	text    string
	at      time.Time
	pending bool
}

// hoverFresh is how long an answer is shown before the host is asked
// again; maxHovers bounds the links remembered, oldest forgotten first.
const (
	hoverFresh = 10 * time.Second
	maxHovers  = 256
)

// hoverAsking is what a link's hover shows until the host answers.
const hoverAsking = "boxer link — asking the host what it does…"

// NewHovers builds the hover cache of a surface whose texts author wrote.
// spawn starts the host's describe off the render goroutine, as
// FollowLogged's does; a nil bus or spawn answers no hover.
func NewHovers(bus app.BusI, author Author, spawn func(f func(ctx context.Context)) (started bool)) (inst *Hovers) {
	return &Hovers{bus: bus, author: author, spawn: spawn, entries: make(map[string]*hoverEntry)}
}

// Text is the hover text of a claimed link: the host's reading, or a line
// saying it is being asked for.
func (inst *Hovers) Text(link string) (text string) {
	if inst == nil || inst.bus == nil || inst.spawn == nil {
		return ""
	}
	inst.mu.Lock()
	e := inst.entries[link]
	if e == nil {
		e = &hoverEntry{text: hoverAsking}
		inst.entries[link] = e
		inst.order = append(inst.order, link)
		for len(inst.order) > maxHovers {
			delete(inst.entries, inst.order[0])
			inst.order = inst.order[1:]
		}
	}
	ask := !e.pending && (e.at.IsZero() || time.Since(e.at) > hoverFresh)
	if ask {
		e.pending = true
	}
	text = e.text
	inst.mu.Unlock()
	if ask {
		bus, author := inst.bus, inst.author
		started := inst.spawn(func(ctx context.Context) {
			rep, err := Describe(ctx, bus, link, author)
			t := rep.Text()
			if err != nil {
				t = "boxer link — the host did not say what it does: " + err.Error()
			}
			inst.mu.Lock()
			e.text, e.at, e.pending = t, time.Now(), false
			inst.mu.Unlock()
			hostwake.Wake()
		})
		if !started {
			inst.mu.Lock()
			e.pending = false
			inst.mu.Unlock()
		}
	}
	return
}

// HoverSet keeps one Hovers per author, for a surface whose texts have
// several authors — a transcript holds the person's words and a model's.
type HoverSet struct {
	bus   app.BusI
	spawn func(f func(ctx context.Context)) (started bool)
	mu    sync.Mutex
	m     map[Author]*Hovers
}

// NewHoverSet builds the hover caches of one surface.
func NewHoverSet(bus app.BusI, spawn func(f func(ctx context.Context)) (started bool)) (inst *HoverSet) {
	return &HoverSet{bus: bus, spawn: spawn, m: make(map[Author]*Hovers)}
}

// For is the link hover of text author wrote, for markdown.Input.LinkHover;
// nil when the surface has no bus to ask over.
func (inst *HoverSet) For(author Author) (hover func(link string) string) {
	if inst == nil || inst.bus == nil || inst.spawn == nil {
		return nil
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	h := inst.m[author]
	if h == nil {
		h = NewHovers(inst.bus, author, inst.spawn)
		inst.m[author] = h
	}
	return h.Text
}
