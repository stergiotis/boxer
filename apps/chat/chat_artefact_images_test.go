package chat

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// testPNG is a w×h PNG whose pixels depend on seed, so two seeds differ.
func testPNG(t *testing.T, w, h int, seed uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x) + seed, G: uint8(y), B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// testStore is a store whose sealed files live under the test's directory.
func testStore(t *testing.T, limits imageLimits) *imageStore {
	t.Helper()
	dir := t.TempDir()
	s := newImageStore(limits)
	s.create = func() (*sealed.File, error) { return sealed.CreateIn(dir) }
	t.Cleanup(s.close)
	return s
}

func roomy() imageLimits { return imageLimits{bytes: 1 << 24, count: 8, pixels: 1 << 20} }

func TestImageStoreSealsOncePerHash(t *testing.T) {
	s := testStore(t, roomy())
	data := testPNG(t, 40, 30, 1)
	h1, w, h, err := s.put(data)
	require.NoError(t, err)
	assert.Equal(t, [2]int{40, 30}, [2]int{w, h})
	used, files := s.usage()
	h2, _, _, err := s.put(data)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
	used2, files2 := s.usage()
	assert.Equal(t, used, used2, "the same bytes cost nothing again")
	assert.Equal(t, 1, files2)
	assert.Equal(t, 1, files)

	back, err := s.read(h1)
	require.NoError(t, err)
	assert.Equal(t, data, back)
	th, ok := s.thumbnail(h1)
	require.True(t, ok)
	assert.Equal(t, uint32(40), th.SrcWidthPx)
}

func TestImageStoreBudget(t *testing.T) {
	a, b := testPNG(t, 40, 30, 1), testPNG(t, 40, 30, 2)
	// Room for a and its sealing overhead, not for b as well.
	s := testStore(t, imageLimits{bytes: int64(len(a)) + int64(len(b))/2, count: 8, pixels: 1 << 20})
	_, _, _, err := s.put(a)
	require.NoError(t, err)
	used, _ := s.usage()
	_, _, _, err = s.put(b)
	var be errImageBudget
	require.ErrorAs(t, err, &be)
	used2, files := s.usage()
	assert.Equal(t, used, used2, "a refused put changes nothing")
	assert.Equal(t, 1, files)
}

func TestImageStoreRefusesPixelsBeforeDecoding(t *testing.T) {
	s := testStore(t, imageLimits{bytes: 1 << 24, count: 8, pixels: 100})
	_, _, _, err := s.put(testPNG(t, 20, 20, 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than the 100")
	_, _, _, err = s.put([]byte("not a png"))
	assert.ErrorContains(t, err, "not a PNG")
}

func TestImageStoreRetainPinPurge(t *testing.T) {
	s := testStore(t, roomy())
	ha, _, _, err := s.put(testPNG(t, 10, 10, 1))
	require.NoError(t, err)
	hb, _, _, err := s.put(testPNG(t, 10, 10, 2))
	require.NoError(t, err)
	unpin := s.pin(hb)
	s.retain(map[string]bool{})
	assert.False(t, s.has(ha))
	assert.True(t, s.has(hb), "a pinned hash survives a collection")
	unpin()
	s.retain(map[string]bool{})
	assert.False(t, s.has(hb))
	used, files := s.usage()
	assert.Zero(t, used)
	assert.Zero(t, files)
}

func TestCropPNG(t *testing.T) {
	data := testPNG(t, 40, 30, 7)
	out, err := cropPNG(data, [4]int{5, 6, 10, 8}, 1<<20)
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 10, 8), img.Bounds())
	src, _ := png.Decode(bytes.NewReader(data))
	assert.Equal(t, color.NRGBAModel.Convert(src.At(5, 6)), color.NRGBAModel.Convert(img.At(0, 0)))

	for _, r := range [][4]int{{35, 0, 10, 5}, {-1, 0, 5, 5}, {0, 0, 0, 5}, {0, 25, 5, 6}} {
		_, err = cropPNG(data, r, 1<<20)
		assert.ErrorContains(t, err, "not inside", r)
	}
}

func TestImageName(t *testing.T) {
	set := []artImage{{name: "screenshot-1.png"}, {name: "a.png"}}
	n, reason := imageName("", set)
	require.Empty(t, reason)
	assert.Equal(t, "screenshot-2.png", n)
	_, reason = imageName("a.png", set)
	assert.Contains(t, reason, "taken")
	for _, bad := range []string{"A.png", "a b.png", "a.jpg", "../a.png", ".png"} {
		_, reason = imageName(bad, set)
		assert.NotEmpty(t, reason, bad)
	}
}

// testArtefact is an artefact whose store lives under the test's directory.
func testArtefact(t *testing.T) *artefact {
	t.Helper()
	art := newArtefact()
	art.store = testStore(t, roomy())
	return art
}

