package sysmfacts_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// componentPaths is the DTO set the store is generated over. Adding a domain
// (ADR-0184 M4/M5) adds a line here and to the vocabulary, in that order.
var componentPaths = []string{
	"./syscpu_dto.go",
	"./syscpuinfo_dto.go",
	"./sysmem_dto.go",
	"./syspsi_dto.go",
	"./sysnet_dto.go",
	"./sysdiskmount_dto.go",
	"./sysdiskio_dto.go",
	"./sysbattery_dto.go",
	"./sysgpu_dto.go",
	"./sysproc_dto.go",
	"./sysproccmd_dto.go",
	"./syssocket_dto.go",
	"./systopology_dto.go",
}

func generate(t *testing.T, ids map[string]uint64) (outDir string, err error) {
	t.Helper()
	outDir = t.TempDir()
	err = storegen.Input{
		PackageName:    "sysmfacts",
		StoreName:      "Sysmetrics",
		ComponentPaths: componentPaths,
		OutDir:         outDir,
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts",
		Ids:            ids,
	}.Generate()
	return
}

func vocabIds(t *testing.T) map[string]uint64 {
	t.Helper()
	ids, err := storegen.MembershipIds(sysmvocab.NkRegistry)
	require.NoError(t, err)
	return ids
}

func readOut(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err, "read %s", name)
	return string(b)
}

// TestDtosGenerateAgainstTheFactsSchema is what validates the DTOs at all:
// until a store is generated over them, an lw: tag naming a section the facts
// schema does not have, or a membership the vocabulary does not register, is
// just a string in a struct tag.
func TestDtosGenerateAgainstTheFactsSchema(t *testing.T) {
	out, err := generate(t, vocabIds(t))
	require.NoError(t, err)

	for _, f := range []string{"syscpu_dto.out.go", "syscpuinfo_dto.out.go", "sysmem_dto.out.go"} {
		src := readOut(t, out, f)
		// FixedIdsWrapper emits a deliberately non-compiling symbol for a
		// membership it cannot resolve, rather than a silent zero. Catch it
		// here instead of at the build of a generated package.
		assert.NotContains(t, src, "MISSING_MEMBERSHIP_ID",
			"%s names a membership the vocabulary does not register", f)
	}

	// Every kind's ids are the vocabulary's, not declaration-order ones. The
	// const block is gofmt-aligned, so the binding is matched across whatever
	// padding the emitter produced rather than against one exact spelling.
	assertConstBinding(t, readOut(t, out, "syscpu_dto.out.go"),
		"kindSysmCpuLoadAvg15", sysmvocab.MembCpuLoadAvg15.GetId().Value())
	assertConstBinding(t, readOut(t, out, "sysmem_dto.out.go"),
		"kindSysmMemArcMinBytes", sysmvocab.MembMemArcMinBytes.GetId().Value())
	assertConstBinding(t, readOut(t, out, "syscpuinfo_dto.out.go"),
		"kindSysmCpuInfoHost", sysmvocab.MembCpuInfoHost.GetId().Value())
}

// assertConstBinding checks that src binds name to id in a generated const
// block, tolerating the alignment padding gofmt introduces.
func assertConstBinding(t *testing.T, src, name string, id uint64) {
	t.Helper()
	assert.Regexp(t, `\b`+name+`\s+uint64\s*=\s*`+strconv.FormatUint(id, 10)+`\b`, src,
		"membership %q must be baked as %d", name, id)
}

// TestPerCorePercentUsesTheArrayLane pins the `ct=u8h` override on
// SysCpu.PerCorePercent.
//
// A top-level []uint8 is the same Go type as []byte, which the DTO grammar
// classifies as a scalar variable-length blob. Without the override the field
// still generates, still writes and still reads — as one opaque blob instead
// of a per-core array — so nothing but this assertion distinguishes the two.
func TestPerCorePercentUsesTheArrayLane(t *testing.T) {
	out, err := generate(t, vocabIds(t))
	require.NoError(t, err)
	cpu := readOut(t, out, "syscpu_dto.out.go")
	assert.Contains(t, cpu, "u8ArraySecAttr_PerCorePercent",
		"PerCorePercent must bind the u8Array section")
	assert.Contains(t, cpu, "for _, v := range row.PerCorePercent",
		"the u8-array lane emits a per-element loop; a blob lane would write the slice whole")
}

// TestThreeKindsShareTheSymbolSection records why this layout needs the
// registry-stable id regime at all.
//
// All three kinds bind `symbol` (their kind label and host). Under
// declaration-order ids the generator refuses exactly this — components must
// then own disjoint sections, because two kinds' distinct memberships could
// carry the same wire id. The vocabulary's globally-unique ids replace that
// gate with id-level disjointness, which is what lets domains co-reside in one
// store instead of needing a table each.
func TestThreeKindsShareTheSymbolSection(t *testing.T) {
	out, err := generate(t, vocabIds(t))
	require.NoError(t, err)
	store := readOut(t, out, "sysmetrics_store.out.go")
	for _, kind := range []string{`"SysCpu"`, `"SysCpuInfo"`, `"SysMem"`} {
		assert.Contains(t, store, kind, "the store must carry all three kinds")
	}
	for _, f := range []string{"syscpu_dto.out.go", "syscpuinfo_dto.out.go", "sysmem_dto.out.go"} {
		assert.Contains(t, readOut(t, out, f), "symbolSec",
			"%s binds the shared symbol section", f)
	}
}

// TestGenerationRefusesCollidingIds proves the id-injectivity gate is live for
// this layout rather than merely documented. With sections shared across kinds
// it is the only thing standing between a bad snapshot and two kinds
// cross-reading each other's attributes.
func TestGenerationRefusesCollidingIds(t *testing.T) {
	ids := vocabIds(t)
	ids["sysmMemHost"] = ids["sysmCpuHost"]
	_, err := generate(t, ids)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "share one id")
}

// TestGenerationRefusesAMissingMembership is the other half: a DTO tag the
// vocabulary does not register must fail generation, not bake a zero.
func TestGenerationRefusesAMissingMembership(t *testing.T) {
	ids := vocabIds(t)
	delete(ids, "sysmCpuTotalPct")
	_, err := generate(t, ids)
	require.Error(t, err)
	assert.Contains(t, ebtest.Text(t, err), "sysmCpuTotalPct")
}

// The generated Set satisfies the registry contract (ADR-0189 M0/M1). This is
// the seam where the two milestones meet: the emitter's output and the
// registry's admission rules are written in different packages and could
// disagree about what a usable Set is.
func TestComponentSQLRegistersAndResolves(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(sysmfacts.SysmetricsComponentSQL))

	assert.Len(t, r.Kinds(), 13, "every kind the store carries should be published")

	b, ok := r.Lookup("SysMem")
	require.True(t, ok)
	assert.Equal(t, sysmfacts.SysmetricsTableName, b.Table)
	assert.Equal(t, "Sysmetrics", b.Store)

	// The published Filter is the one the Scan verb uses — same string, not a
	// second rendering of the same idea.
	assert.Contains(t, b.Filter, "hasAll(", "the filter should be the baked artefact")
	assert.Contains(t, b.Projection, "CAST(tuple(", "the projection is a named-tuple extraction")

	// Presence is a strict prefix-by-content of Filter: Filter is
	// joinAnd(presence ++ validator), so both halves appear in it. That is the
	// property ADR-0189 records as the duplication cost of publishing all four.
	assert.Contains(t, b.Filter, b.Presence)
	assert.Contains(t, b.Filter, b.Validator)
}
