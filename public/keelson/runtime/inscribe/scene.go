package inscribe

import (
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// OpE is what a mark draws (ADR-0297 §SD5).
type OpE uint8

const (
	OpHighlight OpE = iota + 1
	OpCallout
	OpArrow
	OpStep
	OpSpotlight
)

var opNames = map[string]OpE{"highlight": OpHighlight, "callout": OpCallout, "arrow": OpArrow,
	"step": OpStep, "spotlight": OpSpotlight}

// ParseOp reads an op by its name.
func ParseOp(s string) (op OpE, ok bool) {
	op, ok = opNames[s]
	return
}

func (inst OpE) String() string {
	for k, v := range opNames {
		if v == inst {
			return k
		}
	}
	return "unknown"
}

// The scene's bounds (ADR-0297 §SD7, §SD5).
const (
	// MaxPerTask is how many marks one task may hold.
	MaxPerTask = 16
	// MaxScene is how many the scene holds across tasks.
	MaxScene = 64
	// MaxText is the longest note or label, in characters.
	MaxText = 120
	// MaxTargets is the most targets one mark names.
	MaxTargets = 8
)

// Mark is one mark a task asked for.
type Mark struct {
	Task    string
	Id      string
	Op      OpE
	Targets []Anchor
	// Text is the note or label; plain and single-line.
	Text string
}

// Item is a mark as the scene holds it: with its task's hue, its
// step number when it is a step, and its arrival order.
type Item struct {
	Mark
	Hue  int
	Step int
	Seq  uint64
}

// Scene holds every task's marks. Safe for concurrent use: the agent
// service writes from the bus, the overlay reads on the render goroutine.
type Scene struct {
	mu    sync.Mutex
	items []Item
	seq   uint64
	// hues are the hues of tasks holding marks; steps the last step
	// number each task drew.
	hues  map[string]int
	steps map[string]int
}

// NewScene returns an empty scene.
func NewScene() *Scene {
	return &Scene{hues: map[string]int{}, steps: map[string]int{}}
}

// Len is the number of marks held.
func (inst *Scene) Len() (n int) {
	inst.mu.Lock()
	n = len(inst.items)
	inst.mu.Unlock()
	return
}

// Put adds a mark, or replaces the task's mark with the same
// id. It refuses what the op does not take, text past MaxText, and what
// would take the task past MaxPerTask or the scene past MaxScene; it never
// evicts another mark.
func (inst *Scene) Put(a Mark) (err error) {
	if a.Task == "" || a.Id == "" {
		err = eh.Errorf("inscribe: a mark names its task and an id")
		return
	}
	if err = checkTargets(a.Op, a.Targets); err != nil {
		return
	}
	a.Text = strings.Join(strings.Fields(a.Text), " ")
	if n := len([]rune(a.Text)); n > MaxText {
		err = eb.Build().Int("len", n).Int("max", MaxText).Errorf("inscribe: the text is too long")
		return
	}
	if (a.Op == OpCallout || a.Op == OpStep) && a.Text == "" {
		err = eb.Build().Str("op", a.Op.String()).Errorf("inscribe: the op needs text")
		return
	}
	a.Targets = slices.Clone(a.Targets)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.seq++
	for i := range inst.items {
		it := &inst.items[i]
		if it.Task == a.Task && it.Id == a.Id {
			// A step replaced by a step keeps its number.
			step := 0
			switch {
			case a.Op == OpStep && it.Op == OpStep:
				step = it.Step
			case a.Op == OpStep:
				inst.steps[a.Task]++
				step = inst.steps[a.Task]
			}
			*it = Item{Mark: a, Hue: it.Hue, Step: step, Seq: inst.seq}
			return
		}
	}
	held := 0
	for _, it := range inst.items {
		if it.Task == a.Task {
			held++
		}
	}
	switch {
	case held >= MaxPerTask:
		err = eb.Build().Int("max", MaxPerTask).Errorf("inscribe: the task holds as many marks as it may; clear some")
		return
	case len(inst.items) >= MaxScene:
		err = eb.Build().Int("max", MaxScene).Errorf("inscribe: the overlay is full; the person clears it")
		return
	}
	hue, ok := inst.hues[a.Task]
	if !ok {
		hue = inst.freeHueLocked()
		inst.hues[a.Task] = hue
	}
	step := 0
	if a.Op == OpStep {
		inst.steps[a.Task]++
		step = inst.steps[a.Task]
	}
	inst.items = append(inst.items, Item{Mark: a, Hue: hue, Step: step, Seq: inst.seq})
	return
}

// freeHueLocked is the hue the fewest tasks hold, lowest first: distinct
// until there are more tasks than hues.
func (inst *Scene) freeHueLocked() (hue int) {
	count := make([]int, len(neon))
	for _, h := range inst.hues {
		count[h]++
	}
	for h := range count {
		if count[h] < count[hue] {
			hue = h
		}
	}
	return
}

func checkTargets(op OpE, targets []Anchor) (err error) {
	n := len(targets)
	want := ""
	switch op {
	case OpHighlight, OpSpotlight:
		if n < 1 || n > MaxTargets {
			want = "one or more targets"
		}
	case OpCallout, OpStep:
		if n != 1 {
			want = "one target"
		}
	case OpArrow:
		if n != 2 {
			want = "two targets, from and to"
		}
	default:
		err = eh.Errorf("inscribe: no such op")
		return
	}
	if want != "" {
		err = eb.Build().Str("op", op.String()).Int("targets", n).Errorf("inscribe: the op takes %s", want)
		return
	}
	for _, t := range targets {
		if !t.Valid() {
			err = eh.Errorf("inscribe: a target names a window, a window and a rect, or a viewport rect")
			return
		}
	}
	return
}

// Clear removes the task's mark by id, or all of the task's when id
// is empty, and returns how many went.
func (inst *Scene) Clear(task string, id string) (n int) {
	return inst.removeWhere(func(it Item) bool { return it.Task == task && (id == "" || it.Id == id) })
}

// ClearAll removes every mark: the person's clear.
func (inst *Scene) ClearAll() (n int) {
	return inst.removeWhere(func(Item) bool { return true })
}

// ClearWindow removes the marks of task — of every task when task is
// empty — that point at window: it left the grant, or closed.
func (inst *Scene) ClearWindow(task string, window uint64) (n int) {
	return inst.removeWhere(func(it Item) bool {
		if task != "" && it.Task != task {
			return false
		}
		for _, t := range it.Targets {
			if t.Window == window {
				return true
			}
		}
		return false
	})
}

// Count is the number of marks task holds.
func (inst *Scene) Count(task string) (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, it := range inst.items {
		if it.Task == task {
			n++
		}
	}
	return
}

// Tasks lists the tasks holding marks, in order of their first.
func (inst *Scene) Tasks() (tasks []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, it := range inst.items {
		if !slices.Contains(tasks, it.Task) {
			tasks = append(tasks, it.Task)
		}
	}
	return
}

// Snapshot copies the marks in arrival order of their latest put.
func (inst *Scene) Snapshot() (items []Item) {
	inst.mu.Lock()
	items = slices.Clone(inst.items)
	inst.mu.Unlock()
	slices.SortStableFunc(items, func(a, b Item) int {
		switch {
		case a.Seq < b.Seq:
			return -1
		case a.Seq > b.Seq:
			return 1
		}
		return 0
	})
	return
}

func (inst *Scene) removeWhere(drop func(Item) bool) (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	kept := inst.items[:0]
	for _, it := range inst.items {
		if drop(it) {
			n++
			continue
		}
		kept = append(kept, it)
	}
	inst.items = kept
	for task := range inst.hues {
		held := false
		for _, it := range inst.items {
			if it.Task == task {
				held = true
				break
			}
		}
		if !held {
			// A task's hue and step numbers last while it has marks.
			delete(inst.hues, task)
			delete(inst.steps, task)
		}
	}
	return
}
