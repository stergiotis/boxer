// Package imageupload is the Go-side half of the image upload-cost trial
// (../README.md): what it costs to turn stored samples into the RGBA8 pixels
// the paintImage opcode takes, which is the work a display-recipe change asks
// of the Go side before any upload. It builds nothing a viewer would ship; the
// kernels are the plainest loops that do the job, so the figures are a floor
// for unoptimised Go and not a tuned implementation.
//
// Run with:
//
//	go test -tags="$(cat ./tags)" -run '^$' -bench . -benchtime 20x -count 5 ./doc/trials/image-upload-cost/harness/
package imageupload

import (
	"fmt"
	"math"
	"sync"
	"testing"
)

// mapLUT8 maps 8-bit samples through a 256-entry table of packed RGBA.
func mapLUT8(dst []uint32, src []uint8, lut *[256]uint32) {
	for i, v := range src {
		dst[i] = lut[v]
	}
}

// mapLUT16 maps 16-bit samples through a 65536-entry table of packed RGBA:
// limits, transfer function and colour map folded into one lookup, rebuilt
// when the recipe changes.
func mapLUT16(dst []uint32, src []uint16, lut *[65536]uint32) {
	for i, v := range src {
		dst[i] = lut[v]
	}
}

// mapF32Linear windows a float sample to [lo, hi], maps it linearly onto a
// 256-entry palette and gives NaN its own colour.
func mapF32Linear(dst []uint32, src []float32, lo, hi float32, pal *[256]uint32, nan uint32) {
	scale := 255 / (hi - lo)
	for i, v := range src {
		if v != v {
			dst[i] = nan
			continue
		}
		t := (v - lo) * scale
		if t < 0 {
			t = 0
		} else if t > 255 {
			t = 255
		}
		dst[i] = pal[uint8(t)]
	}
}

// mapF32Table windows a float sample and looks the result up in a 4096-entry
// table that already holds transfer function and colour map, so the transfer
// function costs nothing per pixel.
func mapF32Table(dst []uint32, src []float32, lo, hi float32, tab *[4096]uint32, nan uint32) {
	scale := 4095 / (hi - lo)
	for i, v := range src {
		if v != v {
			dst[i] = nan
			continue
		}
		t := (v - lo) * scale
		if t < 0 {
			t = 0
		} else if t > 4095 {
			t = 4095
		}
		dst[i] = tab[uint16(t)]
	}
}

// mapF32Asinh evaluates an asinh stretch per pixel with the standard library:
// the transfer function done the direct way.
func mapF32Asinh(dst []uint32, src []float32, lo, hi float32, pal *[256]uint32, nan uint32) {
	const a = 0.1
	norm := 1 / (hi - lo)
	k := 255 / math.Asinh(1/a)
	for i, v := range src {
		if v != v {
			dst[i] = nan
			continue
		}
		t := float64((v - lo) * norm)
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		dst[i] = pal[uint8(math.Asinh(t/a)*k)]
	}
}

// rows splits n pixels into workers contiguous runs and maps them in parallel.
func rows(n, workers int, f func(lo, hi int)) {
	if workers <= 1 {
		f(0, n)
		return
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo, hi := n*w/workers, n*(w+1)/workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			f(lo, hi)
		}()
	}
	wg.Wait()
}

// xorshift is a fixed-seed generator: the sources are the same in every run.
type xorshift uint64

func (x *xorshift) next() uint64 {
	v := uint64(*x)
	v ^= v << 13
	v ^= v >> 7
	v ^= v << 17
	*x = xorshift(v)
	return v
}

// The two source shapes. Noise spreads lookups over the whole table, the worst
// case for its cache footprint; smooth is a ramp with a little noise, nearer
// to what an image is.
func source16(n, width int, smooth bool) []uint16 {
	g := xorshift(0x9e3779b97f4a7c15)
	out := make([]uint16, n)
	for i := range out {
		r := g.next()
		if smooth {
			out[i] = uint16((i%width)*65535/width/2 + int(r&0xff))
		} else {
			out[i] = uint16(r)
		}
	}
	return out
}

