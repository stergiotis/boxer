package npy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// The three ways a file is refused. Every error this package returns for a
// file matches one of them under [errors.Is], and its message names what
// was refused.
var (
	// ErrUnsupported: the file uses something this reader does not read.
	ErrUnsupported = errors.New("the file uses something this reader does not read")
	// ErrLimit: the file is over a pixel, byte or count limit.
	ErrLimit = errors.New("over a limit")
	// ErrMalformed: the file does not follow the format.
	ErrMalformed = errors.New("malformed")
)

type refusal struct {
	what string
	kind error
}

func (inst *refusal) Error() string { return inst.what + ": " + inst.kind.Error() }
func (inst *refusal) Unwrap() error { return inst.kind }

func refuse(kind error, what string) error { return &refusal{what: what, kind: kind} }

// DefaultMaxPixels bounds a plane at 64 megapixels.
const DefaultMaxPixels int64 = 64 << 20

// DefaultMaxBytes bounds the samples of a whole stack at 1 GiB.
const DefaultMaxBytes int64 = 1 << 30

// DefaultMaxPlanes bounds the number of planes of a stack, whose per-plane
// bookkeeping is not counted in MaxBytes.
const DefaultMaxPlanes = 1 << 16

// MaxMembers is the most members an NPZ archive may list.
const MaxMembers = 10_000

// maxArchiveHeaderBytes bounds what listing an archive inflates: the sum of
// its arrays' headers.
const maxArchiveHeaderBytes = 64 << 20

// DecoderName is what [imaging.Provenance.Decoder] says of a stack this
// package read.
const DecoderName = "boxer NPY reader"

// Options configures a decode. The zero value is usable.
type Options struct {
	// MaxPixels refuses a plane with more pixels than this; 0 means
	// [DefaultMaxPixels].
	MaxPixels int64
	// MaxBytes refuses an array whose samples take more bytes than this;
	// 0 means [DefaultMaxBytes].
	MaxBytes int64
	// MaxPlanes refuses an array of more planes than this; 0 means
	// [DefaultMaxPlanes].
	MaxPlanes int
	// Member names the member of an NPZ archive to read, with or without
	// its ".npy"; empty reads the archive's only array.
	Member string
}

func (inst Options) limits() (maxPixels, maxBytes int64, maxPlanes int) {
	maxPixels, maxBytes, maxPlanes = inst.MaxPixels, inst.MaxBytes, inst.MaxPlanes
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxPlanes <= 0 {
		maxPlanes = DefaultMaxPlanes
	}
	return
}

// Member is one member of an NPZ archive.
type Member struct {
	Name string
	// Array reports a member whose name ends in ".npy"; only those are read.
	Array bool
	// Header is the member's NPY header, when it is an array and its header
	// could be read.
	Header Header
	// Readable reports whether [Decode] reads the member; Reason says why
	// not when it does not.
	Readable bool
	Reason   string
}

// Info is what a file holds, read from its headers alone: an NPY file's
// header, or an NPZ archive's members.
type Info struct {
	Archive bool
	Header  Header
	Members []Member
}

var zipLocal = []byte("PK\x03\x04")

// Sniff reports whether head, the first bytes of a file, begins an NPY
// file, or a zip archive whose first member's name ends in ".npy" — the
// shape numpy's savez writes. An archive is recognised only when head holds
// that first name.
func Sniff(head []byte) bool {
	if bytes.HasPrefix(head, magic) {
		return true
	}
	if !bytes.HasPrefix(head, zipLocal) || len(head) < 30 {
		return false
	}
	n := int(binary.LittleEndian.Uint16(head[26:28]))
	if len(head) < 30+n {
		return false
	}
	return strings.HasSuffix(string(head[30:30+n]), ".npy")
}

// Inspect reports what a file holds, reading no samples: the header of an
// NPY file, or the members of an NPZ archive with each array's header. ctx
// is honoured between an archive's members.
func Inspect(ctx context.Context, r io.ReaderAt, size int64) (info Info, err error) {
	var head [6]byte
	n, _ := r.ReadAt(head[:], 0)
	switch {
	case bytes.HasPrefix(head[:n], magic):
		info.Header, err = readHeader(io.NewSectionReader(r, 0, size))
		return
	case bytes.HasPrefix(head[:n], zipLocal[:2]):
		info.Archive = true
		info.Members, err = InspectArchive(ctx, r, size)
		return
	}
	return info, refuse(ErrMalformed, "neither an NPY file nor a zip archive")
}

