package storegen_test

import (
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource yields whatever pairs a test hands it, standing in for a
// registry so the refusals can be reached at all — a real registry validates
// and normalizes its keys, which is what makes them unreachable in production.
type fakeSource struct {
	names []naming.StylableName
	keys  []registry.RegisteredNaturalKey
}

func (f fakeSource) IterateAll() iter.Seq2[naming.StylableName, registry.RegisteredNaturalKey] {
	return func(yield func(naming.StylableName, registry.RegisteredNaturalKey) bool) {
		for i, n := range f.names {
			if !yield(n, f.keys[i]) {
				return
			}
		}
	}
}

// scratchRegistry builds a throwaway vocabulary at a tag-value base no
// in-tree vocabulary uses, so its RegisteredNaturalKey values are real ones.
func scratchRegistry(t *testing.T, names ...string) (*registry.HumanReadableNaturalKeyRegistry[*contract.VcsManagedContract], []registry.RegisteredNaturalKey) {
	t.Helper()
	c := contract.NewVcsManagedContract()
	tv := registry.MustNewTagValueRegistry(
		identifier.TagValue(64), naming.LowerSpinalCase, 4, c)
	base := tv.MustBegin("storegenProbeMembers", 0).End()
	nk := registry.MustNewNaturalKeyRegistry(
		base.GetTagValue(), 8, naming.LowerSpinalCase, identifier.UntaggedId(0), c)
	keys := make([]registry.RegisteredNaturalKey, 0, len(names))
	for _, n := range names {
		keys = append(keys, nk.MustBegin(naming.StylableName(n)).End())
	}
	return nk, keys
}

func TestMembershipIds_ResolvesAgainstARegistry(t *testing.T) {
	nk, keys := scratchRegistry(t, "probeAlpha", "probeBeta")
	ids, err := storegen.MembershipIds(nk)
	require.NoError(t, err)
	// The registry stores lower-spinal; the map is keyed the way a DTO's lw:
	// tag spells the membership.
	assert.Equal(t, map[string]uint64{
		"probeAlpha": keys[0].GetId().Value(),
		"probeBeta":  keys[1].GetId().Value(),
	}, ids)
}

// TestMembershipIds_RoundTripsEveryRegisteredName is the check ADR-0183 D1
// asks for, over a real vocabulary: every registered natural key survives the
// spinal→lowerCamel conversion and converts back to the name it was stored
// under. A converter regression at a digit boundary — the documented lossy
// case — goes red here rather than as an id that matches no row.
func TestMembershipIds_RoundTripsEveryRegisteredName(t *testing.T) {
	n := 0
	for stored := range vdd.KeelsonHrNkRegistry.IterateAll() {
		camel := naming.ConvertNameStyle(stored, naming.LowerCamelCase)
		back := naming.ConvertNameStyle(camel, naming.LowerSpinalCase)
		assert.Equal(t, stored, back, "membership %q does not round-trip via %q", stored, camel)
		n++
	}
	require.NotZero(t, n, "the vdd vocabulary linked empty — the snapshot would be empty too")
}

func TestMembershipIds_AgreesWithTheVocabularysOwnAccessor(t *testing.T) {
	ids, err := storegen.MembershipIds(vdd.KeelsonHrNkRegistry)
	require.NoError(t, err)
	// The value factswrapper's generated Init resolves at run time is the value
	// a store bakes at generation time. That equality is the whole point of the
	// package; if it ever parts, reads match nothing and say nothing.
	assert.Equal(t, vdd.MembCgSubject.GetId().Value(), ids["cgSubject"])
	assert.Equal(t, vdd.MembNaturalKey.GetId().Value(), ids["naturalKey"])
	for name, id := range ids {
		assert.NotZero(t, id, "membership %q resolved to zero", name)
	}
}

func TestMembershipIds_RefusesConvergingNames(t *testing.T) {
	_, keys := scratchRegistry(t, "probeAlpha", "probeBeta")
	// Two spellings a registry would have normalized to one; only a non-registry
	// source can present them separately.
	_, err := storegen.MembershipIds(fakeSource{
		names: []naming.StylableName{"probe-alpha", "probeAlpha"},
		keys:  keys,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "converge")
}

func TestMembershipIds_RefusesZeroId(t *testing.T) {
	var zero registry.RegisteredNaturalKey
	_, err := storegen.MembershipIds(fakeSource{
		names: []naming.StylableName{"probe-alpha"},
		keys:  []registry.RegisteredNaturalKey{zero},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zero")
}

func TestMembershipIds_RefusesNilSource(t *testing.T) {
	_, err := storegen.MembershipIds(nil)
	require.Error(t, err)
}

func TestGenerate_RefusesEmptySnapshot(t *testing.T) {
	err := storegen.Input{
		PackageName: "probe", StoreName: "Probe", OutDir: t.TempDir(),
		ImportPath: "example.com/probe",
	}.Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "membership-id snapshot")
}

// TestGenerate_EmitsAFactsBoundStore is the end-to-end check: a component
// bound to the real boxer.facts schema generates, and what comes out is
// qualified to boxer.facts and carries the caller's ids rather than
// declaration-order ones.
func TestGenerate_EmitsAFactsBoundStore(t *testing.T) {
	out := t.TempDir()
	ids := map[string]uint64{
		// Deliberately far from 1..N, so a positional id leaking into any
		// artefact could not accidentally match.
		"storegenProbeHost":  7001,
		"storegenProbeCount": 7002,
		"storegenProbeRatio": 7003,
	}
	require.NoError(t, storegen.Input{
		PackageName:    "probe",
		StoreName:      "Probe",
		ComponentPaths: []string{"./testdata/probe_dto.go"},
		OutDir:         out,
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen/probe",
		Ids:            ids,
	}.Generate())

	store := readFile(t, filepath.Join(out, "probe_store.out.go"))
	assert.Contains(t, store, `ProbeTableName = "boxer.facts"`,
		"every runtime statement routes through this const, so it carries the qualification")
	for name, id := range ids {
		assert.Contains(t, store, strconv.FormatUint(id, 10),
			"membership %q's caller-assigned id must reach the store", name)
	}

	ddl := readFile(t, filepath.Join(out, "facts_ddl_clickhouse.out.sql"))
	assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS boxer.facts")

	// The DML scaffolding lands under internal/lowlevel, and the component
	// codec beside the store.
	for _, f := range []string{
		"probe_dto.out.go",
		filepath.Join("internal", "lowlevel", "facts_dml.out.go"),
	} {
		_, err := os.Stat(filepath.Join(out, f))
		require.NoError(t, err, "expected %s", f)
	}

	// Read access is NOT emitted — the store binds `factsschema/ra`, the same
	// package `codec/factswrapper` puts in every wire codec. Asserting the
	// absence rather than the reference is what has teeth: an emitted copy
	// would compile, decode correctly, and silently cost ~206 KB per
	// facts-bound store (ADR-0184, Consequences).
	_, err := os.Stat(filepath.Join(out, "internal", "lowlevel", "facts_ra.out.go"))
	assert.True(t, os.IsNotExist(err),
		"a facts-bound store must not emit its own read-access copy")
	assert.Contains(t, store, `"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"`)
	assert.Contains(t, store, "ra.NewReadAccessFactsPlainEntityIdAttributes()",
		"the class names must carry factsschema's stylable name, not this generator's _table default")

	// The ids must be consts in the codec, not runtime lookups: a store bakes
	// them into its Scan filter SQL, so they cannot come from an init().
	codec := readFile(t, filepath.Join(out, "probe_dto.out.go"))
	assert.Contains(t, codec, "uint64 = 7001")
	assert.NotContains(t, codec, "vdd.Memb",
		"the store-side codec resolves ids at generation time, not from vdd at init")

	// Store and codec must declare ONE package, or the directory does not
	// compile. The emitted codec keeps the DTO's own package clause, so this
	// only holds because the fixture agrees with PackageName — the gate in
	// Generate is what keeps that from being an accident.
	assert.Contains(t, codec, "package probe\n")
	assert.Contains(t, store, "package probe\n")
}

// TestGenerate_EmitsNoDdlExecutionSurface is the ADR-0184 §SD2 guarantee.
//
// `chstore.SetupTable` is the sole DDL author for boxer.facts, so a store
// generated here must carry no way to run DDL at all — not a discouraged
// method, an absent one. VerifySchema remains and matters more, since nothing
// in the store guarantees the live table's shape.
func TestGenerate_EmitsNoDdlExecutionSurface(t *testing.T) {
	out := t.TempDir()
	require.NoError(t, storegen.Input{
		PackageName:    "probe",
		StoreName:      "Probe",
		ComponentPaths: []string{"./testdata/probe_dto.go"},
		OutDir:         out,
		ImportPath:     "example.com/probe",
		Ids:            map[string]uint64{"storegenProbeHost": 7001, "storegenProbeCount": 7002, "storegenProbeRatio": 7003},
	}.Generate())

	store := readFile(t, filepath.Join(out, "probe_store.out.go"))
	assert.NotContains(t, store, "func (inst *ProbeStore) EnsureTable",
		"a facts-bound store must not be able to provision boxer.facts")
	assert.NotContains(t, store, "DDLTail",
		"DDLTail exists only as EnsureTable's raw suffix; leaving it would be config that does nothing")
	assert.Contains(t, store, "func (inst *ProbeStore) VerifySchema",
		"VerifySchema is how an externally provisioned store checks the table it was given")

	// The DDL file is still written: it is the physical schema the store
	// decodes positionally, and whoever does provision the table needs it.
	assert.Contains(t, readFile(t, filepath.Join(out, "facts_ddl_clickhouse.out.sql")),
		"CREATE TABLE IF NOT EXISTS boxer.facts")
}

func TestGenerate_RefusesComponentFromAnotherPackage(t *testing.T) {
	// The failure this prevents is a `go build` error naming neither the DTO
	// nor the setting that caused it, raised whenever someone first compiles
	// the generated package.
	err := storegen.Input{
		PackageName:    "somethingElse",
		StoreName:      "Probe",
		ComponentPaths: []string{"./testdata/probe_dto.go"},
		OutDir:         t.TempDir(),
		ImportPath:     "example.com/probe",
		Ids:            map[string]uint64{"storegenProbeHost": 7001},
	}.Generate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares package")
	assert.Contains(t, err.Error(), "somethingElse")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return string(b)
}
