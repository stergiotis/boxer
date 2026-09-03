package diag

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/big"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// offsetError is the error the walk returns: the sentinel it violated and
// the byte offset of the head it happened at. It is a local type rather
// than an eb-built error because the eb package reaches this package's
// parent, and the parent's CLI reaches here — the wrapping is what
// errors.Is needs and nothing more.
type offsetError struct {
	off      int
	sentinel error
}

func (e *offsetError) Error() string {
	return e.sentinel.Error() + " at byte " + strconv.Itoa(e.off)
}

func (e *offsetError) Unwrap() error { return e.sentinel }

// CBOR major types, unshifted.
const (
	majorUint   = 0
	majorNeg    = 1
	majorBytes  = 2
	majorText   = 3
	majorArray  = 4
	majorMap    = 5
	majorTag    = 6
	majorSimple = 7
)

// Additional-information values with a meaning of their own.
const (
	aiOneByte    = 24
	aiTwoBytes   = 25
	aiFourBytes  = 26
	aiEightBytes = 27
	aiIndefinite = 31
	breakByte    = 0xff
)

// printer walks one input and appends the rendering to out. A measuring
// printer (noSpans) keeps out and drops the spans; pretty mode spawns one
// per container to learn the container's compact width.
type printer struct {
	opts    Options
	data    []byte
	off     int
	out     []byte
	spans   []Span
	noSpans bool
	indent  string
	width   int
	depth   int
	path    []PathElem
	scratch []byte
	// failOff and failErr record the first failure: the offset of the head
	// that could not be read and the sentinel it violated, for degrade.
	failOff int
	failErr error
	// keyDepth is positive while a map key is being rendered; nothing
	// inside a key is annotated, the value is.
	keyDepth int
	// lastFiller is where the trailing run of filler spans began, or -1
	// when the last emit was not filler — degrade drops it so the failure
	// does not land on a half-indented line.
	lastFiller int
}

func newPrinter(data []byte, opts Options, noSpans bool) (p *printer) {
	p = &printer{
		opts:    opts,
		data:    data,
		noSpans: noSpans,
		indent:  opts.Indent,
		width:   opts.Width,
		out:     make([]byte, 0, 2*len(data)+16),
		scratch: make([]byte, 0, 64),
	}
	p.lastFiller = -1
	if p.indent == "" {
		p.indent = DefaultIndent
	}
	if p.width <= 0 {
		p.width = DefaultWidth
	}
	if !noSpans {
		p.spans = make([]Span, 0, len(data)/2+8)
	}
	return
}

// run renders the whole input: one item, or a sequence of them, and on
// failure the degradation tail.
func (p *printer) run() (err error) {
	if len(p.data) == 0 {
		err = p.fail(0, ErrEmpty)
		p.emit(CategoryError, "/ error: no input /")
		return
	}
	first := true
	for p.off < len(p.data) {
		if !first {
			if p.opts.Compact {
				p.emit(CategoryFiller, ", ")
			} else {
				p.emit(CategoryFiller, "\n")
			}
		}
		first = false
		err = p.item(false)
		if err != nil {
			p.degrade()
			return
		}
		if !p.opts.Sequence && p.off < len(p.data) {
			err = p.fail(p.off, ErrTrailingBytes)
			p.degrade()
			return
		}
	}
	return
}

// degrade writes the failure and the unparsed remainder after whatever was
// rendered before it: the bytes from the head that could not be read.
func (p *printer) degrade() {
	errOff := p.failOff
	if p.lastFiller >= 0 {
		p.out = p.out[:p.lastFiller]
		if !p.noSpans {
			n := len(p.spans)
			for n > 0 && p.spans[n-1].Start >= p.lastFiller {
				n--
			}
			p.spans = p.spans[:n]
		}
		p.lastFiller = -1
	}
	if len(p.out) > 0 {
		if p.opts.Compact {
			p.emit(CategoryFiller, " ")
		} else {
			p.emit(CategoryFiller, "\n")
		}
	}
	p.emit(CategoryError, "/ error: "+p.failErr.Error()+" at byte "+strconv.Itoa(errOff)+" /")
	if errOff < len(p.data) {
		p.emit(CategoryFiller, " ")
		p.scratch = p.scratch[:0]
		p.scratch = append(p.scratch, "h'"...)
		p.scratch = hex.AppendEncode(p.scratch, p.data[errOff:])
		p.scratch = append(p.scratch, '\'')
		p.emit(CategoryBytes, string(p.scratch))
	}
}

