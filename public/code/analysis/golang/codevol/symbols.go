package codevol

import (
	"debug/elf"
	"os"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// PackageSymbols is one package's contribution to the linked binary, as the
// linker left it after dead-code elimination.
type PackageSymbols struct {
	PkgPath    string
	ModulePath string
	Party      Party
	NumSymbols int
	// TextBytes is machine code. DataBytes is everything else the symbol
	// table sizes — read-only data, initialised data, BSS.
	//
	// They are separate because conflating them is actively misleading: in
	// the boxer binary crypto/internal/fips140/drbg.memory is a single 32 MiB
	// zero-filled buffer, 42% of all sized bytes and not code at all. Summing
	// the two makes the standard library look like half the program.
	TextBytes uint64
	DataBytes uint64
}

// SymbolReport is one reading of the binary's own symbol table.
type SymbolReport struct {
	Packages  []PackageSymbols
	TotalText uint64
	TotalData uint64
	// Unattributed counts sized symbols that carry no derivable package —
	// linker-synthesised itabs and type descriptors, mostly.
	Unattributed int
	// ModuleExact records whether module attribution came from the in-binary
	// module list (true) or was left unresolved (false). Package attribution
	// is always heuristic; see [PackageOfSymbol].
	ModuleExact bool
}

// ReadSelfSymbols reads the running executable's symbol table and rolls it up
// per package.
//
// This is the "what actually shipped" lens, and unlike a call-graph analysis
// it is a fact rather than an approximation: the linker already decided, and
// this reads the decision. On the boxer binary it costs about 30 ms and 38 MB
// for 122k symbols, and its text total agrees with `go tool nm` to within
// 0.01%.
//
// idx may be nil, in which case module attribution is skipped and every row
// reports the stdlib party.
func ReadSelfSymbols(idx *ModuleIndex) (rep SymbolReport, err error) {
	var exe string
	exe, err = os.Executable()
	if err != nil {
		err = eh.Errorf("cannot locate own executable: %w", err)
		return
	}
	return readSymbolsFile(exe, idx)
}

// readSymbolsFile is ReadSelfSymbols' testable core: tests point it at a
// binary they built rather than at the test runner itself.
func readSymbolsFile(path string, idx *ModuleIndex) (rep SymbolReport, err error) {
	var f *elf.File
	// Non-ELF platforms (Mach-O, PE) fail here. That is a clean, reported
	// failure rather than a wrong answer, and the caller degrades to an empty
	// table — the keelson convention is empty-not-absent.
	f, err = elf.Open(path)
	if err != nil {
		err = eh.Errorf("cannot read own symbol table (non-ELF platform, or stripped binary): %w", err)
		return
	}
	defer func() { _ = f.Close() }()

	var syms []elf.Symbol
	syms, err = f.Symbols()
	if err != nil {
		err = eh.Errorf("binary carries no symbol table (built with -ldflags=-s?): %w", err)
		return
	}

	// Precompute which section indices are executable so text/data can be
	// told apart without re-checking flags per symbol.
	execSection := make([]bool, len(f.Sections))
	for i, s := range f.Sections {
		execSection[i] = s.Flags&elf.SHF_EXECINSTR != 0
	}

	acc := make(map[string]*PackageSymbols, 2048)
	rep.ModuleExact = idx != nil
	for i := range syms {
		s := &syms[i]
		if s.Size == 0 {
			continue
		}
		isText := int(s.Section) < len(execSection) && execSection[s.Section]
		if isText {
			rep.TotalText += s.Size
		} else {
			rep.TotalData += s.Size
		}
		pkg := PackageOfSymbol(s.Name)
		if pkg == "" {
			rep.Unattributed++
			continue
		}
		p := acc[pkg]
		if p == nil {
			mod, party := idx.Lookup(pkg)
			p = &PackageSymbols{PkgPath: pkg, ModulePath: mod, Party: party}
			acc[pkg] = p
		}
		p.NumSymbols++
		if isText {
			p.TextBytes += s.Size
		} else {
			p.DataBytes += s.Size
		}
	}

	rep.Packages = make([]PackageSymbols, 0, len(acc))
	for _, p := range acc {
		rep.Packages = append(rep.Packages, *p)
	}
	sort.Slice(rep.Packages, func(i, j int) bool {
		if rep.Packages[i].TextBytes != rep.Packages[j].TextBytes {
			return rep.Packages[i].TextBytes > rep.Packages[j].TextBytes
		}
		return rep.Packages[i].PkgPath < rep.Packages[j].PkgPath
	})
	return
}

// linkerPrefixes are synthesised symbol prefixes that wrap a real package's
// name. Stripping them attributes the symbol to the package it describes.
// Interface-table symbols are deliberately absent from this list: such a
// symbol names two types, so attributing it would mean picking one.
var linkerPrefixes = []string{
	"type:.eq.", "type:.hash.", "type:", "go:info.", "go:string.", "go:func.",
}

// PackageOfSymbol derives an import path from a Go linker symbol name.
//
// The rule is "everything up to the first '.' after the last '/'", which is
// exact for ordinary function and method symbols and approximate for the
// rest: generic instantiations and some synthesised symbols yield keys that
// are not real import paths. Module attribution does not depend on this
// precision (a longest-prefix module match tolerates a too-long key), which
// is why the party split stays exact while the package grain does not.
//
// Returns "" when no package can be derived.
func PackageOfSymbol(name string) (pkg string) {
	s := name
	for _, p := range linkerPrefixes {
		s = strings.TrimPrefix(s, p)
	}
	if strings.HasPrefix(s, "go:itab.") {
		return ""
	}
	// A generic instantiation carries its type arguments in brackets; they
	// can contain both '/' and '.', so cut them before applying the rule.
	if i := strings.IndexByte(s, '['); i >= 0 {
		s = s[:i]
	}
	slash := strings.LastIndexByte(s, '/')
	dot := strings.IndexByte(s[slash+1:], '.')
	if dot < 0 {
		return ""
	}
	return s[:slash+1+dot]
}
