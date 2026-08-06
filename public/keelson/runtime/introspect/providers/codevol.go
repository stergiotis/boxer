// codevol — what this binary is made of, and how much of it is somebody
// else's (ADR-0173 §SD1/§SD2).
//
// These two tables are the cheap tiers: they read the running binary itself,
// so unlike keelson('go_packages') they need no Go toolchain, no source tree
// and no module cache, and they belong in the static provider set that an
// appliance build carries. keelson('go_packages') answers the same question
// from source and is registered only in dev compositions; the two join on the
// import path.

package providers

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/code/analysis/golang/codevol"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// CodevolSourceI is the providers' view of the binary being described.
// Production passes the self-reading source below; tests pass a fixture,
// which is the only way to exercise these tables in the default lane — see
// selfCodevolSource for why.
type CodevolSourceI interface {
	Get() (mods []codevol.ModuleInfo, rep codevol.SymbolReport)
}

// selfCodevolSource reads the running binary, once.
//
// A caveat that shapes every test in this area: `go test` links the binary it
// runs **without a symbol table** (`go test -c` does not), so under the
// default lane this source always yields an empty SymbolReport. A `go build`
// binary carries the table and the tables fill. Assertions about real symbol
// data therefore belong to the integration lane or to a fixture source.
type selfCodevolSource struct {
	once    sync.Once
	mods    []codevol.ModuleInfo
	symbols codevol.SymbolReport
}

var codevolShared selfCodevolSource

// Get reads the binary on first call and caches it. The binary cannot change
// under a running process, so the ~30 ms symbol read is paid once rather than
// per query.
func (c *selfCodevolSource) Get() (mods []codevol.ModuleInfo, rep codevol.SymbolReport) {
	c.once.Do(func() {
		var ok bool
		c.mods, ok = codevol.Modules()
		if !ok {
			// No build info at all. Rare (some non-module builds); both
			// tables degrade to empty rather than erroring.
			log.Info().Msg("codevol: binary carries no build info; go_modules and go_symbols are empty")
			return
		}
		var err error
		c.symbols, err = codevol.ReadSelfSymbols(codevol.NewModuleIndex(c.mods))
		if err != nil {
			// A stripped binary or a non-ELF platform. go_modules still
			// answers; go_symbols is empty, and this line is the reason.
			log.Info().Err(err).Msg("codevol: no readable symbol table; go_symbols is empty")
		}
	})
	return c.mods, c.symbols
}

// RegisterCodevol registers the two self-inspection tables into r. A nil src
// means "describe the binary I am running in", which is what every host
// wants; RegisterStatic passes nil. Tests pass a fixture.
func RegisterCodevol(r *introspect.Registry, src CodevolSourceI) (err error) {
	if src == nil {
		src = &codevolShared
	}
	if err = r.Register(goModulesProvider{src: src}); err != nil {
		return
	}
	return r.Register(goSymbolsProvider{src: src})
}

// --- go_modules --------------------------------------------------------------

// goModulesProvider exposes the module list the linker recorded in this
// binary. It is the floor of the ADR-0173 tiering: free, and available
// wherever the process runs.
//
// Note for readers of a small result: a `go test` binary carries the main
// module but no dependency list, so under test this table has one row. A
// `go build` binary carries the full list.
type goModulesProvider struct{ src CodevolSourceI }

func (goModulesProvider) Name() string                         { return "go_modules" }
func (goModulesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (goModulesProvider) Schema() *arrow.Schema                { return goModulesTable(nil).Schema() }

func (inst goModulesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	mods, _ := inst.src.Get()
	return goModulesTable(mods).Build(proj, len(mods)), nil
}

func goModulesTable(rows []codevol.ModuleInfo) *introspect.Table {
	return introspect.NewTable().
		String("path", func(i int) string { return rows[i].Path }).
		String("version", func(i int) string { return rows[i].Version }).
		// sum is empty for the main module and for replaced modules.
		String("sum", func(i int) string { return rows[i].Sum }).
		// replaced_by is non-empty when a replace directive redirected the
		// module, in which case path/version do not describe the code that
		// actually shipped.
		String("replaced_by", func(i int) string { return rows[i].ReplacedBy }).
		Bool("is_main", func(i int) bool { return rows[i].IsMain }).
		// party is first | third — the axis the code-volume question is
		// asked on. No module is stdlib, so no row here carries it.
		String("party", func(i int) string { return string(rows[i].Party) })
}

// --- go_symbols --------------------------------------------------------------

// goSymbolsProvider exposes what the linker kept, per package, read from this
// binary's own symbol table.
//
// Empty when the binary is stripped (`-ldflags=-s`) or the platform is not
// ELF; the boot log carries the reason. Empty is a fact about the build, not
// a query failure — the keelson('sbom') convention.
type goSymbolsProvider struct{ src CodevolSourceI }

func (goSymbolsProvider) Name() string                         { return "go_symbols" }
func (goSymbolsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (goSymbolsProvider) Schema() *arrow.Schema                { return goSymbolsTable(nil, false).Schema() }

func (inst goSymbolsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	_, rep := inst.src.Get()
	return goSymbolsTable(rep.Packages, rep.ModuleExact).Build(proj, len(rep.Packages)), nil
}

func goSymbolsTable(rows []codevol.PackageSymbols, moduleExact bool) *introspect.Table {
	attribution := "heuristic"
	if moduleExact {
		attribution = "exact"
	}
	return introspect.NewTable().
		// pkg_path is derived from symbol names, so it is exact for ordinary
		// functions and approximate for synthesised symbols. Join against
		// keelson('go_packages').import_path to keep only real packages.
		String("pkg_path", func(i int) string { return rows[i].PkgPath }).
		// module_path is resolved by longest-prefix match against the module
		// list this same binary declares, so it is exact whenever
		// module_attribution says so — which is what makes the party split
		// trustworthy even though pkg_path is not.
		String("module_path", func(i int) string { return rows[i].ModulePath }).
		String("party", func(i int) string { return string(rows[i].Party) }).
		String("module_attribution", func(int) string { return attribution }).
		Int64("num_symbols", func(i int) int64 { return int64(rows[i].NumSymbols) }).
		// text_bytes is machine code; data_bytes is everything else the
		// symbol table sizes. Summing them is misleading — one 32 MiB
		// zero-filled stdlib buffer is 42% of this binary's sized bytes.
		Uint64("text_bytes", func(i int) uint64 { return rows[i].TextBytes }).
		Uint64("data_bytes", func(i int) uint64 { return rows[i].DataBytes })
}
