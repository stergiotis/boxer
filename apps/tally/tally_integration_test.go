//go:build integration

package tally

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// TestComponentsOnARealEntry_LiveServer is ADR-0200 M5's worked example on
// the store's own rows: a snapshot's root row is an entry AND a snapshot
// (ADR-0198 M1), an ordinary file is an entry alone — read back through the
// component probes the Info pane runs, against whatever mount the local
// store holds.
func TestComponentsOnARealEntry_LiveServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sc, err := connect(ctx)
	if err != nil {
		t.Skipf("no store: %v", err)
	}
	defer sc.close()
	rows, err := sc.listMounts(ctx)
	require.NoError(t, err)
	var mount mountRow
	for _, r := range rows {
		if len(r.snapshots) > 0 {
			mount = r
			break
		}
	}
	if mount.id == 0 {
		t.Skip("no mount with a complete snapshot; take one with `boxer fs snapshot`")
	}
	snap := mount.snapshots[0].Snap

	kinds := func(p string) (names []string) {
		hits, herr := loadComponents(ctx, sc.exec, componentsql.Default, mount.id, snap, p)
		require.NoError(t, herr)
		for _, h := range hits {
			names = append(names, h.kind)
		}
		return
	}
	root := kinds(".")
	assert.Contains(t, root, "LadingEntry", "the root row is an entry")
	assert.Contains(t, root, "LadingSnapshot", "and the commit record rides it")

	view, verr := sc.view(mount.id, snap)
	require.NoError(t, verr)
	entries, rerr := fs.ReadDir(view, ".")
	require.NoError(t, rerr)
	var file string
	for _, e := range entries {
		if !e.IsDir() {
			file = e.Name()
			break
		}
	}
	if file == "" {
		t.Skip("the mount's root holds no file")
	}
	fileKinds := kinds(file)
	assert.Contains(t, fileKinds, "LadingEntry")
	assert.NotContains(t, fileKinds, "LadingSnapshot", "an ordinary entry is not a snapshot")
}
