package fits

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"testing"
)

// A FITS writer for tests, card by card, so a test knows exactly what the
// file holds. It is written from the standard as the reader is; the
// fixtures under testdata, written by astropy, are what keeps the two from
// sharing a misunderstanding.

type kv struct{ k, v string }

// rawCard is an 80-column card from a keyword and a value field as it is
// to appear after "= ", or a whole card when v is empty and k is longer
// than 8.
func rawCard(k, v string) string {
	var s string
	switch {
	case v == "" && len(k) > 8:
		s = k
	case v == "":
		s = k
	default:
		s = padRight(k, 8) + "= " + v
	}
	if len(s) > 80 {
		panic("card longer than 80 columns: " + s)
	}
	return padRight(s, 80)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func num(v float64) string  { return strconv.FormatFloat(v, 'G', -1, 64) }
func inum(v int64) string   { return strconv.FormatInt(v, 10) }

// testHDU is one HDU to write: the mandatory keywords come from bitpix,
// axes and ext; cards follow them; data is the data unit, unpadded.
type testHDU struct {
	ext    string // "" for the primary HDU, else the XTENSION value
	bitpix int
	axes   []int64
	cards  []kv
	data   []byte
	pcount int64
}

func (inst testHDU) header() string {
	var b strings.Builder
	if inst.ext == "" {
		b.WriteString(rawCard("SIMPLE", "T"))
	} else {
		b.WriteString(rawCard("XTENSION", quote(inst.ext)))
	}
	b.WriteString(rawCard("BITPIX", inum(int64(inst.bitpix))))
	b.WriteString(rawCard("NAXIS", inum(int64(len(inst.axes)))))
	for i, a := range inst.axes {
		b.WriteString(rawCard("NAXIS"+strconv.Itoa(i+1), inum(a)))
	}
	if inst.ext != "" {
		b.WriteString(rawCard("PCOUNT", inum(inst.pcount)))
		b.WriteString(rawCard("GCOUNT", "1"))
	}
	for _, c := range inst.cards {
		b.WriteString(rawCard(c.k, c.v))
	}
	b.WriteString(rawCard("END", ""))
	return b.String()
}

func writeFITS(hdus ...testHDU) []byte {
	var buf bytes.Buffer
	for _, h := range hdus {
		buf.WriteString(h.header())
		pad(&buf, ' ')
		buf.Write(h.data)
		pad(&buf, 0)
	}
	return buf.Bytes()
}

func pad(buf *bytes.Buffer, with byte) {
	if r := buf.Len() % blockLen; r != 0 {
		buf.Write(bytes.Repeat([]byte{with}, blockLen-r))
	}
}

// sampleAt is the test's asymmetric sample at a 0-based FITS index
// (NAXIS1 first): distinct at every index for every kind.
func sampleAt(bitpix int, idx []int64, axes []int64) float64 {
	flat := int64(0)
	for i := len(idx) - 1; i >= 0; i-- {
		flat = flat*axes[i] + idx[i]
	}
	switch bitpix {
	case 8:
		return float64(flat*7%251 + 1)
	case 16:
		return float64(flat*977%65000 - 32000)
	case 32:
		return float64(flat*1_000_003 - 2_000_000_000)
	case -32:
		return float64(float32(float64(flat)*0.375 - 11.5))
	case -64:
		return float64(flat)*1.0000001e-3 - 1e9
	}
	return float64(flat)
}

// dataOf encodes the samples of sampleAt over every index, NAXIS1 fastest,
// big-endian.
func dataOf(bitpix int, axes []int64, at func(idx []int64) float64) []byte {
	n := int64(1)
	for _, a := range axes {
		n *= a
	}
	bpp := abs(bitpix) / 8
	out := make([]byte, 0, n*int64(bpp))
	idx := make([]int64, len(axes))
	for range n {
		v := at(idx)
		switch bitpix {
		case 8:
			out = append(out, byte(v))
		case 16:
			out = binary.BigEndian.AppendUint16(out, uint16(int16(v)))
		case 32:
			out = binary.BigEndian.AppendUint32(out, uint32(int32(v)))
		case 64:
			out = binary.BigEndian.AppendUint64(out, uint64(int64(v)))
		case -32:
			out = binary.BigEndian.AppendUint32(out, math.Float32bits(float32(v)))
		case -64:
			out = binary.BigEndian.AppendUint64(out, math.Float64bits(v))
		}
		for i := range idx {
			idx[i]++
			if idx[i] < axes[i] {
				break
			}
			idx[i] = 0
		}
	}
	return out
}

func rampHDU(bitpix int, axes []int64, cards ...kv) testHDU {
	return testHDU{bitpix: bitpix, axes: axes, cards: cards,
		data: dataOf(bitpix, axes, func(idx []int64) float64 { return sampleAt(bitpix, idx, axes) })}
}

func TestWriterCardsAre80Columns(t *testing.T) {
	b := writeFITS(rampHDU(16, []int64{3, 2}))
	if len(b)%blockLen != 0 {
		t.Fatalf("file of %d bytes is not whole blocks", len(b))
	}
}
