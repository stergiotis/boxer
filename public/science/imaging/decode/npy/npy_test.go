package npy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// An NPY writer for tests, so a test knows exactly what the file holds. It
// is written from the format description as the reader is; the fixtures
// under testdata, written by numpy, keep the two from sharing a
// misunderstanding.

func headerDict(descr string, fortran bool, shape []int64) string {
	dims := make([]string, len(shape))
	for i, s := range shape {
		dims[i] = strconv.FormatInt(s, 10)
	}
	tuple := "(" + strings.Join(dims, ", ")
	if len(shape) == 1 {
		tuple += ","
	}
	tuple += ")"
	f := "False"
	if fortran {
		f = "True"
	}
	return "{'descr': '" + descr + "', 'fortran_order': " + f + ", 'shape': " + tuple + ", }"
}

// npyFile writes magic, version, the header text padded to 64 bytes and
// ended with a newline, then data.
func npyFile(major uint8, header string, data []byte) []byte {
	var b bytes.Buffer
	b.Write(magic)
	b.WriteByte(major)
	b.WriteByte(0)
	pre := 10
	if major >= 2 {
		pre = 12
	}
	total := pre + len(header) + 1
	padded := header + strings.Repeat(" ", (64-total%64)%64) + "\n"
	if major >= 2 {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(padded)))
	} else {
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(padded)))
	}
	b.WriteString(padded)
	b.Write(data)
	return b.Bytes()
}

var codes = map[string]imaging.SampleKindE{"u1": imaging.SampleKindU8, "u2": imaging.SampleKindU16, "i2": imaging.SampleKindI16,
	"i4": imaging.SampleKindI32, "f4": imaging.SampleKindF32, "f8": imaging.SampleKindF64}

// valueAt is the test's sample at a C-order flat index: distinct at every
// index for every kind.
func valueAt(code string, flat int64) float64 {
	switch code {
	case "u1":
		return float64(flat*7%251 + 1)
	case "u2":
		return float64(flat*977%65000 + 300)
	case "i2":
		return float64(flat*977%65000 - 32000)
	case "i4":
		return float64(flat*1_000_003 - 2_000_000_000)
	case "f4":
		return float64(float32(float64(flat)*0.375 - 11.5))
	}
	return float64(flat)*1.0000001e-3 - 1e9
}

// encode writes the samples in C or Fortran order in the descr's byte order.
func encode(descr string, fortran bool, shape []int64) []byte {
	code := descr[1:]
	var bo binary.AppendByteOrder = binary.LittleEndian
	if descr[0] == '>' {
		bo = binary.BigEndian
	}
	k := len(shape)
	n := int64(1)
	for _, s := range shape {
		n *= s
	}
	out := make([]byte, 0, n*8)
	idx := make([]int64, k)
	for range n {
		flat := int64(0)
		for i := range k {
			flat = flat*shape[i] + idx[i]
		}
		v := valueAt(code, flat)
		switch code {
		case "u1":
			out = append(out, byte(v))
		case "u2", "i2":
			out = bo.AppendUint16(out, uint16(int64(v)))
		case "i4":
			out = bo.AppendUint32(out, uint32(int64(v)))
		case "f4":
			out = bo.AppendUint32(out, math.Float32bits(float32(v)))
		case "f8":
			out = bo.AppendUint64(out, math.Float64bits(v))
		}
		// Advance the index: last fastest in C order, first in Fortran.
		if fortran {
			for i := 0; i < k; i++ {
				idx[i]++
				if idx[i] < shape[i] {
					break
				}
				idx[i] = 0
			}
		} else {
			for i := k - 1; i >= 0; i-- {
				idx[i]++
				if idx[i] < shape[i] {
					break
				}
				idx[i] = 0
			}
		}
	}
	return out
}

func array(major uint8, descr string, fortran bool, shape []int64) []byte {
	return npyFile(major, headerDict(descr, fortran, shape), encode(descr, fortran, shape))
}

func decodeBytes(b []byte, opts Options) (imaging.Stack, error) {
	return Decode(context.Background(), bytes.NewReader(b), int64(len(b)), "test.npy", opts)
}

// storedAtC is the stack's sample at a C-order index of the array.
func storedAtC(t *testing.T, s imaging.Stack, idx []int64) (float64, bool) {
	t.Helper()
	k := len(idx)
	sel := make([]int, k-2)
	for i := range sel {
		sel[i] = int(idx[i])
	}
	p, err := s.Plane(sel)
	require.NoError(t, err)
	return p.StoredAt(int(idx[k-1]), int(idx[k-2]))
}

