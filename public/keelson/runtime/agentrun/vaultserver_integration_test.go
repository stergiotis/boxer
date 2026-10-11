//go:build integration

package agentrun

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// Two stores over one record, the shape two processes on one server take
// (ADR-0319 §SD1): what one appends the other reads, what the other
// erases the first no longer reads, and the chain holds for both.
func TestTwoVaultedStoresOverOneRecord(t *testing.T) {
	fixedOpenings(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "chdb")
	execA, err := chexec.NewLocalExecutor(path, memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	execB, err := chexec.NewLocalExecutor(path, memory.NewGoAllocator())
	require.NoError(t, err)
	layout := agentrunstore.Layout{Database: "twotest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, execA, layout))
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, execA, layout))
	open := func(exec recordstore.ExecutorI) *VaultedStore {
		ledger, vault := NewSqlStore(exec, layout), NewSqlBodyStore(exec, layout)
		t.Cleanup(ledger.Close)
		t.Cleanup(vault.Close)
		return NewVaulted(ledger, vault)
	}
	a, b := open(execA), open(execB)

	wa, err := Open(ctx, a, Run{Id: "cell/two", Cell: "cell", OwnerApp: "chat"})
	require.NoError(t, err)
	written, err := wa.Append(ctx, Entry{Kind: KindUser, Body: "a name"}, Entry{Kind: KindOutcome, Key: "c1", Role: "tool", Body: `{"customer":"a name"}`})
	require.NoError(t, err)
	require.NoError(t, wa.Close(ctx, StateClosed))

	wb, err := Reopen(ctx, b, "cell/two")
	require.NoError(t, err, "the second store reads the first's record and the chain verifies")
	assert.Equal(t, written, wb.Entries(), "bodies and openings included")
	require.NoError(t, wb.Erase(ctx, "person", 2))

	seen, err := a.Entries(ctx, "cell/two", 0)
	require.NoError(t, err)
	require.Len(t, seen, 3)
	assert.Equal(t, "a name", seen[0].Body)
	assert.Empty(t, seen[1].Body, "the first store no longer reads what the second erased")
	assert.Equal(t, KindErased, seen[2].Kind)
	r, found, err := b.GetRun(ctx, "cell/two")
	require.NoError(t, err)
	require.True(t, found)
	broken, err := VerifyRun(r, seen)
	require.NoError(t, err)
	assert.Zero(t, broken, "the chain holds across the two stores")
}

// Under a server, the part an erasure rewrote is gone from the disk: the
// mutation leaves it outdated, and the vault table's settings have the
// cleanup thread remove it within seconds (ADR-0320 §SD4). clickhouse
// local runs no cleanup, so this needs the server binary.
func TestAnErasureLeavesNoOutdatedPartOnTheServer(t *testing.T) {
	fixedOpenings(t)
	ctx := context.Background()
	url := startScratchClickhouse(t)
	exec, err := storeexec.New(chclient.New(chclient.Config{URL: url, User: "default"}, nil), memory.NewGoAllocator())
	require.NoError(t, err)
	layout := agentrunstore.Layout{Database: "srvtest"}
	require.NoError(t, agentrunstore.ProvisionIn(ctx, exec, layout))
	require.NoError(t, agentrunstore.ProvisionBodyIn(ctx, exec, layout))
	ledger, vault := NewSqlStore(exec, layout), NewSqlBodyStore(exec, layout)
	defer ledger.Close()
	defer vault.Close()
	st := NewVaulted(ledger, vault)

	w, err := Open(ctx, st, Run{Id: "cell/srv", Cell: "cell", OwnerApp: "chat"})
	require.NoError(t, err)
	_, err = w.Append(ctx, Entry{Kind: KindUser, Body: "a name"}, Entry{Kind: KindOutcome, Key: "c1", Role: "tool", Body: `{"customer":"a name"}`})
	require.NoError(t, err)
	parts := func(active bool) (n uint64) {
		sql := "SELECT count() FROM system.parts WHERE database = 'srvtest' AND table = '" + agentrunstore.TableNameBody + "' AND active = " + strconv.FormatBool(active)
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			require.NoError(t, qerr)
			if rec.NumRows() > 0 {
				n = rec.Column(0).(*array.Uint64).Value(0)
			}
			rec.Release()
		}
		return
	}
	require.NotZero(t, parts(true), "the vault has a part")

	require.NoError(t, w.Erase(ctx, "person", 2))
	assert.EqualValues(t, 1, bodyCount(t, ctx, exec, layout, "cell/srv"), "the mutation applied before the call returned")
	deadline := time.Now().Add(30 * time.Second)
	for parts(false) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("an outdated part still holds the erased bytes after the cleanup period:\n%s", describe(t, ctx, exec,
				"SELECT name, active, remove_time, refcount FROM system.parts WHERE database = 'srvtest' AND table = '"+agentrunstore.TableNameBody+"'",
				"SELECT engine_full FROM system.tables WHERE database = 'srvtest' AND name = '"+agentrunstore.TableNameBody+"'"))
		}
		time.Sleep(250 * time.Millisecond)
	}
	assert.NotZero(t, parts(true), "the rewritten part is the only one")
	entries, err := st.Entries(ctx, "cell/srv", 0)
	require.NoError(t, err)
	r, _, err := st.GetRun(ctx, "cell/srv")
	require.NoError(t, err)
	broken, err := VerifyRun(r, entries)
	require.NoError(t, err)
	assert.Zero(t, broken)
}