func TestRevisionsCarryTheSet(t *testing.T) {
	art := testArtefact(t)
	h, w, hh, err := art.store.put(testPNG(t, 12, 9, 3))
	require.NoError(t, err)
	// Sealed but not yet named by a revision: pinned, as a tool call pins it.
	unpin := art.store.pin(h)
	shot := artImage{name: "screenshot-1.png", hash: h, w: w, h: hh}
	n1, err := art.commit(0, artRevision{text: "a\n"})
	require.NoError(t, err)
	n2, err := art.commit(n1, artRevision{text: "a\n", images: []artImage{shot}, ownImages: true})
	require.NoError(t, err)
	unpin()
	n3, err := art.commit(n2, artRevision{text: "a\nb\n"})
	require.NoError(t, err)
	assert.Len(t, art.headImages(), 1, "a text write keeps the set")

	// Revert to revision 1 drops the image from the head; the bytes stay,
	// since revisions 2 and 3 still name them.
	_, err = art.revert(n1)
	require.NoError(t, err)
	assert.Empty(t, art.headImages())
	assert.True(t, art.store.has(h))

	// Purging frees the bytes and marks every entry naming them.
	art.purge(h)
	assert.False(t, art.store.has(h))
	n5, err := art.revert(n3)
	require.NoError(t, err)
	imgs := art.headImages()
	require.Len(t, imgs, 1)
	assert.True(t, imgs[0].purged, "a revert to a revision naming purged bytes reports them purged")
	assert.Equal(t, 5, n5)
}

func TestRewoundImagesAreFreedOnceTheHeadMoves(t *testing.T) {
	art := testArtefact(t)
	h, _, _, err := art.store.put(testPNG(t, 8, 8, 4))
	require.NoError(t, err)
	n1, err := art.commit(0, artRevision{text: "a\n", images: []artImage{{name: "x.png", hash: h}}, ownImages: true})
	require.NoError(t, err)
	dropped := art.truncate(0)
	require.Len(t, dropped, 1)
	assert.True(t, art.store.has(h), "a rewound turn may still be reinstated")
	assert.True(t, art.reinstate(0, dropped))
	assert.Len(t, art.headImages(), 1)

	dropped = art.truncate(0)
	_, err = art.commit(0, artRevision{text: "b\n"})
	require.NoError(t, err)
	assert.False(t, art.store.has(h), "after a commit the rewound revision cannot come back, so its bytes go")
	assert.False(t, art.reinstate(0, dropped))
	_ = n1
}

// imageRig is a coordinator with an artefact under Edit whose captures come
// from capture.
func imageRig(t *testing.T, capture func(windows []uint64, crop *[4]float32) ([]byte, string)) (*coordinator, *artefact) {
	t.Helper()
	coord := newCoordinator(nil, nil, "conv")
	art := testArtefact(t)
	art.setPolicy(artPolicy{write: true})
	coord.offerArtefact(art)
	coord.captureHook = func(_ context.Context, windows []uint64, crop *[4]float32) ([]byte, string) {
		return capture(windows, crop)
	}
	return coord, art
}

func callTool(t *testing.T, coord *coordinator, name string, args map[string]any) (content string, activity string) {
	t.Helper()
	return coord.dispatch(context.Background(), toolOrigin{turn: "t1"}, openaichat.ToolCall{Name: name}, args)
}

func TestImageToolsCaptureCopyCropRemove(t *testing.T) {
	shot := testPNG(t, 64, 48, 9)
	var gotWindows []uint64
	var gotCrop *[4]float32
	coord, art := imageRig(t, func(windows []uint64, crop *[4]float32) ([]byte, string) {
		gotWindows, gotCrop = windows, crop
		return shot, ""
	})

	content, activity := callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{3.0, 4.0},
		"crop": map[string]any{"x": 1.0, "y": 2.0, "w": 30.0, "h": 20.0}})
	var w imageWriteView
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, 1, w.Revision)
	require.NotNil(t, w.Image)
	assert.Equal(t, "screenshot-1.png", w.Image.Name)
	assert.Equal(t, []int{64, 48}, []int{w.Image.Width, w.Image.Height})
	assert.Equal(t, []uint64{3, 4}, gotWindows)
	assert.Equal(t, &[4]float32{1, 2, 30, 20}, gotCrop)
	assert.Contains(t, activity, "screenshot-1.png 64×48")
	usedAfterCapture := w.Budget.Bytes

	content, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 1.0, "from": "screenshot-1.png", "name": "before.png"})
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, "before.png", w.Image.Name)
	assert.Equal(t, usedAfterCapture, w.Budget.Bytes, "a copy costs no bytes")

	content, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": 2.0, "from": "before.png", "x": 4.0, "y": 4.0, "w": 16.0, "h": 8.0})
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, "screenshot-2.png", w.Image.Name)
	assert.Equal(t, []int{16, 8}, []int{w.Image.Width, w.Image.Height})
	assert.Equal(t, []int{4, 4, 16, 8}, w.Image.Rect)
	assert.Greater(t, w.Budget.Bytes, usedAfterCapture)

	content, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": 3.0, "from": "before.png", "x": 60.0, "y": 0.0, "w": 16.0, "h": 8.0})
	assert.Contains(t, content, "not inside")

	content, _ = callTool(t, coord, "artefact_remove_image", map[string]any{"base_revision": 3.0, "name": "before.png", "purge": true})
	assert.Contains(t, content, "screenshot-1.png has the same bytes", "a purge may not take bytes another name still shows")
	content, _ = callTool(t, coord, "artefact_remove_image", map[string]any{"base_revision": 3.0, "name": "before.png"})
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, "before.png", w.Removed)
	assert.False(t, w.Purged)

	content, _ = callTool(t, coord, "artefact_images", map[string]any{})
	var list imagesView
	require.NoError(t, json.Unmarshal([]byte(content), &list), content)
	assert.Equal(t, 4, list.Revision)
	require.Len(t, list.Images, 2)
	assert.Equal(t, "capture", list.Images[0].Source)
	assert.Equal(t, "crop", list.Images[1].Source)
	assert.Equal(t, "before.png", list.Images[1].From)

	// The text is untouched by every change of the set.
	_, text := art.head()
	assert.Empty(t, text)
}

