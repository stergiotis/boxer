package npy

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Header is what an NPY header states (ADR-0398 §SD8).
type Header struct {
	Major, Minor uint8
	// Descr is the "descr" value: the type string as written, or for a
	// structured type the text of its field list.
	Descr        string
	FortranOrder bool
	Shape        []int64
	// DataOffset is where the array data begins, from the start of the
	// NPY stream.
	DataOffset int64
}

// MaxHeaderLen bounds the header dictionary the reader accepts.
const MaxHeaderLen = 1 << 20

var magic = []byte("\x93NUMPY")

// dtype is a descr this reader reads.
type dtype struct {
	kind     imaging.SampleKindE
	size     int
	bigEndin bool
}

// readHeader reads the magic, version and header of an NPY stream.
func readHeader(rd io.Reader) (h Header, err error) {
	var pre [10]byte
	if _, err = io.ReadFull(rd, pre[:8]); err != nil {
		return h, eb.Build().Errorf("read the NPY magic: %w", errors.Join(refuse(ErrMalformed, "shorter than the NPY magic"), err))
	}
	if string(pre[:6]) != string(magic) {
		return h, refuse(ErrMalformed, "the stream does not begin with the NPY magic")
	}
	h.Major, h.Minor = pre[6], pre[7]
	var hlen int64
	switch {
	case h.Major == 1 && h.Minor == 0:
		if _, err = io.ReadFull(rd, pre[8:10]); err != nil {
			return h, eb.Build().Errorf("read the header length: %w", errors.Join(ErrMalformed, err))
		}
		hlen = int64(binary.LittleEndian.Uint16(pre[8:10]))
		h.DataOffset = 10
	case (h.Major == 2 || h.Major == 3) && h.Minor == 0:
		var l [4]byte
		if _, err = io.ReadFull(rd, l[:]); err != nil {
			return h, eb.Build().Errorf("read the header length: %w", errors.Join(ErrMalformed, err))
		}
		hlen = int64(binary.LittleEndian.Uint32(l[:]))
		h.DataOffset = 12
	default:
		return h, refuse(ErrUnsupported, "NPY format version "+strconv.Itoa(int(h.Major))+"."+strconv.Itoa(int(h.Minor)))
	}
	if hlen > MaxHeaderLen {
		return h, eb.Build().Int64("headerLen", hlen).Errorf("read the header: %w", refuse(ErrLimit, "the NPY header is longer than the reader accepts"))
	}
	h.DataOffset += hlen
	text := make([]byte, hlen)
	if _, err = io.ReadFull(rd, text); err != nil {
		return h, eb.Build().Int64("headerLen", hlen).Errorf("read the header: %w", errors.Join(refuse(ErrMalformed, "the header is cut short"), err))
	}
	err = h.parse(text)
	return
}

// parse reads the header dictionary: a Python literal with exactly the
// keys descr, fortran_order and shape, in any order.
func (inst *Header) parse(text []byte) error {
	p := &literal{s: text}
	v, err := p.value(0)
	if err != nil {
		return err
	}
	p.space()
	if p.i != len(p.s) {
		return refuse(ErrMalformed, "the header has text after its dictionary")
	}
	if v.kind != pyDict {
		return refuse(ErrMalformed, "the header is not a dictionary")
	}
	seen := 0
	for i, k := range v.keys {
		val := v.items[i]
		switch k {
		case "descr":
			seen |= 1
			switch val.kind {
			case pyStr:
				inst.Descr = val.s
			case pyList:
				inst.Descr = string(val.text)
				return refuse(ErrUnsupported, "a structured dtype "+inst.Descr)
			default:
				return refuse(ErrMalformed, "descr is neither a type string nor a field list")
			}
		case "fortran_order":
			seen |= 2
			if val.kind != pyBool {
				return refuse(ErrMalformed, "fortran_order is not True or False")
			}
			inst.FortranOrder = val.b
		case "shape":
			seen |= 4
			if val.kind != pyTuple {
				return refuse(ErrMalformed, "shape is not a tuple")
			}
			inst.Shape = make([]int64, len(val.items))
			for j, d := range val.items {
				if d.kind != pyInt || d.i < 0 {
					return refuse(ErrMalformed, "shape holds something other than non-negative integers")
				}
				inst.Shape[j] = d.i
			}
		default:
			return refuse(ErrMalformed, "the header has the unknown key "+strconv.Quote(k))
		}
	}
	if seen != 7 {
		return refuse(ErrMalformed, "the header lacks one of descr, fortran_order and shape")
	}
	return nil
}

