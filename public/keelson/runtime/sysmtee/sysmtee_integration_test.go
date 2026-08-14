//go:build integration

package sysmtee_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmtee"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

// liveStore binds the generated store to the CLICKHOUSE_* server, skipping when
// it is unreachable.
func liveStore(t *testing.T) (*sysmfacts.SysmetricsStore, *chclient.Client) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(store.Close)
	return store, client
}

// TestVerifySchema_AgreesWithTheProvisionedTable is the load-bearing check for
// ADR-0184 §SD2.
//
// The store is generated from the boxer.facts TableDesc but never provisions
// the table; `chstore.SetupTable` does. Those are two schema authors reading
// one source, and nothing at runtime forces them to agree — a drift would not
// fail to write, it would write rows that decode as absent. This is the
// assertion that the split is safe.
func TestVerifySchema_AgreesWithTheProvisionedTable(t *testing.T) {
	store, _ := liveStore(t)
	err := store.VerifySchema(context.Background())
	require.NoError(t, err,
		"the generated store disagrees with the live boxer.facts — chstore and storegen have drifted")
}

// TestTee_RoundTripsThroughTheLiveTable writes samples through the tee and
// reads them back with the store's own Scan verb: the whole M3 path, against
// the table the runtime actually uses.
func TestTee_RoundTripsThroughTheLiveTable(t *testing.T) {
	store, _ := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))

	// A host token unique to this run, so the assertions cannot see rows left
	// by an earlier one and the test never has to delete from a live table.
	host := "sysmtee-it-" + time.Now().UTC().Format("20060102150405.000000")

	bus := inprocbus.NewInst(zerolog.Nop())
	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}

	tee, err := sysmtee.Start(sysmtee.Options{
		Bus:           bus.NewClient("tee", subCap),
		Store:         store,
		Host:          host,
		FlushInterval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	codec := sysmetricsbus.NewCBORCodec()
	pub := bus.NewClient("producer", pubCap)
	const samples = 3
	for i := range samples {
		payload, encErr := codec.Encode(&sysmsnap.BundleSnapshot{
			SampledAtUnixMs: time.Now().UnixMilli() + int64(i),
			CPU: &sysmsnap.CPUSnapshot{
				TotalPercent:   uint8(40 + i),
				PerCorePercent: []uint8{10, 20},
				PerCoreFreqMHz: []uint32{3000, 3100},
				LoadAvg1:       1.25,
				ModelName:      "Integration CPU",
				LogicalCores:   2,
			},
			Mem: &sysmsnap.MemSnapshot{TotalBytes: 16 << 30, AvailableBytes: 8 << 30},
		})
		require.NoError(t, encErr)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(host), payload))
	}

	require.Eventually(t, func() bool {
		return tee.Stats().Flushed >= 2*samples+1
	}, 10*time.Second, 50*time.Millisecond, "rows never became durable: %+v", tee.Stats())
	require.NoError(t, tee.Stop())

	// Read back through the store's own Scan verb, which resolves the baked
	// membership ids — so this exercises the read direction of the same id
	// snapshot the write used.
	// The key is recomputed here rather than exported from the tee, so the
	// assertion is an independent oracle for the derivation, not a restatement
	// of it.
	key := xxh3.HashString(host + "/" + string(sysmsnap.DomainCPU))
	seen := 0
	for ent, scanErr := range store.ScanSysCpu(ctx, recordstore.ScanOpts{
		ExtraPredicate: sysmfacts.SysmetricsColKey + " = " + strconv.FormatUint(key, 10),
		Limit:          32,
	}) {
		require.NoError(t, scanErr)
		require.NotNil(t, ent)
		seen++
	}
	assert.Equal(t, samples, seen, "every cpu sample written must read back")
}
