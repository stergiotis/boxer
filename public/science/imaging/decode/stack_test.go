package decode

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging/decode/fits"
	"github.com/stergiotis/boxer/public/science/imaging/decode/npy"
	"github.com/stergiotis/boxer/public/science/imaging/decode/tiff"
)

// fixture reads a file of a reader package's testdata.
func fixture(t *testing.T, pkg, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pkg, "testdata", name))
	require.NoError(t, err)
	return b
}

func decodeStack(t *testing.T, b []byte, name string, opts Options) error {
	t.Helper()
	_, err := DecodeStack(context.Background(), bytes.NewReader(b), int64(len(b)), name, opts)
	return err
}

func TestSniffTellsTheStackFormats(t *testing.T) {
	for _, tc := range []struct {
		pkg, name string
		want      FormatE
	}{
		{"tiff", "geo_area.tif", FormatTIFF},
		{"tiff", "bigtiff_tiled_planar_i16.tif", FormatTIFF},
		{"tiff", "lzw_pred2_u16_rgb_be.tif", FormatTIFF},
		{"fits", "tan_cd.fits", FormatFITS},
		{"fits", "bitpix16.fits.gz", FormatFITS},
		{"npy", "u2_le.npy", FormatNPY},
		{"npy", "single.npz", FormatNPZ},
	} {
		got := Sniff(fixture(t, tc.pkg, tc.name))
		assert.Equal(t, tc.want, got, tc.name)
		assert.True(t, got.IsStack(), tc.name)
	}
	assert.False(t, FormatPNG.IsStack())
	assert.False(t, FormatJPEG.IsStack())
	assert.Zero(t, Sniff([]byte("PK\x03\x04")), "a zip archive too short to name its first member")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte("not a FITS file at all"))
	require.NoError(t, zw.Close())
	assert.Zero(t, Sniff(gz.Bytes()), "gzip of something other than FITS")
	for _, f := range AllFormats {
		assert.NotEqual(t, "unknown", f.String())
	}
}

// DecodeStack reads each format through its reader: the same stack the
// reader gives, its digest included.
func TestDecodeStackDispatchesToTheReader(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		pkg, name string
		direct    func(b []byte) ([32]byte, error)
	}{
		{"tiff", "multipage_u8.tif", func(b []byte) ([32]byte, error) {
			s, err := tiff.Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", tiff.Options{})
			return s.Hash(), err
		}},
		{"fits", "cube_i16.fits", func(b []byte) ([32]byte, error) {
			s, err := fits.Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", fits.Options{})
			return s.Hash(), err
		}},
		{"fits", "bitpix16.fits.gz", func(b []byte) ([32]byte, error) {
			s, err := fits.Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", fits.Options{})
			return s.Hash(), err
		}},
		{"npy", "cube_u2.npy", func(b []byte) ([32]byte, error) {
			s, err := npy.Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", npy.Options{})
			return s.Hash(), err
		}},
		{"npy", "single.npz", func(b []byte) ([32]byte, error) {
			s, err := npy.Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", npy.Options{})
			return s.Hash(), err
		}},
	} {
		b := fixture(t, tc.pkg, tc.name)
		st, err := DecodeStack(ctx, bytes.NewReader(b), int64(len(b)), tc.name, Options{})
		require.NoError(t, err, tc.name)
		want, err := tc.direct(b)
		require.NoError(t, err, tc.name)
		assert.Equal(t, want, st.Hash(), tc.name)
		assert.Equal(t, tc.name, st.Provenance().SourceName)
	}
}

// A refusal keeps the reader's sentinel and the reader's sentence.
func TestDecodeStackKeepsTheReadersRefusals(t *testing.T) {
	err := decodeStack(t, fixture(t, "tiff", "refused_jpeg.tif"), "j.tif", Options{})
	require.ErrorIs(t, err, tiff.ErrUnsupported)
	assert.Contains(t, err.Error(), "JPEG")

	err = decodeStack(t, fixture(t, "fits", "tilecomp.fits"), "t.fits", Options{})
	require.ErrorIs(t, err, fits.ErrUnsupported)
	assert.Contains(t, err.Error(), "tile-compressed")

	err = decodeStack(t, fixture(t, "npy", "object.npy"), "o.npy", Options{})
	require.ErrorIs(t, err, npy.ErrUnsupported)

	err = decodeStack(t, fixture(t, "npy", "multi.npz"), "m.npz", Options{})
	require.ErrorIs(t, err, npy.ErrUnsupported)
	assert.Contains(t, err.Error(), "several arrays")

	// The limits reach the readers.
	for _, tc := range []struct {
		pkg, name string
		limit     error
	}{
		{"tiff", "geo_area.tif", tiff.ErrLimit},
		{"fits", "tan_cd.fits", fits.ErrLimit},
		{"npy", "u2_le.npy", npy.ErrLimit},
	} {
		err = decodeStack(t, fixture(t, tc.pkg, tc.name), tc.name, Options{MaxPixels: 4})
		assert.ErrorIs(t, err, tc.limit, tc.name)
		err = decodeStack(t, fixture(t, tc.pkg, tc.name), tc.name, Options{MaxBytes: 8})
		assert.ErrorIs(t, err, tc.limit, tc.name)
	}
	err = decodeStack(t, fixture(t, "fits", "cube_i16.fits"), "c.fits", Options{MaxPlanes: 1})
	assert.ErrorIs(t, err, fits.ErrLimit)
	err = decodeStack(t, fixture(t, "npy", "cube_u2.npy"), "c.npy", Options{MaxPlanes: 1})
	assert.ErrorIs(t, err, npy.ErrLimit)

	// PNG is an image, not a stack, and the stack formats are not images.
	err = decodeStack(t, pngSignature, "p.png", Options{})
	assert.Contains(t, err.Error(), "not a stack")
	_, err = Decode(fixture(t, "tiff", "geo_area.tif"), "g.tif", Options{})
	assert.Contains(t, err.Error(), "DecodeStack")
	err = decodeStack(t, []byte("hello"), "h.txt", Options{})
	assert.Contains(t, err.Error(), "not a TIFF, FITS, NPY or NPZ file")
	assert.False(t, errors.Is(err, tiff.ErrMalformed))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := fixture(t, "fits", "cube_i16.fits")
	_, err = DecodeStack(ctx, bytes.NewReader(b), int64(len(b)), "c.fits", Options{})
	assert.ErrorIs(t, err, context.Canceled)
}