// dtype maps the descr to a sample kind, or refuses it by name.
func (inst Header) dtype() (d dtype, err error) {
	s := inst.Descr
	if len(s) < 2 {
		return d, refuse(ErrUnsupported, "the dtype "+strconv.Quote(s))
	}
	order, code := s[0], s[1:]
	switch order {
	case '<', '>', '|':
	case '=':
		// The format description says a file is read the same on every
		// machine; numpy writes an explicit order, and '=' would name the
		// writer's.
		return d, refuse(ErrUnsupported, "the native byte order '=' in dtype "+strconv.Quote(s)+", which does not say which order the file holds")
	default:
		return d, refuse(ErrUnsupported, "the dtype "+strconv.Quote(s)+" without a byte-order character")
	}
	d.bigEndin = order == '>'
	switch code {
	case "u1":
		return dtype{kind: imaging.SampleKindU8, size: 1}, nil
	case "u2":
		d.kind, d.size = imaging.SampleKindU16, 2
	case "i2":
		d.kind, d.size = imaging.SampleKindI16, 2
	case "i4":
		d.kind, d.size = imaging.SampleKindI32, 4
	case "f4":
		d.kind, d.size = imaging.SampleKindF32, 4
	case "f8":
		d.kind, d.size = imaging.SampleKindF64, 8
	}
	if d.kind != 0 {
		if order == '|' {
			return d, refuse(ErrMalformed, "the multi-byte dtype "+strconv.Quote(s)+" with no byte order")
		}
		return d, nil
	}
	what := "the dtype " + strconv.Quote(s)
	switch {
	case code == "O" || code == "O8" || code == "O4":
		return d, refuse(ErrUnsupported, "an object array (dtype "+strconv.Quote(s)+"): reading it would require unpickling, which this reader never does")
	case code == "b1":
		what = "a boolean array (dtype " + strconv.Quote(s) + ")"
	case code == "i1":
		what = "8-bit signed integers (dtype " + strconv.Quote(s) + ")"
	case code == "u4":
		what = "32-bit unsigned integers (dtype " + strconv.Quote(s) + ")"
	case code == "u8" || code == "i8":
		what = "64-bit integers (dtype " + strconv.Quote(s) + ")"
	case code == "f2":
		what = "16-bit floats (dtype " + strconv.Quote(s) + ")"
	case code == "f12" || code == "f16" || code == "g":
		what = "extended-precision floats (dtype " + strconv.Quote(s) + ")"
	case strings.HasPrefix(code, "c"):
		what = "complex numbers (dtype " + strconv.Quote(s) + ")"
	case strings.HasPrefix(code, "S") || strings.HasPrefix(code, "a") || strings.HasPrefix(code, "U"):
		what = "strings (dtype " + strconv.Quote(s) + ")"
	case strings.HasPrefix(code, "M8"):
		what = "datetimes (dtype " + strconv.Quote(s) + ")"
	case strings.HasPrefix(code, "m8"):
		what = "timedeltas (dtype " + strconv.Quote(s) + ")"
	case strings.HasPrefix(code, "V"):
		what = "raw or structured records (dtype " + strconv.Quote(s) + ")"
	}
	return d, refuse(ErrUnsupported, what)
}

