package aec

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrCorrupt marks a coded stream this decoder cannot follow: it ends
// before the sample count is reached, or a block's identifier or codeword
// is outside what the parameters allow.
var ErrCorrupt = errors.New("corrupt ccsds 121.0 stream")

// ErrParams marks parameters outside the standard: a block size other than
// 8, 16, 32 or 64, a resolution outside 1…32, a reference interval outside
// 1…4096, or the restricted option set at a resolution above 4.
var ErrParams = errors.New("ccsds 121.0 parameters outside the standard")

// Params are the coder's parameters, all of which the standard leaves to
// the transport to communicate (§5.3.2.2); template 5.42 carries them.
type Params struct {
	// Bits is the sample resolution n, 1 to 32.
	Bits uint8
	// BlockSize is J: 8, 16, 32 or 64 samples per block.
	BlockSize uint8
	// ReferenceInterval is r, the number of blocks between reference
	// samples, 1 to 4096.
	ReferenceInterval uint16
	// Preprocess says the unit-delay predictor and mapper were applied,
	// and so a reference sample opens every interval.
	Preprocess bool
	// Signed says samples are two's complement of Bits, which changes the
	// mapper's range; GRIB fields are unsigned.
	Signed bool
	// Restricted selects the restricted option set, legal only for Bits ≤ 4.
	Restricted bool
	// PadInterval says the coded stream is padded to an octet boundary at
	// the end of every reference sample interval — a transport option of
	// template 5.42's flags, not of the standard's coder.
	PadInterval bool
}

func (inst Params) checkE() (err error) {
	if inst.Bits < 1 || inst.Bits > 32 {
		err = eb.Build().Uint8("bits", inst.Bits).Errorf("resolution: %w", ErrParams)
		return
	}
	switch inst.BlockSize {
	case 8, 16, 32, 64:
	default:
		err = eb.Build().Uint8("blockSize", inst.BlockSize).Errorf("block size: %w", ErrParams)
		return
	}
	if inst.ReferenceInterval < 1 || inst.ReferenceInterval > 4096 {
		err = eb.Build().Uint16("rsi", inst.ReferenceInterval).Errorf("reference sample interval: %w", ErrParams)
		return
	}
	if inst.Restricted && inst.Bits > 4 {
		err = eb.Build().Uint8("bits", inst.Bits).Errorf("restricted option set above 4 bits: %w", ErrParams)
	}
	return
}

// idBits is the option identifier length for the resolution and option
// set (table 5-1): the basic set uses 3 bits up to 8, 4 up to 16, 5 up to
// 32; the restricted set 1 bit for n ≤ 2 and 2 for n ≤ 4.
func (inst Params) idBits() (n uint8) {
	switch {
	case inst.Restricted && inst.Bits <= 2:
		n = 1
	case inst.Restricted:
		n = 2
	case inst.Bits <= 8:
		n = 3
	case inst.Bits <= 16:
		n = 4
	default:
		n = 5
	}
	return
}

// segmentBlocks is the zero-block segment length (§3.5.2): a reference
// sample interval is cut into segments of 64 blocks, the last possibly
// shorter, and the remainder-of-segment codeword runs to a segment's end.
const segmentBlocks = 64

// reader pulls bits MSB-first from the coded stream; reading past the end
// latches bad.
type reader struct {
	b   []byte
	pos uint64
	bad bool
}

func (inst *reader) bit() (v uint64) {
	if inst.bad {
		return
	}
	if inst.pos >= uint64(len(inst.b))*8 {
		inst.bad = true
		return
	}
	v = uint64(inst.b[inst.pos>>3]>>(7-inst.pos&7)) & 1
	inst.pos++
	return
}

func (inst *reader) bits(n uint8) (v uint64) {
	if n == 0 || inst.bad {
		return
	}
	end := inst.pos + uint64(n)
	if end > uint64(len(inst.b))*8 {
		inst.bad = true
		return
	}
	for inst.pos < end {
		byteIdx := inst.pos >> 3
		bitOff := inst.pos & 7
		avail := 8 - bitOff
		need := end - inst.pos
		if need < avail {
			avail = need
		}
		chunk := uint64(inst.b[byteIdx]) >> (8 - bitOff - avail) & (1<<avail - 1)
		v = v<<avail | chunk
		inst.pos += avail
	}
	return
}

