// Package scenetest is the headless scene support of ADR-0224's
// 2026-09-12 update, shared by the graphview and nav tests: a fffi2
// channel with no host behind it, so Render runs without a client, and
// the canvas and area handles a View derives, so a scripted register lands
// on the widget's own surfaces. It imports no testing package; a test
// calls Install and registers the returned reset as its cleanup.
package scenetest

import (
	"iter"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// discardChannel drops every paint command and never receives anything.
// Its method names are the channel interface's, not this package's.
type discardChannel struct{}

var _ runtime.ChannelI[*runtime.Unmarshaller] = discardChannel{}

func (discardChannel) SyncMultiUseMsg(uint64, []byte) {}
func (discardChannel) SendSingleUseMsg([]byte)        {}
func (discardChannel) FlushMessages()                 {}
func (discardChannel) ReceiveMsg() iter.Seq[*runtime.Unmarshaller] {
	return func(func(*runtime.Unmarshaller) bool) {}
}

// Install points the fffi2 runtime at a channel that discards every paint
// command and clears the scripted registers of the current state manager.
// The returned func clears them again, for a test's cleanup.
func Install() (reset func()) {
	typed.SetCurrentFffiVar(runtime.NewFffi2[*runtime.Unmarshaller](discardChannel{}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	return sm.ScriptReset
}

// Handles derives the canvas and area handles a graphview.View constructed
// with key on ids will use, without consuming the id stack's state, so a
// test can script their registers.
func Handles(ids *c.WidgetIdStack, key string) (canvas, area widgethandle.WidgetHandle) {
	for range c.IdScope(ids.PrepareStr(key)) {
		canvas = widgethandle.Make(ids.PrepareStr("graphview-canvas").Derive())
		area = widgethandle.Make(ids.PrepareStr("graphview-area").Derive())
	}
	return
}
