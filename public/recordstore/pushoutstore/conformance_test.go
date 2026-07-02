package pushoutstore

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/storagetest"
	"github.com/stergiotis/boxer/public/recordstore/chexec"
)

// TestPushoutStorageConformance runs pushout's storage conformance suite
// against the ClickHouse-backed adapter — the ADR-0100 S3 acceptance gate
// for the recordstore primitive set. Each check opens (and for the
// durability check reopens) a fresh clickhouse-local --path location;
// MergeTree makes rows durable across the executor's one-shot processes.
func TestPushoutStorageConformance(t *testing.T) {
	if _, err := chexec.NewLocalExecutor(t.TempDir(), nil); err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	storagetest.Run(t, func(location string) (repo.StorageI, error) {
		exec, err := chexec.NewLocalExecutor(location, nil)
		if err != nil {
			return nil, err
		}
		return Open(context.Background(), exec, nil, PushoutStoreConfig{CacheCapacity: 64})
	})
}
