package chat

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// testTIFF is an Exif block whose one directory entry is the orientation
// tag, in the byte order given.
func testTIFF(bo binary.ByteOrder, orientation uint16) []byte {
	b := make([]byte, 26)
	if bo == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}
	bo.PutUint16(b[2:], 42)
	bo.PutUint32(b[4:], 8)
	bo.PutUint16(b[8:], 1)
	bo.PutUint16(b[10:], 0x0112)
	bo.PutUint16(b[12:], 3)
	bo.PutUint32(b[14:], 1)
	bo.PutUint16(b[18:], orientation)
	return b
}

// withPNGChunk is a PNG with one more chunk after its header.
func withPNGChunk(data []byte, kind string, body []byte) []byte {
	const afterHeader = 8 + 4 + 4 + 13 + 4
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	return append(append(append([]byte(nil), data[:afterHeader]...), chunk...), data[afterHeader:]...)
}

// withJPEGSegment is a JPEG with one more segment right after its start.
func withJPEGSegment(data []byte, marker byte, body []byte) []byte {
	seg := []byte{0xff, marker}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(body)+2))
	seg = append(seg, body...)
	return append(append(append([]byte(nil), data[:2]...), seg...), data[2:]...)
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(4 * x), G: uint8(8 * y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

// The six pixels of a 3×2 image, a b c over d e f, as each orientation tag
// says they are shown, row by row.
var uprightWant = map[uint16]struct {
	w, h int
	px   string
}{
	1: {3, 2, "abcdef"},
	2: {3, 2, "cbafed"},
	3: {3, 2, "fedcba"},
	4: {3, 2, "defabc"},
	5: {2, 3, "adbecf"},
	6: {2, 3, "daebfc"},
	7: {2, 3, "fcebda"},
	8: {2, 3, "cfbead"},
}

func TestAnUploadIsTurnedTheWayItsTagSays(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for i, ch := range "abcdef" {
		src.SetNRGBA(i%3, i/3, color.NRGBA{R: uint8(ch), G: 1, B: 2, A: 255})
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, src))
	for o, want := range uprightWant {
		for _, bo := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
			out, ext, err := reencodeUpload(withPNGChunk(buf.Bytes(), "eXIf", testTIFF(bo, o)), 1<<20)
			require.NoError(t, err)
			assert.Equal(t, extPNG, ext)
			got, err := png.Decode(bytes.NewReader(out))
			require.NoError(t, err)
			require.Equal(t, [2]int{want.w, want.h}, [2]int{got.Bounds().Dx(), got.Bounds().Dy()}, "orientation %d", o)
			var px []byte
			for y := range want.h {
				for x := range want.w {
					r, _, _, _ := got.At(x, y).RGBA()
					px = append(px, byte(r>>8))
				}
			}
			assert.Equal(t, want.px, string(px), "orientation %d", o)
			assert.NotContains(t, string(out), "eXIf", "the tag does not outlive the turn it asked for")
		}
	}
}

func TestAnUploadKeepsNothingOfTheFileButItsPixels(t *testing.T) {
	exif := append([]byte("Exif\x00\x00"), testTIFF(binary.LittleEndian, 6)...)
	exif = append(exif, "GPS 47.3769N 8.5417E"...)
	file := withJPEGSegment(testJPEG(t, 40, 20), 0xfe, []byte("taken on a phone of mine"))
	file = withJPEGSegment(file, 0xe1, exif)
	require.Equal(t, 6, fileOrientation(file, extJPEG))

	out, ext, err := reencodeUpload(file, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, extJPEG, ext)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format)
	assert.Equal(t, [2]int{20, 40}, [2]int{cfg.Width, cfg.Height}, "a quarter turn, as the tag says")
	for _, gone := range []string{"Exif", "GPS", "phone of mine"} {
		assert.NotContains(t, string(out), gone)
	}

	withText := withPNGChunk(testPNG(t, 8, 8, 3), "tEXt", []byte("Comment\x00written at my desk"))
	out, _, err = reencodeUpload(withText, 1<<20)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "my desk")
}

