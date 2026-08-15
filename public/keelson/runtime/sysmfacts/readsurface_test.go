package sysmfacts_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSurfaceRegenEnvVar rewrites the golden instead of comparing against it.
// A test-only variable, so it is outside the ADR-0009 registry — the
// `BOXER_VOCAB_GOLDEN_REGEN` precedent one directory over.
const readSurfaceRegenEnvVar = "BOXER_SYSMFACTS_READSURFACE_REGEN"

const readSurfaceGolden = "testdata/read-surface.golden"

// readSurfaceQueries is one authored query per storage shape in this
// vocabulary, written the way ADR-0184 §SD6 says a reader should write one:
// through `LW_GET*` against a section, never as hand-written array
// arithmetic.
//
// Two rules of the surface are load-bearing here, and neither is optional on
// this table:
//
//   - **The channel token is mandatory.** Every `boxer.facts` section carries
//     more than one membership channel, so a call without `chan:` is refused
//     rather than defaulted. `low-card-ref` is the ordinary
//     one-membership-per-attribute channel this vocabulary writes on.
//   - **The verb follows the section, not the Go field.** `symbol` and `bool`
//     store one value per attribute and read through `LW_GET`; every
//     `*Array` section stores a run per attribute and reads through
//     `LW_GET_LIST` — including where the DTO field is a plain scalar, which
//     is then the run's only element. `LW_GET` and `LW_GET_NULL` are refused
//     on those sections outright, so an optional field's absence reads as an
//     empty run rather than as NULL.
//
// The set covers every section the DTOs use — `symbol`, `symbolArray`,
// `stringArray`, `bool`, `u8Array`, `u16Array`, `u32Array`, `u64Array`,
// `i32Array`, `i64Array`, `f32Array` — because the golden's job is to notice
// a naming-convention change, and a section absent from the set is a section
// the golden cannot speak for.
var readSurfaceQueries = []struct {
	name string
	sql  string
}{
	{
		// The CPU sample carries the property that makes the section-plus-
		// membership addressing worth pinning: a scalar and a list live in
		// the *same* section, told apart by nothing at read time — both are
		// runs, and only the writer's arity says the scalar's run holds one.
		// `UsageWatts` is the optional one: None is an empty run.
		name: "cpu",
		sql: `SELECT LW_GET('symbol', 'sysmCpuHost', 'chan:low-card-ref') AS host,
       arrayElement(LW_GET_LIST('u8Array', 'sysmCpuTotalPct', 'chan:low-card-ref'), 1) AS total_pct,
       LW_GET_LIST('u8Array', 'sysmCpuPerCorePct', 'chan:low-card-ref') AS per_core_pct,
       LW_GET_LIST('u32Array', 'sysmCpuPerCoreFreqMhz', 'chan:low-card-ref') AS per_core_freq_mhz,
       arrayElement(LW_GET_LIST('f32Array', 'sysmCpuLoadAvg1', 'chan:low-card-ref'), 1) AS load_avg_1,
       LW_GET_LIST('f32Array', 'sysmCpuUsageWatts', 'chan:low-card-ref') AS usage_watts,
       LW_GET_LIST('i32Array', 'sysmCpuActiveCpus', 'chan:low-card-ref') AS active_cpus
FROM boxer.facts`,
	},
	{
		// The descriptor kind, written once per host rather than per tick.
		name: "cpuinfo",
		sql: `SELECT LW_GET('symbol', 'sysmCpuInfoHost', 'chan:low-card-ref') AS host,
       LW_GET('symbol', 'sysmCpuModelName', 'chan:low-card-ref') AS model_name,
       arrayElement(LW_GET_LIST('i32Array', 'sysmCpuLogicalCores', 'chan:low-card-ref'), 1) AS logical_cores
FROM boxer.facts`,
	},
	{
		// Plain unsigned scalars — the shape a load study reads.
		name: "mem",
		sql: `SELECT LW_GET('symbol', 'sysmMemHost', 'chan:low-card-ref') AS host,
       arrayElement(LW_GET_LIST('u64Array', 'sysmMemTotalBytes', 'chan:low-card-ref'), 1) AS total_bytes,
       arrayElement(LW_GET_LIST('u64Array', 'sysmMemAvailableBytes', 'chan:low-card-ref'), 1) AS available_bytes,
       arrayElement(LW_GET_LIST('u64Array', 'sysmMemUsedBytes', 'chan:low-card-ref'), 1) AS used_bytes
FROM boxer.facts`,
	},
	{
		// The only `bool` section in the vocabulary, beside the pressure
		// averages it qualifies: unavailable PSI is not zero pressure.
		name: "psi",
		sql: `SELECT LW_GET('symbol', 'sysmPsiHost', 'chan:low-card-ref') AS host,
       LW_GET('bool', 'sysmPsiAvailable', 'chan:low-card-ref') AS available,
       arrayElement(LW_GET_LIST('f32Array', 'sysmPsiCpuSomeAvg10', 'chan:low-card-ref'), 1) AS cpu_some_avg10,
       arrayElement(LW_GET_LIST('u64Array', 'sysmPsiCpuSomeTotalUs', 'chan:low-card-ref'), 1) AS cpu_some_total_us
FROM boxer.facts`,
	},
	{
		// M4's alignment contract seen from the read side: index i of every
		// list describes the same interface, and nothing in leeway enforces
		// it. A query that reads them apart is how the pairing goes wrong.
		name: "net",
		sql: `SELECT LW_GET('symbol', 'sysmNetHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('symbolArray', 'sysmNetName', 'chan:low-card-ref') AS name,
       LW_GET_LIST('i32Array', 'sysmNetIndex', 'chan:low-card-ref') AS if_index,
       LW_GET_LIST('u8Array', 'sysmNetUp', 'chan:low-card-ref') AS up,
       LW_GET_LIST('u64Array', 'sysmNetRxBytesPerSec', 'chan:low-card-ref') AS rx_bytes_per_sec
FROM boxer.facts`,
	},
	{
		name: "diskmount",
		sql: `SELECT LW_GET('symbol', 'sysmDiskMountHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('symbolArray', 'sysmDiskMountPoint', 'chan:low-card-ref') AS mount_point,
       LW_GET_LIST('u64Array', 'sysmDiskMountFreeBytes', 'chan:low-card-ref') AS free_bytes,
       LW_GET_LIST('f32Array', 'sysmDiskMountUsedPct', 'chan:low-card-ref') AS used_pct
FROM boxer.facts`,
	},
	{
		name: "gpu",
		sql: `SELECT LW_GET('symbol', 'sysmGpuHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('symbolArray', 'sysmGpuName', 'chan:low-card-ref') AS name,
       LW_GET_LIST('u8Array', 'sysmGpuBusyPct', 'chan:low-card-ref') AS busy_pct,
       LW_GET_LIST('f32Array', 'sysmGpuTempC', 'chan:low-card-ref') AS temp_c,
       LW_GET_LIST('u32Array', 'sysmGpuFreqMhz', 'chan:low-card-ref') AS freq_mhz
FROM boxer.facts`,
	},
	{
		// M5's fan-out domain, stored column-major: "this process over time"
		// is an array walk, which is the trade the milestone recorded.
		// `sysmProcStartedAtMs` is the vocabulary's only `i64Array`.
		name: "proc",
		sql: `SELECT LW_GET('symbol', 'sysmProcHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('u32Array', 'sysmProcPid', 'chan:low-card-ref') AS pid,
       LW_GET_LIST('symbolArray', 'sysmProcName', 'chan:low-card-ref') AS name,
       LW_GET_LIST('f32Array', 'sysmProcCpuPct', 'chan:low-card-ref') AS cpu_pct,
       LW_GET_LIST('i64Array', 'sysmProcStartedAtMs', 'chan:low-card-ref') AS started_at_ms
FROM boxer.facts`,
	},
	{
		// The sensitive kind (`--tee-proc-cmd`, off by default). It reads
		// like any other; the default is what keeps it unwritten, and
		// `stringArray` appears nowhere else.
		name: "proccmd",
		sql: `SELECT LW_GET('symbol', 'sysmProcCmdHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('u32Array', 'sysmProcCmdPid', 'chan:low-card-ref') AS pid,
       LW_GET_LIST('stringArray', 'sysmProcCmdLine', 'chan:low-card-ref') AS cmd_line
FROM boxer.facts`,
	},
	{
		// The vocabulary's only `u16Array`.
		name: "socket",
		sql: `SELECT LW_GET('symbol', 'sysmSocketHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('symbolArray', 'sysmSocketProto', 'chan:low-card-ref') AS proto,
       LW_GET_LIST('u16Array', 'sysmSocketPort', 'chan:low-card-ref') AS port,
       LW_GET_LIST('u32Array', 'sysmSocketPid', 'chan:low-card-ref') AS pid
FROM boxer.facts`,
	},
	{
		// M6's adjacency list. The node number is stored rather than left
		// implicit in array position precisely so a query like this one may
		// filter without the parent references dangling.
		name: "topology",
		sql: `SELECT LW_GET('symbol', 'sysmTopologyHost', 'chan:low-card-ref') AS host,
       LW_GET_LIST('u32Array', 'sysmTopoNodeIdx', 'chan:low-card-ref') AS node_idx,
       LW_GET_LIST('i32Array', 'sysmTopoParentIdx', 'chan:low-card-ref') AS parent_idx,
       LW_GET_LIST('symbolArray', 'sysmTopoKind', 'chan:low-card-ref') AS kind,
       LW_GET_LIST('u64Array', 'sysmTopoCacheSizeBytes', 'chan:low-card-ref') AS cache_size_bytes
FROM boxer.facts`,
	},
}

