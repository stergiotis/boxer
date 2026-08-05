// Package covsnap holds the decoded, pure-data model of Go coverage
// meta-data and counter snapshots (ADR-0169 §SD2/§SD3). Like sysmsnap it is
// stdlib-only so consumers (bus codec, providers, applets) can import the
// types without pulling in the decoder or the runtime/coverage seam.
package covsnap

// CounterModeE mirrors the numeric covermode values of the coverage
// meta-data file format (version 1); the values are part of the on-disk
// format, not of this package.
type CounterModeE uint8

const (
	ModeInvalid CounterModeE = iota
	ModeSet
	ModeCount
	ModeAtomic
	ModeRegOnly
	ModeTestMain
)

func (m CounterModeE) String() string {
	switch m {
	case ModeSet:
		return "set"
	case ModeCount:
		return "count"
	case ModeAtomic:
		return "atomic"
	case ModeRegOnly:
		return "regonly"
	case ModeTestMain:
		return "testmain"
	}
	return "invalid"
}

// GranularityE mirrors the numeric counter-granularity values of the
// coverage meta-data file format (version 1).
type GranularityE uint8

const (
	GranularityInvalid GranularityE = iota
	GranularityPerBlock
	GranularityPerFunc
)

func (g GranularityE) String() string {
	switch g {
	case GranularityPerBlock:
		return "perblock"
	case GranularityPerFunc:
		return "perfunc"
	}
	return "invalid"
}

// UnitMeta is one coverable unit (basic block) of a function. The on-disk
// format (version 1) carries five fields per unit; the Parent field defs.go
// documents for intraline units is never emitted.
type UnitMeta struct {
	StLine  uint32
	StCol   uint32
	EnLine  uint32
	EnCol   uint32
	NxStmts uint32
}

// FuncMeta is the lookup information for one function. UnitBase is the
// function's first index in the profile-wide global unit index (ADR-0169
// §SD3): units are numbered in meta enumeration order — packages in file
// order, functions in index order, units in order — so the function owns
// [UnitBase, UnitBase+len(Units)).
type FuncMeta struct {
	Name     string
	SrcFile  string
	Lit      bool
	UnitBase uint32
	NumStmts uint32
	Units    []UnitMeta
}

// PkgMeta is the lookup information for one package. Funcs is indexed by
// the FuncIdx that counter snapshots reference.
type PkgMeta struct {
	Path       string
	Name       string
	ModulePath string
	UnitBase   uint32
	NumUnits   uint32
	NumStmts   uint32
	Funcs      []FuncMeta
}

// MetaProfile is a fully decoded coverage meta-data blob: the once-per-build
// lookup side of a coverage stream, identified by Hash. Pkgs is indexed by
// the PkgIdx that counter snapshots reference.
type MetaProfile struct {
	Hash        [16]byte
	Mode        CounterModeE
	Granularity GranularityE
	TotalUnits  uint32
	TotalStmts  uint32
	Pkgs        []PkgMeta
}

// LookupFunc resolves a (PkgIdx, FuncIdx) pair from a counter snapshot.
func (p *MetaProfile) LookupFunc(pkgIdx uint32, funcIdx uint32) (pkg *PkgMeta, fn *FuncMeta, ok bool) {
	if int(pkgIdx) >= len(p.Pkgs) {
		return nil, nil, false
	}
	pkg = &p.Pkgs[pkgIdx]
	if int(funcIdx) >= len(pkg.Funcs) {
		return nil, nil, false
	}
	fn = &pkg.Funcs[funcIdx]
	ok = true
	return
}

// FuncCounters is the counter payload of one function: Counters[i] belongs
// to the function's Units[i]. Counter emission skips never-executed
// functions, so a snapshot holds entries only for functions that ran.
type FuncCounters struct {
	PkgIdx   uint32
	FuncIdx  uint32
	Counters []uint32
}

// CounterSnapshot is a decoded counter blob: the periodic side of a
// coverage stream. MetaHash joins it to the MetaProfile of the build that
// produced it. Funcs concatenates all segments of the blob (a live
// WriteCounters snapshot always has exactly one); Args carries the first
// segment's args table (argv, GOOS, GOARCH).
type CounterSnapshot struct {
	MetaHash [16]byte
	Args     map[string]string
	Funcs    []FuncCounters
}
