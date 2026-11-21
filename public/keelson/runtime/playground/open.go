package playground

import (
	"context"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Open opens the playground on launch over the audited `runtime.windowhost.open`
// subject. It names the kind and no app, so the host resolves which app
// opens — the launchable app taking the kind, the one designated at the
// wiring site if several do, or a refusal naming them (ADR-0361 §SD2). The
// caller's manifest needs Pub on windowopen.OpenSubject; caller identity is
// attributed by the bus. Open blocks on the bus round-trip, so call it off
// the frame loop. On success it returns the opened window's key.
func Open(ctx context.Context, bus app.BusI, launch Launch) (windowKey uint64, err error) {
	cfg, err := buscodec.Encode(launch)
	if err != nil {
		err = eh.Errorf("playground: encode launch config: %w", err)
		return
	}
	return windowopen.RequestOpenKind(ctx, bus, Kind, cfg)
}