func (p *printer) fail(off int, sentinel error) error {
	if p.failErr == nil {
		p.failOff = off
		p.failErr = sentinel
	}
	return &offsetError{off: off, sentinel: sentinel}
}

// emit appends one span. Every byte of out goes through here, which is what
// keeps the spans contiguous.
func (p *printer) emit(cat CategoryE, s string) {
	if cat == CategoryFiller {
		if p.lastFiller < 0 {
			p.lastFiller = len(p.out)
		}
	} else {
		p.lastFiller = -1
	}
	if !p.noSpans {
		p.spans = append(p.spans, Span{Category: cat, Start: len(p.out), Stop: len(p.out) + len(s), Text: s})
	}
	p.out = append(p.out, s...)
}

// column is the width of the current output line.
func (p *printer) column() int {
	i := bytes.LastIndexByte(p.out, '\n')
	return len(p.out) - i - 1
}

func (p *printer) newline(depth int) {
	p.scratch = p.scratch[:0]
	p.scratch = append(p.scratch, '\n')
	for range depth {
		p.scratch = append(p.scratch, p.indent...)
	}
	p.emit(CategoryFiller, string(p.scratch))
}

// head reads one item head: major type, additional information and the
// argument. For ai 31 the argument is 0 and the caller decides whether an
// indefinite length is admissible there.
func (p *printer) head() (mt byte, ai byte, val uint64, err error) {
	if p.off >= len(p.data) {
		err = p.fail(p.off, ErrTruncated)
		return
	}
	ib := p.data[p.off]
	mt = ib >> 5
	ai = ib & 0x1f
	p.off++
	var n int
	switch {
	case ai < aiOneByte:
		val = uint64(ai)
		return
	case ai == aiOneByte:
		n = 1
	case ai == aiTwoBytes:
		n = 2
	case ai == aiFourBytes:
		n = 4
	case ai == aiEightBytes:
		n = 8
	case ai == aiIndefinite:
		return
	default:
		err = p.fail(p.off-1, ErrReservedInfo)
		return
	}
	if p.off+n > len(p.data) {
		err = p.fail(p.off-1, ErrTruncated)
		return
	}
	switch n {
	case 1:
		val = uint64(p.data[p.off])
	case 2:
		val = uint64(binary.BigEndian.Uint16(p.data[p.off:]))
	case 4:
		val = uint64(binary.BigEndian.Uint32(p.data[p.off:]))
	case 8:
		val = binary.BigEndian.Uint64(p.data[p.off:])
	}
	p.off += n
	return
}

// atBreak reports whether the next byte is the break stop code, consuming
// it when it is.
func (p *printer) atBreak() (found bool, err error) {
	if p.off >= len(p.data) {
		err = p.fail(p.off, ErrTruncated)
		return
	}
	if p.data[p.off] == breakByte {
		p.off++
		found = true
	}
	return
}

func (p *printer) scalarCat(cat CategoryE, inKey bool) CategoryE {
	if inKey {
		return CategoryKey
	}
	return cat
}

// item renders one data item at the current offset. inKey marks a map
// key, whose scalar spans take CategoryKey.
func (p *printer) item(inKey bool) (err error) {
	start := p.off
	mt, ai, val, err := p.head()
	if err != nil {
		return
	}
	switch mt {
	case majorUint:
		if ai == aiIndefinite {
			return p.fail(start, ErrIndefiniteHead)
		}
		p.emit(p.scalarCat(CategoryNumber, inKey), strconv.FormatUint(val, 10))
		p.annotate()
	case majorNeg:
		if ai == aiIndefinite {
			return p.fail(start, ErrIndefiniteHead)
		}
		p.emit(p.scalarCat(CategoryNumber, inKey), negativeText(val))
		p.annotate()
	case majorBytes:
		if ai == aiIndefinite {
			return p.chunks(mt, inKey)
		}
		if p.off+int(val) > len(p.data) || val > uint64(len(p.data)) {
			return p.fail(start, ErrTruncated)
		}
		b := p.data[p.off : p.off+int(val)]
		p.off += int(val)
		p.byteString(b, inKey)
		p.annotate()
	case majorText:
		if ai == aiIndefinite {
			return p.chunks(mt, inKey)
		}
		if p.off+int(val) > len(p.data) || val > uint64(len(p.data)) {
			return p.fail(start, ErrTruncated)
		}
		b := p.data[p.off : p.off+int(val)]
		if !utf8.Valid(b) {
			return p.fail(start, ErrInvalidUTF8)
		}
		p.off += int(val)
		p.emit(p.scalarCat(CategoryText, inKey), textString(p.scratch[:0], b))
		p.annotate()
	case majorArray:
		return p.container(start, false, ai == aiIndefinite, val)
	case majorMap:
		return p.container(start, true, ai == aiIndefinite, val)
	case majorTag:
		if ai == aiIndefinite {
			return p.fail(start, ErrIndefiniteHead)
		}
		return p.tag(val, inKey)
	case majorSimple:
		return p.simple(start, ai, val, inKey)
	}
	return
}

