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
	// Do is the verb: click, type, set_value, focus, scroll_into_view, key,
	// scroll, wait, capture, cadence, resize, note, sleep.
	Do string `json:"do"`

	// Anchor (ADR-0127 §SD4). Id wins; then name/contains plus role; nth
	// disambiguates. Absent for verbs that target no widget.
	ID       uint64 `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Contains string `json:"contains,omitempty"`
	Role     string `json:"role,omitempty"`
	Nth      *int   `json:"nth,omitempty"`

	// Text carries the payload of `type`, `set_value`, `key` and `capture`
	// (the capture's file name) and the prose of `note`.
	Text string `json:"text,omitempty"`

	// X, Y are the last-rung coordinate target, for painter-only widgets that
	// have no node. Also the scroll delta for `scroll`.
	X float32 `json:"x,omitempty"`
	Y float32 `json:"y,omitempty"`

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
	// the tree cannot report on. Also the duration of `sleep`.
	SettleMs int `json:"settleMs,omitempty"`

	// Comment is free prose kept beside the step; the runner logs it.
	Comment string `json:"comment,omitempty"`
}

// hasAnchor reports whether the step names a widget at all.
func (inst Step) hasAnchor() bool {
	return inst.ID != 0 || inst.Name != "" || inst.Contains != "" || inst.Role != ""
}

// locator builds the anchor from a step.
func (inst Step) locator() (loc Locator) {
	loc = Locator{ID: inst.ID, Name: inst.Name, NameContains: inst.Contains, Role: inst.Role}
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
	case inst.ID != 0:
		b.WriteString(" #" + strconv.FormatUint(inst.ID, 10))
	}
	if inst.Role != "" {
		b.WriteString(" (" + inst.Role + ")")
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
		if err = runStep(c, st, node, opts); err != nil {
			return eb.Build().Int("step", i+1).Str("step_desc", st.describe()).
				Errorf("step failed: %w", err)
		}
		// Every verb but a pure observation can move the UI, so the cached
		// tree is assumed stale unless the verb only read.
		if st.Do != "wait" && st.Do != "note" && st.Do != "capture" {
			stale = true
		}
		settle := st.SettleMs
		if settle == 0 {
			settle = opts.SettleMs
		}
		if settle > 0 {
			time.Sleep(time.Duration(settle) * time.Millisecond)
		}
		log.Info().Msg(st.describe())
	}
	return nil
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
		time.Sleep(150 * time.Millisecond)
	}
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
		if node != nil {
			return c.ClickNode(node.GetId())
		}
		// No anchor given: the coordinate rung, for a painter-only target.
		return c.ClickAt(st.X, st.Y)
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