// vocabLookup answers the membership-id question ADR-0171 §SD4 leaves to the
// client. Unknown names error rather than resolving to zero, matching
// providers.MembershipLookup — a silent miss expands into a predicate that
// matches nothing.
type vocabLookup map[string]uint64

func (inst vocabLookup) LookupMembership(name string) (id uint64, err error) {
	id, ok := inst[name]
	if !ok {
		err = eh.Errorf("sysmfacts: no such sysmetrics membership: %s", name)
	}
	return
}

// factsColumnNames is `boxer.facts`'s physical column list off the generated
// Arrow schema — the same source the DML builders write through, so the
// expansion cannot be checked against a stale hand-copied list.
func factsColumnNames() (names []string) {
	fields := dml.CreateSchemaFacts().Fields()
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return
}

// expandReadSurface runs the client-side extraction pass the way a host
// binding this vocabulary would: the facts schema for the lanes, the
// sysmetrics registry for the ids.
func expandReadSurface(t *testing.T, sql string) (out string, err error) {
	t.Helper()
	database, _, qualified := strings.Cut(sysmfacts.SysmetricsTableName, ".")
	require.True(t, qualified && database != "", "the store's table name should be database-qualified")

	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{sysmfacts.SysmetricsTableName: factsColumnNames()}))
	return constructsql.ExtractExpandPassWithIds(
		resolver, vocabLookup(vocabIds(t)), database).Run(sql)
}

