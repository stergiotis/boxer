package grib

import (
	"bytes"
	"errors"
	"io"
	"iter"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// indicator is the string every message opens with. Anything before it is
// framing the reader skips: GTS bulletin headers, NCEP blocking records,
// padding, junk (ADR-0292 §R4).
var indicator = []byte("GRIB")

// endMarker closes every message. It is checked, never searched for: a data
// section may legitimately end in these four bytes.
var endMarker = []byte("7777")

// grib1LargeFlag is the top bit of edition 1's 24-bit total length. When
// set, ECMWF's convention for messages past 2²³−1 bytes applies: the field
// holds the length in units of 120, and the data section's coded length
// carries the remainder (see grib1Length).
const grib1LargeFlag = 0x800000

// maxSectionLength bounds any single section. A GRIB2 section length is a
// 32-bit field; a message length a 64-bit one. Bounding both by the bytes
// actually present is what keeps a corrupt length from driving an
// allocation.
const maxMessageLength = 1 << 40

// sourceI is random access to a file's bytes. slice may alias the
// underlying storage; callers never write through the result.
type sourceI interface {
	slice(off int64, n int64) (b []byte, err error)
	size() (n int64)
}

type bytesSource struct {
	buf []byte
}

var _ sourceI = bytesSource{}

func (inst bytesSource) size() (n int64) {
	return int64(len(inst.buf))
}

func (inst bytesSource) slice(off int64, n int64) (b []byte, err error) {
	size := int64(len(inst.buf))
	if off < 0 || n < 0 || off > size || n > size-off {
		err = eb.Build().Int64("offset", off).Int64("length", n).Int64("size", size).Errorf("read past end of file: %w", ErrMalformed)
		return
	}
	b = inst.buf[off : off+n : off+n]
	return
}

type readerAtSource struct {
	r io.ReaderAt
	n int64
}

var _ sourceI = readerAtSource{}

func (inst readerAtSource) size() (n int64) {
	return inst.n
}

func (inst readerAtSource) slice(off int64, n int64) (b []byte, err error) {
	if off < 0 || n < 0 || off > inst.n || n > inst.n-off {
		err = eb.Build().Int64("offset", off).Int64("length", n).Int64("size", inst.n).Errorf("read past end of file: %w", ErrMalformed)
		return
	}
	b = make([]byte, n)
	_, err = inst.r.ReadAt(b, off)
	if err != nil && !(errors.Is(err, io.EOF) && n == 0) {
		err = eb.Build().Int64("offset", off).Int64("length", n).Errorf("read: %w", err)
		return
	}
	err = nil
	return
}

// ScanBytes yields the messages of buf in file order; see [Scan].
func ScanBytes(buf []byte) iter.Seq2[*Message, error] {
	return scan(bytesSource{buf: buf}, 0)
}

// Scan yields the messages of r in file order, starting at offset 0. Bytes
// before, between and after messages that are not GRIB are skipped and
// counted in [Message.Skipped]; a message whose declared length runs past
// the end of r, or that does not end in 7777, is yielded as an error and
// ends the scan — every message before it stands (ADR-0292 §R4). Each
// yielded message has its sections parsed and its fields located; values
// are decoded on demand. The message aliases nothing of r once yielded.
func Scan(r io.ReaderAt, size int64) iter.Seq2[*Message, error] {
	return scan(readerAtSource{r: r, n: size}, 0)
}

// ScanFrom is [Scan] starting at a byte offset, the entry point an index or
// a sidecar inventory drives (ADR-0292 §R9). The offset need not be exact:
// the indicator is searched for from there.
func ScanFrom(r io.ReaderAt, size int64, offset int64) iter.Seq2[*Message, error] {
	return scan(readerAtSource{r: r, n: size}, offset)
}

// scanWindow is how far ahead of the cursor the indicator is searched for
// in one read. Framing between messages is tens of bytes; a window this
// size costs one read per message on a clean file.
const scanWindow = 64 << 10

func scan(src sourceI, start int64) iter.Seq2[*Message, error] {
	return func(yield func(*Message, error) bool) {
		pos := start
		size := src.size()
		if pos < 0 || pos > size {
			yield(nil, eb.Build().Int64("offset", pos).Int64("size", size).Errorf("scan offset outside file: %w", ErrMalformed))
			return
		}
		for pos < size {
			// Find the next indicator.
			at, skipped, err := findIndicator(src, pos)
			if err != nil {
				yield(nil, err)
				return
			}
			if at < 0 {
				return
			}
			m, length, err := readMessage(src, at)
			if err != nil {
				yield(nil, err)
				return
			}
			m.Skipped = skipped
			if !yield(m, nil) {
				return
			}
			pos = at + length
		}
	}
}

// findIndicator returns the offset of the next "GRIB" at or after pos, or
// −1 when none remains, and how many bytes were skipped to reach it.
func findIndicator(src sourceI, pos int64) (at int64, skipped int64, err error) {
	size := src.size()
	at = -1
	for pos < size {
		n := min(int64(scanWindow), size-pos)
		var b []byte
		b, err = src.slice(pos, n)
		if err != nil {
			return
		}
		i := bytes.Index(b, indicator)
		if i >= 0 {
			at = pos + int64(i)
			skipped += int64(i)
			return
		}
		// Keep the last three bytes so an indicator split across windows is
		// still found.
		step := n - int64(len(indicator)) + 1
		if step <= 0 {
			skipped += n
			return
		}
		skipped += step
		pos += step
	}
	return
}

// readMessage reads the message whose indicator is at off and parses its
// sections. length is the number of bytes the message occupies in the
// file, which is what the scan advances by.
func readMessage(src sourceI, off int64) (m *Message, length int64, err error) {
	head, err := src.slice(off, min(16, src.size()-off))
	if err != nil {
		return
	}
	if len(head) < 8 {
		err = eb.Build().Int64("offset", off).Errorf("indicator section truncated: %w", ErrMalformed)
		return
	}
	edition := head[7]
	switch edition {
	case 1:
		length, err = grib1Length(src, off, head)
		if err != nil {
			return
		}
	case 2:
		if len(head) < 16 {
			err = eb.Build().Int64("offset", off).Errorf("indicator section truncated: %w", ErrMalformed)
			return
		}
		u := uint64(0)
		for _, c := range head[8:16] {
			u = u<<8 | uint64(c)
		}
		if u > maxMessageLength {
			err = eb.Build().Int64("offset", off).Uint64("length", u).Errorf("message length not credible: %w", ErrMalformed)
			return
		}
		length = int64(u)
	default:
		err = eb.Build().Int64("offset", off).Uint8("edition", edition).Errorf("unknown grib edition: %w", ErrMalformed)
		return
	}
	if length < 16 || length > src.size()-off {
		err = eb.Build().Int64("offset", off).Int64("length", length).Int64("available", src.size()-off).Errorf("message truncated: %w", ErrMalformed)
		return
	}
	raw, err := src.slice(off, length)
	if err != nil {
		return
	}
	if !bytes.Equal(raw[length-4:], endMarker) {
		err = eb.Build().Int64("offset", off).Int64("length", length).Str("found", string(raw[length-4:])).Errorf("message does not end in 7777: %w", ErrMalformed)
		return
	}
	// Own the bytes: a ReaderAt source hands out fresh slices, a bytes
	// source aliases the caller's buffer, and a Message outlives the scan.
	raw = bytes.Clone(raw)
	m = &Message{Offset: off, Length: length, Edition: edition, raw: raw}
	switch edition {
	case 1:
		m.grib1Large = int64(head[4])&(grib1LargeFlag>>16) != 0
		err = m.parseGrib1()
	case 2:
		err = m.parseGrib2()
	}
	if err != nil {
		m = nil
	}
	return
}

// grib1Length decodes edition 1's total length. The field is 24 bits; a
// message past 2²³−1 bytes uses ECMWF's convention: the top bit is set, the
// low 23 bits hold the length in units of 120 bytes, and the data section's
// own coded length (which is then under 120) carries what to subtract:
// total = units × 120 − codedDataLength + 4. Measured on an 11.4 MB NCEP
// sea-surface-temperature field in the survey corpus (2026-09-27), where
// the field read 8 483 560 and the file was 11 394 230 bytes.
func grib1Length(src sourceI, off int64, head []byte) (length int64, err error) {
	coded := int64(head[4])<<16 | int64(head[5])<<8 | int64(head[6])
	if coded&grib1LargeFlag == 0 {
		length = coded
		return
	}
	// Walk the section lengths to the data section.
	units := coded &^ grib1LargeFlag
	pos := off + 8
	pds, err := grib1SectionLength(src, pos)
	if err != nil {
		return
	}
	flagsB, err := src.slice(pos+7, 1)
	if err != nil {
		return
	}
	flags := flagsB[0]
	pos += pds
	if flags&0x80 != 0 {
		var gds int64
		gds, err = grib1SectionLength(src, pos)
		if err != nil {
			return
		}
		pos += gds
	}
	if flags&0x40 != 0 {
		var bms int64
		bms, err = grib1SectionLength(src, pos)
		if err != nil {
			return
		}
		pos += bms
	}
	bds, err := grib1SectionLength(src, pos)
	if err != nil {
		return
	}
	if bds >= 120 {
		err = eb.Build().Int64("offset", off).Int64("codedDataLength", bds).Errorf("large grib1 message with a data section length not under 120: %w", ErrMalformed)
		return
	}
	length = units*120 - bds + 4
	return
}

func grib1SectionLength(src sourceI, pos int64) (n int64, err error) {
	b, err := src.slice(pos, 3)
	if err != nil {
		return
	}
	n = int64(b[0])<<16 | int64(b[1])<<8 | int64(b[2])
	if n < 3 {
		err = eb.Build().Int64("offset", pos).Int64("length", n).Errorf("grib1 section length not credible: %w", ErrMalformed)
	}
	return
}