// fs reads one fundamental-sequence codeword: m zeros then a one, meaning
// m. A run longer than limit zeros is corrupt rather than a very large
// value, which bounds the work a damaged stream can cause.
func (inst *reader) fs(limit uint64) (m uint64) {
	for !inst.bad {
		if inst.bit() == 1 {
			return
		}
		m++
		if m > limit {
			inst.bad = true
			return
		}
	}
	return
}

// Decode decodes count samples from data under p into dst (grown as
// needed). Trailing fill bits after the last block are ignored, as the
// standard permits at the end of a packet or file (§5.3.2.1).
func Decode(dst []uint32, data []byte, count int, p Params) (samples []uint32, err error) {
	err = p.checkE()
	if err != nil {
		return
	}
	if count < 0 {
		err = eb.Build().Int("count", count).Errorf("sample count: %w", ErrParams)
		return
	}
	if cap(dst) < count {
		dst = make([]uint32, count)
	}
	samples = dst[:count]
	j := int(p.BlockSize)
	n := p.Bits
	idLen := p.idBits()
	idAllOnes := uint64(1)<<idLen - 1
	sampleMax := uint64(1)<<n - 1
	// fsLimit bounds a unary codeword: a preprocessed sample is at most
	// 2^n − 1, a second-extension symbol at most about 2^(2n+1), a zero
	// run at most 63 blocks. One generous bound for all.
	fsLimit := uint64(1)<<min(2*uint(n)+2, 40) + 64
	r := &reader{b: data}
	d := decoder{p: p, j: j, sampleMax: sampleMax}
	block := make([]uint64, j) // preprocessed samples δ of the current block
	rsi := int(p.ReferenceInterval)
	produced := 0
	blockInInterval := 0 // 0 … rsi−1
	zeroRun := 0         // all-zero blocks still owed by a zero-block CDS
	for produced < count {
		refBlock := p.Preprocess && blockInInterval == 0
		if zeroRun > 0 {
			// A zero block within a run: every δ is 0. A reference sample
			// was read with the run's first block.
			for i := range block {
				block[i] = 0
			}
			zeroRun--
		} else {
			id := r.bits(idLen)
			if r.bad {
				err = corruptE(produced, "identifier past the end of the stream")
				return
			}
			var ref uint64
			hasRef := false
			readRef := func() {
				if refBlock {
					ref = r.bits(n)
					hasRef = true
				}
			}
			switch {
			case id == 0:
				// Low-entropy options: one more bit selects.
				if r.bit() == 0 {
					// Zero block: the run length as a codeword, counted within
					// the 64-block segment; ROS runs to the segment's end.
					readRef()
					m := r.fs(fsLimit)
					if r.bad {
						err = corruptE(produced, "zero-block run codeword")
						return
					}
					segPos := blockInInterval % segmentBlocks
					segLen := min(segmentBlocks, rsi-blockInInterval+segPos)
					var run int
					switch {
					case m < 4:
						run = int(m) + 1
					case m == 4:
						run = segLen - segPos
					default:
						run = int(m)
					}
					if run < 1 || segPos+run > segLen {
						err = corruptE(produced, "zero-block run beyond its segment")
						return
					}
					zeroRun = run - 1
					for i := range block {
						block[i] = 0
					}
				} else {
					// Second extension: J/2 symbols, each a pair of samples.
					// With a reference sample a zero is inserted before the
					// J−1 samples, so the J/2 pairs cover J symbols and the
					// inserted zero lands in block[0], which the reference
					// then overwrites.
					readRef()
					for pair := 0; pair < j/2; pair++ {
						gamma := r.fs(fsLimit)
						if r.bad {
							err = corruptE(produced, "second-extension codeword")
							return
						}
						// Invert γ = (a+b)(a+b+1)/2 + b: the largest s with
						// s(s+1)/2 ≤ γ is a+b.
						s := uint64(isqrt(8*gamma+1)-1) / 2
						for s*(s+1)/2 > gamma {
							s--
						}
						for (s+1)*(s+2)/2 <= gamma {
							s++
						}
						b := gamma - s*(s+1)/2
						a := s - b
						block[2*pair] = a
						block[2*pair+1] = b
					}
				}
			case id == idAllOnes:
				// No compression: J (or J−1 after a reference) samples at n bits.
				readRef()
				start := 0
				if hasRef {
					start = 1
				}
				for i := start; i < j; i++ {
					block[i] = r.bits(n)
				}
				if r.bad {
					err = corruptE(produced, "uncoded samples past the end of the stream")
					return
				}
			default:
				// Split-sample with k = id − 1 (k = 0 is the fundamental
				// sequence): the FS codes of all samples, then k LSBs each.
				k := uint8(id - 1)
				if k >= n {
					err = corruptE(produced, "split-sample identifier beyond the resolution")
					return
				}
				readRef()
				start := 0
				if hasRef {
					start = 1
				}
				for i := start; i < j; i++ {
					block[i] = r.fs(fsLimit)
				}
				if r.bad {
					err = corruptE(produced, "fundamental-sequence codeword")
					return
				}
				for i := start; i < j; i++ {
					block[i] = block[i]<<k | r.bits(k)
				}
				if r.bad {
					err = corruptE(produced, "split bits past the end of the stream")
					return
				}
			}
			if hasRef {
				block[0] = ref
			}
			d.hasRef = hasRef
		}
		// Undo the preprocessor and emit.
		produced, err = d.emit(samples, produced, block, refBlock && d.hasRef)
		if err != nil {
			return
		}
		d.hasRef = false
		blockInInterval++
		if blockInInterval == rsi {
			blockInInterval = 0
			if p.PadInterval {
				r.pos = (r.pos + 7) &^ 7
			}
		}
	}
	return
}