// TestReadSurfaceExpansionMatchesTheGolden is the ADR-0184 §SD6 check: the
// metric sections are readable through `LW_GET*`, and what those calls expand
// into is pinned.
//
// It is a golden rather than a set of assertions because the thing worth
// catching is a *change*, not a property: a re-aspected section, a renamed
// membership, or a different physical spelling all keep the query valid and
// silently change which column it reads. The store's own round-trip cannot
// see that — it writes and reads through the same generated code, so a
// renaming moves both sides together.
func TestReadSurfaceExpansionMatchesTheGolden(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# The sysmetrics read surface, expanded (ADR-0184 §SD6).\n")
	sb.WriteString("#\n")
	sb.WriteString("# Each block is one authored query and what the client-side LW_GET* pass\n")
	sb.WriteString("# ships to the server. A changed line is a changed physical column, so a\n")
	sb.WriteString("# re-aspected section or a renamed membership goes red here rather than at\n")
	sb.WriteString("# the first dump against a real server.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Regenerate with " + readSurfaceRegenEnvVar + "=1 go test ./public/keelson/runtime/sysmfacts/...\n")

	for _, q := range readSurfaceQueries {
		out, err := expandReadSurface(t, q.sql)
		require.NoErrorf(t, err, "%s: the authored query should expand", q.name)

		sb.WriteString("\n## " + q.name + "\n")
		sb.WriteString("-- authored\n" + q.sql + "\n")
		sb.WriteString("-- expanded\n" + out + "\n")
	}
	got := sb.String()

	if os.Getenv(readSurfaceRegenEnvVar) != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(readSurfaceGolden), 0o755))
		require.NoError(t, os.WriteFile(readSurfaceGolden, []byte(got), 0o644))
		t.Skip("golden rewritten; unset " + readSurfaceRegenEnvVar + " to compare against it")
	}

	want, err := os.ReadFile(readSurfaceGolden)
	require.NoError(t, err, "read the golden; regenerate with "+readSurfaceRegenEnvVar+"=1")
	assert.Equal(t, string(want), got,
		"the read surface moved; a changed physical name means rows already written are read by a different column")
}

