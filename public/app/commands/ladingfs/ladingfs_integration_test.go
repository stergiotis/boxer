//go:build integration

package ladingfs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
)

const policyTestMount identifier.TaggedId = 0xF5F5_0198_0002_0042

// scratchDb returns a database of the test's own on the server the
// CLICKHOUSE_* variables name, dropped at its end.
func scratchDb(t *testing.T, cli *chclient.Client, db string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" SYNC"))
	t.Cleanup(func() { _ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC") })
}

func liveClient(t *testing.T) *chclient.Client {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	c := chclient.New(cfg, nil)
	if c.Ping(context.Background()) != nil {
		t.Skipf("no live ClickHouse at %s", env.RedactURL(cfg.URL))
	}
	return c
}

func count(t *testing.T, c *chclient.Client, sql string) string {
	t.Helper()
	body, err := c.Query(context.Background(), sql+" FORMAT TabSeparated")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

// runSnapshotCmd runs `snapshot` with args and returns its error.
func runSnapshotCmd(args ...string) error {
	root := &cli.Command{Name: "ladingfs", Commands: []*cli.Command{newSnapshotCommand()}, Writer: &bytes.Buffer{}}
	return root.Run(context.Background(), append([]string{"ladingfs", "snapshot"}, args...))
}

func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o600))
	return dir
}

// With --name on a database where nothing has created the facts table yet,
// provisioning sets it up beside the store's own tables, and the policy
// record lands there.
func TestSnapshotWithANameProvisionsThePolicyTable(t *testing.T) {
	require.True(t, policyTestMount.IsValid())
	c := liveClient(t)
	const db = "ladingfs_it_policy"
	scratchDb(t, c, db)
	require.NoError(t, runSnapshotCmd("--mount", "0xF5F5019800020042", "--database", db, "--name", "it-mount", tree(t)))
	assert.Equal(t, "1", count(t, c, "SELECT count() FROM "+ladingschema.Layout{Database: db}.PolicyTable()), "one policy record")
	assert.NotEqual(t, "0", count(t, c, "SELECT count() FROM "+ladingschema.Layout{Database: db}.MetaTable()), "the tree was walked")
	// A second run finds everything in place.
	require.NoError(t, runSnapshotCmd("--mount", "0xF5F5019800020042", "--database", db, "--name", "it-mount", tree(t)))
}

// With --no-provision a missing policy table fails the command before the
// walk: the store's tables stay empty instead of holding a snapshot whose
// policy record was never written.
func TestSnapshotWithANameAndNoProvisionRefusesBeforeTheWalk(t *testing.T) {
	c := liveClient(t)
	const db = "ladingfs_it_nopolicy"
	scratchDb(t, c, db)
	exec, err := storeexec.New(c, nil)
	require.NoError(t, err)
	layout := ladingschema.Layout{Database: db}
	require.NoError(t, lading.ProvisionIn(context.Background(), exec, layout, ladingschema.ProfileCorpus))

	err = runSnapshotCmd("--mount", "0xF5F5019800020042", "--database", db, "--no-provision", "--name", "it-mount", tree(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy table")
	assert.Equal(t, "0", count(t, c, "SELECT count() FROM "+layout.MetaTable()), "nothing was walked")
}
