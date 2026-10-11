//go:build integration

package agentschema

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scratchServer is a ClickHouse server started for one test on free ports
// under a temp directory: its own data, access store and users, the
// custom_settings prefix the scope settings need, and the row-policy
// default under test. It is killed when the test ends.
type scratchServer struct {
	http string
}

func startScratchServer(t *testing.T, readRowsWithoutPolicies bool) (s scratchServer) {
	t.Helper()
	bin, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("no clickhouse binary on PATH")
	}
	dir := t.TempDir()
	for _, sub := range []string{"data", "tmp", "user_files", "access", "format_schemas"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o755))
	}
	tcp, httpPort := freePort(t), freePort(t)
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
  <custom_settings_prefixes>custom_</custom_settings_prefixes>
  <access_control_improvements>
    <users_without_row_policies_can_read_rows>%t</users_without_row_policies_can_read_rows>
  </access_control_improvements>
</clickhouse>
`, tcp, httpPort, dir, dir, dir, dir, dir, dir, readRowsWithoutPolicies)
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
	s.http = "http://127.0.0.1:" + strconv.Itoa(httpPort)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, perr := http.Get(s.http + "/ping")
		if perr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Skipf("clickhouse server did not come up on %s", s.http)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// query runs sql as user over HTTP and returns the TSV rows.
func (inst scratchServer) query(user string, sql string) (rows [][]string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.http+"/", strings.NewReader(sql))
	if err != nil {
		return
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Format", "TSV")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
		return
	}
	for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return
}

func (inst scratchServer) exec(t *testing.T, user string, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		_, err := inst.query(user, s)
		require.NoError(t, err, s)
	}
}

func (inst scratchServer) count(t *testing.T, user string, sql string) int64 {
	t.Helper()
	rows, err := inst.query(user, sql)
	require.NoError(t, err, sql)
	require.Len(t, rows, 1, sql)
	n, err := strconv.ParseInt(rows[0][0], 10, 64)
	require.NoError(t, err)
	return n
}

const (
	scratchAdmin = "default"
	scratchAgent = "agent42"
)

// seedJournal is the journal the server tests read: two conversations,
// three runs, the pinned run r1 with two entries; and a notes table with a
// withheld membership on one row of the pinned run.
func seedJournal(t *testing.T, s scratchServer) {
	t.Helper()
	s.exec(t, scratchAdmin,
		"CREATE DATABASE IF NOT EXISTS harness",
		"CREATE DATABASE IF NOT EXISTS scratch",
		"CREATE TABLE agent_runs (id String, conversation String, owner_app String) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE agent_run_entries (run String, ordinal UInt64, at_ms Int64, kind String, key String, role String, body String, calls String, body_digest String, calls_digest String, name String, book String, form String, target UInt64, `from` UInt64, effect String, turn String, label_level String, label_tier String, label_exposure Array(String), label_origin Array(String), writer_run String, writer_turn String, writer_ordinal UInt64, prev UInt64, digest String, key_id String, parent UInt64, attachments String) ENGINE = MergeTree ORDER BY (run, ordinal)",
		"INSERT INTO agent_runs VALUES ('r1', 'c1', 'chat'), ('r2', 'c1', 'chat'), ('r3', 'c2', 'other')",
		"INSERT INTO agent_run_entries (run, ordinal, kind, body, digest) VALUES ('r1', 1, 'user', 'hello', 'd1'), ('r1', 2, 'reply', 'hi', 'd2'), ('r2', 1, 'user', 'other run', 'd3'), ('r3', 1, 'user', 'other conversation', 'd4')",
		"CREATE TABLE scratch.notes (run String, ord UInt64, note String, contact String, secret String, memberships Array(String)) ENGINE = MergeTree ORDER BY ord",
		"INSERT INTO scratch.notes VALUES ('r1', 1, 'a', 'alice@x', 's', ['personal', 'public']), ('r1', 2, 'b', 'bob', 's', ['public']), ('r2', 3, 'c', 'carol', 's', ['credential'])",
	)
}

// Against a server (ADR-0313 §SD2, §SD3, law L18): the principal reads
// each relation at its own scope and nothing else — the three backstops on
// one base table do not AND together; a definer view written without its
// scope is still cut down by its backstop; the pinned scope cannot be
// moved from the agent's session; and the preflight passes where the
// server reads through a restrictive policy by default.
func TestTheServerPrincipalReadsEachRelationAtItsScope(t *testing.T) {
	s := startScratchServer(t, true)
	seedJournal(t, s)

	setup, probe, teardown := ServerPreflight("harness")
	s.exec(t, scratchAdmin, setup...)
	assert.NoError(t, PreflightCount(s.count(t, scratchAdmin, probe)), "the server reads through a restrictive policy by default")
	s.exec(t, scratchAdmin, teardown...)

	relations := append(JournalRelations(), withholding())
	scopes := Scopes{ScopeSetting: "r1", ScopeConversation: "c1", ScopeApp: "chat"}
	stmts, err := ServerDDL(Principal{User: scratchAgent, Profile: scratchAgent + "_p", Database: "harness"}, relations, scopes)
	require.NoError(t, err)
	s.exec(t, scratchAdmin, stmts...)

	assert.EqualValues(t, 2, s.count(t, scratchAgent, "SELECT count() FROM harness.my_run"), "the pinned run's rows alone")
	rows, err := s.query(scratchAgent, "SELECT DISTINCT run FROM harness.my_conversation")
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"r2"}}, rows, "the conversation's other run, not the pinned one and not another conversation's")
	assert.EqualValues(t, 3, s.count(t, scratchAgent, "SELECT count() FROM harness.agent_history"), "the app's runs, all three relations on one table")
	rows, err = s.query(scratchAgent, "SELECT ord, contact FROM harness.notes ORDER BY ord")
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1", ""}, {"2", "bob"}}, rows, "withheld, not absent: the contact is blank on the withheld row")

	_, err = s.query(scratchAgent, "SELECT count() FROM agent_run_entries")
	assert.Error(t, err, "the base table is not the agent's to read")

	// A definer view written without its scope: the backstop fires inside
	// it (law L18) and the agent still sees the pinned run alone.
	s.exec(t, scratchAdmin, "CREATE OR REPLACE VIEW harness.my_run SQL SECURITY DEFINER DEFINER = `default` AS SELECT * FROM agent_run_entries SETTINGS `custom_agent_relation` = 'my_run'")
	assert.EqualValues(t, 2, s.count(t, scratchAgent, "SELECT count() FROM harness.my_run"), "the backstop cuts a view written without its scope")
	assert.EqualValues(t, 0, s.count(t, scratchAgent, "SELECT count() FROM harness.my_run WHERE run != 'r1'"))

	// The scope is pinned READONLY: the agent cannot move it from its query
	// or its session, and the marker set on the query does not reach the
	// view's own.
	_, err = s.query(scratchAgent, "SELECT count() FROM harness.my_run SETTINGS custom_agent_run = 'r2'")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "452", "SETTING_CONSTRAINT_VIOLATION")
	assert.EqualValues(t, 1, s.count(t, scratchAgent, "SELECT count() FROM harness.my_conversation SETTINGS custom_agent_relation = 'my_run'"), "the view's marker wins over the query's")

	// The admin's read of the base table is unfiltered: a backstop folds to
	// a constant outside its relation.
	assert.EqualValues(t, 4, s.count(t, scratchAdmin, "SELECT count() FROM agent_run_entries"))
}

// With users_without_row_policies_can_read_rows off, restrictive policies
// with no permissive one beside them hide every row from every reader,
// with no refusal: the preflight counts none and refuses the setup, which
// is why it exists.
func TestThePreflightRefusesAServerThatHidesRowsByDefault(t *testing.T) {
	s := startScratchServer(t, false)
	seedJournal(t, s)
	setup, probe, teardown := ServerPreflight("harness")
	s.exec(t, scratchAdmin, setup...)
	n := s.count(t, scratchAdmin, probe)
	s.exec(t, scratchAdmin, teardown...)
	assert.EqualValues(t, 0, n)
	assert.ErrorContains(t, PreflightCount(n), "users_without_row_policies_can_read_rows")
}