// No authored call may survive the pass. An LW_GET that quietly declined
// would travel to the server as an unknown function — true, and only at run
// time, against a server that may not even be provisioned.
func TestReadSurfaceLeavesNoExtractionCallBehind(t *testing.T) {
	for _, q := range readSurfaceQueries {
		require.Containsf(t, q.sql, "LW_GET", "%s: the authored query should read through the surface", q.name)

		out, err := expandReadSurface(t, q.sql)
		require.NoErrorf(t, err, "%s", q.name)
		for _, name := range []string{"LW_GET", "LW_GET_NULL", "LW_GET_LIST"} {
			assert.NotContainsf(t, out, name+"(", "%s: a %s call survived expansion", q.name, name)
		}
		assert.Truef(t,
			strings.Contains(out, "LW_VALUE_BY_TAG_EQUAL") || strings.Contains(out, "LW_LIST_BY_TAG_EQUAL"),
			"%s: the expansion should land in the read-back family", q.name)
	}
}

// The sections are read through the read-back family, not through arithmetic
// this test wrote. Asserting the family by name is what makes the layering in
// doc/explanation/leeway-sql-read-surface.md checkable rather than advisory.
func TestReadSurfaceExpandsIntoTheReadBackFamily(t *testing.T) {
	scalar, err := expandReadSurface(t,
		"SELECT LW_GET('symbol', 'sysmMemHost', 'chan:low-card-ref') FROM boxer.facts")
	require.NoError(t, err)
	assert.Contains(t, scalar, "LW_VALUE_BY_TAG_EQUAL",
		"a one-value-per-attribute section should expand to the value form")
	assert.NotContains(t, scalar, "LW_LIST_BY_TAG_EQUAL",
		"a query with no run-valued read should not expand to the list form")

	list, err := expandReadSurface(t,
		"SELECT LW_GET_LIST('u64Array', 'sysmMemTotalBytes', 'chan:low-card-ref') FROM boxer.facts")
	require.NoError(t, err)
	assert.Contains(t, list, "LW_LIST_BY_TAG_EQUAL",
		"an array-valued section should expand to the list form")
	assert.NotContains(t, list, "LW_VALUE_BY_TAG_EQUAL",
		"a run-valued read should not reach for the scalar form")
}

