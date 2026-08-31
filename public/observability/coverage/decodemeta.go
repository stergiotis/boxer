package coverage

import (
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
		return nil, eb.Build().Int("size", len(data)).Int("headerSize", metaFileHeaderSize).Errorf("coverage meta-data blob is shorter than its header")
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
		return nil, eb.Build().Uint32("version", version).Uint32("pinnedVersion", metaFileVersion).Errorf("coverage meta-data blob has an unknown version")
	}
	var totalLength, entries uint64
	totalLength, err = r.u64()
	if err != nil {
		return
	}
	if totalLength > uint64(len(data)) {
		return nil, eb.Build().Uint64("claimed", totalLength).Int("have", len(data)).Errorf("coverage meta-data blob is shorter than its header claims")
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
		return nil, eb.Build().Uint64("entries", entries).Int("remaining", r.remaining()).Errorf("coverage meta-data blob corrupt: package entries exceed the bytes remaining")
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
			return nil, eb.Build().Int("package", i).Uint64("start", off).Uint64("end", off+length).Uint64("totalLength", totalLength).Errorf("coverage meta-data blob corrupt: package spans beyond the total length")
		}
		err = decodeMetaPackage(data[off:off+length], &prof.Pkgs[i], &unitBase, &stmtTotal)
		if err != nil {
			return nil, eb.Build().Int("package", i).Errorf("coverage meta-data package: %w", err)
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
		return eb.Build().Uint32("claimed", length).Int("have", len(blob)).Errorf("package blob is shorter than its header claims")
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
		return eb.Build().Uint32("functionOffsets", numFuncs).Int("remaining", r.remaining()).Errorf("package blob corrupt: function offsets exceed the bytes remaining")
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
			return "", eb.Build().Uint32("index", idx).Int("entries", len(strs)).Errorf("string index is out of range")
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
			return eb.Build().Uint32("offset", foff).Int("function", i).Errorf("malformed function offset")
		}
		fr := &byteReader{b: blob, off: int(foff)}
		err = decodeMetaFunc(fr, strs, &pkg.Funcs[i], *unitBase)
		if err != nil {
			return eb.Build().Int("function", i).Errorf("decode function: %w", err)
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
		return eb.Build().Uint64("unitCount", numUnits).Int("remaining", r.remaining()).Errorf("corrupt unit count exceeds the bytes remaining")
	}
	fn.Units = make([]covsnap.UnitMeta, numUnits)
	for k := range fn.Units {
		u := &fn.Units[k]
		var v uint64
		for f, dst := range []*uint32{&u.StLine, &u.StCol, &u.EnLine, &u.EnCol, &u.NxStmts} {
			v, err = r.uleb()
			if err != nil {
				return eb.Build().Int("unit", k).Int("field", f).Errorf("decode unit field: %w", err)
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
		return nil, eb.Build().Uint64("entries", n).Int("remaining", r.remaining()).Errorf("corrupt string table: entries exceed the bytes remaining")
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
