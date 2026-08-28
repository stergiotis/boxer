package carrierclient

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"google.golang.org/protobuf/proto"
)

// One-byte WebSocket message-type prefixes (ADR-0024 SD6). Mirrors
// `rust/imzero2/src/imzero2/inputproto.rs`; the prefixes are framing, not part
// of the protobuf schema, so they are the one thing here not generated.
const (
	prefixVideo   byte = 0x01
	prefixInput   byte = 0x02
	prefixSession byte = 0x03
	prefixMesh    byte = 0x04
)

// AccessKit action codes (ADR-0154 SD3), pinned by the wire rather than by
// AccessKit's own enum. Mirrors `treemap.rs`.
const (
	ActionClick          uint32 = 0
	ActionFocus          uint32 = 1
	ActionSetValue       uint32 = 2
	ActionScrollIntoView uint32 = 3
)

// TreeNode flag bits (ADR-0154 SD2).
const (
	FlagDisabled uint32 = 1
	FlagHidden   uint32 = 2
	FlagFocused  uint32 = 4
	FlagSelected uint32 = 8
)

// Client drives an imzero2 headless host over its remote-access carrier
// (ADR-0024) — the seam that needs no compositor, no browser and no
// `egui_inspection` port.
//
// It is deliberately *not* a viewer: video access units are counted and
// discarded. Pixels come back through [Client.Capture], which asks the host to
// write a PNG and tells us where it landed, so a caller chooses the moment
// rather than sifting a periodic frame dump.
//
// One goroutine may call the request methods; they serialize internally.
type Client struct {
	ws  *wsConn
	log zerolog.Logger

	// sendMu serializes frame writes. Reads happen on the caller's goroutine
	// inside the request methods, so no read lock is needed.
	sendMu sync.Mutex

	hello      *SessionHello
	framesSeen uint64
}

// Config holds the connection parameters.
type Config struct {
	// URL of the carrier's WebSocket, e.g. "ws://127.0.0.1:8089/". Note the
	// carrier serves the viewer page on the same port and on port+1.
	URL string
	// Label shown in the host's roster (ADR-0086), for the operator's benefit
	// when a human viewer is watching the same host.
	Label string
	// DialTimeout bounds the TCP connect and the opening handshake.
	DialTimeout time.Duration
	Logger      zerolog.Logger
}

// Connect dials the carrier and completes the session handshake: it announces
// itself, then waits for the host's SessionHello so the caller knows the stream
// geometry before it computes anything in logical points.
//
// The first connection to a carrier is admitted ACTIVE (ADR-0086), and only the
// active connection's input, tree requests and captures are honoured — so a
// driver run against a host someone is already watching will be passive and
// silently ineffective. [Client.Hello] plus the host's roster log are how that
// shows up; taking the session from a human is deliberately not automatic.
func Connect(cfg Config) (inst *Client, err error) {
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.Label == "" {
		cfg.Label = "carrierclient"
	}
	ws, err := wsDial(cfg.URL, cfg.DialTimeout)
	if err != nil {
		return nil, err
	}
	inst = &Client{ws: ws, log: cfg.Logger}
	// webcodecs=false is the honest answer — this client decodes nothing — and
	// it keeps the host from offering it a takeover it cannot use well.
	err = inst.sendControl(&SessionControl{
		Control: &SessionControl_ClientHello{ClientHello: &ClientHello{
			Webcodecs: false,
			Label:     cfg.Label,
		}},
	})
	if err != nil {
		_ = ws.close()
		return nil, err
	}
	// The hello is the host's first control message; wait for it so callers can
	// rely on Hello() being populated.
	deadline := time.Now().Add(cfg.DialTimeout)
	for inst.hello == nil {
		if _, err = inst.pump(deadline); err != nil {
			_ = ws.close()
			return nil, eh.Errorf("unable to read session hello: %w", err)
		}
	}
	return inst, nil
}

// Hello returns the stream geometry the host announced, refreshed whenever it
// re-announces (a resize or a codec switch).
func (inst *Client) Hello() *SessionHello { return inst.hello }

