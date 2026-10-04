package capture

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

type fakeSource struct {
	rendered [][]uint64
	result   SourceResult
}

func (inst *fakeSource) RenderSvg(w uint64, recheck func() bool) (string, error) {
	inst.rendered = append(inst.rendered, []uint64{w})
	return "src-1", nil
}
func (inst *fakeSource) RenderPixels(ws []uint64, recheck func() bool) (string, error) {
	inst.rendered = append(inst.rendered, ws)
	return "src-1", nil
}
func (inst *fakeSource) SourceStatus(string) (SourceResult, bool) { return inst.result, true }

func covers(ws ...uint64) Facts {
	return Facts{Covered: func(w uint64) bool {
		for _, x := range ws {
			if x == w {
				return true
			}
		}
		return false
	}}
}

// stamp is a policy that permits with an obligation of its own.
type stamp struct{ name string }

func (inst stamp) Name() string { return "stamp" }
func (inst stamp) Decide(Request, Facts) Decision {
	return Decision{Effect: EffectPermit, Obligations: []Obligation{{Name: inst.name, Version: 1}}}
}

type refuse struct{}

func (inst refuse) Name() string                   { return "refuse" }
func (inst refuse) Decide(Request, Facts) Decision { return Decision{Effect: EffectDeny, Reason: "no"} }

func rgba(w, h int) []byte {
	b := make([]byte, w*h*4)
	for i := range b {
		b[i] = 0xff
	}
	return b
}

func TestTheGrantPolicyDeniesAnUncoveredWindow(t *testing.T) {
	d := GrantPolicy{}.Decide(Request{Windows: []uint64{1, 2}, Format: FormatPng}, covers(1))
	assert.Equal(t, EffectDeny, d.Effect)
	assert.Contains(t, d.Reason, "window 2")
	d = GrantPolicy{}.Decide(Request{Windows: []uint64{1}, Format: FormatPng}, covers(1))
	require.Equal(t, EffectPermit, d.Effect)
	require.Len(t, d.Obligations, 1)
	assert.Equal(t, []uint64{1}, d.Obligations[0].Scope.Windows)
}

func TestDenyOverrides(t *testing.T) {
	req := Request{Windows: []uint64{1}, Format: FormatPng}
	assert.Equal(t, EffectDeny, DenyOverrides().Decide(req, covers(1)).Effect)
	d := DenyOverrides(GrantPolicy{}, refuse{}).Decide(req, covers(1))
	assert.Equal(t, EffectDeny, d.Effect)
	assert.Equal(t, "refuse", d.Policy)
	d = DenyOverrides(GrantPolicy{}, stamp{name: "scope"}).Decide(req, covers(1))
	assert.Equal(t, EffectPermit, d.Effect)
	assert.Len(t, d.Obligations, 2)
}

func TestAnObligationWithoutAHandlerDeniesBeforeAnythingRenders(t *testing.T) {
	src := &fakeSource{}
	s := NewService(DenyOverrides(GrantPolicy{}, stamp{name: "watermark"}), NewRegistry(), src)
	id, d, err := s.Capture(Request{Windows: []uint64{1}, Format: FormatPng}, covers(1), nil)
	require.NoError(t, err)
	assert.Empty(t, id)
	assert.Equal(t, EffectDeny, d.Effect)
	assert.Contains(t, d.Reason, "watermark")
	assert.Empty(t, src.rendered)
}

func TestADecisionWithoutAScopeDrawsNothing(t *testing.T) {
	src := &fakeSource{}
	s := NewService(stamp{name: ObligationScope}, NewRegistry(), src)
	_, d, err := s.Capture(Request{Windows: []uint64{1}, Format: FormatPng}, covers(1), nil)
	require.NoError(t, err)
	assert.Equal(t, EffectDeny, d.Effect)
	assert.Empty(t, src.rendered)
}

func TestAnSvgCaptureIsOfOneWindowUncropped(t *testing.T) {
	s := NewService(GrantPolicy{}, NewRegistry(), &fakeSource{})
	_, d, _ := s.Capture(Request{Windows: []uint64{1, 2}, Format: FormatSvg}, covers(1, 2), nil)
	assert.Equal(t, EffectDeny, d.Effect)
	crop := image.Rect(0, 0, 1, 1)
	_, d, _ = s.Capture(Request{Windows: []uint64{1}, Format: FormatSvg, Crop: &crop}, covers(1), nil)
	assert.Equal(t, EffectDeny, d.Effect)
}

func TestAPngCaptureIsScopedCroppedEncodedAndDigested(t *testing.T) {
	src := &fakeSource{result: SourceResult{Phase: opwire.PhaseCompleted, Rgba: rgba(8, 6), Width: 8, Height: 6, PixelsPerPoint: 2}}
	s := NewService(GrantPolicy{}, NewRegistry(), src)
	dir := t.TempDir()
	s.SetSealedDir(dir)
	crop := image.Rect(1, 1, 3, 2) // points; ×2 → 4 × 2 pixels
	id, d, err := s.Capture(Request{Windows: []uint64{7}, Format: FormatPng, Crop: &crop}, covers(7), func() bool { return true })
	require.NoError(t, err)
	require.Equal(t, EffectPermit, d.Effect)
	assert.Equal(t, [][]uint64{{7}}, src.rendered)

	st, ok := s.Status(id)
	require.True(t, ok)
	require.Equal(t, opwire.PhaseCompleted, st.Phase, st.Reason)
	assert.Equal(t, "image/png", st.MediaType)
	assert.Empty(t, st.Path, "a capture has no path")
	b, media, err := s.Bytes(id)
	require.NoError(t, err)
	assert.Equal(t, "image/png", media)
	listed, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, listed, "the sealed artifact has no directory entry")
	img, err := png.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 4, 2), img.Bounds())

	info, ok := s.Info(id)
	require.True(t, ok)
	sum := blake3.Sum256(b)
	assert.Equal(t, hex.EncodeToString(sum[:]), info.Digest)
	assert.Equal(t, []string{"scope@1"}, info.Obligations)

	s.Release(id, 0)
	_, _, err = s.Bytes(id)
	assert.Error(t, err, "a released capture is gone with its key")
}

func TestAnSvgCaptureIsSealedFromTheRendersBytes(t *testing.T) {
	src := &fakeSource{result: SourceResult{Phase: opwire.PhaseCompleted, Svg: []byte("<svg/>")}}
	s := NewService(GrantPolicy{}, NewRegistry(), src)
	s.SetSealedDir(t.TempDir())
	id, d, err := s.Capture(Request{Windows: []uint64{1}, Format: FormatSvg}, covers(1), nil)
	require.NoError(t, err)
	require.Equal(t, EffectPermit, d.Effect)
	st, _ := s.Status(id)
	require.Equal(t, opwire.PhaseCompleted, st.Phase, st.Reason)
	b, media, err := s.Bytes(id)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", media)
	assert.Equal(t, "<svg/>", string(b))
}

func TestAFailedRenderFailsTheCapture(t *testing.T) {
	src := &fakeSource{result: SourceResult{Phase: opwire.PhaseFailed, Reason: "the grant no longer covers the windows"}}
	s := NewService(GrantPolicy{}, NewRegistry(), src)
	id, _, err := s.Capture(Request{Windows: []uint64{1}, Format: FormatPng}, covers(1), nil)
	require.NoError(t, err)
	st, _ := s.Status(id)
	assert.Equal(t, opwire.PhaseFailed, st.Phase)
	assert.Contains(t, st.Reason, "no longer covers")
}
