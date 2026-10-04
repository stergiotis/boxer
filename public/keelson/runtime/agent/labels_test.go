package agent

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func TestConfinedContentStaysAHandleForARemoteModel(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	r.host.person(7, func(d *doc) { d.confined = true })
	r.host.frame(7)
	g := r.grant(ModeAct)
	q := r.call(g, "q", "get_text", "{}")
	require.Equal(t, "completed", q.Phase)
	assert.True(t, q.Confined)
	res, err := r.cli.Read(ctx, g.Handle, q.ResultRef)
	require.NoError(t, err)
	assert.Empty(t, res.Text, "no text of a confined result for a remote model")
	assert.Equal(t, q.ResultRef, res.DataHandle)
	assert.True(t, res.Confined)

	insts, err := r.cli.List(ctx, g.Handle)
	require.NoError(t, err)
	require.Len(t, insts, 1)
	assert.Empty(t, insts[0].Title, "a confined window's title is withheld")
}

func TestConfinedContentReachesALocalModel(t *testing.T) {
	r := newRigWith(t, func(cfg *Config) {
		cfg.TestGrants = true
		cfg.ModelLocal = func() bool { return true }
	})
	r.host.person(7, func(d *doc) { d.confined = true })
	r.host.frame(7)
	g := r.grant(ModeAct)
	q := r.call(g, "q", "get_text", "{}")
	res, err := r.cli.Read(context.Background(), g.Handle, q.ResultRef)
	require.NoError(t, err)
	assert.Equal(t, `{"text":"start"}`, res.Text)
}

func TestUntrustedContentIsAttributedAndTaints(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	q := r.call(g, "q", "get_text", "{}")
	res, err := r.cli.Read(context.Background(), g.Handle, q.ResultRef)
	require.NoError(t, err)
	assert.True(t, res.Untrusted)
	assert.Contains(t, res.Source, "window 7")
	assert.Contains(t, res.Source, "get_text")

	r.call(g, "q2", "get_text", "{}")
	var tainted bool
	for _, a := range r.svc.Actions() {
		if a.Key == "q2" {
			tainted = tainted || a.Tainted
		}
	}
	assert.True(t, tainted, "every action after the read records the taint")
}

func TestAReferenceArgumentIsResolvedForTheApp(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	q := r.call(g, "q", "get_text", "{}")
	out := r.call(g, "u", "use_ref", `{"source":"`+q.ResultRef+`"}`)
	require.Equal(t, "accepted", out.Phase, out.Reason)
	r.host.frame(7)
	got, err := buscodec.Decode[textResult](r.host.docs[7].gotRef)
	require.NoError(t, err)
	assert.Equal(t, "start", got.Text, "the app reads what the reference names")

	out = r.call(g, "u2", "use_ref", `{"source":"ref-nothing"}`)
	assert.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "names no result")
}

