package coverage

// Fuzz targets for the coverage blob decoders. Neither format has an encoder
// in this package, so the laws are totality and bounded cost rather than a
// round trip:
//
//	FuzzDecodeMeta     — DecodeMeta never panics; the bytes it allocates stay
//	                     within a constant factor of the input; an accepted
//	                     profile numbers its units contiguously from zero,
//	                     sums them into its totals, and holds no more units
//	                     than the input has bytes (each unit costs five).
//	FuzzDecodeCounters — DecodeCounters never panics; the bytes it allocates
//	                     stay within a constant factor of the input; an
//	                     accepted snapshot holds no more function records or
//	                     counters than the input has bytes.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzDecodeMeta -fuzztime 60s ./public/observability/coverage/

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fuzzAllocFactor bounds allocation per input byte. The largest legitimate
// expansion is a unit record: five ULEB bytes decode into a 20-byte UnitMeta.
const fuzzAllocFactor = 64

// fuzzAllocSlack absorbs the fixed cost of error construction and the
// decoders' small bookkeeping allocations.
const fuzzAllocSlack = 1 << 20

func addFixtureSeeds(f *testing.F, name string) {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	// The truncation and bit-flip neighbourhood the unit test sweeps
	// exhaustively; a sample is enough to steer the fuzzer toward it.
	for _, cut := range []int{0, 16, 32, len(data) / 2, len(data) - 1} {
		f.Add(append([]byte(nil), data[:cut]...))
	}
	for _, i := range []int{8, 24, 40, len(data) / 2, len(data) - 8} {
		flipped := append([]byte(nil), data...)
		flipped[i] ^= 0xff
		f.Add(flipped)
	}
}

// allocatedBy reports the bytes the heap handed out while fn ran.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func FuzzDecodeMeta(f *testing.F) {
	addFixtureSeeds(f, "covmeta.bin")
	f.Add(syntheticMeta(1, 1, 3, 0))
	f.Add(syntheticMeta(1, 3, 3, 0))

	f.Fuzz(func(t *testing.T, data []byte) {
		var err error
		alloc := allocatedBy(func() {
			_, err = DecodeMeta(data)
		})
		if limit := uint64(fuzzAllocFactor*len(data) + fuzzAllocSlack); alloc > limit {
			t.Fatalf("DecodeMeta allocated %d bytes for a %d-byte input (limit %d)", alloc, len(data), limit)
		}
		if err != nil {
			return // rejection is fine; panics and blow-ups are not
		}
		prof, _ := DecodeMeta(data)
		if uint64(prof.TotalUnits) > uint64(len(data)) {
			t.Fatalf("%d units decoded from %d bytes", prof.TotalUnits, len(data))
		}
		next := uint32(0)
		stmts := uint32(0)
		for i := range prof.Pkgs {
			pkg := &prof.Pkgs[i]
			if pkg.UnitBase != next {
				t.Fatalf("package %d: unit base %d, want %d", i, pkg.UnitBase, next)
			}
			for j := range pkg.Funcs {
				fn := &pkg.Funcs[j]
				if fn.UnitBase != next {
					t.Fatalf("package %d function %d: unit base %d, want %d", i, j, fn.UnitBase, next)
				}
				next += uint32(len(fn.Units))
				stmts += fn.NumStmts
			}
			if pkg.NumUnits != next-pkg.UnitBase {
				t.Fatalf("package %d: NumUnits %d, want %d", i, pkg.NumUnits, next-pkg.UnitBase)
			}
		}
		if prof.TotalUnits != next || prof.TotalStmts != stmts {
			t.Fatalf("totals (%d units, %d stmts), want (%d, %d)", prof.TotalUnits, prof.TotalStmts, next, stmts)
		}
	})
}

func FuzzDecodeCounters(f *testing.F) {
	addFixtureSeeds(f, "covcounters.bin")

	f.Fuzz(func(t *testing.T, data []byte) {
		var err error
		alloc := allocatedBy(func() {
			_, err = DecodeCounters(data)
		})
		if limit := uint64(fuzzAllocFactor*len(data) + fuzzAllocSlack); alloc > limit {
			t.Fatalf("DecodeCounters allocated %d bytes for a %d-byte input (limit %d)", alloc, len(data), limit)
		}
		if err != nil {
			return // rejection is fine; panics and blow-ups are not
		}
		snap, _ := DecodeCounters(data)
		if len(snap.Funcs) > len(data) {
			t.Fatalf("%d function records decoded from %d bytes", len(snap.Funcs), len(data))
		}
		counters := 0
		for _, fc := range snap.Funcs {
			counters += len(fc.Counters)
		}
		if counters > len(data) {
			t.Fatalf("%d counters decoded from %d bytes", counters, len(data))
		}
	})
}

// syntheticMeta builds a meta blob of pkgs package entries that all span
// one package blob, whose funcs function offsets all name one record of units
// units, with the record's name at string index nameIdx. The toolchain writes
// neither shared spans nor shared records; syntheticMeta(1, 1, n, 0) is the
// well-formed shape.
func syntheticMeta(pkgs int, funcs int, units int, nameIdx uint64) []byte {
	le := binary.LittleEndian
	pkg := make([]byte, metaSymbolHeaderSize)
	le.PutUint32(pkg[36:40], 1)
	le.PutUint32(pkg[40:44], uint32(funcs))
	recOff := uint32(metaSymbolHeaderSize + 4*funcs + 3)
	for range funcs {
		pkg = le.AppendUint32(pkg, recOff)
	}
	pkg = append(pkg, 1, 1, 'a') // string table: one entry, "a"
	pkg = binary.AppendUvarint(pkg, uint64(units))
	pkg = binary.AppendUvarint(pkg, nameIdx)
	pkg = append(pkg, 0) // file "a"
	pkg = append(pkg, make([]byte, 5*units+1)...)
	le.PutUint32(pkg[0:4], uint32(len(pkg)))

	hdr := make([]byte, metaFileHeaderSize)
	copy(hdr, covMetaMagic[:])
	le.PutUint32(hdr[4:8], metaFileVersion)
	le.PutUint64(hdr[16:24], uint64(pkgs))
	pkgOff := uint64(metaFileHeaderSize + 16*pkgs)
	for range pkgs {
		hdr = le.AppendUint64(hdr, pkgOff)
	}
	for range pkgs {
		hdr = le.AppendUint64(hdr, uint64(len(pkg)))
	}
	out := append(hdr, pkg...)
	le.PutUint64(out[8:16], uint64(len(out)))
	return out
}
