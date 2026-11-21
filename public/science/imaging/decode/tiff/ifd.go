package tiff

import (
	"encoding/binary"
	"io"
	"math"
	"strconv"
	"strings"
)

// Field types of a directory entry (TIFF 6.0 §2, BigTIFF).
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeSByte     = 6
	typeUndefined = 7
	typeSShort    = 8
	typeSLong     = 9
	typeSRational = 10
	typeFloat     = 11
	typeDouble    = 12
	typeIFD       = 13
	typeLong8     = 16
	typeSLong8    = 17
	typeIFD8      = 18
)

// typeSize is the size of one value of a field type; 0 for a type this
// reader does not know, whose entries are ignored as TIFF 6.0 asks.
func typeSize(t uint16) uint64 {
	switch t {
	case typeByte, typeASCII, typeSByte, typeUndefined:
		return 1
	case typeShort, typeSShort:
		return 2
	case typeLong, typeSLong, typeFloat, typeIFD:
		return 4
	case typeRational, typeSRational, typeDouble, typeLong8, typeSLong8, typeIFD8:
		return 8
	}
	return 0
}

// maxEntries bounds the entries of one directory; a classic directory
// cannot hold more, and no BigTIFF writer needs more.
const maxEntries = 1<<16 - 1

// maxDirectories bounds the length of a directory chain.
const maxDirectories = 1 << 16

// maxSmallValue bounds what is read of a tag this reader reads whole and
// expects to be small: the GeoTIFF tags, GDAL's, the colour map.
const maxSmallValue = 4 << 20

// entry is one directory entry. Its value is not read until asked for.
type entry struct {
	tag    uint16
	typ    uint16
	count  uint64
	inline [8]byte
	offset uint64
}

// file is the container: the byte source, its order and its flavour.
type file struct {
	r     io.ReaderAt
	size  int64
	order binary.ByteOrder
	big   bool
}

// span checks that n bytes at off lie inside the file.
func (inst *file) span(off, n uint64) bool {
	return off <= uint64(inst.size) && n <= uint64(inst.size)-off
}

// read reads n bytes at off, refusing a span outside the file before
// allocating.
func (inst *file) read(off, n uint64, what string) ([]byte, error) {
	if !inst.span(off, n) {
		return nil, malformed(what + " lies outside the file")
	}
	b := make([]byte, n)
	if n == 0 {
		return b, nil
	}
	if _, err := inst.r.ReadAt(b, int64(off)); err != nil {
		return nil, malformedErr(what+" cannot be read", err)
	}
	return b, nil
}

// parseHeader reads the 8- or 16-byte header and returns the offset of the
// first directory.
func parseHeader(r io.ReaderAt, size int64) (f *file, first uint64, err error) {
	f = &file{r: r, size: size}
	if size < 8 {
		return nil, 0, malformed("shorter than a TIFF header")
	}
	head, err := f.read(0, min(16, uint64(size)), "the header")
	if err != nil {
		return nil, 0, err
	}
	switch {
	case head[0] == 'I' && head[1] == 'I':
		f.order = binary.LittleEndian
	case head[0] == 'M' && head[1] == 'M':
		f.order = binary.BigEndian
	default:
		return nil, 0, malformed("no TIFF byte-order mark")
	}
	switch f.order.Uint16(head[2:]) {
	case 42:
		first = uint64(f.order.Uint32(head[4:]))
	case 43:
		if len(head) < 16 {
			return nil, 0, malformed("shorter than a BigTIFF header")
		}
		if f.order.Uint16(head[4:]) != 8 || f.order.Uint16(head[6:]) != 0 {
			return nil, 0, unsupported("BigTIFF offset size " + itoa(uint64(f.order.Uint16(head[4:]))))
		}
		f.big = true
		first = f.order.Uint64(head[8:])
	default:
		return nil, 0, malformed("version is neither 42 (TIFF) nor 43 (BigTIFF)")
	}
	return f, first, nil
}

// parseIFD reads the directory at off: its entries, and the offset of the
// next directory, 0 at the end of the chain.
func (inst *file) parseIFD(off uint64) (entries []entry, next uint64, err error) {
	countSize, entrySize, nextSize := uint64(2), uint64(12), uint64(4)
	if inst.big {
		countSize, entrySize, nextSize = 8, 20, 8
	}
	head, err := inst.read(off, countSize, "a directory")
	if err != nil {
		return nil, 0, err
	}
	var n uint64
	if inst.big {
		n = inst.order.Uint64(head)
	} else {
		n = uint64(inst.order.Uint16(head))
	}
	if n == 0 {
		return nil, 0, malformed("a directory has no entries")
	}
	if n > maxEntries {
		return nil, 0, malformed("a directory has more entries than the format allows")
	}
	body, err := inst.read(off+countSize, n*entrySize+nextSize, "a directory's entries")
	if err != nil {
		return nil, 0, err
	}
	entries = make([]entry, 0, n)
	for i := range n {
		b := body[i*entrySize:]
		e := entry{tag: inst.order.Uint16(b), typ: inst.order.Uint16(b[2:])}
		if inst.big {
			e.count = inst.order.Uint64(b[4:])
			copy(e.inline[:], b[12:20])
			e.offset = inst.order.Uint64(b[12:])
		} else {
			e.count = uint64(inst.order.Uint32(b[4:]))
			copy(e.inline[:4], b[8:12])
			e.offset = uint64(inst.order.Uint32(b[8:]))
		}
		entries = append(entries, e)
	}
	tail := body[n*entrySize:]
	if inst.big {
		next = inst.order.Uint64(tail)
	} else {
		next = uint64(inst.order.Uint32(tail))
	}
	return entries, next, nil
}