func TestTheCallCarriesTheOnBehalfOfContext(t *testing.T) {
	r := newRig(t, true)
	g, err := r.cli.Request(context.Background(), GrantRequest{Destinations: []string{"http:tiles"},
		Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	require.NoError(t, err)
	ok, why := r.svc.AllowDestination(g.Task, 1, "http:tiles")
	assert.True(t, ok, why)
	ok, why = r.svc.AllowDestination(g.Task, 1, "llm")
	assert.False(t, ok)
	assert.Contains(t, why, "does not list")
	ok, _ = r.svc.AllowDestination(g.Task, 2, "http:tiles")
	assert.False(t, ok, "a moved epoch refuses")
	require.NoError(t, r.cli.Stop(context.Background(), g.Handle))
	ok, why = r.svc.AllowDestination(g.Task, 2, "http:tiles")
	assert.False(t, ok)
	assert.Contains(t, why, "ended")
}

// Launch says how far the window had loaded rather than reporting it
// opened: a Mount that failed, or one that had not returned within the
// bound, reaches the caller and the window list (ADR-0269 §SD3).
func TestLaunchReportsAWindowThatFailedOrIsStillOpening(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g, err := r.cli.Request(ctx, GrantRequest{Launches: []GrantLaunch{{App: "doc", Mode: ModeAct, Count: 2}}})
	require.NoError(t, err)

	r.host.mu.Lock()
	r.host.openAs, r.host.openReason = opwire.LoadFailed, "no database"
	r.host.mu.Unlock()
	got, err := r.cli.Launch(ctx, g.Handle, "doc", "", nil)
	require.NoError(t, err, "the window exists: it shows the person the failure")
	assert.Equal(t, "failed", got.Load)
	assert.Equal(t, "no database", got.LoadReason)

	r.host.mu.Lock()
	r.host.openAs, r.host.openReason = opwire.LoadOpening, ""
	r.host.mu.Unlock()
	start := time.Now()
	got2, err := r.cli.Launch(ctx, g.Handle, "doc", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "opening", got2.Load)
	assert.GreaterOrEqual(t, time.Since(start), launchSettle, "launch waited its bound")

	insts, err := r.cli.List(ctx, g.Handle)
	require.NoError(t, err)
	loads := map[uint64]string{}
	for _, i := range insts {
		loads[i.Instance] = i.Load
	}
	assert.Equal(t, "failed", loads[got.Instance])
	assert.Equal(t, "opening", loads[got2.Instance])
}

func TestATaskOpensTheWindowsItsGrantAllows(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g, err := r.cli.Request(ctx, GrantRequest{Launches: []GrantLaunch{{App: "doc", Mode: ModeAct, Count: 1}}})
	require.NoError(t, err)
	got, err := r.cli.Launch(ctx, g.Handle, "doc", "", nil)
	require.NoError(t, err)
	key := got.Instance
	assert.Equal(t, "ready", got.Load, "a host that does not track loading reports its windows ready")
	r.host.frame(key)
	out, err := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: key, Operation: "get_text", Args: "{}", Key: "q"})
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase, "the opened window joins the task")

	_, err = r.cli.Launch(ctx, g.Handle, "doc", "", nil)
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "one window was allowed")

	require.NoError(t, r.cli.Stop(ctx, g.Handle))
	assert.Equal(t, g.Task, r.svc.leftByTask(key), "the window passes to the person")
}

// A capture of several windows names each one the grant lacks; a crop
// reaches the capture service; and a capture is labelled by the most
// sensitive window it draws, so a confined one stays a handle for a remote
// model (ADR-0281 §SD6).
func TestACaptureIsScopedAndLabelledByItsWindows(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g := r.grant(ModeObserve)
	out, err := r.cli.CaptureWith(ctx, CaptureRequest{Handle: g.Handle, Instances: []uint64{7, 99}, Format: CaptureFormatPng, Key: "two"})
	require.NoError(t, err)
	assert.Equal(t, "input_required", out.Phase)
	assert.Contains(t, out.Reason, "window 99")

	crop := [4]float32{0, 0, 1, 1}
	out, err = r.cli.CaptureWith(ctx, CaptureRequest{Handle: g.Handle, Instances: []uint64{7}, Format: CaptureFormatPng, Crop: &crop, Key: "crop"})
	require.NoError(t, err)
	require.Equal(t, "completed", out.Phase, out.Reason)
	res, err := r.cli.Read(ctx, g.Handle, out.Job)
	require.NoError(t, err)
	b, err := os.ReadFile(res.Path)
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	assert.Equal(t, 1, img.Bounds().Dx(), "the crop, one point at one pixel per point")

	r.host.person(7, func(d *doc) { d.confined = true })
	r.host.frame(7)
	out, err = r.cli.CaptureWith(ctx, CaptureRequest{Handle: g.Handle, Instances: []uint64{7}, Format: CaptureFormatPng, Key: "confined"})
	require.NoError(t, err)
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined)
	_, err = r.cli.Read(ctx, g.Handle, out.Job)
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "confined capture")
}