// layout checks the shape against the model and the limits and returns the
// plane size and count and the sample bytes.
func (inst Header) layout(d dtype, maxPixels, maxBytes int64, maxPlanes int) (rows, cols, planes, total int64, err error) {
	switch {
	case len(inst.Shape) == 0:
		return 0, 0, 0, 0, refuse(ErrUnsupported, "a zero-dimensional array: not an image")
	case len(inst.Shape) == 1:
		return 0, 0, 0, 0, refuse(ErrUnsupported, "a one-dimensional array: not an image")
	case len(inst.Shape) > 32:
		return 0, 0, 0, 0, refuse(ErrUnsupported, "an array of more than 32 dimensions")
	}
	k := len(inst.Shape)
	rows, cols = inst.Shape[k-2], inst.Shape[k-1]
	planes = 1
	for _, s := range inst.Shape {
		if s == 0 {
			return 0, 0, 0, 0, refuse(ErrUnsupported, "an array with a dimension of length 0: no samples")
		}
	}
	for _, s := range inst.Shape[:k-2] {
		if planes > math.MaxInt64/s {
			return 0, 0, 0, 0, refuse(ErrLimit, "the array has more planes than the plane limit")
		}
		planes *= s
	}
	if rows > math.MaxInt32 || cols > math.MaxInt32 || rows*cols > maxPixels {
		return 0, 0, 0, 0, eb.Build().Int64("rows", rows).Int64("cols", cols).Int64("maxPixels", maxPixels).
			Errorf("check the plane size: %w", refuse(ErrLimit, "a plane has more pixels than the pixel limit"))
	}
	if planes > int64(maxPlanes) {
		return 0, 0, 0, 0, eb.Build().Int64("planes", planes).Int("maxPlanes", maxPlanes).
			Errorf("check the plane count: %w", refuse(ErrLimit, "the array has more planes than the plane limit"))
	}
	// rows·cols < 2^62 and planes < 2^31, so the product fits when it is
	// under the byte limit, which is checked by division first.
	per := rows * cols * int64(d.size)
	if planes > maxBytes/per {
		return 0, 0, 0, 0, eb.Build().Int64("planes", planes).Int64("planeBytes", per).Int64("maxBytes", maxBytes).
			Errorf("check the sample bytes: %w", refuse(ErrLimit, "the array's samples take more bytes than the byte limit"))
	}
	return rows, cols, planes, planes * per, nil
}

// The header literal: Python's syntax for the few value shapes a header
// holds — strings, integers, True, False, None, tuples, lists and
// dictionaries.

//codelint:enum-prefix=py
type pyKindE uint8

const (
	pyStr pyKindE = iota + 1
	pyInt
	pyBool
	pyNone
	pyTuple
	pyList
	pyDict
)

type pyValue struct {
	kind  pyKindE
	s     string
	i     int64
	b     bool
	items []pyValue
	keys  []string
	text  []byte
}

type literal struct {
	s []byte
	i int
}

const maxDepth = 16

func (inst *literal) space() {
	for inst.i < len(inst.s) {
		switch inst.s[inst.i] {
		case ' ', '\t', '\n', '\r':
			inst.i++
		default:
			return
		}
	}
}

func (inst *literal) peek() byte {
	inst.space()
	if inst.i >= len(inst.s) {
		return 0
	}
	return inst.s[inst.i]
}