func eachIndex(shape []int64, f func(idx []int64, flat int64)) {
	n := int64(1)
	for _, s := range shape {
		n *= s
	}
	idx := make([]int64, len(shape))
	for flat := range n {
		f(idx, flat)
		for i := len(shape) - 1; i >= 0; i-- {
			idx[i]++
			if idx[i] < shape[i] {
				break
			}
			idx[i] = 0
		}
	}
}

func TestEveryKindOrderShapeAndVersionReadsBack(t *testing.T) {
	shapes := [][]int64{{3, 5}, {1, 4}, {4, 1}, {2, 3, 4}, {2, 3, 2, 5}}
	for code, kind := range codes {
		for _, order := range []byte{'<', '>'} {
			descr := string(order) + code
			if code == "u1" {
				descr = "|u1"
			}
			for _, shape := range shapes {
				for _, fortran := range []bool{false, true} {
					for _, major := range []uint8{1, 2, 3} {
						s, err := decodeBytes(array(major, descr, fortran, shape), Options{})
						require.NoError(t, err, "%s %v fortran %v v%d", descr, shape, fortran, major)
						assert.Equal(t, kind, s.Kind())
						k := len(shape)
						assert.Equal(t, int(shape[k-1]), s.Width())
						assert.Equal(t, int(shape[k-2]), s.Height())
						require.Len(t, s.Axes(), k-2)
						for i, a := range s.Axes() {
							assert.Equal(t, "axis"+strconv.Itoa(i), a.Name)
							assert.Equal(t, int(shape[i]), a.Size)
						}
						eachIndex(shape, func(idx []int64, flat int64) {
							got, valid := storedAtC(t, s, idx)
							require.True(t, valid)
							require.Equal(t, valueAt(code, flat), got, "%s %v fortran %v at %v", descr, shape, fortran, idx)
						})
						prov := s.Provenance()
						assert.Equal(t, "npy", prov.Format)
						assert.Equal(t, DecoderName, prov.Decoder)
						assert.Equal(t, fortran, prov.Derivations.Has(imaging.DerivationAxesReordered))
						assert.False(t, prov.Derivations.Has(imaging.DerivationRowsFlipped))
						assert.True(t, s.Placement().IsZero())
						p, _ := s.Plane(make([]int, k-2))
						assert.True(t, p.Transform().IsIdentity())
						_, hasInvalid := p.Invalid()
						assert.False(t, hasInvalid)
					}
				}
			}
		}
	}
}

func TestNaNIsInvalid(t *testing.T) {
	data := binary.LittleEndian.AppendUint32(nil, math.Float32bits(float32(math.NaN())))
	data = binary.LittleEndian.AppendUint32(data, math.Float32bits(2))
	s, err := decodeBytes(npyFile(1, headerDict("<f4", false, []int64{1, 2}), data), Options{})
	require.NoError(t, err)
	p, _ := s.Plane(nil)
	_, valid := p.StoredAt(0, 0)
	assert.False(t, valid)
	v, valid := p.StoredAt(1, 0)
	assert.True(t, valid)
	assert.Equal(t, 2.0, v)
}

func TestHeaderLiteralIsToleratedAndChecked(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6}
	good := []string{
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, 3), }",
		"{'shape':(2,3),'fortran_order':False,'descr':'|u1'}",
		"{\"descr\" : \"|u1\" ,\n 'fortran_order' : False , 'shape' : ( 2 , 3 , ) }",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2L, 3L)}",
	}
	for _, h := range good {
		s, err := decodeBytes(npyFile(1, h, data), Options{})
		require.NoError(t, err, h)
		assert.Equal(t, 3, s.Width(), h)
	}
	bad := map[string]string{
		"{'descr': '|u1', 'shape': (2, 3)}":                                         "lacks one of",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, 3), 'extra': 1}":     "unknown key",
		"{'descr': '|u1', 'descr': '|u1', 'fortran_order': False, 'shape': (2, 3)}": "repeats the key",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (6)}":                    "without its comma",
		"{'descr': '|u1', 'fortran_order': False, 'shape': [2, 3]}":                 "shape is not a tuple",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, -3)}":                "non-negative",
		"{'descr': '|u1', 'fortran_order': 0, 'shape': (2, 3)}":                     "fortran_order is not",
		"{'descr': '|u1, 'fortran_order': False, 'shape': (2, 3)}":                  "",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, 3)} x":               "text after",
		"['descr']": "not a dictionary",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, 99999999999999999999)}": "does not fit",
		"{'descr': (((((((((((((((((((1,),),),),),),),),),),),),),),),),),),)}":        "nests too deeply",
		"{'descr': '|u1', 'fortran_order': False, 'shape': (2, 3)":                     "not closed",
		"{'descr': '|u1', 'fortran_order': Fals, 'shape': (2, 3)}":                     "not a literal",
		"{1: 2}":             "key in the header is not a string",
		"{'descr': '\\x41'}": "escape",
	}
	for h, says := range bad {
		_, err := decodeBytes(npyFile(1, h, data), Options{})
		require.Error(t, err, h)
		assert.ErrorIs(t, err, ErrMalformed, h)
		assert.Contains(t, err.Error(), says, h)
	}
}

