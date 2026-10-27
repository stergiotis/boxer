package mdedit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// The parse notes what it lacks, a lane reads it from the snapshot, and the
// next parse has it: a CommonMark path relative to the document, an
// Obsidian embed by name anywhere, and nothing for a URL or a missing file.
func TestSnapshotImagesArriveAfterTheParseThatWantedThem(t *testing.T) {
	snap := fstest.MapFS{
		"notes/doc.md":                 {Data: []byte("x")},
		"notes/img/rel.png":            {Data: pngOf(t, 4, 3)},
		"attachments/shot.png":         {Data: pngOf(t, 5, 2)},
		"deep/er/attachments/shot.png": {Data: pngOf(t, 1, 1)},
		"notes/broken.png":             {Data: []byte("not a png")},
	}
	inst := &App{}
	inst.snapImages = newSnapshotImages(snap, "notes/doc.md", "k")
	r := imageResolver{inst: inst}
	refs := []string{"img/rel.png", "shot.png", "https://example.org/a.png", "gone.png", "broken.png"}
	for _, ref := range refs {
		_, _, _, ok := r.LoadImage(ref)
		assert.False(t, ok, ref)
	}
	deadline := time.Now().Add(5 * time.Second)
	for inst.snapImages.jobKey != "" || len(inst.snapImages.wanted) > 0 {
		require.True(t, time.Now().Before(deadline), "the read did not land")
		inst.pollSnapshotImages()
		time.Sleep(time.Millisecond)
	}
	assert.False(t, inst.docOk, "the landed images ask for a parse")

	_, w, h, ok := r.LoadImage("img/rel.png")
	require.True(t, ok)
	assert.Equal(t, [2]uint32{4, 3}, [2]uint32{w, h})
	_, w, _, ok = r.LoadImage("shot.png")
	require.True(t, ok)
	assert.Equal(t, uint32(5), w, "by name, the shortest path wins")
	for _, ref := range refs[2:] {
		_, _, _, ok = r.LoadImage(ref)
		assert.False(t, ok, ref)
	}
	assert.Empty(t, inst.snapImages.wanted, "a ref found missing is not asked for again")
}

func TestADocumentNotFromASnapshotHasNoImages(t *testing.T) {
	inst := &App{}
	_, _, _, ok := imageResolver{inst: inst}.LoadImage("a.png")
	assert.False(t, ok)
	inst.snapImages = newSnapshotImages(fstest.MapFS{}, "a.md", "k")
	inst.setSnapshotImages(nil)
	assert.Nil(t, inst.snapImages)
}