func TestAnUploadIsRefusedBeforeItIsDecoded(t *testing.T) {
	_, _, err := reencodeUpload([]byte("GIF89a not an image the set takes"), 1<<20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither a PNG nor a JPEG")
	_, _, err = reencodeUpload(testPNG(t, 64, 64, 1), 1000)
	var px errImagePixels
	require.ErrorAs(t, err, &px)
	// A block that is no TIFF, and one cut short, leave the image as it is.
	assert.Equal(t, orientUpright, tiffOrientation([]byte("not a tiff block")))
	assert.Equal(t, orientUpright, tiffOrientation(testTIFF(binary.BigEndian, 6)[:15]))
	assert.Equal(t, orientUpright, tiffOrientation(testTIFF(binary.BigEndian, 9)))
}

func TestAddUploadIsThePersonsRevision(t *testing.T) {
	art := testArtefact(t)
	name, n, err := art.addUpload(testPNG(t, 30, 20, 5), "Passport Scan.PNG")
	require.NoError(t, err)
	assert.Equal(t, "upload-1.png", name)
	assert.Equal(t, 1, n)
	name, n, err = art.addUpload(testJPEG(t, 24, 16), "IMG_0042.jpeg")
	require.NoError(t, err)
	assert.Equal(t, "upload-2.jpg", name)
	assert.Equal(t, 2, n)

	set := art.headImages()
	require.Len(t, set, 2)
	e := set[0]
	assert.Equal(t, imageSourceUpload, e.source)
	assert.True(t, e.uploaded)
	assert.Equal(t, e.hash, e.rootHash())
	assert.Equal(t, [2]int{30, 20}, [2]int{e.w, e.h})
	assert.Contains(t, imageOrigin(e), "Passport Scan.PNG", "the panel tells the person which file it was")
	revs := art.revisions()
	assert.Equal(t, revSourcePerson, revs[1].source)
	assert.Equal(t, "you · + upload-2.jpg", revLabel(revs[1]))

	// What the model reads of it names no file.
	listed, err := json.Marshal([]imageView{viewOfImage(set[0]), viewOfImage(set[1])})
	require.NoError(t, err)
	assert.Contains(t, string(listed), `"source":"upload"`)
	assert.NotContains(t, string(listed), "Passport")
	assert.NotContains(t, string(listed), "IMG_0042")

	used, files := art.store.usage()
	_, _, err = art.addUpload([]byte("%PDF-1.7 not an image"), "notes.pdf")
	require.Error(t, err)
	n, _ = art.head()
	assert.Equal(t, 2, n, "a refused file changes nothing")
	usedAfter, filesAfter := art.store.usage()
	assert.Equal(t, [2]int64{used, int64(files)}, [2]int64{usedAfter, int64(filesAfter)})

	art.mu.Lock()
	art.proposal = &artProposal{}
	art.mu.Unlock()
	_, _, err = art.addUpload(testPNG(t, 8, 8, 7), "late.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accept or reject it first")
}

func TestAddUploadKeepsToTheBudget(t *testing.T) {
	art := testArtefact(t)
	art.store.limits.count = 1
	_, _, err := art.addUpload(testPNG(t, 8, 8, 1), "a.png")
	require.NoError(t, err)
	_, _, err = art.addUpload(testPNG(t, 8, 8, 2), "b.png")
	var budget errImageBudget
	require.ErrorAs(t, err, &budget)
	assert.Equal(t, "names", budget.what)
	_, files := art.store.usage()
	assert.Equal(t, 1, files, "nothing is sealed for an image the set has no room to name")

	art = testArtefact(t)
	art.store.limits.bytes = 64
	_, _, err = art.addUpload(testPNG(t, 64, 64, 1), "big.png")
	require.ErrorAs(t, err, &budget)
	assert.Equal(t, "bytes", budget.what)
	assert.Empty(t, art.headImages())
}

func TestTheModelsToolsOnAnUpload(t *testing.T) {
	coord, art := imageRig(t, func([]uint64, *[4]float32) ([]byte, string) { return nil, "no capture in this test" })
	_, _, err := art.addUpload(testJPEG(t, 40, 30), "site plan.jpg")
	require.NoError(t, err)

	content, _ := callTool(t, coord, "artefact_images", map[string]any{})
	assert.Contains(t, content, `"name":"upload-1.jpg"`)
	assert.Contains(t, content, `"source":"upload"`)
	assert.NotContains(t, content, "site plan")

	content, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 1.0, "from": "upload-1.jpg", "name": "plan.png"})
	assert.Contains(t, content, "ending in .jpg", "a copy keeps its source's bytes, and so its extension")
	var w imageWriteView
	content, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 1.0, "from": "upload-1.jpg", "name": "plan.jpg"})
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, "plan.jpg", w.Image.Name)

	content, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": 2.0, "from": "plan.jpg", "x": 2.0, "y": 2.0, "w": 16.0, "h": 8.0})
	require.NoError(t, json.Unmarshal([]byte(content), &w), content)
	assert.Equal(t, "screenshot-1.png", w.Image.Name, "a crop is a PNG, whatever it was cut from")
	assert.Equal(t, []int{16, 8}, []int{w.Image.Width, w.Image.Height})

	set := art.headImages()
	require.Len(t, set, 3)
	for _, e := range set {
		assert.True(t, e.uploaded, "%s descends from the upload", e.name)
		assert.Equal(t, set[0].hash, e.rootHash(), e.name)
	}
	data, err := art.store.read(set[2].hash)
	require.NoError(t, err)
	assert.Equal(t, "image/png", imageMediaType(data))
	data, err = art.store.read(set[0].hash)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", imageMediaType(data))
}