// Close tears the connection down.
func (inst *Client) Close() (err error) { return inst.ws.close() }

// sendControl frames and sends one session-control message.
func (inst *Client) sendControl(msg *SessionControl) (err error) {
	return inst.send(prefixSession, msg)
}

// SendInput sends one input event.
func (inst *Client) SendInput(ev *InputEvent) (err error) {
	return inst.send(prefixInput, ev)
}

func (inst *Client) send(prefix byte, msg proto.Message) (err error) {
	body, err := proto.Marshal(msg)
	if err != nil {
		return eh.Errorf("unable to marshal wire message: %w", err)
	}
	framed := make([]byte, 0, 1+len(body))
	framed = append(framed, prefix)
	framed = append(framed, body...)
	inst.sendMu.Lock()
	defer inst.sendMu.Unlock()
	return inst.ws.writeBinary(framed)
}

// pump reads one message and folds it into the client's state, returning the
// session control it carried (nil for video and mesh frames, which are counted
// and dropped).
func (inst *Client) pump(deadline time.Time) (ctl *SessionControl, err error) {
	payload, err := inst.ws.readBinary(deadline)
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, nil
	}
	prefix, body := payload[0], payload[1:]
	switch prefix {
	case prefixVideo, prefixMesh:
		// Not a viewer: the stream is counted so a caller can tell the host is
		// producing, and otherwise discarded.
		inst.framesSeen++
		return nil, nil
	case prefixSession:
		ctl = &SessionControl{}
		if err = proto.Unmarshal(body, ctl); err != nil {
			return nil, eh.Errorf("unable to decode session control: %w", err)
		}
		if h, ok := ctl.GetControl().(*SessionControl_Hello); ok {
			inst.hello = h.Hello
			inst.log.Debug().
				Uint32("widthPx", h.Hello.GetWidthPx()).
				Uint32("heightPx", h.Hello.GetHeightPx()).
				Float32("pixelsPerPoint", h.Hello.GetPixelsPerPoint()).
				Msg("carrier session hello")
		}
		return ctl, nil
	default:
		inst.log.Debug().Int("prefix", int(prefix)).Msg("unknown carrier message prefix")
		return nil, nil
	}
}

// Idle keeps the connection serviced for d: it pumps and discards whatever
// the carrier sends — answering its keepalive pings on the way — and returns
// once d has elapsed. A plain time.Sleep leaves the pings unanswered and the
// carrier reaps an unresponsive peer after its keepalive window (about ten
// seconds), so every pause in a trace goes through here. A connection the
// carrier closes, or that stalls mid-frame, is an error.
func (inst *Client) Idle(d time.Duration) (err error) {
	deadline := time.Now().Add(d)
	for {
		if _, err = inst.pump(deadline); err == nil {
			continue
		}
		switch {
		case errors.Is(err, errIdleTimeout):
			return nil
		case err == io.EOF:
			return eh.Errorf("carrier closed the connection")
		case os.IsTimeout(err):
			return eh.Errorf("carrier stalled mid-frame")
		default:
			return err
		}
	}
}

// awaitControl pumps until a control message satisfies want, or the deadline
// passes. Everything else seen on the way is folded in as usual.
func (inst *Client) awaitControl(
	deadline time.Time,
	want func(*SessionControl) bool,
) (ctl *SessionControl, err error) {
	for {
		ctl, err = inst.pump(deadline)
		if err != nil {
			if err == io.EOF {
				return nil, eh.Errorf("carrier closed the connection")
			}
			if os.IsTimeout(err) {
				return nil, eh.Errorf("timed out waiting for the carrier")
			}
			return nil, err
		}
		if ctl != nil && want(ctl) {
			return ctl, nil
		}
	}
}

