package externaledit

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

func call[Req any, Resp any](ctx context.Context, bus app.BusI, subject string, req Req, d time.Duration) (resp Resp, err error) {
	b, err := buscodec.Encode(req)
	if err != nil {
		return resp, eh.Errorf("externaledit: encode: %w", err)
	}
	raw, err := app.RequestWithin(ctx, bus, d, subject, b)
	if err != nil {
		return resp, eb.Build().Str("subject", subject).Errorf("externaledit: %w", err)
	}
	resp, err = buscodec.Decode[Resp](raw)
	if err != nil {
		err = eh.Errorf("externaledit: decode: %w", err)
	}
	return
}

// Give hands text over under name and returns the edit's id. It blocks on
// the bus; call it off the frame goroutine.
func Give(ctx context.Context, bus app.BusI, name string, text []byte) (id string, err error) {
	r, err := call[Offer, Offered](ctx, bus, SubjectOffer, Offer{Name: name, Text: text}, 10*time.Second)
	if err != nil {
		return
	}
	if r.Reason != "" {
		return "", eb.Build().Str("reason", r.Reason).Errorf("externaledit: refused: %s", r.Reason) //boxer:lint disable=CS013 reason="Editing.Status shows err.Error() to the person; the service\'s reason is the message"
	}
	return r.Id, nil
}

// Open opens the host's editor on the edit id names. It blocks on the bus;
// call it off the frame goroutine.
func Open(ctx context.Context, bus app.BusI, id string) (err error) {
	b, err := buscodec.Encode(launchcfg.ExternalEditLaunch{At: time.Now().UTC(), Id: id})
	if err != nil {
		return eh.Errorf("externaledit: encode the launch config: %w", err)
	}
	_, err = windowopen.RequestOpenKind(ctx, bus, launchcfg.Kind, b)
	return
}

// Next waits for the first revision after after, for at most d. A revision
// with the same Seq means nothing changed in time.
func Next(ctx context.Context, bus app.BusI, id string, after uint64, d time.Duration) (rev Revision, err error) {
	if d <= 0 || d > MaxWait {
		d = MaxWait
	}
	rev, err = call[Wait, Revision](ctx, bus, SubjectWait, Wait{Id: id, After: after, WaitMs: uint32(d / time.Millisecond)}, d+5*time.Second)
	if err == nil && rev.Reason != "" && !rev.Closed {
		err = eb.Build().Str("reason", rev.Reason).Errorf("externaledit: refused: %s", rev.Reason) //boxer:lint disable=CS013 reason="Editing.Status shows err.Error() to the person; the service\'s reason is the message"
	}
	return
}

// End closes an edit; its file is removed.
func End(ctx context.Context, bus app.BusI, id string) (err error) {
	r, err := call[CloseRequest, Closed](ctx, bus, SubjectClose, CloseRequest{Id: id}, 10*time.Second)
	if err == nil && r.Reason != "" {
		err = eb.Build().Str("reason", r.Reason).Errorf("externaledit: refused: %s", r.Reason) //boxer:lint disable=CS013 reason="Editing.Status shows err.Error() to the person; the service\'s reason is the message"
	}
	return
}

// Locate tells an editor where an edit's file is, naming the session that
// will edit it.
func Locate(ctx context.Context, bus app.BusI, id string, session string, agent bool) (dir string, file string, err error) {
	r, err := call[Resolve, Resolved](ctx, bus, SubjectResolve, Resolve{Id: id, Session: session, Agent: agent}, 10*time.Second)
	if err != nil {
		return
	}
	if r.Reason != "" {
		return "", "", eb.Build().Str("reason", r.Reason).Errorf("externaledit: refused: %s", r.Reason) //boxer:lint disable=CS013 reason="Editing.Status shows err.Error() to the person; the service\'s reason is the message"
	}
	return r.Dir, r.File, nil
}