func sourceF32(n int) []float32 {
	g := xorshift(0x9e3779b97f4a7c15)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(g.next()&0xffff) / 65535 * 1.2 // a fifth of it clips high
	}
	return out
}

func palette() *[256]uint32 {
	var p [256]uint32
	for i := range p {
		p[i] = uint32(i)<<24 | uint32(255-i)<<16 | uint32(i/2)<<8 | 0xff
	}
	return &p
}

var benchSizes = []struct {
	name string
	w, h int
}{
	{"tile512", 512, 512},
	{"viewport4k", 3840, 2160},
}

var benchWorkers = []int{1, 4, 16}

func BenchmarkMap(b *testing.B) {
	pal := palette()
	var lut8 [256]uint32
	copy(lut8[:], pal[:])
	lut16 := new([65536]uint32)
	for i := range lut16 {
		lut16[i] = pal[i>>8]
	}
	tab := new([4096]uint32)
	for i := range tab {
		tab[i] = pal[i>>4]
	}
	const nan = 0xff00ffff
	for _, sz := range benchSizes {
		n := sz.w * sz.h
		dst := make([]uint32, n)
		src8 := make([]uint8, n)
		for i, v := range source16(n, sz.w, false) {
			src8[i] = uint8(v)
		}
		noise16 := source16(n, sz.w, false)
		smooth16 := source16(n, sz.w, true)
		f32 := sourceF32(n)
		kernels := []struct {
			name string
			f    func(lo, hi int)
		}{
			{"lut8", func(lo, hi int) { mapLUT8(dst[lo:hi], src8[lo:hi], &lut8) }},
			{"lut16-noise", func(lo, hi int) { mapLUT16(dst[lo:hi], noise16[lo:hi], lut16) }},
			{"lut16-smooth", func(lo, hi int) { mapLUT16(dst[lo:hi], smooth16[lo:hi], lut16) }},
			{"f32-linear", func(lo, hi int) { mapF32Linear(dst[lo:hi], f32[lo:hi], 0, 1, pal, nan) }},
			{"f32-table4096", func(lo, hi int) { mapF32Table(dst[lo:hi], f32[lo:hi], 0, 1, tab, nan) }},
			{"f32-asinh", func(lo, hi int) { mapF32Asinh(dst[lo:hi], f32[lo:hi], 0, 1, pal, nan) }},
		}
		for _, k := range kernels {
			for _, workers := range benchWorkers {
				b.Run(fmt.Sprintf("%s/%s/workers=%d", sz.name, k.name, workers), func(b *testing.B) {
					b.SetBytes(int64(4 * n)) // RGBA8 bytes produced
					for b.Loop() {
						rows(n, workers, k.f)
					}
				})
			}
		}
	}
}

// TestKernelsAgree pins what the benchmark measures: the table kernels give
// the colour the direct evaluation gives, NaN gets its own colour, and the
// window clamps.
func TestKernelsAgree(t *testing.T) {
	pal := palette()
	tab := new([4096]uint32)
	for i := range tab {
		tab[i] = pal[i>>4]
	}
	const nan = 0xff00ffff
	src := []float32{-1, 0, 0.25, 0.5, 1, 2, float32(math.NaN())}
	lin := make([]uint32, len(src))
	tabd := make([]uint32, len(src))
	mapF32Linear(lin, src, 0, 1, pal, nan)
	mapF32Table(tabd, src, 0, 1, tab, nan)
	for i := range src {
		if lin[i] != tabd[i] {
			t.Errorf("sample %v: linear %#x, table %#x", src[i], lin[i], tabd[i])
		}
	}
	if lin[0] != pal[0] || lin[5] != pal[255] || lin[6] != nan {
		t.Errorf("clamp or NaN: %#x %#x %#x", lin[0], lin[5], lin[6])
	}
	lut16 := new([65536]uint32)
	for i := range lut16 {
		lut16[i] = pal[i>>8]
	}
	s16 := []uint16{0, 255, 256, 32768, 65535}
	d16 := make([]uint32, len(s16))
	mapLUT16(d16, s16, lut16)
	for i, v := range s16 {
		if d16[i] != pal[v>>8] {
			t.Errorf("lut16 sample %d: %#x", v, d16[i])
		}
	}
}