// Tree asks for the accessibility tree and returns the next snapshot
// (ADR-0154 SD1).
//
// The request also switches AccessKit generation on host-side for as long as
// something is asking, so the first call costs a pass more than later ones.
func (inst *Client) Tree(timeout time.Duration) (snap *TreeSnapshot, err error) {
	err = inst.sendControl(&SessionControl{
		Control: &SessionControl_TreeRequest{TreeRequest: &TreeRequest{}},
	})
	if err != nil {
		return nil, err
	}
	ctl, err := inst.awaitControl(time.Now().Add(timeout), func(c *SessionControl) bool {
		_, ok := c.GetControl().(*SessionControl_TreeSnapshot)
		return ok
	})
	if err != nil {
		return nil, eh.Errorf("unable to read tree snapshot: %w", err)
	}
	return ctl.GetTreeSnapshot(), nil
}

// Capture asks the host to write the current frame as a PNG under its dump
// directory and returns the acknowledgement (ADR-0154 SD4).
//
// The host reduces name to a basename and owns the directory, so the path it
// reports back may differ from what was asked for; use the returned path.
// A host with no IMZERO2_HEADLESS_DUMP_DIR ignores the request, which surfaces
// here as a timeout.
func (inst *Client) Capture(name string, timeout time.Duration) (done *CaptureDone, err error) {
	err = inst.sendControl(&SessionControl{
		Control: &SessionControl_CaptureRequest{CaptureRequest: &CaptureRequest{Name: name}},
	})
	if err != nil {
		return nil, err
	}
	ctl, err := inst.awaitControl(time.Now().Add(timeout), func(c *SessionControl) bool {
		_, ok := c.GetControl().(*SessionControl_CaptureDone)
		return ok
	})
	if err != nil {
		return nil, eb.Build().Str("name", name).
			Errorf("unable to read capture acknowledgement (is IMZERO2_HEADLESS_DUMP_DIR set?): %w", err)
	}
	return ctl.GetCaptureDone(), nil
}

// ClickNode actuates a widget by node id, with no coordinates involved
// (ADR-0154 SD3).
func (inst *Client) ClickNode(nodeID uint64) (err error) {
	return inst.action(nodeID, ActionClick, "")
}

// FocusNode moves keyboard focus to a node.
func (inst *Client) FocusNode(nodeID uint64) (err error) {
	return inst.action(nodeID, ActionFocus, "")
}

// SetNodeValue sets a widget's value directly — sliders and drag values take a
// numeric string, text edits the literal text.
func (inst *Client) SetNodeValue(nodeID uint64, value string) (err error) {
	return inst.action(nodeID, ActionSetValue, value)
}

// ScrollNodeIntoView brings a node into the visible area, which is how a driver
// reaches a widget the layout has scrolled away.
func (inst *Client) ScrollNodeIntoView(nodeID uint64) (err error) {
	return inst.action(nodeID, ActionScrollIntoView, "")
}

func (inst *Client) action(nodeID uint64, action uint32, value string) (err error) {
	return inst.SendInput(&InputEvent{
		Event: &InputEvent_AccesskitAction{AccesskitAction: &AccessKitAction{
			NodeId: nodeID,
			Action: action,
			Value:  value,
		}},
	})
}

// MoveMouse and ClickAt are the coordinate fallback, for painter-only widgets
// (a timeline, a treemap, a map) that have no accessibility node at all.
// Prefer the node actions above wherever a node exists: a coordinate that has
// gone stale still lands somewhere, and the wrong widget answers.
func (inst *Client) MoveMouse(x, y float32) (err error) {
	return inst.SendInput(&InputEvent{
		Event: &InputEvent_MouseMove{MouseMove: &MouseMove{X: x, Y: y}},
	})
}

