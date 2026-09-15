package chatview

import (
	"fmt"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// FlagsE is a message's bitset of markers.
type FlagsE uint8

const (
	// FlagSystem marks a system line — a join, a leave, an encryption notice
	// — drawn centred without a bubble. Its Sender is ignored.
	FlagSystem FlagsE = 1 << iota
	// FlagDeleted marks a retracted message: the bubble shows a placeholder
	// and the body is not drawn.
	FlagDeleted
	// FlagEdited marks an edited message; [Model.EditedMS] carries when.
	FlagEdited
)

// StatusE is a message's delivery status, shown on the viewer's own bubbles.
type StatusE uint8

const (
	StatusNone StatusE = iota
	StatusSent
	StatusDelivered
	StatusRead
	StatusFailed
)

// LayoutE selects the transcript's arrangement.
type LayoutE uint8

const (
	// LayoutAuto derives the layout from the data: a dialogue when exactly
	// two participants speak and the viewer is one of them, a group
	// otherwise.
	LayoutAuto LayoutE = iota
	// LayoutDialogue: no names, no avatars; the viewer right, the other left.
	LayoutDialogue
	// LayoutGroup: names in the participant's colour and an initials disc on
	// every cluster; the viewer's own bubbles on the right when a viewer is
	// set.
	LayoutGroup
)

// Participant is one party to the conversation.
type Participant struct {
	Name string
	// Color is the participant's colour for the name and the avatar disc. A
	// zero Color takes the design system's qualitative cycle by index.
	Color color.Color
}

// Model is the transcript, one slice per attribute over message ordinals in
// display order (ascending time). Every per-message slice has the same length;
// EditedMS may be nil when no message is edited, and the reaction slices may
// all be nil when there are none. [Model.Validate] checks the invariants.
type Model struct {
	// TimeMS is the send time, UTC epoch milliseconds, ascending.
	TimeMS []int64
	// Sender indexes Participants; -1 for a system line.
	Sender []int32
	// Body is the plain text. A message the host draws through [Input.Block]
	// still carries it: the quote strip and the reply preview read it.
	Body []string
	// ReplyTo is the ordinal of the quoted message, -1 for none.
	ReplyTo []int32
	Flags   []FlagsE
	Status  []StatusE
	// EditedMS is when an edited message was edited; read only where
	// FlagEdited is set. May be nil.
	EditedMS []int64

	Participants []Participant

	// The reactions of message i are ReactionKey[ReactionOff[i]:ReactionOff[i+1]]
	// with their ReactionCount and ReactionWho — a ragged co-array, the
	// leeway values-plus-offsets idiom. len(ReactionOff) is len(TimeMS)+1, or
	// 0 when the model carries no reactions at all.
	ReactionOff   []int32
	ReactionKey   []string
	ReactionCount []int32
	// ReactionWho lists who reacted, already joined for the hover; "" when
	// unknown.
	ReactionWho []string
}

// Len is the number of messages.
func (inst *Model) Len() int { return len(inst.TimeMS) }

// Validate checks the slice lengths and index ranges. A model that fails it
// is not drawn; Render shows the reason instead.
func (inst *Model) Validate() error {
	n := len(inst.TimeMS)
	if len(inst.Sender) != n || len(inst.Body) != n || len(inst.ReplyTo) != n ||
		len(inst.Flags) != n || len(inst.Status) != n {
		return fmt.Errorf("chatview: per-message slices disagree on length (TimeMS %d, Sender %d, Body %d, ReplyTo %d, Flags %d, Status %d)",
			n, len(inst.Sender), len(inst.Body), len(inst.ReplyTo), len(inst.Flags), len(inst.Status))
	}
	if inst.EditedMS != nil && len(inst.EditedMS) != n {
		return fmt.Errorf("chatview: EditedMS has %d entries for %d messages", len(inst.EditedMS), n)
	}
	np := int32(len(inst.Participants))
	for i := range n {
		if s := inst.Sender[i]; s < -1 || s >= np {
			return fmt.Errorf("chatview: message %d names participant %d of %d", i, s, np)
		}
		if r := inst.ReplyTo[i]; r < -1 || int(r) >= n {
			return fmt.Errorf("chatview: message %d replies to ordinal %d of %d", i, r, n)
		}
		if i > 0 && inst.TimeMS[i] < inst.TimeMS[i-1] {
			return fmt.Errorf("chatview: message %d is earlier than message %d", i, i-1)
		}
	}
	if len(inst.ReactionOff) == 0 {
		if len(inst.ReactionKey) != 0 {
			return fmt.Errorf("chatview: %d reaction keys without offsets", len(inst.ReactionKey))
		}
		return nil
	}
	if len(inst.ReactionOff) != n+1 {
		return fmt.Errorf("chatview: ReactionOff has %d entries for %d messages", len(inst.ReactionOff), n)
	}
	nr := len(inst.ReactionKey)
	if len(inst.ReactionCount) != nr || len(inst.ReactionWho) != nr {
		return fmt.Errorf("chatview: reaction slices disagree on length (Key %d, Count %d, Who %d)",
			nr, len(inst.ReactionCount), len(inst.ReactionWho))
	}
	if inst.ReactionOff[0] != 0 || int(inst.ReactionOff[n]) != nr {
		return fmt.Errorf("chatview: ReactionOff spans [%d, %d) for %d reactions", inst.ReactionOff[0], inst.ReactionOff[n], nr)
	}
	for i := range n {
		if inst.ReactionOff[i] > inst.ReactionOff[i+1] {
			return fmt.Errorf("chatview: ReactionOff is not monotone at message %d", i)
		}
	}
	return nil
}

// Reactions returns the reaction slices of message i (all empty when the
// model has none).
func (inst *Model) Reactions(i int) (keys []string, counts []int32, who []string) {
	if len(inst.ReactionOff) == 0 || i < 0 || i+1 >= len(inst.ReactionOff) {
		return
	}
	lo, hi := inst.ReactionOff[i], inst.ReactionOff[i+1]
	return inst.ReactionKey[lo:hi], inst.ReactionCount[lo:hi], inst.ReactionWho[lo:hi]
}

// State is what survives frames. The zero value is usable: nothing selected,
// following the tail, the default window.
type State struct {
	sel      int32 // ordinal+1; 0 none
	unfollow bool
	window   int
	jump     int32 // ordinal+1; 0 none
	paneW    float32
	shown    bool
}

// Selected is the selected message's ordinal, -1 for none.
func (inst *State) Selected() int32 { return inst.sel - 1 }

// SetSelected selects a message by ordinal; a negative ordinal clears.
func (inst *State) SetSelected(ordinal int32) {
	if ordinal < 0 {
		inst.sel = 0
		return
	}
	inst.sel = ordinal + 1
}

// Follow reports whether the view is pinned to the tail.
func (inst *State) Follow() bool { return !inst.unfollow }

// SetFollow pins the view to the tail (true) or releases it.
func (inst *State) SetFollow(on bool) { inst.unfollow = !on }

// Window is how many of the newest messages are drawn.
func (inst *State) Window() int {
	if inst.window <= 0 {
		return defaultWindow
	}
	return inst.window
}

// SetWindow sets how many of the newest messages are drawn; 0 restores the
// default.
func (inst *State) SetWindow(n int) { inst.window = n }

// JumpTo asks the next Render to bring a message into view, widening the
// window when the message is older than it, and releases the tail.
func (inst *State) JumpTo(ordinal int32) {
	if ordinal < 0 {
		inst.jump = 0
		return
	}
	inst.jump = ordinal + 1
	inst.unfollow = true
}

// Block is a host-drawn message body: Render runs at draw time inside the
// bubble and must scope its own widget ids. Height is the height the host
// expects the body to take, advisory in this cut — the scroll area lays the
// body out itself — and the contract the etable path would read.
type Block struct {
	Height float32
	Render func()
}

// Input is the per-frame render request.
type Input struct {
	// Ids is the host's widget id stack; Render opens IdScope(ScopeKey)
	// around the whole transcript, so two instances under one parent need
	// distinct keys.
	Ids      *c.WidgetIdStack
	ScopeKey string
	Model    *Model
	State    *State
	// Viewer is the participant whose messages sit on the right; -1 for
	// nobody.
	Viewer int32
	Layout LayoutE
	// Location formats the day separators and the hover times; nil is UTC.
	Location *time.Location
	// Block draws a message's body in the host's own way; nil, or a false
	// second result, draws Model.Body wrapped.
	Block func(ordinal int) (Block, bool)
	// FillHost tells Render its host already bounds its height, so the
	// transcript fills that rect rather than flooring to a minimum. Dock-tab
	// leaves set it; an unbounded gallery scroll host leaves it false.
	FillHost bool
	// BubbleFraction is the bubble's maximum width as a fraction of the
	// pane's; 0 is the default.
	BubbleFraction float32
}

// Result is what the frame's input did.
type Result struct {
	// Clicked is the ordinal of the bubble clicked this frame, -1 for none.
	// The selection has already moved to it.
	Clicked int32
	// JumpTo is the ordinal of a quote strip clicked this frame, -1 for none.
	// The state's jump has already been requested.
	JumpTo int32
	// OlderWanted reports the "older" affordance was pressed; the window
	// has already widened.
	OlderWanted bool
}
