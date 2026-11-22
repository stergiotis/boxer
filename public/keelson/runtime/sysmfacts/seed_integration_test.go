//go:build integration

package sysmfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmtee"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
)

// seedSamplesIfNone gives a facts table that holds no memory sample a few
// samples of a host of this run's own: memory rows and, beside them, CPU
// rows, which a memory read must not return. The tests of this package
// compare two reads of what the table holds, and a server a release run
// has just started holds nothing of the kind.
//
// A table that already holds memory samples — a server a scraper writes to
// — is left as it is. The rows go through the tee, the one writer of these
// kinds, so they are rows of the shape a running host stores.
func seedSamplesIfNone(t *testing.T, client *chclient.Client) {
	t.Helper()
	ctx := context.Background()
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	probe := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{Keys: recordstoretest.KeyStream(recordstoretest.FactsRowTag)})
	defer probe.Close()
	for _, scanErr := range probe.ScanSysMem(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, scanErr)
		return // the first row is enough: the table holds memory samples
	}

	keys, err := chstore.NewKeyedIdGenerator(exec, chstore.ConfigFromEnv(), sysmtee.IdScope)
	require.NoError(t, err)
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{Keys: keys})
	defer store.Close()

	host := "sysmfacts-it-" + time.Now().UTC().Format("20060102150405.000000")
	bus := inprocbus.NewInst(zerolog.Nop())
	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}
	tee, err := sysmtee.Start(ctx, sysmtee.Options{
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
			Mem: &sysmsnap.MemSnapshot{TotalBytes: 16 << 30, AvailableBytes: uint64(8-i) << 30},
		})
		require.NoError(t, encErr)
		require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject(host), payload))
	}
	require.Eventually(t, func() bool {
		return tee.Stats().Flushed >= 2*samples+1
	}, 10*time.Second, 50*time.Millisecond, "the seeded samples never became durable: %+v", tee.Stats())
	require.NoError(t, tee.Stop())
}