// InspectArchive lists the members of an NPZ archive, and for each array
// its header, read from the start of the member without inflating the
// rest of it. ctx is honoured between members.
func InspectArchive(ctx context.Context, r io.ReaderAt, size int64) (members []Member, err error) {
	zr, err := openZip(r, size)
	if err != nil {
		return nil, err
	}
	members = make([]Member, 0, len(zr.File))
	budget := int64(maxArchiveHeaderBytes)
	for _, f := range zr.File {
		if err = ctx.Err(); err != nil {
			return members, err
		}
		m := Member{Name: f.Name, Array: strings.HasSuffix(f.Name, ".npy")}
		if !m.Array {
			m.Reason = "not an .npy member"
			members = append(members, m)
			continue
		}
		if budget <= 0 {
			m.Reason = "header not read: the archive's headers together exceed what the reader inflates to list them"
			members = append(members, m)
			continue
		}
		var rc io.ReadCloser
		rc, err = openMember(f)
		if err == nil {
			m.Header, err = readHeader(rc)
			_ = rc.Close()
		}
		budget -= max(m.Header.DataOffset, 8)
		if err == nil {
			err = checkMember(m.Header, Options{})
		}
		if err != nil {
			m.Reason = err.Error()
			err = nil
		} else {
			m.Readable = true
		}
		members = append(members, m)
	}
	return
}

// checkMember refuses a header the reader does not read or that is over
// the limits.
func checkMember(h Header, opts Options) error {
	d, err := h.dtype()
	if err != nil {
		return err
	}
	maxPixels, maxBytes, maxPlanes := opts.limits()
	_, _, _, _, err = h.layout(d, maxPixels, maxBytes, maxPlanes)
	return err
}

func openZip(r io.ReaderAt, size int64) (*zip.Reader, error) {
	if size < 0 {
		return nil, refuse(ErrMalformed, "negative file size")
	}
	zr, err := zip.NewReader(r, size)
	if errors.Is(err, zip.ErrInsecurePath) {
		// A member name is only a label here; nothing touches a file
		// system.
		err = nil
	}
	if err != nil {
		return nil, eb.Build().Errorf("open the zip archive: %w", errors.Join(refuse(ErrMalformed, "not a readable zip archive"), err))
	}
	if len(zr.File) > MaxMembers {
		return nil, eb.Build().Int("members", len(zr.File)).Errorf("list the archive: %w", refuse(ErrLimit, "the archive has more members than the reader lists"))
	}
	return zr, nil
}

func openMember(f *zip.File) (io.ReadCloser, error) {
	if f.Method != zip.Store && f.Method != zip.Deflate {
		return nil, refuse(ErrUnsupported, "zip compression method "+strconv.Itoa(int(f.Method))+" of member "+strconv.Quote(f.Name))
	}
	if f.Flags&0x1 != 0 {
		return nil, refuse(ErrUnsupported, "the encrypted member "+strconv.Quote(f.Name))
	}
	rc, err := f.Open()
	if err != nil {
		return nil, eb.Build().Str("member", f.Name).Errorf("open the member: %w", errors.Join(ErrMalformed, err))
	}
	return rc, nil
}