func TestImageToolsAreWritesUnderTheSettings(t *testing.T) {
	coord, art := imageRig(t, func([]uint64, *[4]float32) ([]byte, string) { return nil, "no" })
	art.setPolicy(artPolicy{})
	content, _ := callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	assert.Contains(t, content, "read the artefact, not change it")
	art.setPolicy(artPolicy{write: true})
	content, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 5.0, "windows": []any{1.0}})
	assert.Contains(t, content, "at revision 0, not 5")
	content, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	assert.Contains(t, content, "error: no")
	content, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{}})
	assert.Contains(t, content, "windows is required")
	content, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 0.0, "from": "x.png"})
	assert.Contains(t, content, "no screenshot is named")

	assert.NotContains(t, toolNames(imageTools(true, false)), "artefact_capture", "a capture needs Apps")
	assert.Contains(t, toolNames(imageTools(true, true)), "artefact_capture")
	assert.Equal(t, []string{"artefact_images"}, toolNames(imageTools(false, true)))
	for _, x := range imageTools(true, true) {
		var v any
		require.NoError(t, json.Unmarshal(x.Parameters, &v), x.Name)
	}
}

func TestImageToolsRefusePastTheBudget(t *testing.T) {
	shot := testPNG(t, 32, 32, 5)
	coord, art := imageRig(t, func([]uint64, *[4]float32) ([]byte, string) { return shot, "" })
	art.store.limits.count = 1
	content, _ := callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	require.NotContains(t, content, "error")
	content, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 1.0, "from": "screenshot-1.png"})
	assert.Contains(t, content, "budget is full: names 2 of 1")
	n, _ := art.head()
	assert.Equal(t, 1, n, "a refused change makes no revision")

	art.store.limits.count = 8
	art.store.limits.bytes = 10
	shot = testPNG(t, 32, 32, 6)
	content, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 1.0, "windows": []any{1.0}})
	assert.Contains(t, content, "budget is full: bytes")
}

// Under Ask first an image change waits for the person; rejected, the bytes
// it sealed are freed and the set is as it was.
func TestImageChangeWaitsUnderAskFirst(t *testing.T) {
	shot := testPNG(t, 20, 20, 3)
	coord, art := imageRig(t, func([]uint64, *[4]float32) ([]byte, string) { return shot, "" })
	art.setPolicy(artPolicy{write: true, ask: true})
	type result struct{ content string }
	done := make(chan result, 1)
	go func() {
		c, _ := callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{2.0}})
		done <- result{c}
	}()
	var p *artProposal
	require.Eventually(t, func() bool { p = art.pending(); return p != nil }, 5*time.Second, 5*time.Millisecond)
	require.NotNil(t, p.image)
	assert.Equal(t, "screenshot-1.png", p.image.name)
	assert.True(t, art.store.has(p.image.hash), "the proposal's image is held while it waits")
	hash := p.image.hash
	p.decide(false)
	r := <-done
	assert.Contains(t, r.content, `"rejected":true`)
	assert.Empty(t, art.headImages())
	assert.False(t, art.store.has(hash), "a rejected capture's bytes are freed")
}

func toolNames(ts []openaichat.Tool) (out []string) {
	for _, x := range ts {
		out = append(out, x.Name)
	}
	return
}

func TestUnionBounds(t *testing.T) {
	assert.Nil(t, unionBounds(nil))
	assert.Nil(t, unionBounds([]windowGeom{{W: 0, H: 10}}), "a window that has not drawn has no rect")
	assert.Equal(t, &[4]float32{10, 20, 300, 200}, unionBounds([]windowGeom{{X: 10, Y: 20, W: 300, H: 200}}))
	assert.Equal(t, &[4]float32{10, 5, 490, 215},
		unionBounds([]windowGeom{{X: 10, Y: 20, W: 300, H: 200}, {X: 200, Y: 5, W: 300, H: 50}}))
	assert.Equal(t, &[4]float32{0, 0, 90, 40}, unionBounds([]windowGeom{{X: -10, Y: -10, W: 100, H: 50}}),
		"clipped to the viewport's top-left")
}
