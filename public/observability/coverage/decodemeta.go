package coverage

import (
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Format constants of the coverage meta-data file, version 1 (ADR-0169
// §SD2). The layout is toolchain-internal; this decoder is pinned to the
// documented version and refuses anything newer, and the integration-lane
// test decodes blobs emitted by the live toolchain to catch drift.
var covMetaMagic = [4]byte{0x00, 0x63, 0x76, 0x6d} // \x00 c v m
const metaFileVersion = 1
const metaFileHeaderSize = 56   // MetaFileHeader wire size
const metaSymbolHeaderSize = 44 // MetaSymbolHeader wire size

// DecodeMeta decodes the blob written by runtime/coverage.WriteMeta (equally
// a GOCOVERDIR covmeta.* file) into the once-per-build lookup model,
// assigning the profile-wide global unit index along the way.
func DecodeMeta(data []byte) (prof *covsnap.MetaProfile, err error) {
	if len(data) < metaFileHeaderSize {
		return nil, eh.Errorf("coverage meta-data blob truncated: %d bytes is shorter than the %d-byte header", len(data), metaFileHeaderSize)
	}
	r := &byteReader{b: data}
	var magic []byte
	magic, err = r.take(4)
	if err != nil {
		return
	}
	if magic[0] != covMetaMagic[0] || magic[1] != covMetaMagic[1] || magic[2] != covMetaMagic[2] || magic[3] != covMetaMagic[3] {
		return nil, eh.Errorf("not a coverage meta-data blob (bad magic)")
	}
	var version uint32
	version, err = r.u32()
	if err != nil {
		return
	}
	if version > metaFileVersion {
		return nil, eh.Errorf("coverage meta-data blob has unknown version %d (decoder is pinned to %d)", version, metaFileVersion)
	}
	var totalLength, entries uint64
	totalLength, err = r.u64()
	if err != nil {
		return
	}
	if totalLength > uint64(len(data)) {
		return nil, eh.Errorf("coverage meta-data blob truncated: header claims %d bytes, have %d", totalLength, len(data))
	}
	entries, err = r.u64()
	if err != nil {
		return
	}
	prof = &covsnap.MetaProfile{}
	var hash []byte
	hash, err = r.take(16)
	if err != nil {
		return nil, err
	}
	copy(prof.Hash[:], hash)
	// StrTabOffset, StrTabLength: the file-level string table is unused for
	// decoding (each package blob carries its own); skip the fields.
	err = r.skip(8)
	if err != nil {
		return nil, err
	}
	var mode, gran uint8
	mode, err = r.u8()
	if err != nil {
		return nil, err
	}
	gran, err = r.u8()
	if err != nil {
		return nil, err
	}
	prof.Mode = covsnap.CounterModeE(mode)
	prof.Granularity = covsnap.GranularityE(gran)
	err = r.skip(6)
	if err != nil {
		return nil, err
	}

	if entries > uint64(r.remaining())/16 {
		return nil, eh.Errorf("coverage meta-data blob corrupt: %d package entries exceed remaining %d bytes", entries, r.remaining())
	}
	pkgOffsets := make([]uint64, entries)
	pkgLengths := make([]uint64, entries)
	for i := range pkgOffsets {
		pkgOffsets[i], err = r.u64()
		if err != nil {
			return nil, err
		}
	}
	for i := range pkgLengths {
		pkgLengths[i], err = r.u64()
		if err != nil {
			return nil, err
		}
	}

	prof.Pkgs = make([]covsnap.PkgMeta, entries)
	unitBase := uint32(0)
	stmtTotal := uint32(0)
	for i := range prof.Pkgs {
		off := pkgOffsets[i]
		length := pkgLengths[i]
		if off > totalLength || length > totalLength || off+length > totalLength {
			return nil, eh.Errorf("coverage meta-data blob corrupt: package %d spans [%d,%d) beyond total length %d", i, off, off+length, totalLength)
		}
		err = decodeMetaPackage(data[off:off+length], &prof.Pkgs[i], &unitBase, &stmtTotal)
		if err != nil {
			return nil, eh.Errorf("coverage meta-data package %d: %w", i, err)
		}
	}
	prof.TotalUnits = unitBase
	prof.TotalStmts = stmtTotal
	return
}

// decodeMetaPackage decodes one package blob: symbol header, function
// offset table, string table, then per-function records.
func decodeMetaPackage(blob []byte, pkg *covsnap.PkgMeta, unitBase *uint32, stmtTotal *uint32) (err error) {
	r := &byteReader{b: blob}
	var length, pkgNameIdx, pkgPathIdx, modulePathIdx, numFuncs uint32
	length, err = r.u32()
	if err != nil {
		return
	}
	if uint64(length) > uint64(len(blob)) {
		return eh.Errorf("package blob truncated: header claims %d bytes, have %d", length, len(blob))
	}
	pkgNameIdx, err = r.u32()
	if err != nil {
		return
	}
	pkgPathIdx, err = r.u32()
	if err != nil {
		return
	}
	modulePathIdx, err = r.u32()
	if err != nil {
		return
	}
	err = r.skip(16 + 1 + 3) // per-package MetaHash, unused byte, padding
	if err != nil {
		return
	}
	_, err = r.u32() // NumFiles: derivable from the units, not retained
	if err != nil {
		return
	}
	numFuncs, err = r.u32()
	if err != nil {
		return
	}

	if uint64(numFuncs) > uint64(r.remaining())/4 {
		return eh.Errorf("package blob corrupt: %d function offsets exceed remaining %d bytes", numFuncs, r.remaining())
	}
	funcOffsets := make([]uint32, numFuncs)
	for i := range funcOffsets {
		funcOffsets[i], err = r.u32()
		if err != nil {
			return
		}
	}

	var strs []string
	strs, err = decodeStringTable(r)
	if err != nil {
		return
	}
	str := func(idx uint32) (s string, err error) {
		if int(idx) >= len(strs) {
			return "", eh.Errorf("string index %d out of range (table has %d entries)", idx, len(strs))
		}
		return strs[idx], nil
	}
	pkg.Name, err = str(pkgNameIdx)
	if err != nil {
		return
	}
	pkg.Path, err = str(pkgPathIdx)
	if err != nil {
		return
	}
	pkg.ModulePath, err = str(modulePathIdx)
	if err != nil {
		return
	}

	pkg.UnitBase = *unitBase
	pkg.Funcs = make([]covsnap.FuncMeta, numFuncs)
	for i := range pkg.Funcs {
		foff := funcOffsets[i]
		if foff < metaSymbolHeaderSize || uint64(foff) > uint64(len(blob)) {
			return eh.Errorf("malformed offset %d for function %d", foff, i)
		}
		fr := &byteReader{b: blob, off: int(foff)}
		err = decodeMetaFunc(fr, strs, &pkg.Funcs[i], *unitBase)
		if err != nil {
			return eh.Errorf("function %d: %w", i, err)
		}
		fn := &pkg.Funcs[i]
		*unitBase += uint32(len(fn.Units))
		*stmtTotal += fn.NumStmts
	}
	pkg.NumUnits = *unitBase - pkg.UnitBase
	pkg.NumStmts = 0
	for i := range pkg.Funcs {
		pkg.NumStmts += pkg.Funcs[i].NumStmts
	}
	return
}

func decodeMetaFunc(r *byteReader, strs []string, fn *covsnap.FuncMeta, unitBase uint32) (err error) {
	var numUnits, nameIdx, fileIdx uint64
	numUnits, err = r.uleb()
	if err != nil {
		return
	}
	nameIdx, err = r.uleb()
	if err != nil {
		return
	}
	fileIdx, err = r.uleb()
	if err != nil {
		return
	}
	if int(nameIdx) >= len(strs) || int(fileIdx) >= len(strs) {
		return eh.Errorf("function name/file string index out of range")
	}
	fn.Name = strs[nameIdx]
	fn.SrcFile = strs[fileIdx]
	fn.UnitBase = unitBase
	if numUnits > uint64(r.remaining()) {
		return eh.Errorf("corrupt unit count %d exceeds remaining %d bytes", numUnits, r.remaining())
	}
	fn.Units = make([]covsnap.UnitMeta, numUnits)
	for k := range fn.Units {
		u := &fn.Units[k]
		var v uint64
		for f, dst := range []*uint32{&u.StLine, &u.StCol, &u.EnLine, &u.EnCol, &u.NxStmts} {
			v, err = r.uleb()
			if err != nil {
				return eh.Errorf("unit %d field %d: %w", k, f, err)
			}
			*dst = uint32(v)
		}
		fn.NumStmts += u.NxStmts
	}
	var lit uint64
	lit, err = r.uleb()
	if err != nil {
		return
	}
	fn.Lit = lit != 0
	return
}

// decodeStringTable reads a ULEB-framed string table: entry count, then per
// entry a length and the bytes.
func decodeStringTable(r *byteReader) (strs []string, err error) {
	var n uint64
	n, err = r.uleb()
	if err != nil {
		return
	}
	if n > uint64(r.remaining()) {
		return nil, eh.Errorf("corrupt string table: %d entries exceed remaining %d bytes", n, r.remaining())
	}
	strs = make([]string, 0, n)
	for range n {
		var slen uint64
		slen, err = r.uleb()
		if err != nil {
			return nil, err
		}
		var b []byte
		b, err = r.take(int(slen))
		if err != nil {
			return nil, err
		}
		strs = append(strs, string(b))
	}
	return
}
