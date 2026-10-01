package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func TestConfinedContentStaysAHandleForARemoteModel(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	r.host.docs[7].confined = true
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
	r.host.docs[7].confined = true
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
