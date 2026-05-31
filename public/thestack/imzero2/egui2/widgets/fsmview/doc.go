//go:build llm_generated_opus47

// Package fsmview provides a two-level finite-state-machine visualization
// widget for the ImZero2 framework.
//
// Level 1 is a compact chip (built on widgets/badge) that shows the
// current state of an FSM. Clicking the chip opens a floating Window
// containing Level 2 — a side-by-side table and force-directed graph
// view of the full state machine with the active state highlighted.
//
// Basic usage:
//
//	m := fsmview.NewMachine[string]("red", 16,
//	    fsmview.WithLabel(strings.ToUpper),
//	    fsmview.WithStateOrder([]string{"red", "yellow", "green"}),
//	).
//	    AddRule("red", "green").
//	    AddRule("green", "yellow").
//	    AddRule("yellow", "red")
//
//	v := fsmview.New(ids, "traffic", m)
//	for range c.Window(...).KeepIter() {
//	    v.Render()
//	}
//
// # Tethered mode
//
// [Widget.Tethered] promotes the level-1 chip to a "tethered inspector
// summary" (ADR-0046): the state badge gains an [inspector.AnchorToggle] and
// the level-2 window is linked back to it by the spring-animated bezier
// [inspector.AnchorTether]. Pair with [Widget.Summary] for a caller-supplied
// stat line and [Widget.BadgeTone] to colour the badge by state severity. Off
// by default — plain chips keep the click-to-open popup.
//
// # Choice of FSM library
//
// The widget couples tightly to [statetrooper.FSM]: generic over the
// caller's state type ([comparable] constraint), small public surface,
// no callback machinery (which clashes with the immediate-mode poll-each-
// frame discipline). statetrooper's ruleset is unexported, so [Machine]
// mirrors rules locally for graph/table enumeration — see [Machine.AddRule].
//
// # State management
//
// The widget is composite, not an FFFI2 primitive — popup-open / selected-
// renderer state lives on the *[Widget] receiver and the caller holds
// the pointer across frames. Multiple instances coexist safely (each
// scopes its emitted ids by scopeKey).
//
// # Concurrency
//
// Both [Machine] and [Widget] expect single-goroutine access from the
// render thread. The underlying [statetrooper.FSM] is mutex-protected,
// but [Machine]'s local mirrors (rules, edge labels, node-id cache,
// display order) are not — calling [Machine.AddRule] from one goroutine
// while another reads via [Machine.Edges] or [Widget.Render] races on
// the unsynchronised maps. Typical IM usage (setup on the main
// goroutine, render on the render goroutine, no cross-thread mutation)
// is safe.
//
// See [ADR-0045] for the full design rationale, alternatives considered,
// and milestone plan.
//
// [statetrooper.FSM]: https://pkg.go.dev/github.com/hishamk/statetrooper#FSM
// [ADR-0045]: ../../../../../../../doc/adr/0045-imzero2-fsmview-widget.md
package fsmview
