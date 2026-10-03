package breadcrumbs

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Model is the trail, one slice per attribute over item ordinals. Labels
// fixes the item count; each optional slice is nil or exactly as long.
type Model struct {
	// Labels names each item. It is the item's button text.
	Labels []string
	// Icons is a glyph drawn before an item's label; "" for none.
	Icons []string
	// Done marks an item finished; it is drawn with a tick.
	Done []bool
	// Locked is why an item cannot be visited; "" for an item that can.
	Locked []string
}

// Len is the number of items.
func (inst *Model) Len() int { return len(inst.Labels) }

// Validate reports a co-array whose length disagrees with Labels.
func (inst *Model) Validate() (err error) {
	n := len(inst.Labels)
	check := func(name string, l int) {
		if err == nil && l != 0 && l != n {
			err = eb.Build().Str("slice", name).Int("len", l).Int("labels", n).Errorf("co-array length disagrees with Labels")
		}
	}
	check("Icons", len(inst.Icons))
	check("Done", len(inst.Done))
	check("Locked", len(inst.Locked))
	return
}

func (inst *Model) icon(i int) (s string) {
	if i < len(inst.Icons) {
		s = inst.Icons[i]
	}
	return
}

func (inst *Model) done(i int) (b bool) {
	if i < len(inst.Done) {
		b = inst.Done[i]
	}
	return
}

func (inst *Model) locked(i int) (s string) {
	if i < len(inst.Locked) {
		s = inst.Locked[i]
	}
	return
}

// State is what the trail keeps between frames: the current item. The zero
// value is usable and makes the last item current.
type State struct {
	cur int32 // ordinal+1; 0 means the last item
}

// Current is the current item's ordinal, or -1 when the last item is
// current by default.
func (inst *State) Current() int32 { return inst.cur - 1 }

// SetCurrent makes an item current; a negative ordinal returns to the
// default, the last item.
func (inst *State) SetCurrent(ordinal int32) {
	if ordinal < 0 {
		inst.cur = 0
		return
	}
	inst.cur = ordinal + 1
}

// resolve is the current ordinal for a trail of n items: the set one while
// it is in range, the last one otherwise.
func (inst *State) resolve(n int) (cur int) {
	cur = int(inst.cur) - 1
	if cur < 0 || cur >= n {
		cur = n - 1
	}
	return
}