// negativeText renders a major-type-1 argument: -1 - val, through big.Int
// when that does not fit int64 — the fxamacker spelling.
func negativeText(val uint64) string {
	if val > math.MaxInt64 {
		bi := new(big.Int).SetUint64(val)
		bi.Add(bi, big.NewInt(1))
		bi.Neg(bi)
		return bi.String()
	}
	return strconv.FormatInt(int64(-1)^int64(val), 10)
}

// byteString writes h'…', folded into rows in pretty mode when
// Options.BytesFold asks for it.
func (p *printer) byteString(b []byte, inKey bool) {
	cat := p.scalarCat(CategoryBytes, inKey)
	fold := p.opts.BytesFold
	if p.opts.Compact || fold <= 0 || len(b) <= fold {
		p.scratch = p.scratch[:0]
		p.scratch = append(p.scratch, "h'"...)
		p.scratch = hex.AppendEncode(p.scratch, b)
		p.scratch = append(p.scratch, '\'')
		p.emit(cat, string(p.scratch))
		return
	}
	p.emit(cat, "h'")
	for i := 0; i < len(b); i += fold {
		p.newline(p.depth + 1)
		end := min(i+fold, len(b))
		p.scratch = p.scratch[:0]
		p.scratch = hex.AppendEncode(p.scratch, b[i:end])
		p.emit(cat, string(p.scratch))
	}
	p.newline(p.depth)
	p.emit(cat, "'")
}