func TestAnExportBringsAnUploadBackAsOne(t *testing.T) {
	src := testArtefact(t)
	_, _, err := src.addUpload(testJPEG(t, 20, 10), "x.jpg")
	require.NoError(t, err)
	hash, _, _, err := src.store.put(testPNG(t, 12, 12, 4))
	require.NoError(t, err)
	up := src.headImages()[0]
	kept, err := src.store.read(up.hash)
	require.NoError(t, err)
	shot, err := src.store.read(hash)
	require.NoError(t, err)

	const text = "# Notes\n"
	rec := exportRecord{Revision: 3, TextDigest: agentrun.ContentDigest(text), Images: []exportImage{
		{Name: up.name, Hash: up.hash, Upload: true},
		// As a record written before uploads names a screenshot.
		{Name: "screenshot-1.png", Hash: hash},
	}}
	v := fstest.MapFS{exportDocPath: {Data: []byte(text)}, up.name: {Data: kept}, "screenshot-1.png": {Data: shot}}
	art := testArtefact(t)
	require.NoError(t, restoreFrom(v, rec, art))
	set := art.headImages()
	require.Len(t, set, 2)
	assert.True(t, set[0].uploaded)
	assert.False(t, set[1].uploaded, "an image of a record that says nothing counts as a capture")
}

// This chat's captures lets a capture through on the grant of its windows.
// No grant covers a file the person added: it asks once per content, there
// as under Ask once per image, and a setting raised while it waits does not
// answer for the person (ADR-0385 §SD4).
func TestAnUploadAsksOnceUnderThisChatsCaptures(t *testing.T) {
	coord, art, got := discloseRig(t, agent.PixelsCaptures, nil)
	_, _, err := art.addUpload(testPNG(t, 20, 20, 9), "mine.png")
	require.NoError(t, err)

	r, _ := view(t, coord, "screenshot-1.png")
	assert.Equal(t, "shown", r.Status, "a capture does not ask")
	assert.Nil(t, coord.pixelAskNow())

	done := viewAsync(t, coord, "upload-1.png")
	a := waitPixelAsk(t, coord)
	assert.True(t, a.uploaded)
	a.decide(pixelAllowed)
	require.Equal(t, "shown", (<-done).Status)
	asked := (*got)[len(*got)-1]
	assert.Equal(t, "upload", asked.Source)
	assert.Equal(t, agent.DecidedByPerson, asked.DecidedBy)
	assert.Equal(t, agent.PixelsCaptures, asked.Level)

	coord.beginPixelTurn()
	r, _ = view(t, coord, "upload-1.png")
	assert.Equal(t, "shown", r.Status, "allowed once, it does not ask again")
	assert.Equal(t, agent.DecidedByConsent, (*got)[len(*got)-1].DecidedBy)

	n, _ := art.head()
	_, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": float64(n), "from": "upload-1.png", "x": 0.0, "y": 0.0, "w": 8.0, "h": 8.0})
	done = viewAsync(t, coord, "screenshot-2.png")
	waitPixelAsk(t, coord).decide(pixelDeclined)
	assert.Equal(t, "declined", (<-done).Status, "a crop of an upload is other content, and asks")

	coord.setPixels(pixelPolicy{level: agent.PixelsAskEach, modelLocal: true})
	coord.beginPixelTurn()
	done = viewAsync(t, coord, "screenshot-2.png")
	a = waitPixelAsk(t, coord)
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	select {
	case r := <-done:
		t.Fatalf("the setting answered for the person: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}
	a.decide(pixelAllowed)
	assert.Equal(t, "shown", (<-done).Status)
}

// A view a setting let through is no consent of the person's: lowering the
// level afterwards asks.
func TestASettingsAllowingIsNoConsent(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsAskEach)
	done := viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord)
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	require.Equal(t, "shown", (<-done).Status)

	coord.setPixels(pixelPolicy{level: agent.PixelsAskOnce, modelLocal: true})
	coord.beginPixelTurn()
	done = viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord).decide(pixelAllowed)
	assert.Equal(t, "shown", (<-done).Status)
}

// The setting needs the artefact and nothing else: a conversation without
// Apps has images too, the person's.
func TestPixelsHoldWithoutApps(t *testing.T) {
	perms := defaultPermissions().withPixels(agent.PixelsAskOnce, false)
	assert.Equal(t, agent.PixelsAskOnce, perms.pixelsIn(true))
	assert.Equal(t, agent.PixelsNone, perms.pixelsIn(false))
}
