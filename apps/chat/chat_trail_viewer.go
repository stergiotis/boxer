package chat

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Open in trail viewer (ADR-0350, proposed): the trail viewer opened on the
// conversation case — its agentic rows and the audit events that reference
// it — over the last week. The viewer is a window of its own; chat only
// names the case.

// trailSpan is the window Open in trail viewer asks for, ending now.
const trailSpan = 7 * 24 * time.Hour

const tipOpenTrailViewer = "Open the trail viewer on this conversation: its model calls, actions and grants, and the audit events that reference it, over the last week."

var atomsOpenTrailViewer = c.Atoms().Text(icons.PhFootprints + " Open in trail viewer").Keep()

// trailLaunchConfig is the viewer's launch config for conversation conv.
func trailViewerLaunchConfig(conv string) (cfg []byte, err error) {
	cfg, err = buscodec.Encode(launchcfg.TrailViewerLaunch{Case: "conversation", Value: conv, LastNanos: int64(trailSpan)})
	if err != nil {
		err = eh.Errorf("encode the trail viewer's launch config: %w", err)
	}
	return
}

// openTrail asks the window host for a trail viewer on conv. It makes a bus
// round trip, so it runs off the render thread.
func openTrailViewer(bus app.BusI, conv string) (note string, err error) {
	cfg, err := trailViewerLaunchConfig(conv)
	if err != nil {
		return
	}
	if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
		err = eh.Errorf("open the trail viewer: %w", err)
		return
	}
	note = "opened the trail viewer on conversation " + conv
	return
}

// renderOpenTrailViewer is the Analytics tab's Open in trail viewer button.
func (inst *App) renderOpenTrailViewer(busy bool) {
	for range c.HoverText(tipOpenTrailViewer).KeepIter() {
		if c.Button(inst.ids.PrepareStr("stats-trail"), atomsOpenTrailViewer).SendResp().HasPrimaryClicked() && !busy {
			inst.openTrailViewerGesture()
		}
	}
}

// openTrailGesture opens the viewer on this window's conversation, off the
// render thread, reporting through the handover note.
func (inst *App) openTrailViewerGesture() {
	bus, conv := inst.bus, inst.conv.id
	switch {
	case bus == nil:
		inst.handoverNote = "no bus: Open in trail viewer needs the app runtime"
		return
	case conv == "":
		inst.handoverNote = "no conversation yet: Open in trail viewer needs one"
		return
	}
	inst.handoverNote = ""
	inst.handover.Start(inst.tasks, bgjob.Spec{Kind: "chat-open-trail", Title: "open the trail viewer on the conversation"},
		func(ctx context.Context) (note *string, err error) {
			n, err := openTrailViewer(bus, conv)
			if err != nil {
				return
			}
			note = &n
			return
		})
}