func TestRefusedTypesAreNamed(t *testing.T) {
	cases := map[string]string{
		"|b1": "boolean", "|i1": "8-bit signed", "<u4": "32-bit unsigned", "<i8": "64-bit integers", ">u8": "64-bit integers",
		"<f2": "16-bit floats", "<f16": "extended-precision", "<c8": "complex", "<c16": "complex", "|S5": "strings", "<U3": "strings",
		"<M8[D]": "datetimes", "<m8[s]": "timedeltas", "|V8": "raw or structured", "=f8": "native byte order", "f8": "without a byte-order",
		"|O": "unpickling", "x": "the dtype",
	}
	for descr, says := range cases {
		_, err := decodeBytes(npyFile(1, headerDict(descr, false, []int64{2, 2}), make([]byte, 64)), Options{})
		require.Error(t, err, descr)
		assert.ErrorIs(t, err, ErrUnsupported, descr)
		assert.Contains(t, err.Error(), says, descr)
	}
	_, err := decodeBytes(npyFile(1, "{'descr': [('x', '<f4'), ('y', '<i2')], 'fortran_order': False, 'shape': (2,)}", make([]byte, 12)), Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "structured dtype")
	_, err = decodeBytes(npyFile(1, headerDict("|u2", false, []int64{2, 2}), make([]byte, 8)), Options{})
	assert.ErrorIs(t, err, ErrMalformed)
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name string
		file []byte
		opts Options
		want error
		says string
	}{
		{"1-D", array(1, "<f4", false, []int64{5}), Options{}, ErrUnsupported, "one-dimensional"},
		{"0-D", npyFile(1, "{'descr': '<f4', 'fortran_order': False, 'shape': ()}", make([]byte, 4)), Options{}, ErrUnsupported, "zero-dimensional"},
		{"empty", npyFile(1, headerDict("<f4", false, []int64{0, 3}), nil), Options{}, ErrUnsupported, "length 0"},
		{"version 4", func() []byte { b := array(1, "|u1", false, []int64{2, 2}); b[6] = 4; return b }(), Options{}, ErrUnsupported, "version 4.0"},
		{"pixels", array(1, "|u1", false, []int64{10, 10}), Options{MaxPixels: 99}, ErrLimit, "pixel limit"},
		{"bytes", array(1, "<i2", false, []int64{2, 10, 10}), Options{MaxBytes: 399}, ErrLimit, "byte limit"},
		{"planes", array(1, "|u1", false, []int64{3, 5, 1, 1}), Options{MaxPlanes: 14}, ErrLimit, "plane limit"},
		{"huge header", func() []byte {
			b := append([]byte{}, magic...)
			b = append(b, 2, 0)
			return binary.LittleEndian.AppendUint32(b, 1<<30)
		}(), Options{}, ErrLimit, "header is longer"},
		{"truncated", array(1, "<f8", false, []int64{4, 4})[:150], Options{}, ErrMalformed, "past the end"},
		{"cut header", array(1, "<f8", false, []int64{4, 4})[:40], Options{}, ErrMalformed, "cut short"},
		{"not npy", []byte("hello world"), Options{}, ErrMalformed, "neither"},
		{"vast shape", npyFile(1, headerDict("<f8", false, []int64{1 << 40, 1 << 40, 1 << 40}), nil), Options{}, ErrLimit, "limit"},
	}
	for _, c := range cases {
		_, err := decodeBytes(c.file, c.opts)
		require.Error(t, err, c.name)
		assert.ErrorIs(t, err, c.want, c.name)
		assert.Contains(t, err.Error(), c.says, c.name)
	}
	full := array(2, "<f4", true, []int64{2, 3, 4})
	for n := range len(full) {
		_, _ = decodeBytes(full[:n], Options{})
		_, _ = Inspect(context.Background(), bytes.NewReader(full[:n]), int64(n))
	}
}

type zipEntry struct {
	name   string
	data   []byte
	method uint16
}

func zipOf(t *testing.T, entries ...zipEntry) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
		require.NoError(t, err)
		_, err = w.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return b.Bytes()
}