// Decode reads an NPY file, or one array of an NPZ archive, into a stack
// (ADR-0398 §SD8): the last two dimensions are rows and columns, and each
// leading dimension is a selector axis. sourceName is recorded in the
// provenance; it is a name for a person, not a path this function opens.
// Limits are checked from the array's header before any sample is read.
func Decode(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (stack imaging.Stack, err error) {
	var head [6]byte
	n, _ := r.ReadAt(head[:], 0)
	switch {
	case bytes.HasPrefix(head[:n], magic):
		stack, err = decodeNPY(ctx, r, size, sourceName, opts)
	case bytes.HasPrefix(head[:n], zipLocal[:2]):
		stack, err = decodeNPZ(ctx, r, size, sourceName, opts)
	default:
		err = refuse(ErrMalformed, "neither an NPY file nor a zip archive")
	}
	if err != nil {
		return imaging.Stack{}, eb.Build().Str("source", sourceName).Errorf("read NPY: %w", err)
	}
	return
}

func decodeNPY(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (imaging.Stack, error) {
	sr := io.NewSectionReader(r, 0, size)
	h, err := readHeader(sr)
	if err != nil {
		return imaging.Stack{}, err
	}
	prov := imaging.Provenance{SourceName: sourceName, Format: "npy"}
	return decodeArray(ctx, h, sr, size-h.DataOffset, prov, opts)
}

func decodeNPZ(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (stack imaging.Stack, err error) {
	zr, err := openZip(r, size)
	if err != nil {
		return
	}
	var chosen *zip.File
	arrays := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".npy") {
			arrays = append(arrays, f.Name)
		}
	}
	switch {
	case opts.Member != "":
		for _, f := range zr.File {
			if f.Name == opts.Member || f.Name == opts.Member+".npy" {
				chosen = f
				break
			}
		}
		if chosen == nil {
			return stack, refuse(ErrUnsupported, "no member named "+strconv.Quote(opts.Member)+" (arrays: "+strings.Join(arrays, ", ")+")")
		}
		if !strings.HasSuffix(chosen.Name, ".npy") {
			return stack, refuse(ErrUnsupported, "the member "+strconv.Quote(chosen.Name)+", which is not an .npy array")
		}
	case len(arrays) == 0:
		return stack, refuse(ErrUnsupported, "an archive with no .npy member")
	case len(arrays) > 1:
		return stack, refuse(ErrUnsupported, "an archive of several arrays ("+strings.Join(arrays, ", ")+") without Options.Member naming one")
	default:
		for _, f := range zr.File {
			if f.Name == arrays[0] {
				chosen = f
				break
			}
		}
	}
	rc, err := openMember(chosen)
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	h, err := readHeader(rc)
	if err != nil {
		return stack, eb.Build().Str("member", chosen.Name).Errorf("read the member's header: %w", err)
	}
	prov := imaging.Provenance{SourceName: sourceName, Format: "npz",
		Notes: []string{"member " + chosen.Name + " of " + strconv.Itoa(len(zr.File)) + " in the archive"}}
	// The data is read from the inflating stream, never more of it than
	// the header's shape asks for, which the limits bound; a member that
	// claims more is not inflated further.
	stack, err = decodeArray(ctx, h, rc, -1, prov, opts)
	if err != nil {
		return stack, eb.Build().Str("member", chosen.Name).Errorf("read the member: %w", err)
	}
	// One byte more reaches the end of the member, where the zip reader
	// checks its CRC and size.
	var one [1]byte
	m, rerr := rc.Read(one[:])
	switch {
	case m > 0:
		return imaging.Stack{}, eb.Build().Str("member", chosen.Name).Errorf("read the member: %w", refuse(ErrMalformed, "the member holds bytes after the array data"))
	case rerr != nil && rerr != io.EOF:
		return imaging.Stack{}, eb.Build().Str("member", chosen.Name).Errorf("check the member: %w", errors.Join(refuse(ErrMalformed, "the member fails the zip checks"), rerr))
	}
	return
}

// decodeArray reads the samples that follow a header from rd. available is
// the bytes left in the stream, or -1 when the stream does not know.
func decodeArray(ctx context.Context, h Header, rd io.Reader, available int64, prov imaging.Provenance, opts Options) (stack imaging.Stack, err error) {
	d, err := h.dtype()
	if err != nil {
		return
	}
	maxPixels, maxBytes, maxPlanes := opts.limits()
	rows, cols, planes, total, err := h.layout(d, maxPixels, maxBytes, maxPlanes)
	if err != nil {
		return
	}
	if available >= 0 && total > available {
		return stack, eb.Build().Int64("bytes", total).Int64("available", available).
			Errorf("check the data: %w", refuse(ErrMalformed, "the array data extends past the end of the file"))
	}
	k := len(h.Shape)
	axes := make([]imaging.Axis, k-2)
	for i := range axes {
		axes[i] = imaging.Axis{Name: "axis" + strconv.Itoa(i), Role: imaging.AxisRoleSelector, Size: int(h.Shape[i])}
	}
	prov.Decoder = DecoderName
	prov.StoredBitDepth = uint8(8 * d.size)
	shape := make([]string, k)
	for i, s := range h.Shape {
		shape[i] = strconv.FormatInt(s, 10)
	}
	prov.StoredColorType = "dtype " + h.Descr + ", shape (" + strings.Join(shape, ", ") + ")"
	if h.FortranOrder {
		prov.StoredColorType += ", Fortran order"
		prov.Derivations |= imaging.DerivationAxesReordered
	}
	w, hh, np := int(cols), int(rows), int(planes)
	ps := make([]imaging.Plane, np)
	var perr error
	sh := h.Shape
	f := h.FortranOrder
	switch d.kind {
	case imaging.SampleKindU8:
		bufs := allocate[uint8](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 1, func(src []byte, dst []uint8) { copy(dst, src) }); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneU8(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case imaging.SampleKindU16:
		bufs := allocate[uint16](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 2, func(src []byte, dst []uint16) {
			for i := range dst {
				dst[i] = order(d).Uint16(src[2*i:])
			}
		}); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneU16(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case imaging.SampleKindI16:
		bufs := allocate[int16](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 2, func(src []byte, dst []int16) {
			for i := range dst {
				dst[i] = int16(order(d).Uint16(src[2*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneI16(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case imaging.SampleKindI32:
		bufs := allocate[int32](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 4, func(src []byte, dst []int32) {
			for i := range dst {
				dst[i] = int32(order(d).Uint32(src[4*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneI32(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case imaging.SampleKindF32:
		bufs := allocate[float32](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 4, func(src []byte, dst []float32) {
			for i := range dst {
				dst[i] = math.Float32frombits(order(d).Uint32(src[4*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneF32(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case imaging.SampleKindF64:
		bufs := allocate[float64](np, w*hh)
		if err = readSamples(ctx, rd, bufs, sh, f, 8, func(src []byte, dst []float64) {
			for i := range dst {
				dst[i] = math.Float64frombits(order(d).Uint64(src[8*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				ps[i], perr = imaging.NewPlaneF64(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	}
	if err != nil {
		return stack, eb.Build().Errorf("read samples: %w", err)
	}
	stack, err = imaging.NewStack(axes, ps, 0, imaging.Placement{}, prov)
	if err != nil {
		return imaging.Stack{}, eb.Build().Errorf("assemble the stack: %w", errors.Join(ErrMalformed, err))
	}
	return
}

func order(d dtype) binary.ByteOrder {
	if d.bigEndin {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func allocate[T any](planes, samples int) [][]T {
	out := make([][]T, planes)
	for i := range out {
		out[i] = make([]T, samples)
	}
	return out
}

const readChunk = 1 << 20

// readSamples reads the array's samples from rd in chunks, checking ctx
// between them. In C order the stream is the planes one after another, row
// by row. In Fortran order the first index varies fastest; each sample is
// put where the C order would have it.
func readSamples[T any](ctx context.Context, rd io.Reader, planes [][]T, shape []int64, fortran bool, bpp int, conv func(src []byte, dst []T)) error {
	k := len(shape)
	per := shape[k-2] * shape[k-1]
	total := int64(len(planes)) * per
	buf := make([]byte, min(int64(readChunk), total*int64(bpp)))
	var tmp []T
	// C strides of every index, and the running C position of the
	// current Fortran-ordered sample.
	var strides, idx []int64
	var ci int64
	if fortran {
		tmp = make([]T, len(buf)/bpp)
		strides = make([]int64, k)
		idx = make([]int64, k)
		s := int64(1)
		for i := k - 1; i >= 0; i-- {
			strides[i] = s
			s *= shape[i]
		}
	}
	var g int64
	for g < total {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(int64(len(buf)/bpp), total-g)
		chunk := buf[:n*int64(bpp)]
		if _, err := io.ReadFull(rd, chunk); err != nil {
			return eb.Build().Int64("sample", g).Errorf("read data: %w", errors.Join(refuse(ErrMalformed, "the array data is cut short"), err))
		}
		if !fortran {
			for len(chunk) > 0 {
				p, off := g/per, g%per
				run := min(per-off, int64(len(chunk)/bpp))
				conv(chunk[:run*int64(bpp)], planes[p][off:off+run])
				chunk = chunk[run*int64(bpp):]
				g += run
			}
			continue
		}
		vals := tmp[:n]
		conv(chunk, vals)
		for _, v := range vals {
			planes[ci/per][ci%per] = v
			for i := range k {
				idx[i]++
				ci += strides[i]
				if idx[i] < shape[i] {
					break
				}
				ci -= shape[i] * strides[i]
				idx[i] = 0
			}
		}
		g += n
	}
	return nil
}
