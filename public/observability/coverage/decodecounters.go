package coverage

import (
	"encoding/binary"

	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Format constants of the coverage counter-data file, version 1 (ADR-0169
// §SD2); same pinning policy as the meta decoder.
var covCounterMagic = [4]byte{0x00, 0x63, 0x77, 0x6d} // \x00 c w m
const counterFileVersion = 1
const counterFileFooterSize = 16

// Counter flavors of the on-disk format.
const (
	ctrFlavorRaw     = 1 // fixed u32 values, endianness per header flag
	ctrFlavorULeb128 = 2
)

// DecodeCounters decodes the blob written by runtime/coverage.WriteCounters
// (equally a GOCOVERDIR covcounters.* file) into the periodic snapshot
// model. The blob carries entries only for functions that executed;
// Counters[i] pairs with the function's Units[i] from the MetaProfile whose
// Hash equals MetaHash.
func DecodeCounters(data []byte) (snap *covsnap.CounterSnapshot, err error) {
	r := &byteReader{b: data}
	var magic []byte
	magic, err = r.take(4)
	if err != nil {
		return
	}
	if !counterMagicOk(magic) {
		return nil, eh.Errorf("not a coverage counter blob (bad magic)")
	}
	var version uint32
	version, err = r.u32()
	if err != nil {
		return
	}
	if version > counterFileVersion {
		return nil, eh.Errorf("coverage counter blob has unknown version %d (decoder is pinned to %d)", version, counterFileVersion)
	}
	snap = &covsnap.CounterSnapshot{}
	var hash []byte
	hash, err = r.take(16)
	if err != nil {
		return nil, err
	}
	copy(snap.MetaHash[:], hash)
	var flavor, bigEndian uint8
	flavor, err = r.u8()
	if err != nil {
		return nil, err
	}
	bigEndian, err = r.u8()
	if err != nil {
		return nil, err
	}
	err = r.skip(6)
	if err != nil {
		return nil, err
	}

	// The footer carries the segment count.
	if len(data) < counterFileFooterSize {
		return nil, eh.Errorf("coverage counter blob truncated: no footer")
	}
	ftr := data[len(data)-counterFileFooterSize:]
	if !counterMagicOk(ftr[0:4]) {
		return nil, eh.Errorf("coverage counter blob corrupt: bad footer magic")
	}
	numSegments := binary.LittleEndian.Uint32(ftr[8:12])
	if numSegments == 0 {
		return nil, eh.Errorf("coverage counter blob corrupt: zero segments")
	}

	for seg := range numSegments {
		if seg > 0 {
			// Each appended segment is preceded by the prior segment's
			// footer copy; skip it.
			err = r.skip(counterFileFooterSize)
			if err != nil {
				return nil, err
			}
		}
		err = decodeCounterSegment(r, flavor, bigEndian != 0, seg == 0, snap)
		if err != nil {
			return nil, eb.Build().Uint32("seg", seg).Errorf("coverage counter segment: %w", err)
		}
	}
	return
}

func counterMagicOk(b []byte) bool {
	return b[0] == covCounterMagic[0] && b[1] == covCounterMagic[1] && b[2] == covCounterMagic[2] && b[3] == covCounterMagic[3]
}

func decodeCounterSegment(r *byteReader, flavor uint8, bigEndian bool, keepArgs bool, snap *covsnap.CounterSnapshot) (err error) {
	var fcnEntries uint64
	var strTabLen, argsLen uint32
	fcnEntries, err = r.u64()
	if err != nil {
		return
	}
	strTabLen, err = r.u32()
	if err != nil {
		return
	}
	argsLen, err = r.u32()
	if err != nil {
		return
	}

	var strTab, argsTab []byte
	strTab, err = r.take(int(strTabLen))
	if err != nil {
		return
	}
	argsTab, err = r.take(int(argsLen))
	if err != nil {
		return
	}
	err = r.padTo4()
	if err != nil {
		return
	}

	if keepArgs {
		snap.Args, err = decodeArgsTable(strTab, argsTab)
		if err != nil {
			return
		}
	}

	// Each function record is at least three values; guard against a corrupt
	// count before growing the slice from it.
	if fcnEntries > uint64(r.remaining()) {
		return eh.Errorf("corrupt function count %d exceeds remaining %d bytes", fcnEntries, r.remaining())
	}
	readVal := func() (v uint32, err error) {
		switch flavor {
		case ctrFlavorULeb128:
			var u uint64
			u, err = r.uleb()
			v = uint32(u)
			return
		case ctrFlavorRaw:
			var b []byte
			b, err = r.take(4)
			if err != nil {
				return
			}
			if bigEndian {
				v = binary.BigEndian.Uint32(b)
			} else {
				v = binary.LittleEndian.Uint32(b)
			}
			return
		}
		return 0, eb.Build().Uint8("flavor", flavor).Errorf("unknown counter flavor")
	}
	for i := range fcnEntries {
		var numCtrs, pkgIdx, funcIdx uint32
		numCtrs, err = readVal()
		if err != nil {
			return
		}
		pkgIdx, err = readVal()
		if err != nil {
			return
		}
		funcIdx, err = readVal()
		if err != nil {
			return
		}
		if uint64(numCtrs) > uint64(r.remaining()) {
			return eh.Errorf("function record %d: corrupt counter count %d exceeds remaining %d bytes", i, numCtrs, r.remaining())
		}
		fc := covsnap.FuncCounters{
			PkgIdx:   pkgIdx,
			FuncIdx:  funcIdx,
			Counters: make([]uint32, numCtrs),
		}
		for k := range fc.Counters {
			fc.Counters[k], err = readVal()
			if err != nil {
				return
			}
		}
		snap.Funcs = append(snap.Funcs, fc)
	}
	return
}

// decodeArgsTable resolves the args key/value pairs (string-table indexes)
// of a counter segment: argc/argvN plus GOOS/GOARCH.
func decodeArgsTable(strTab []byte, argsTab []byte) (args map[string]string, err error) {
	var strs []string
	strs, err = decodeStringTable(&byteReader{b: strTab})
	if err != nil {
		return
	}
	ar := &byteReader{b: argsTab}
	var n uint64
	n, err = ar.uleb()
	if err != nil {
		return
	}
	get := func() (s string, err error) {
		var idx uint64
		idx, err = ar.uleb()
		if err != nil {
			return
		}
		if int(idx) >= len(strs) {
			return "", eh.Errorf("args table string index %d out of range (table has %d entries)", idx, len(strs))
		}
		return strs[idx], nil
	}
	args = make(map[string]string, n)
	for range n {
		var k, v string
		k, err = get()
		if err != nil {
			return nil, err
		}
		v, err = get()
		if err != nil {
			return nil, err
		}
		args[k] = v
	}
	return
}