// The verb is the section's, not the Go field's — the rule that makes
// ADR-0184 §SD6's `LW_GET('f32Array', …)` spelling unrunnable. Pinned in both
// directions so neither error message can quietly become a silent default.
func TestSectionArityChoosesTheVerb(t *testing.T) {
	_, err := expandReadSurface(t,
		"SELECT LW_GET('f32Array', 'sysmCpuLoadAvg1', 'chan:low-card-ref') FROM boxer.facts")
	require.Error(t, err, "a run-valued section should refuse the scalar verb")
	assert.Contains(t, err.Error(), "LW_GET_LIST", "the error should name the verb to use")

	_, err = expandReadSurface(t,
		"SELECT LW_GET_LIST('symbol', 'sysmCpuHost', 'chan:low-card-ref') FROM boxer.facts")
	require.Error(t, err, "a one-value section should refuse the list verb")

	// The channel token is not optional either: `boxer.facts` sections carry
	// several, so there is no channel to default to.
	_, err = expandReadSurface(t,
		"SELECT LW_GET('symbol', 'sysmCpuHost') FROM boxer.facts")
	require.Error(t, err, "a multi-channel section should refuse an unqualified call")
	assert.Contains(t, err.Error(), "chan:", "the error should name the token to add")
}

// The §SD6 caveat, pinned so it cannot drift silently: a ref channel carries
// a registry id, and a *name* only resolves where a registry is bound.
//
// This is the ergonomic cost SD6 records, and it is worth a test because the
// mitigation it names is partial — providers.MembershipLookup, which backs
// `keelson('memberships')`, resolves against the runtime vocabulary only, so
// a sysmetrics membership name does not resolve there today.
func TestRefChannelTakesAnIdWithoutARegistry(t *testing.T) {
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{sysmfacts.SysmetricsTableName: factsColumnNames()}))
	unbound := constructsql.ExtractExpandPass(resolver, "boxer")

	const byNameSQL = "SELECT LW_GET_LIST('f32Array', 'sysmCpuLoadAvg1', 'chan:low-card-ref') FROM boxer.facts"

	_, err := unbound.Run(byNameSQL)
	require.Error(t, err, "an unbound host should refuse a membership NAME, not expand it to nothing")

	ids := vocabIds(t)
	id, ok := ids["sysmCpuLoadAvg1"]
	require.True(t, ok, "the membership should be in the vocabulary")

	byId, err := unbound.Run("SELECT LW_GET_LIST('f32Array', '" + strconv.FormatUint(id, 10) +
		"', 'chan:low-card-ref') FROM boxer.facts")
	require.NoError(t, err, "the id form is what an ad-hoc query writes today")

	byName, err := expandReadSurface(t, byNameSQL)
	require.NoError(t, err, "with the registry bound, the name resolves")
	assert.Equal(t, byId, byName,
		"the two spellings should ship the same statement; only the authoring differs")
}

// Every membership the golden queries name is in the vocabulary. Without
// this, a membership renamed in both the DTO and the vocabulary would leave
// the golden queries as the only stale spelling — and they would fail with a
// lookup error whose cause reads like a test bug.
func TestReadSurfaceQueriesNameLiveMemberships(t *testing.T) {
	ids := vocabIds(t)
	for _, q := range readSurfaceQueries {
		for _, m := range membershipArgs(q.sql) {
			_, ok := ids[m]
			assert.Truef(t, ok, "%s: %s is not a sysmetrics membership", q.name, m)
		}
	}
}

// membershipArgs pulls the second argument out of every LW_GET* call in an
// authored query. Deliberately literal: the queries above are literals too,
// and a parser here would only be able to disagree with the pass.
func membershipArgs(sql string) (out []string) {
	for _, call := range []string{"LW_GET(", "LW_GET_NULL(", "LW_GET_LIST("} {
		rest := sql
		for {
			i := strings.Index(rest, call)
			if i < 0 {
				break
			}
			rest = rest[i+len(call):]
			args := rest
			if end := strings.Index(args, ")"); end >= 0 {
				args = args[:end]
			}
			parts := strings.Split(args, ",")
			if len(parts) < 2 {
				continue
			}
			out = append(out, strings.Trim(strings.TrimSpace(parts[1]), "'"))
		}
	}
	return
}
