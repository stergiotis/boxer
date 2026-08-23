package portolan

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Map's seams that need no host: the arrival path between the loader
// and the pyramid. Drawing and input are exercised by the headless scene
// (scripts/dev/portolan-map-scene.sh).

// errorTileTransport 404s every tile and serves one 2×2 PNG at the error URL.
type errorTileTransport struct {
	errURL string
	png    []byte
}

func (tr *errorTileTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() == tr.errURL {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(tr.png))}, nil
	}
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestMap_ErrorTileURL(t *testing.T) {
	// TileLayer's errorTileUrl: a tile that fails is re-pointed at the error
	// image, drawn from it, and still counts as an error. The error image is
	// fetched once for every failed tile (the byte cache), and a tile that
	// shows it is asked for again — not served from the error image — when
	// another copy of it is requested.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0, B: 0xff, A: 0xff})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	const errURL = "http://tiles.example/error.png"
	tr := &errorTileTransport{errURL: errURL, png: buf.Bytes()}

	src := NewTileSource("http://tiles.example/{z}/{x}/{y}.png")
	src.ErrorTileURL = errURL
	m := New(nil, Options{Source: src, Loader: LoaderOptions{Workers: 2, Transport: tr}, Center: LL(0, 0), Zoom: 1})
	defer m.Close()
	m.view.SetSize(Pt(300, 300))
	m.view.SetView(m.opts.Center, m.opts.Zoom)
	m.pyramid.Sync(m.view, m.view.TakeEvents())
	requested := m.pyramid.Stats().TileLoadStart
	require.Positive(t, requested)

	deadline := time.Now().Add(5 * time.Second)
	for m.pyramid.IsLoading() && time.Now().Before(deadline) {
		m.absorbArrivals(m.loader.Drain(), time.Now())
		time.Sleep(2 * time.Millisecond)
	}
	require.False(t, m.pyramid.IsLoading(), "every tile settled")

	st := m.pyramid.Stats()
	assert.Equal(t, requested, st.TileError, "every tile is an error")
	assert.Equal(t, 0, st.TileLoad)
	for coords, wrapped := range m.wrappedOf {
		px := m.pixels[wrapped]
		if assert.NotNil(t, px, "%v has the error image's pixels", coords) {
			assert.Equal(t, uint32(0xff00ffff), px.px[0])
		}
		assert.Contains(t, m.errTiles, wrapped, "%v is marked as showing the error image", coords)
		assert.True(t, m.pyramid.Failed(coords))
	}
	// A second request for a wrapped tile that shows the error image goes to
	// the loader instead of being served from the error image at once.
	var any TileCoords
	for c := range m.wrappedOf {
		any = c
		break
	}
	before := m.loader.Pending()
	m.requestTile(any, m.wrappedOf[any])
	assert.Equal(t, before+1, m.loader.Pending(), "re-requested from the loader")
	m.holders[m.wrappedOf[any]]-- // undo the test's extra holder
}
