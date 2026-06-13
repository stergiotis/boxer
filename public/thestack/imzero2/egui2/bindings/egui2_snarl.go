package bindings

// SnarlEventKindE discriminates the variants of SnarlEvent. Mirror of the
// SNARL_EV_* constants in src/rust/src/imzero2/interpreter.rs; change
// both in lockstep if the set evolves.
type SnarlEventKindE uint32

const (
	SnarlEventKindNodeMoved         SnarlEventKindE = 1
	SnarlEventKindNodeRemoved       SnarlEventKindE = 2
	SnarlEventKindConnectionAdded   SnarlEventKindE = 3
	SnarlEventKindConnectionRemoved SnarlEventKindE = 4
	SnarlEventKindNodeSelected      SnarlEventKindE = 5
	SnarlEventKindNodeDeselected    SnarlEventKindE = 6
	SnarlEventKindNodeOpenChanged   SnarlEventKindE = 7
)

// SnarlWireStyleE picks the rendering style for wires. Mirror of
// egui_snarl::ui::WireStyle.
type SnarlWireStyleE uint8

const (
	SnarlWireStyleBezier5     SnarlWireStyleE = 0 // 5th-degree Bezier (default)
	SnarlWireStyleAxisAligned SnarlWireStyleE = 1
	SnarlWireStyleBezier3     SnarlWireStyleE = 2
	SnarlWireStyleLine        SnarlWireStyleE = 3
)

// SnarlBackgroundPatternE picks the background pattern. Mirror of
// egui_snarl::ui::BackgroundPattern.
type SnarlBackgroundPatternE uint8

const (
	SnarlBackgroundNone SnarlBackgroundPatternE = 0
	SnarlBackgroundGrid SnarlBackgroundPatternE = 1 // default
)

// SnarlPinSideE selects whether a pin is an input or an output.
type SnarlPinSideE uint8

const (
	SnarlPinSideInput  SnarlPinSideE = 0
	SnarlPinSideOutput SnarlPinSideE = 1
)

// SnarlEvent is one decoded user-edit event captured during the previous frame.
//
// Per-kind field usage (per the SNARL_EV_* contract):
//
//	NodeMoved          NodeId, X, Y                          (PortA/NodeIdB/PortB = 0)
//	NodeRemoved        NodeId
//	ConnectionAdded    NodeId=src, PortA=srcPort,
//	                   NodeIdB=dst, PortB=dstPort
//	ConnectionRemoved  same shape as ConnectionAdded
//	NodeSelected       NodeId
//	NodeDeselected     NodeId
//	NodeOpenChanged    NodeId, PortA = open ? 1 : 0
//
// EditorId carries the editor the event came from, so a frame with
// multiple SnarlEditor widgets can dispatch correctly.
type SnarlEvent struct {
	EditorId uint64
	Kind     SnarlEventKindE
	NodeId   uint64
	PortA    uint32
	NodeIdB  uint64
	PortB    uint32
	X, Y     float32
}

// FetchSnarlEvents returns the previous frame's snarl interaction
// events, drained and decoded at frame-end by StateManager.Sync. Call
// at most once per frame; the cache is refilled on the next Sync.
//
// The slice is owned by the StateManager and reused next frame —
// callers that need to retain entries past this frame must copy.
//
// Pre-cache (before the M3 dock host) this wrapper invoked the inline
// fetcher directly. That worked at top scope but deadlocked when the
// snarl editor was mounted inside a dock.Tab body (deferred-block
// capture buffers SendIntermediate instead of flushing it to Rust).
// Reading the StateManager cache is the only safe inline-render path.
func FetchSnarlEvents() []SnarlEvent {
	return CurrentApplicationState.StateManager.GetSnarlEvents()
}