// ClickAt presses and releases the primary button at a position. The events are
// sent back to back; the host applies every event queued since the last pass in
// one batch, so egui sees the press and release together.
func (inst *Client) ClickAt(x, y float32) (err error) {
	if err = inst.MoveMouse(x, y); err != nil {
		return err
	}
	for _, pressed := range []bool{true, false} {
		err = inst.SendInput(&InputEvent{
			Event: &InputEvent_MouseButton{MouseButton: &MouseButton{
				X: x, Y: y, Button: 0, Pressed: pressed,
			}},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// TypeText sends printable text to the focused widget.
func (inst *Client) TypeText(text string) (err error) {
	return inst.SendInput(&InputEvent{
		Event: &InputEvent_Text{Text: &TextInput{Text: text}},
	})
}

// PressKey sends a key down/up pair. Names follow the browser's
// KeyboardEvent.key, which is also what egui::Key::from_name accepts
// ("Enter", "ArrowDown", "A").
func (inst *Client) PressKey(key string, modifiers uint32) (err error) {
	for _, pressed := range []bool{true, false} {
		err = inst.SendInput(&InputEvent{
			Event: &InputEvent_Key{Key: &KeyEvent{
				Key: key, Pressed: pressed, Modifiers: modifiers,
			}},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Scroll sends a wheel delta in points at the current pointer position.
func (inst *Client) Scroll(dx, dy float32) (err error) {
	return inst.SendInput(&InputEvent{
		Event: &InputEvent_MouseWheel{MouseWheel: &MouseWheel{Dx: dx, Dy: dy}},
	})
}

// Drag presses the primary button at (x0, y0), moves the pointer to (x1, y1)
// in `steps` evenly spaced moves spread over `dur`, and releases there. The
// press goes out on its own and every move waits its share of `dur`, so the
// host sees a drag-started frame, dragged frames and a drag-stopped frame —
// the shape a pan, a slider or a drag-to-select needs, and the one thing
// [Client.ClickAt]'s back-to-back press/release cannot produce. steps < 1
// becomes one move; dur <= 0 sends the moves without pausing.
func (inst *Client) Drag(x0, y0, x1, y1 float32, steps int, dur time.Duration) (err error) {
	if err = inst.MoveMouse(x0, y0); err != nil {
		return err
	}
	if err = inst.mouseButton(x0, y0, true); err != nil {
		return err
	}
	path := dragPath(x0, y0, x1, y1, steps)
	var pause time.Duration
	if dur > 0 {
		pause = dur / time.Duration(len(path))
	}
	for _, p := range path {
		if pause > 0 {
			time.Sleep(pause)
		}
		if err = inst.MoveMouse(p[0], p[1]); err != nil {
			return err
		}
	}
	return inst.mouseButton(x1, y1, false)
}

func (inst *Client) mouseButton(x, y float32, pressed bool) (err error) {
	return inst.SendInput(&InputEvent{
		Event: &InputEvent_MouseButton{MouseButton: &MouseButton{
			X: x, Y: y, Button: 0, Pressed: pressed,
		}},
	})
}

// dragPath is the `steps` pointer positions a drag from (x0, y0) to (x1, y1)
// visits after the press, the last of them the end point itself. steps < 1 is
// treated as 1.
func dragPath(x0, y0, x1, y1 float32, steps int) (path [][2]float32) {
	if steps < 1 {
		steps = 1
	}
	path = make([][2]float32, steps)
	for i := 1; i <= steps; i++ {
		t := float32(i) / float32(steps)
		path[i-1] = [2]float32{x0 + (x1-x0)*t, y0 + (y1-y0)*t}
	}
	return
}

// SetCadence switches the host's render cadence: 0 = continuous, 1 = reactive.
//
// Reactive is the default and the right one for a served deployment, but a
// driver waiting on an animation or a debounced fetch may want the host
// producing steadily.
func (inst *Client) SetCadence(cadence uint32) (err error) {
	return inst.sendControl(&SessionControl{
		Control: &SessionControl_SetCadence{SetCadence: &SetCadence{Cadence: cadence}},
	})
}

// Resize asks the host for a different logical viewport. The host clamps and
// re-announces its hello, which [Client.Hello] then reflects.
func (inst *Client) Resize(width, height, scale float32) (err error) {
	return inst.sendControl(&SessionControl{
		Control: &SessionControl_ViewportResize{ViewportResize: &ViewportResize{
			LogicalWidth: width, LogicalHeight: height, PixelScale: scale,
		}},
	})
}
