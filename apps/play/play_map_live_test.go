package play

import (
	"context"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
)

// TestMapRasterLive exercises the Map driver's real data path — buildRasterSQL
// + fetchRaster + the Arrow→RGBA pack — against a live ClickHouse over HTTP.
// Gated on PLAY_MAP_LIVE_URL (an HTTP endpoint that can resolve the source);
// PLAY_MAP_LIVE_TABLE overrides the FROM source (default: a plain table name).
// Writes the raster to PLAY_MAP_LIVE_PNG when set, for visual inspection.
//
//	PLAY_MAP_LIVE_URL=http://127.0.0.1:18123 \
//	PLAY_MAP_LIVE_TABLE="remoteSecure('host:9440', default.planes_mercator_sample100, 'website', '')" \
//	PLAY_MAP_LIVE_PNG=/tmp/london.png \
//	go test -tags="$(cat ./tags)" ./apps/play/ -run TestMapRasterLive -v -timeout 180s
func TestMapRasterLive(t *testing.T) {
	url := os.Getenv("PLAY_MAP_LIVE_URL")
	if url == "" {
		t.Skip("set PLAY_MAP_LIVE_URL to a ClickHouse HTTP endpoint")
	}
	table := os.Getenv("PLAY_MAP_LIVE_TABLE")
	if table == "" {
		table = "planes_mercator_sample100"
	}
	// A remote() source over a transatlantic link is slow per tile; give the
	// round-trip ample headroom for the test.
	mapFetchTimeout = 150 * time.Second

	client := NewClient(ClientConfig{URL: url, User: "default"}, nil)

	const w, h uint32 = 384, 384
	b, ok := bboxFromLatLon(51.3, 51.7, -0.6, 0.3) // Greater London — dense airspace
	if !ok {
		t.Fatal("degenerate bbox")
	}
	sql := buildRasterSQL(b, w, h, sanitizeTable(table), 100)
	ctx, cancel := context.WithTimeout(context.Background(), mapFetchTimeout)
	defer cancel()
	rec, _, _, exErr := clientExecutor{client: client}.execute(ctx, sql, memory.NewGoAllocator())
	if exErr != nil {
		t.Fatalf("execute: %v", exErr)
	}
	defer rec.Release()
	pixels, err := packRaster(rec, w, h)
	if err != nil {
		t.Fatalf("packRaster: %v", err)
	}
	if got := uint32(len(pixels)); got != w*h {
		t.Fatalf("pixel count = %d, want %d", got, w*h)
	}
	lit := 0
	for _, p := range pixels {
		if p&0xff != 0 {
			lit++
		}
	}
	t.Logf("raster %dx%d: %d/%d lit pixels", w, h, lit, len(pixels))
	if lit == 0 {
		t.Fatal("no lit pixels — query returned an empty raster")
	}

	if out := os.Getenv("PLAY_MAP_LIVE_PNG"); out != "" {
		img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		for i, p := range pixels {
			img.Pix[i*4+0] = byte(p >> 24)
			img.Pix[i*4+1] = byte(p >> 16)
			img.Pix[i*4+2] = byte(p >> 8)
			img.Pix[i*4+3] = byte(p)
		}
		f, ferr := os.Create(out)
		if ferr != nil {
			t.Fatalf("create png: %v", ferr)
		}
		defer f.Close()
		if encErr := png.Encode(f, img); encErr != nil {
			t.Fatalf("encode png: %v", encErr)
		}
		t.Logf("wrote %s", out)
	}
}