// startScratchClickhouse starts a ClickHouse server for one test on free
// ports under a temp directory and returns its HTTP url; it is killed
// when the test ends. The server is needed where clickhouse-local will
// not do: here, for the cleanup thread.
func startScratchClickhouse(t *testing.T) (url string) {
	t.Helper()
	bin, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("no clickhouse binary on PATH")
	}
	dir := t.TempDir()
	for _, sub := range []string{"data", "tmp", "user_files", "access", "format_schemas"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755))
	}
	tcp, httpPort := freeTCPPort(t), freeTCPPort(t)
	config := fmt.Sprintf(`<clickhouse>
  <logger><level>error</level><console>true</console></logger>
  <listen_host>127.0.0.1</listen_host>
  <tcp_port>%d</tcp_port>
  <http_port>%d</http_port>
  <path>%s/data/</path>
  <tmp_path>%s/tmp/</tmp_path>
  <user_files_path>%s/user_files/</user_files_path>
  <format_schema_path>%s/format_schemas/</format_schema_path>
  <access_control_path>%s/access/</access_control_path>
  <users_config>%s/users.xml</users_config>
</clickhouse>
`, tcp, httpPort, dir, dir, dir, dir, dir, dir)
	users := `<clickhouse>
  <profiles><default/></profiles>
  <quotas><default/></quotas>
  <users>
    <default>
      <password></password>
      <networks><ip>127.0.0.1</ip><ip>::1</ip></networks>
      <profile>default</profile>
      <quota>default</quota>
    </default>
  </users>
</clickhouse>
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.xml"), []byte(config), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "users.xml"), []byte(users), 0o644))
	cmd := exec.Command(bin, "server", "--config-file="+filepath.Join(dir, "config.xml"))
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err = cmd.Start(); err != nil {
		t.Skipf("clickhouse server refused to start: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	url = "http://127.0.0.1:" + strconv.Itoa(httpPort) + "/"
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, perr := http.Get(url + "ping")
		if perr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Skipf("clickhouse server did not come up on %s", url)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// describe renders rows of diagnostic queries for a failure message.
func describe(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, sqls ...string) (out string) {
	t.Helper()
	for _, sql := range sqls {
		out += sql + "\n"
		for rec, err := range exec.QueryArrow(ctx, sql) {
			if err != nil {
				out += "  error: " + err.Error() + "\n"
				continue
			}
			for i := 0; i < int(rec.NumRows()); i++ {
				out += "  "
				for c := 0; c < int(rec.NumCols()); c++ {
					out += rec.Column(c).ValueStr(i) + " | "
				}
				out += "\n"
			}
			rec.Release()
		}
	}
	return
}