// raw returns the bytes of an entry's value, refusing one larger than limit.
func (inst *file) raw(e entry, limit uint64) ([]byte, error) {
	ts := typeSize(e.typ)
	what := "the value of tag " + itoa(uint64(e.tag))
	if ts == 0 {
		return nil, malformed(what + " has an unknown field type")
	}
	if e.count > uint64(inst.size)/ts+1 {
		return nil, malformed(what + " counts more values than the file holds")
	}
	n := e.count * ts
	if n > limit {
		return nil, limitErr(what+" is larger than the reader takes", limit)
	}
	inlineSize := uint64(4)
	if inst.big {
		inlineSize = 8
	}
	if n <= inlineSize {
		return e.inline[:n], nil
	}
	return inst.read(e.offset, n, what)
}

// uints reads an entry of an unsigned integer type as uint64 values.
func (inst *file) uints(e entry, limit uint64) ([]uint64, error) {
	switch e.typ {
	case typeByte, typeUndefined, typeShort, typeLong, typeIFD, typeLong8, typeIFD8:
	default:
		return nil, malformed("tag " + itoa(uint64(e.tag)) + " is not of an unsigned integer type")
	}
	b, err := inst.raw(e, limit)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, e.count)
	for i := range out {
		switch e.typ {
		case typeByte, typeUndefined:
			out[i] = uint64(b[i])
		case typeShort:
			out[i] = uint64(inst.order.Uint16(b[2*i:]))
		case typeLong, typeIFD:
			out[i] = uint64(inst.order.Uint32(b[4*i:]))
		default:
			out[i] = inst.order.Uint64(b[8*i:])
		}
	}
	return out, nil
}

// firstUint reads the first value of an entry of an unsigned integer type.
// A tag that should hold one value and holds more is read by its first.
func (inst *file) firstUint(e entry) (uint64, error) {
	if e.count == 0 {
		return 0, malformed("tag " + itoa(uint64(e.tag)) + " has no value")
	}
	all := e.count
	e.count = 1
	switch e.typ {
	case typeByte, typeUndefined, typeShort, typeLong, typeIFD, typeLong8, typeIFD8:
	default:
		return 0, malformed("tag " + itoa(uint64(e.tag)) + " is not of an unsigned integer type")
	}
	// Where the value lies depends on the size of all of them.
	inlineSize := uint64(4)
	if inst.big {
		inlineSize = 8
	}
	ts := typeSize(e.typ)
	if all > uint64(inst.size)/ts+1 {
		return 0, malformed("tag " + itoa(uint64(e.tag)) + " counts more values than the file holds")
	}
	if all*ts > inlineSize {
		b, err := inst.read(e.offset, ts, "the value of tag "+itoa(uint64(e.tag)))
		if err != nil {
			return 0, err
		}
		copy(e.inline[:], b)
	}
	switch e.typ {
	case typeByte, typeUndefined:
		return uint64(e.inline[0]), nil
	case typeShort:
		return uint64(inst.order.Uint16(e.inline[:])), nil
	case typeLong, typeIFD:
		return uint64(inst.order.Uint32(e.inline[:])), nil
	}
	return inst.order.Uint64(e.inline[:]), nil
}

// floats reads an entry of any numeric type as float64 values.
func (inst *file) floats(e entry, limit uint64) ([]float64, error) {
	b, err := inst.raw(e, limit)
	if err != nil {
		return nil, err
	}
	o := inst.order
	out := make([]float64, e.count)
	for i := range out {
		switch e.typ {
		case typeByte, typeUndefined:
			out[i] = float64(b[i])
		case typeSByte:
			out[i] = float64(int8(b[i]))
		case typeShort:
			out[i] = float64(o.Uint16(b[2*i:]))
		case typeSShort:
			out[i] = float64(int16(o.Uint16(b[2*i:])))
		case typeLong, typeIFD:
			out[i] = float64(o.Uint32(b[4*i:]))
		case typeSLong:
			out[i] = float64(int32(o.Uint32(b[4*i:])))
		case typeLong8, typeIFD8:
			out[i] = float64(o.Uint64(b[8*i:]))
		case typeSLong8:
			out[i] = float64(int64(o.Uint64(b[8*i:])))
		case typeRational:
			out[i] = float64(o.Uint32(b[8*i:])) / float64(o.Uint32(b[8*i+4:]))
		case typeSRational:
			out[i] = float64(int32(o.Uint32(b[8*i:]))) / float64(int32(o.Uint32(b[8*i+4:])))
		case typeFloat:
			out[i] = float64(math.Float32frombits(o.Uint32(b[4*i:])))
		case typeDouble:
			out[i] = math.Float64frombits(o.Uint64(b[8*i:]))
		default:
			return nil, malformed("tag " + itoa(uint64(e.tag)) + " is not numeric")
		}
	}
	return out, nil
}

// ascii reads an entry of type ASCII, without its terminating NUL.
func (inst *file) ascii(e entry, limit uint64) (string, error) {
	if e.typ != typeASCII && e.typ != typeByte && e.typ != typeUndefined {
		return "", malformed("tag " + itoa(uint64(e.tag)) + " is not text")
	}
	b, err := inst.raw(e, limit)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\x00"), nil
}

// itoa formats an unsigned integer for a message.
func itoa(v uint64) string { return strconv.FormatUint(v, 10) }