// textString renders a text string with the fxamacker escaping: \t \n \r
// \\ \" by name, other control and non-ASCII bytes as \uXXXX (UTF-16 pairs
// above the BMP). dst is a scratch buffer.
func textString(dst []byte, b []byte) string {
	dst = append(dst, '"')
	for i := 0; i < len(b); {
		c := b[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '\t':
				dst = append(dst, '\\', 't')
			case c == '\n':
				dst = append(dst, '\\', 'n')
			case c == '\r':
				dst = append(dst, '\\', 'r')
			case c == '\\' || c == '"':
				dst = append(dst, '\\', c)
			case c >= ' ' && c <= '~':
				dst = append(dst, c)
			default:
				dst = appendU16(dst, rune(c))
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r < 0x10000 {
			dst = appendU16(dst, r)
		} else {
			r1, r2 := utf16.EncodeRune(r)
			dst = appendU16(dst, r1)
			dst = appendU16(dst, r2)
		}
		i += size
	}
	dst = append(dst, '"')
	return string(dst)
}

func appendU16(dst []byte, r rune) []byte {
	const digits = "0123456789abcdef"
	v := uint16(r)
	return append(dst, '\\', 'u', digits[v>>12], digits[(v>>8)&0xf], digits[(v>>4)&0xf], digits[v&0xf])
}

// chunks renders an indefinite-length byte or text string: `”_` / `""_`
// when it has no chunks, else the chunks in (_ …) like a container.
func (p *printer) chunks(mt byte, inKey bool) (err error) {
	start := p.off - 1
	found, err := p.atBreak()
	if err != nil {
		return
	}
	if found {
		if mt == majorBytes {
			p.emit(p.scalarCat(CategoryBytes, inKey), "''_")
		} else {
			p.emit(p.scalarCat(CategoryText, inKey), `""_`)
		}
		p.annotate()
		return
	}
	multi := p.multiline(start)
	p.emit(CategoryStructural, "(_")
	if !multi {
		p.emit(CategoryFiller, " ")
	}
	p.depth++
	i := 0
	for {
		found, err = p.atBreak()
		if err != nil {
			return
		}
		if found {
			break
		}
		if i > 0 {
			p.emit(CategoryStructural, ",")
			if !multi {
				p.emit(CategoryFiller, " ")
			}
		}
		if multi {
			p.newline(p.depth)
		}
		if p.off >= len(p.data) {
			return p.fail(p.off, ErrTruncated)
		}
		if p.data[p.off]>>5 != mt || p.data[p.off]&0x1f == aiIndefinite {
			return p.fail(p.off, ErrChunkType)
		}
		if err = p.item(inKey); err != nil {
			return
		}
		i++
	}
	p.depth--
	if multi {
		p.newline(p.depth)
	}
	p.emit(CategoryStructural, ")")
	return
}

// multiline decides whether the container starting at start (its head
// offset) is laid out one element per line: never in Compact mode, else
// when its compact rendering overruns the line from the current column.
func (p *printer) multiline(start int) bool {
	if p.opts.Compact {
		return false
	}
	avail := p.width - p.column()
	m := &printer{
		opts:    p.opts,
		data:    p.data,
		off:     start,
		noSpans: true,
		indent:  p.indent,
		width:   p.width,
		out:     p.scratch[:0],
		path:    p.path,
		scratch: make([]byte, 0, 64),
	}
	m.lastFiller = -1
	m.opts.Compact = true
	// A comment is a label on the line, not content: it does not count
	// against the width, so a container that fits stays on one line and
	// carries its comment after it.
	m.opts.Annotate = nil
	if err := m.item(false); err != nil {
		return true
	}
	return len(m.out) > avail
}

// container renders an array or a map, definite or indefinite.
func (p *printer) container(start int, isMap bool, indefinite bool, count uint64) (err error) {
	if p.depth >= maxNesting {
		return p.fail(start, ErrNesting)
	}
	if !indefinite && count > uint64(len(p.data)-p.off) {
		// Every element costs at least one byte, so the count alone proves
		// the item truncated; without this a huge count would spin.
		return p.fail(start, ErrTruncated)
	}
	multi := p.multiline(start)
	open, closer := "[", "]"
	if isMap {
		open, closer = "{", "}"
	}
	p.emit(CategoryStructural, open)
	if indefinite {
		p.emit(CategoryStructural, "_")
		if !multi {
			p.emit(CategoryFiller, " ")
		}
	}
	if multi {
		p.annotate()
	}
	p.depth++
	var i uint64
	for {
		if indefinite {
			var found bool
			found, err = p.atBreak()
			if err != nil {
				return
			}
			if found {
				break
			}
		} else if i >= count {
			break
		}
		if i > 0 {
			p.emit(CategoryStructural, ",")
			if !multi {
				p.emit(CategoryFiller, " ")
			}
		}
		if multi {
			p.newline(p.depth)
		}
		if isMap {
			keyStart := p.off
			p.keyDepth++
			err = p.item(true)
			p.keyDepth--
			if err != nil {
				return
			}
			p.emit(CategoryStructural, ":")
			p.emit(CategoryFiller, " ")
			p.path = append(p.path, PathElem{Kind: PathElemKey, Key: p.data[keyStart:p.off]})
		} else {
			p.path = append(p.path, PathElem{Kind: PathElemIndex, Index: int(i)})
		}
		err = p.item(false)
		p.path = p.path[:len(p.path)-1]
		if err != nil {
			return
		}
		i++
	}
	p.depth--
	if multi {
		p.newline(p.depth)
	}
	p.emit(CategoryStructural, closer)
	if !multi {
		p.annotate()
	}
	return
}

// tag renders `n(content)`. Tags 2 and 3 over a byte string render as the
// bignum they carry, the fxamacker spelling; over anything else the tag is
// shown as any other tag.
func (p *printer) tag(num uint64, inKey bool) (err error) {
	if (num == 2 || num == 3) && p.off < len(p.data) && p.data[p.off]>>5 == majorBytes && p.data[p.off]&0x1f != aiIndefinite {
		start := p.off
		_, _, n, herr := p.head()
		if herr == nil && n <= uint64(len(p.data)) && p.off+int(n) <= len(p.data) {
			b := p.data[p.off : p.off+int(n)]
			p.off += int(n)
			bi := new(big.Int).SetBytes(b)
			if num == 3 {
				bi.Add(bi, big.NewInt(1))
				bi.Neg(bi)
			}
			p.emit(p.scalarCat(CategoryNumber, inKey), bi.String())
			p.annotate()
			return
		}
		p.off = start
		p.failErr = nil
	}
	p.emit(CategoryTag, strconv.FormatUint(num, 10))
	p.emit(CategoryStructural, "(")
	if p.opts.TagComments {
		if name := TagName(num); name != "" {
			p.emit(CategoryComment, "/ "+name+" /")
			p.emit(CategoryFiller, " ")
		}
	}
	p.path = append(p.path, PathElem{Kind: PathElemTag, Tag: num})
	err = p.item(inKey)
	p.path = p.path[:len(p.path)-1]
	if err != nil {
		return
	}
	p.emit(CategoryStructural, ")")
	return
}

// simple renders major type 7: the four named values, simple(n), a float,
// or the break that has no business here.
func (p *printer) simple(start int, ai byte, val uint64, inKey bool) (err error) {
	cat := p.scalarCat(CategorySimple, inKey)
	switch ai {
	case 20:
		p.emit(cat, "false")
	case 21:
		p.emit(cat, "true")
	case 22:
		p.emit(cat, "null")
	case 23:
		p.emit(cat, "undefined")
	case aiTwoBytes, aiFourBytes, aiEightBytes:
		p.emit(p.scalarCat(CategoryNumber, inKey), p.floatText(ai, val))
	case aiIndefinite:
		return p.fail(start, ErrUnexpectedBreak)
	default:
		p.emit(cat, "simple("+strconv.FormatUint(val, 10)+")")
	}
	p.annotate()
	return
}

// floatText renders a float the way fxamacker does: NaN / Infinity /
// -Infinity by name, otherwise the shortest round-trip decimal in the ES6
// style (exponent form outside [1e-6, 1e21)), always with a fraction or an
// exponent so it cannot be read as an integer, plus the §8.1 precision
// suffix when asked.
func (p *printer) floatText(ai byte, val uint64) string {
	var f float64
	switch ai {
	case aiTwoBytes:
		f = float16ToFloat64(uint16(val))
	case aiFourBytes:
		f = float64(math.Float32frombits(uint32(val)))
	default:
		f = math.Float64frombits(val)
	}
	switch {
	case f != f:
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	b := p.scratch[:0]
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		b = strconv.AppendFloat(b, f, 'e', -1, 64)
		n := len(b)
		if n >= 4 && string(b[n-4:n-1]) == "e-0" {
			b = append(b[:n-2], b[n-1])
		}
	} else {
		b = strconv.AppendFloat(b, f, 'f', -1, 64)
	}
	if bytes.IndexByte(b, '.') < 0 {
		if i := bytes.IndexByte(b, 'e'); i < 0 {
			b = append(b, '.', '0')
		} else {
			b = append(b[:i+2], b[i:]...)
			b[i] = '.'
			b[i+1] = '0'
		}
	}
	if p.opts.FloatPrecision {
		switch ai {
		case aiTwoBytes:
			b = append(b, "_1"...)
		case aiFourBytes:
			b = append(b, "_2"...)
		default:
			b = append(b, "_3"...)
		}
	}
	return string(b)
}

// float16ToFloat64 widens an IEEE 754 binary16 bit pattern. Subnormals
// scale by 2^-24; the exponent-all-ones cases keep their payload's
// NaN-ness or sign of infinity.
func float16ToFloat64(h uint16) float64 {
	sign := float64(1)
	if h&0x8000 != 0 {
		sign = -1
	}
	exp := int((h >> 10) & 0x1f)
	mant := float64(h & 0x3ff)
	switch exp {
	case 0:
		return sign * mant * math.Ldexp(1, -24)
	case 0x1f:
		if mant != 0 {
			return math.NaN()
		}
		return math.Inf(int(sign))
	}
	return sign * (1 + mant/1024) * math.Ldexp(1, exp-15)
}

// annotate asks the hook for the current item's comment and writes it
// after the item — after the opening bracket when the item is a container
// that spans lines, so the label sits on the line a reader starts from.
func (p *printer) annotate() {
	if p.opts.Annotate == nil || p.keyDepth > 0 {
		return
	}
	text := p.opts.Annotate(p.path)
	if text == "" {
		return
	}
	p.emit(CategoryFiller, " ")
	p.emit(CategoryComment, "/ "+text+" /")
}
