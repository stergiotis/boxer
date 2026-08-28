package carrierclient

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// trace.go runs a recorded or hand-written interaction against a headless host.
//
// The step vocabulary is ADR-0127 §SD2's, reused verbatim so one trace format
// serves both executors: that ADR's replayer drives the desktop host over the
// inspection seam, this one drives a headless host over the carrier, and a
// trace should not care which it is pointed at. The anchor fields are §SD4's
// ladder — an exact node id first, a name and role next, a position last.
//
// The file is JSON Lines: one step per line, `#` and blank lines ignored, so a
// trace stays diffable and a reviewer can read it without tooling.

// Step is one line of a trace.
type Step struct {
	// Do is the verb: click, hover, drag, type, set_value, focus,
	// scroll_into_view, key, scroll, wait, capture, cadence, resize, note,
	// sleep.
	Do string `json:"do"`

	// Anchor (ADR-0127 §SD4). Id wins; then name/contains plus role; nth
	// disambiguates. Absent for verbs that target no widget.
	//
	// value / valueContains match the accessible value rather than the name,
	// which is the only way to reach static text: egui leaves a Label's name
	// empty and puts the text in the value. See [Locator].
	ID            uint64 `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	Contains      string `json:"contains,omitempty"`
	Value         string `json:"value,omitempty"`
	ValueContains string `json:"valueContains,omitempty"`
	Role          string `json:"role,omitempty"`
	Nth           *int   `json:"nth,omitempty"`

	// Text carries the payload of `type`, `set_value`, `key` and `capture`
	// (the capture's file name) and the prose of `note`.
	Text string `json:"text,omitempty"`

	// X, Y are the last-rung coordinate target, for painter-only widgets that
	// have no node. Also the scroll delta for `scroll`, and the start of a
	// coordinate `drag` (or its delta when the drag is anchored).
	X float32 `json:"x,omitempty"`
	Y float32 `json:"y,omitempty"`

	// ToX, ToY end a coordinate `drag` that started at X, Y. An anchored
	// `drag` ignores them: it starts at the node's centre and reads X, Y as
	// the delta, so a trace can say "drag this by 120 px" without knowing
	// where the widget was laid out. Steps is the number of pointer moves
	// between press and release (default 16), DurationMs the time they are
	// spread over (default 320) — a host at 20 fps sees several moves per
	// frame either way, which is what a real pointer produces.
	ToX        float32 `json:"toX,omitempty"`
	ToY        float32 `json:"toY,omitempty"`
	Steps      int     `json:"steps,omitempty"`
	DurationMs int     `json:"durationMs,omitempty"`

	// Pointer makes an anchored `click` press the resolved node's bounds
	// centre with a synthetic pointer instead of sending it an AccessKit
	// click action.
	//
	// It is the rung between "resolve by anchor" and "click a coordinate",
	// and it exists for widgets whose clickable region is not the node the
	// text lives in. A tree row is the case that found it: the row's sense
	// region sits *behind* its cells so the disclosure control can win its own
	// rect (ADR-0176 SD7), which leaves the row's label an ordinary
	// non-interactive node — findable, but deaf to an action request. Aiming
	// at where that label was drawn hits the region behind it.
	//
	// Prefer the default. An action request goes to the widget that declared
	// it and does not care what is drawn on top; a pointer press is a
	// position, and a tooltip or an overlay in the way will take it.
	Pointer bool `json:"pointer,omitempty"`

	// Modifiers is the key modifier bitmask (1=alt, 2=ctrl, 4=shift,
	// 8=mac_cmd, 16=command), matching egui::Modifiers.
	Modifiers uint32 `json:"modifiers,omitempty"`

	// Cadence for the `cadence` verb: 0 continuous, 1 reactive.
	Cadence uint32 `json:"cadence,omitempty"`

	// W, H, Scale for the `resize` verb.
	W     float32 `json:"w,omitempty"`
	H     float32 `json:"h,omitempty"`
	Scale float32 `json:"scale,omitempty"`

	// SettleMs pauses after the step, for animations and debounced fetches
	// the tree cannot report on. Also the duration of `sleep`. A `capture`
	// pauses before the shot instead, so the pause is what the frame shows.
	SettleMs int `json:"settleMs,omitempty"`

	// Comment is free prose kept beside the step; the runner logs it.
	Comment string `json:"comment,omitempty"`
}

// hasAnchor reports whether the step names a widget at all.
func (inst Step) hasAnchor() bool {
	return inst.ID != 0 || inst.Name != "" || inst.Contains != "" ||
		inst.Value != "" || inst.ValueContains != "" || inst.Role != ""
}

// locator builds the anchor from a step.
func (inst Step) locator() (loc Locator) {
	loc = Locator{
		ID: inst.ID, Name: inst.Name, NameContains: inst.Contains,
		Value: inst.Value, ValueContains: inst.ValueContains, Role: inst.Role,
	}
	if inst.Nth != nil {
		loc.Nth, loc.HasNth = *inst.Nth, true
	}
	return loc
}

// describe renders the step for a log line — the readable half of ADR-0127's
// dual-layer step, produced here rather than stored.
func (inst Step) describe() (s string) {
	var b strings.Builder
	b.WriteString(inst.Do)
	switch {
	case inst.Name != "":
		b.WriteString(" " + strconv.Quote(inst.Name))
	case inst.Contains != "":
		b.WriteString(" ~" + strconv.Quote(inst.Contains))
	case inst.Value != "":
		b.WriteString(" =" + strconv.Quote(inst.Value))
	case inst.ValueContains != "":
		b.WriteString(" ~=" + strconv.Quote(inst.ValueContains))
	case inst.ID != 0:
		b.WriteString(" #" + strconv.FormatUint(inst.ID, 10))
	}
	if inst.Role != "" {
		b.WriteString(" (" + inst.Role + ")")
	}
	if inst.Do == "drag" {
		if inst.hasAnchor() {
			b.WriteString(" by " + fmtPoint(inst.X, inst.Y))
		} else {
			b.WriteString(" " + fmtPoint(inst.X, inst.Y) + " -> " + fmtPoint(inst.ToX, inst.ToY))
		}
	}
	if inst.Text != "" {
		b.WriteString(" " + strconv.Quote(inst.Text))
	}
	return b.String()
}

// ParseTrace reads a JSON Lines trace.
func ParseTrace(r io.Reader) (steps []Step, err error) {
	sc := bufio.NewScanner(r)
	// A tree-heavy trace stays well inside this, but the default 64 KiB line
	// cap is low enough that a long `type` step could trip it.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var st Step
		if err = json.Unmarshal([]byte(text), &st); err != nil {
			return nil, eb.Build().Int("line", line).
				Errorf("unable to parse trace step at line %d: %w", line, err)
		}
		if st.Do == "" {
			return nil, eb.Build().Int("line", line).
				Errorf("trace step at line %d has no \"do\" verb", line)
		}
		steps = append(steps, st)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("unable to read trace: %w", err)
	}
	return steps, nil
}

// RunOptions tunes a trace run.
type RunOptions struct {
	// Timeout bounds each individual request to the host.
	Timeout time.Duration
	// SettleMs is applied after any step that does not set its own.
	SettleMs int
	// DryRun resolves every anchor and reports what it would do, without
	// sending input or writing captures. The cheap way to find out whether a
	// trace still matches the app after a UI change.
	DryRun bool
	Logger zerolog.Logger
}

// RunTrace executes steps against a connected client.
//
// Anchors resolve against a tree fetched lazily and re-fetched after anything
// that could have changed the UI, so a trace pays for one snapshot per group of
// steps rather than one per step.
func RunTrace(c *Client, steps []Step, opts RunOptions) (err error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	var tree *TreeSnapshot
	// stale marks the cached tree as needing a refresh before the next anchor
	// resolution. Starts true so the first anchored step fetches one.
	stale := true
	refresh := func() error {
		if !stale && tree != nil {
			return nil
		}
		var e error
		if tree, e = c.Tree(opts.Timeout); e != nil {
			return e
		}
		stale = false
		return nil
	}

	for i, st := range steps {
		log := opts.Logger.With().Int("step", i+1).Str("do", st.Do).Logger()
		if st.Comment != "" {
			log = log.With().Str("comment", st.Comment).Logger()
		}

		// `wait` polls rather than resolving once: the thing worth waiting for
		// is usually a node that is present but not yet ready — play greys Run
		// while a query is in flight, so "Run is enabled again" is a precise
		// "the result has landed", and a single resolution would just fail on
		// the disabled state it exists to wait out.
		if st.Do == "wait" {
			if !st.hasAnchor() {
				return eb.Build().Int("step", i+1).Errorf("wait needs an anchor")
			}
			if err = waitFor(c, st, opts); err != nil {
				return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
					Errorf("wait failed: %w", err)
			}
			stale = true // the tree it settled on is newer than any we cached
			tree = nil
			log.Info().Msg(st.describe())
			continue
		}

		var node *TreeNode
		switch {
		case st.hasAnchor():
			if err = refresh(); err != nil {
				return eb.Build().Int("step", i+1).Errorf("unable to fetch the tree: %w", err)
			}
			if node, err = Resolve(tree, st.locator()); err != nil {
				return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
					Errorf("unable to resolve the anchor: %w", err)
			}
			log = log.With().Uint64("node", node.GetId()).Str("path", Path(tree, node)).Logger()
		case requiresAnchor(st.Do):
			// `click` may fall back to a coordinate, so it is not on this list;
			// the rest are meaningless without a widget to aim at.
			return eb.Build().Int("step", i+1).Str("do", st.Do).
				Errorf("step needs an anchor (id, name, contains or role)")
		}

		if opts.DryRun {
			log.Info().Msg("dry run: " + st.describe())
			continue
		}
		settle := time.Duration(st.SettleMs) * time.Millisecond
		if settle == 0 {
			settle = time.Duration(opts.SettleMs) * time.Millisecond
		}
		if settleBefore(st.Do) && settle > 0 {
			if err = c.Idle(settle); err != nil {
				return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
					Errorf("settle before the step failed: %w", err)
			}
		}
		if err = runStep(c, st, node, opts); err != nil {
			return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
				Errorf("step failed: %w", err)
		}
		// Every verb but a pure observation can move the UI, so the cached
		// tree is assumed stale unless the verb only read.
		if st.Do != "wait" && st.Do != "note" && st.Do != "capture" {
			stale = true
		}
		if !settleBefore(st.Do) && settle > 0 {
			// Idle, not time.Sleep: the pause must keep answering the
			// carrier's keepalive or a long `sleep` gets the driver reaped.
			if err = c.Idle(settle); err != nil {
				return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
					Errorf("settle after the step failed: %w", err)
			}
		}
		log.Info().Msg(st.describe())
	}
	return nil
}

// settleBefore reports whether a verb's settle runs before it rather than
// after. A capture's pause exists so the frame it takes has settled — an
// animation finished, a debounced fetch landed — which is only useful before
// the shot; every other verb settles afterwards, for what it set in motion.
func settleBefore(do string) bool {
	return do == "capture"
}

// waitFor polls the tree until the step's anchor resolves to a node that is
// present and enabled, or the timeout runs out.
//
// The poll interval is short relative to a human gesture but long relative to a
// frame: each round trip costs the host one AccessKit tree build, and a wait is
// usually seconds long.
func waitFor(c *Client, st Step, opts RunOptions) (err error) {
	deadline := time.Now().Add(opts.Timeout)
	var last error
	for attempt := 0; ; attempt++ {
		var snap *TreeSnapshot
		if snap, err = c.Tree(opts.Timeout); err != nil {
			return err
		}
		node, e := Resolve(snap, st.locator())
		switch {
		case e != nil:
			last = e
		case node.GetFlags()&FlagDisabled != 0:
			last = eb.Build().Uint64("node", node.GetId()).
				Errorf("node is present but disabled")
		default:
			return nil
		}
		if time.Now().After(deadline) {
			return eb.Build().Int("attempts", attempt+1).
				Errorf("still not ready after %s: %w", opts.Timeout, last)
		}
		if err = c.Idle(150 * time.Millisecond); err != nil {
			return err
		}
	}
}

// Defaults for a `drag` step that sets neither: sixteen moves over 320 ms is
// a brisk pan — slow enough that a 20 fps host sees a handful of dragged
// frames, fast enough not to dominate a trace.
const (
	defaultDragSteps      = 16
	defaultDragDurationMs = 320
)

// fmtPoint renders a coordinate pair for a log line.
func fmtPoint(x, y float32) string {
	return "(" + strconv.FormatFloat(float64(x), 'f', -1, 32) + "," +
		strconv.FormatFloat(float64(y), 'f', -1, 32) + ")"
}

// requiresAnchor reports whether a verb is meaningless without a widget.
// `click` is deliberately absent: it falls back to a coordinate, which is the
// last rung of the ladder and the only way to reach a painter-only widget.
func requiresAnchor(do string) bool {
	switch do {
	case "type", "set_value", "focus", "scroll_into_view", "wait":
		return true
	default:
		return false
	}
}

func runStep(c *Client, st Step, node *TreeNode, opts RunOptions) (err error) {
	switch st.Do {
	case "click":
		switch {
		case node != nil && st.Pointer:
			// Anchored, but actuated where the node was drawn — see Step.Pointer.
			x, y := nodeCentre(node)
			return c.ClickAt(x, y)
		case node != nil:
			return c.ClickNode(node.GetId())
		}
		// No anchor given: the coordinate rung, for a painter-only target.
		return c.ClickAt(st.X, st.Y)
	case "hover":
		// Move the pointer without pressing. Coordinate-only, and for the
		// same reason `click` keeps that rung: a hover affordance drawn on the
		// painter lane — a heatmap cell, a plot's crosshair — has no
		// accessibility node to anchor on. Hover state is read from a register
		// one frame behind (ADR-0140), so a `capture` after this sees it.
		//
		// Without this verb no hover affordance in any imzero2 app was
		// reachable from a trace at all, which is how a heatmap shipped with
		// none: the tour could click a cell but never point at one.
		return c.MoveMouse(st.X, st.Y)
	case "type":
		// Focus first: text goes to whatever egui thinks is focused, which
		// without this is whatever the previous step left.
		if err = c.FocusNode(node.GetId()); err != nil {
			return err
		}
		return c.TypeText(st.Text)
	case "set_value":
		return c.SetNodeValue(node.GetId(), st.Text)
	case "focus":
		return c.FocusNode(node.GetId())
	case "scroll_into_view":
		return c.ScrollNodeIntoView(node.GetId())
	case "key":
		return c.PressKey(st.Text, st.Modifiers)
	case "scroll":
		return c.Scroll(st.X, st.Y)
	case "drag":
		// A press, moves, a release — the gesture a pan, a slider or a
		// drag-to-select needs, which `click` cannot produce. Coordinate-only
		// like `hover`, for the same reason: the things worth dragging on the
		// painter lane (a map, a plot, a brush) have no node to anchor on.
		// Anchored, it starts at the node's centre and X, Y is the delta.
		x0, y0, x1, y1 := st.X, st.Y, st.ToX, st.ToY
		if node != nil {
			x0, y0 = nodeCentre(node)
			x1, y1 = x0+st.X, y0+st.Y
		}
		steps, dur := st.Steps, st.DurationMs
		if steps <= 0 {
			steps = defaultDragSteps
		}
		if dur <= 0 {
			dur = defaultDragDurationMs
		}
		return c.Drag(x0, y0, x1, y1, steps, time.Duration(dur)*time.Millisecond)
	case "capture":
		name := st.Text
		if name == "" {
			return eh.Errorf("capture step needs a name in \"text\"")
		}
		done, e := c.Capture(name, opts.Timeout)
		if e != nil {
			return e
		}
		opts.Logger.Info().Str("path", done.GetPath()).
			Uint32("width", done.GetWidth()).Uint32("height", done.GetHeight()).
			Msg("captured")
		return nil
	case "cadence":
		return c.SetCadence(st.Cadence)
	case "resize":
		scale := st.Scale
		if scale <= 0 {
			scale = 1
		}
		return c.Resize(st.W, st.H, scale)
	case "sleep", "note":
		// `sleep` is served by the settle below; `note` is prose for the log.
		return nil
	default:
		return eb.Build().Str("do", st.Do).Errorf("unknown trace verb")
	}
}