func TestArchiveMembers(t *testing.T) {
	a := array(1, "<i2", false, []int64{3, 4})
	c := array(1, ">f4", true, []int64{2, 2, 3})
	z := zipOf(t, zipEntry{"a.npy", a, zip.Store}, zipEntry{"c.npy", c, zip.Deflate}, zipEntry{"readme.txt", []byte("hi"), zip.Deflate},
		zipEntry{"bad.npy", npyFile(1, headerDict("<c16", false, []int64{2, 2}), make([]byte, 64)), zip.Deflate})
	assert.True(t, Sniff(z))
	assert.True(t, Sniff(a))
	assert.False(t, Sniff(zipOf(t, zipEntry{"x.txt", nil, zip.Store})))

	members, err := InspectArchive(context.Background(), bytes.NewReader(z), int64(len(z)))
	require.NoError(t, err)
	require.Len(t, members, 4)
	assert.Equal(t, "a.npy", members[0].Name)
	assert.True(t, members[0].Readable)
	assert.Equal(t, "<i2", members[0].Header.Descr)
	assert.Equal(t, []int64{3, 4}, members[0].Header.Shape)
	assert.True(t, members[1].Header.FortranOrder)
	assert.False(t, members[2].Array)
	assert.False(t, members[3].Readable)
	assert.Contains(t, members[3].Reason, "complex")
	info, err := Inspect(context.Background(), bytes.NewReader(z), int64(len(z)))
	require.NoError(t, err)
	assert.True(t, info.Archive)
	assert.Len(t, info.Members, 4)

	_, err = decodeBytes(z, Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "a.npy, c.npy, bad.npy")
	s, err := decodeBytes(z, Options{Member: "c"})
	require.NoError(t, err)
	assert.Equal(t, "npz", s.Provenance().Format)
	assert.True(t, s.Provenance().Derivations.Has(imaging.DerivationAxesReordered))
	eachIndex([]int64{2, 2, 3}, func(idx []int64, flat int64) {
		got, _ := storedAtC(t, s, idx)
		require.Equal(t, valueAt("f4", flat), got)
	})
	s, err = decodeBytes(z, Options{Member: "a.npy"})
	require.NoError(t, err)
	assert.Equal(t, imaging.SampleKindI16, s.Kind())
	_, err = decodeBytes(z, Options{Member: "readme.txt"})
	assert.ErrorIs(t, err, ErrUnsupported)
	_, err = decodeBytes(z, Options{Member: "nope"})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "no member named")
	_, err = decodeBytes(z, Options{Member: "bad"})
	assert.ErrorIs(t, err, ErrUnsupported)

	one := zipOf(t, zipEntry{"only.npy", a, zip.Deflate}, zipEntry{"x.txt", nil, zip.Store})
	_, err = decodeBytes(one, Options{})
	require.NoError(t, err)
	_, err = decodeBytes(zipOf(t, zipEntry{"x.txt", nil, zip.Store}), Options{})
	assert.ErrorIs(t, err, ErrUnsupported)
	assert.Contains(t, err.Error(), "no .npy member")
}

// A member's limits come from its own header, and no more is inflated
// than the header's shape asks for: a member that declares a small array
// over a vast stream stops after the array and one byte.
func TestArchiveIsZipBombSafe(t *testing.T) {
	small := array(1, "|u1", false, []int64{2, 2})
	bomb := append(small, make([]byte, 64<<20)...)
	z := zipOf(t, zipEntry{"x.npy", bomb, zip.Deflate})
	require.Less(t, len(z), 1<<20, "the stream compresses")
	_, err := decodeBytes(z, Options{})
	assert.ErrorIs(t, err, ErrMalformed)
	assert.Contains(t, err.Error(), "bytes after the array data")

	vast := npyFile(1, headerDict("<f8", false, []int64{1 << 20, 1 << 20}), make([]byte, 1024))
	z = zipOf(t, zipEntry{"x.npy", vast, zip.Deflate})
	_, err = decodeBytes(z, Options{})
	assert.ErrorIs(t, err, ErrLimit)

	short := npyFile(1, headerDict("<f8", false, []int64{4, 4}), make([]byte, 64))
	z = zipOf(t, zipEntry{"x.npy", short, zip.Deflate})
	_, err = decodeBytes(z, Options{})
	assert.ErrorIs(t, err, ErrMalformed)
	assert.Contains(t, err.Error(), "cut short")
}

func TestArchiveChecksumIsChecked(t *testing.T) {
	a := array(1, "|u1", false, []int64{2, 3})
	z := zipOf(t, zipEntry{"a.npy", a, zip.Store})
	// Flip the last sample in the stored member; the CRC no longer agrees.
	i := bytes.Index(z, a)
	require.GreaterOrEqual(t, i, 0)
	z[i+len(a)-1] ^= 0xff
	_, err := decodeBytes(z, Options{})
	assert.ErrorIs(t, err, ErrMalformed)
	assert.True(t, errors.Is(err, zip.ErrChecksum))
}

func TestContextIsHonoured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := array(1, "|u1", false, []int64{4, 4})
	_, err := Decode(ctx, bytes.NewReader(b), int64(len(b)), "x", Options{})
	assert.ErrorIs(t, err, context.Canceled)
}
