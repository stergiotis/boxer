package applink

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostwake"
)

// Hovers answers a markdown surface's link hover (ADR-0409
// §SD8) from what the host said of each link: Text runs on the render
// goroutine once per claimed link each frame and never blocks, so it
// returns the last answer and, for a link not yet asked about, asks the
// host off the render goroutine and wakes a pass when the answer lands. An
// inline link's reading follows from the registry and is kept; a handle's
// changes when its task ends, so it is asked again once its answer has
// aged. A page with many links is asked about a few at a time, and a link
// still on screen is never forgotten, so no frame asks again what the
// previous frame asked.
type Hovers struct {
	bus    app.BusI
	author Author
	spawn  func(f func(ctx context.Context)) (started bool)

	mu      sync.Mutex
	entries map[string]*hoverEntry
	order   []string
	pending int
}

type hoverEntry struct {
	text    string
	at      time.Time
	used    time.Time
	pending bool
}

// hoverFresh is how long a handle's answer is shown before the host is
// asked again; maxHovers is the number of links remembered past which
// links not drawn for hoverIdle are forgotten, oldest first; maxAsking
// bounds the asks in flight.
const (
	hoverFresh = 10 * time.Second
	hoverIdle  = 2 * time.Second
	maxHovers  = 256
	maxAsking  = 4
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
	now := time.Now()
	inst.mu.Lock()
	e := inst.entries[link]
	if e == nil {
		e = &hoverEntry{text: hoverAsking, used: now}
		inst.entries[link] = e
		inst.order = append(inst.order, link)
		inst.forgetIdle(now)
	}
	e.used = now
	stale := e.at.IsZero() || (strings.HasPrefix(link, Prefix+"l/") && now.Sub(e.at) > hoverFresh)
	ask := !e.pending && stale && inst.pending < maxAsking
	if ask {
		e.pending = true
		inst.pending++
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
			inst.pending--
			inst.mu.Unlock()
			hostwake.Wake()
		})
		if !started {
			inst.mu.Lock()
			e.pending = false
			inst.pending--
			inst.mu.Unlock()
		}
	}
	return
}

// forgetIdle forgets, oldest first, links not drawn for hoverIdle while
// more than maxHovers are remembered. A link drawn recently stays however
// many there are: forgetting it would ask about it again next frame. The
// caller holds mu.
func (inst *Hovers) forgetIdle(now time.Time) {
	if len(inst.order) <= maxHovers {
		return
	}
	kept := inst.order[:0]
	for _, l := range inst.order {
		e := inst.entries[l]
		if len(inst.entries) > maxHovers && !e.pending && now.Sub(e.used) > hoverIdle {
			delete(inst.entries, l)
			continue
		}
		kept = append(kept, l)
	}
	inst.order = kept
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