func (inst *literal) value(depth int) (v pyValue, err error) {
	if depth > maxDepth {
		return v, refuse(ErrMalformed, "the header nests too deeply")
	}
	start := inst.i
	defer func() { v.text = inst.s[start:inst.i] }()
	switch c := inst.peek(); {
	case c == '\'' || c == '"':
		v.kind = pyStr
		v.s, err = inst.str()
	case c == '-' || (c >= '0' && c <= '9'):
		v.kind = pyInt
		v.i, err = inst.integer()
	case c == '(' || c == '[':
		end := byte(')')
		v.kind = pyTuple
		if c == '[' {
			end, v.kind = ']', pyList
		}
		inst.i++
		v.items, err = inst.sequence(end, depth)
	case c == '{':
		v.kind = pyDict
		inst.i++
		err = inst.dict(&v, depth)
	default:
		for _, w := range []struct {
			word string
			kind pyKindE
			b    bool
		}{{"True", pyBool, true}, {"False", pyBool, false}, {"None", pyNone, false}} {
			if strings.HasPrefix(string(inst.s[inst.i:]), w.word) {
				inst.i += len(w.word)
				v.kind, v.b = w.kind, w.b
				return
			}
		}
		err = refuse(ErrMalformed, "the header holds a value that is not a literal this reader parses")
	}
	return
}

func (inst *literal) str() (string, error) {
	q := inst.s[inst.i]
	inst.i++
	var b strings.Builder
	for inst.i < len(inst.s) {
		c := inst.s[inst.i]
		inst.i++
		switch c {
		case q:
			return b.String(), nil
		case '\\':
			if inst.i >= len(inst.s) {
				return "", refuse(ErrMalformed, "a string in the header is not closed")
			}
			e := inst.s[inst.i]
			inst.i++
			switch e {
			case '\\', '\'', '"':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", refuse(ErrMalformed, "a string in the header has an escape this reader does not parse")
			}
		case '\n':
			return "", refuse(ErrMalformed, "a string in the header is not closed")
		default:
			b.WriteByte(c)
		}
	}
	return "", refuse(ErrMalformed, "a string in the header is not closed")
}

func (inst *literal) integer() (int64, error) {
	start := inst.i
	if inst.s[inst.i] == '-' {
		inst.i++
	}
	for inst.i < len(inst.s) && inst.s[inst.i] >= '0' && inst.s[inst.i] <= '9' {
		inst.i++
	}
	text := string(inst.s[start:inst.i])
	// Python 2 wrote long integers with an L suffix.
	if inst.i < len(inst.s) && inst.s[inst.i] == 'L' {
		inst.i++
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, refuse(ErrMalformed, "an integer in the header is not one, or does not fit 64 bits")
	}
	return v, nil
}

func (inst *literal) sequence(end byte, depth int) (items []pyValue, err error) {
	for {
		if inst.peek() == end {
			inst.i++
			return items, nil
		}
		var v pyValue
		if v, err = inst.value(depth + 1); err != nil {
			return
		}
		items = append(items, v)
		switch inst.peek() {
		case ',':
			inst.i++
		case end:
			inst.i++
			// A parenthesised single value without a comma is the value,
			// not a tuple.
			if end == ')' && len(items) == 1 {
				return nil, refuse(ErrMalformed, "a one-element tuple without its comma")
			}
			return items, nil
		default:
			return nil, refuse(ErrMalformed, "a sequence in the header is not closed")
		}
	}
}

func (inst *literal) dict(v *pyValue, depth int) error {
	for {
		if inst.peek() == '}' {
			inst.i++
			return nil
		}
		c := inst.peek()
		if c != '\'' && c != '"' {
			return refuse(ErrMalformed, "a dictionary key in the header is not a string")
		}
		k, err := inst.str()
		if err != nil {
			return err
		}
		for _, seen := range v.keys {
			if seen == k {
				return refuse(ErrMalformed, "the header repeats the key "+strconv.Quote(k))
			}
		}
		if inst.peek() != ':' {
			return refuse(ErrMalformed, "a dictionary entry in the header has no colon")
		}
		inst.i++
		val, err := inst.value(depth + 1)
		if err != nil {
			return err
		}
		v.keys = append(v.keys, k)
		v.items = append(v.items, val)
		switch inst.peek() {
		case ',':
			inst.i++
		case '}':
			inst.i++
			return nil
		default:
			return refuse(ErrMalformed, "the header's dictionary is not closed")
		}
	}
}
