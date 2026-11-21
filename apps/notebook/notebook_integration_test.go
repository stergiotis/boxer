//go:build integration

package notebook

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
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// scratchServer starts a ClickHouse server of the test's own in a temporary
// directory and points the environment at it, so nothing here reaches a
// server a person runs.
func scratchServer(t *testing.T) (url string) {
	t.Helper()
	bin, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse is not installed")
	}
	freePort := func() int {
		l, lErr := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, lErr)
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	dir := t.TempDir()
	tcp, httpPort := freePort(), freePort()
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
      <access_management>1</access_management>
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
		resp, pErr := http.Get(url + "ping")
		if pErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Skipf("clickhouse server did not come up on %s", url)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Setenv("CLICKHOUSE_URL", url)
	t.Setenv("CLICKHOUSE_ENDPOINT", "")
	t.Setenv("CLICKHOUSE_USER", "")
	t.Setenv("CLICKHOUSE_PASSWORD", "")
	return url
}

// The whole of a collection against a server and a lading store, both the
// test's own: the records run on the server, the store's write path takes
// the tree under the stated tier, and the window opens the snapshot that
// was written and reads it as sealed. A second collection writes the same
// mount again.
func TestCollectAgainstAServerAndTheLadingStore(t *testing.T) {
	url := scratchServer(t)
	t.Cleanup(egui2test.Install())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exec, err := storeexec.ConnectFromEnv(ctx)
	require.NoError(t, err)
	require.NoError(t, lading.Provision(ctx, exec, ladingschema.ProfileCorpus))
	// A mount's policy is a row of boxer.facts, which no store provisions:
	// the DDL generated beside the policy store is the schema it writes.
	ddl, err := os.ReadFile(filepath.Join("..", "..", "public", "fs", "lading", "ladingpolicy", "facts_ddl_clickhouse.out.sql"))
	require.NoError(t, err)
	for _, statement := range strings.SplitN(string(ddl), ";", 2) {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		resp, pErr := http.Post(url, "text/plain", strings.NewReader(statement))
		require.NoError(t, pErr)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Equalf(t, http.StatusOK, resp.StatusCode, "%s", body)
	}

	bus := inprocbus.NewInst(zerolog.Nop())
	pub, err := ladingpublish.NewService(ladingpublish.Config{Bus: bus, Registry: app.DefaultRegistry,
		Connect: func(ctx context.Context) (recordstore.ExecutorI, error) { return storeexec.ConnectFromEnv(ctx) },
		Limits:  ladingpublish.Limits{Ceiling: agentrun.TierAmber, MaxTtlDays: 90, MaxBytes: 1 << 24}, Log: zerolog.Nop()})
	require.NoError(t, err)
	t.Cleanup(pub.Close)

	inst := newApp()
	inst.bus = bus.NewClient(AppId, manifest.Caps)
	inst.ioCtx = t.Context()
	h := inst.Operations()
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "report"}))

	collect := func(tier string) {
		t.Helper()
		require.NoError(t, command(t, h, opCollectNotebook, CollectArgs{Tier: tier}))
		deadline := time.Now().Add(90 * time.Second)
		for inst.collect.done < inst.collect.seq {
			require.True(t, time.Now().Before(deadline), "the collection did not end in time")
			inst.landCollect()
			time.Sleep(5 * time.Millisecond)
		}
	}
	follow := func() {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for inst.opening != nil || !inst.checked {
			require.True(t, time.Now().Before(deadline), "the snapshot was not opened in time: %s", inst.note)
			inst.poll()
			time.Sleep(5 * time.Millisecond)
		}
	}

	// A tier above the store's ceiling is refused by the store, after the
	// records ran and before anything is written.
	collect("RED")
	require.Contains(t, inst.collect.lastErr, "lading publish", inst.collect.lastErr)
	require.Nil(t, inst.opening)

	collect("GREEN")
	require.Empty(t, inst.collect.lastErr)
	require.NotNil(t, inst.collect.last)
	first := inst.collect.last.ref
	assert.Equal(t, "report.md", first.path)
	assert.Len(t, inst.collect.last.records, 5)
	follow()
	require.NotNil(t, inst.doc)
	assert.Equal(t, "snapshot "+first.String(), inst.doc.origin, inst.note)
	assert.False(t, inst.doc.edited)
	assert.True(t, inst.check.Sealed(), "%+v", inst.check)
	// The figures are what the server answered, the ones the sample holds.
	assert.Contains(t, string(inst.doc.text), "[519.5 ms](val:stats.worst_ms")
	assert.True(t, strings.Contains(string(inst.doc.text), "report.blobs/"))

	// The snapshot's notebook collected again: the same mount, a later
	// snapshot.
	collect("AMBER")
	require.Empty(t, inst.collect.lastErr)
	second := inst.collect.last.ref
	assert.Equal(t, first.mount, second.mount)
	assert.True(t, second.snap.After(first.snap))
	follow()
	assert.Equal(t, "snapshot "+second.String(), inst.doc.origin)
	assert.True(t, inst.check.Sealed(), "%+v", inst.check)
	require.NoError(t, inst.Unmount(nil))

	// The store lists the notebook under its mount's name, at its newest
	// snapshot, and another window opens it from the list's name for it.
	sc, err := connectLading(ctx)
	require.NoError(t, err)
	l, err := sc.list(ctx)
	sc.close()
	require.NoError(t, err)
	require.Len(t, l.files, 1)
	assert.Equal(t, "report", l.files[0].mount)
	assert.Equal(t, second.String(), l.files[0].ref.String())

	other := newApp()
	require.ErrorContains(t, command(t, other.Operations(), opOpenSnapshot, OpenSnapshotArgs{Snapshot: "not a ref"}), "a snapshot is named")
	require.NoError(t, command(t, other.Operations(), opOpenSnapshot, OpenSnapshotArgs{Snapshot: l.files[0].ref.String()}))
	deadline := time.Now().Add(60 * time.Second)
	for other.opening != nil || !other.checked {
		require.True(t, time.Now().Before(deadline), "the snapshot was not opened in time: %s", other.docErr)
		other.poll()
		time.Sleep(5 * time.Millisecond)
	}
	assert.Equal(t, "snapshot "+second.String(), other.doc.origin)
	assert.True(t, other.check.Sealed(), "%+v", other.check)
	doc, err := query[Document](t, other.Operations(), opGetDocument)
	require.NoError(t, err)
	assert.Len(t, doc.Records, 5)
	require.NoError(t, other.Unmount(nil))

	// The text edited elsewhere and handed back with its snapshot: it is
	// shown over the snapshot's stored results, and the record it changed
	// is stale until the notebook is collected.
	edited := strings.Replace(doc.Text, "numbers(96)", "numbers( 96 )", 1)
	require.NotEqual(t, doc.Text, edited)
	raw, err := buscodec.Encode(launchcfg.NotebookLaunch{Text: edited, Name: "report.md",
		Mount: second.mount.Value(), SnapNanos: second.snap.UnixNano(), Path: second.path})
	require.NoError(t, err)
	back := newApp()
	handed, err := back.handOver(raw)
	require.NoError(t, err)
	require.True(t, handed)
	deadline = time.Now().Add(60 * time.Second)
	for back.opening != nil || !back.checked {
		require.True(t, time.Now().Before(deadline), "the handed-over notebook was not opened in time: %s", back.docErr)
		back.poll()
		time.Sleep(5 * time.Millisecond)
	}
	assert.Equal(t, edited, string(back.doc.text))
	assert.True(t, back.doc.edited)
	assert.Equal(t, []string{"p99_by_hour"}, back.check.Stale)
	assert.Empty(t, back.check.Problems, "the stored results are found beside the snapshot's text")
	require.NoError(t, back.Unmount(nil))
}

// A live run against a server of the test's own, through the path a window
// takes: readonly and the stamp ride the request and the server takes them,
// the report sample is shown over what the server computed, and a record
// that would write is refused by the server, not only by the class gate.
func TestRunLiveAgainstAServer(t *testing.T) {
	url := scratchServer(t)
	t.Cleanup(egui2test.Install())
	inst := newApp()
	h := inst.Operations()
	inst.run = serverRun(chclient.Config{URL: url, User: "default"}, string(AppId), "run-live", 1, LiveMaxBytes)
	require.NoError(t, command(t, h, opOpenSample, OpenSampleArgs{Name: "report"}))
	read(t, inst)
	require.NoError(t, command(t, h, opRunLive, struct{}{}))
	ran(t, inst)
	require.Empty(t, inst.pane.lastErr)
	st, err := query[LiveState](t, h, opGetReading)
	require.NoError(t, err)
	assert.Equal(t, readingNameLive, st.Reading)
	assert.Equal(t, 5, st.Records)
	assert.Empty(t, st.Problems)
	assert.Empty(t, st.Refused)

	// The gate is the class; readonly is what holds behind it.
	_, err = inst.run(context.Background(), "CREATE TABLE t (x UInt8) ENGINE = Memory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "readonly")
}