func corruptE(at int, what string) (err error) {
	err = eb.Build().Int("samplesDecoded", at).Str("failure", what).Errorf("stream: %w", ErrCorrupt)
	return
}

// decoder holds the preprocessor's state across blocks.
type decoder struct {
	p         Params
	j         int
	sampleMax uint64
	prev      uint64 // last reconstructed sample x_{i−1}
	havePrev  bool
	hasRef    bool
}

// emit reconstructs one block of preprocessed samples into the output,
// inverting the mapper and the unit-delay predictor when the preprocessor
// is on. withRef says block[0] is an uncoded reference sample.
func (inst *decoder) emit(out []uint32, produced int, block []uint64, withRef bool) (next int, err error) {
	next = produced
	for i := 0; i < inst.j && next < len(out); i++ {
		var x uint64
		switch {
		case !inst.p.Preprocess:
			x = block[i]
		case withRef && i == 0:
			x = block[0]
		default:
			if !inst.havePrev {
				err = corruptE(next, "preprocessed sample before any reference sample")
				return
			}
			x, err = inst.unmap(block[i], inst.prev)
			if err != nil {
				return
			}
		}
		if x > inst.sampleMax {
			err = corruptE(next, "sample exceeds the resolution")
			return
		}
		out[next] = uint32(x)
		inst.prev = x
		inst.havePrev = true
		next++
	}
	return
}

// unmap inverts the prediction-error mapper of §4.4 for the unit-delay
// predictor: the prediction is the previous sample; θ is its distance to
// the nearer bound; δ ≤ 2θ codes ±Δ alternately (even: +Δ/2, odd:
// −(δ+1)/2), and δ > 2θ codes the errors only possible towards the farther
// bound, as θ + |Δ|.
func (inst *decoder) unmap(delta uint64, pred uint64) (x uint64, err error) {
	n := uint(inst.p.Bits)
	var xmin, xmax int64
	var predS int64
	if inst.p.Signed {
		xmin, xmax = -(int64(1) << (n - 1)), int64(1)<<(n-1)-1
		predS = int64(pred)
		if predS > xmax {
			predS -= int64(1) << n
		}
	} else {
		xmin, xmax = 0, int64(inst.sampleMax)
		predS = int64(pred)
	}
	theta := min(predS-xmin, xmax-predS)
	d := int64(delta)
	var errv int64
	switch {
	case d <= 2*theta:
		if d%2 == 0 {
			errv = d / 2
		} else {
			errv = -(d + 1) / 2
		}
	case predS-xmin < xmax-predS:
		errv = d - theta
	default:
		errv = theta - d
	}
	v := predS + errv
	if v < xmin || v > xmax {
		err = corruptE(0, "mapped prediction error leaves the sample range")
		return
	}
	if v < 0 {
		v += int64(1) << n
	}
	x = uint64(v)
	return
}

// isqrt is the integer square root, for inverting the second extension.
func isqrt(v uint64) (r uint64) {
	if v == 0 {
		return
	}
	r = uint64(1) << ((64 - leadingZeros(v)) / 2)
	for {
		nr := (r + v/r) / 2
		if nr >= r {
			// Converged; fix the last step.
			for r*r > v {
				r--
			}
			for (r+1)*(r+1) <= v {
				r++
			}
			return
		}
		r = nr
	}
}

func leadingZeros(v uint64) (n uint) {
	for v>>63 == 0 {
		v <<= 1
		n++
		if n == 64 {
			return
		}
	}
	return
}
