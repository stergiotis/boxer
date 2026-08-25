package chstore_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
)

const (
	configFromEnvChildKey = "CHSTORE_CONFIG_FROM_ENV_CHILD"
	configFromEnvMarker   = "RESOLVED\t"
	envDefaultsMarker     = "FILLED\t"
)

// TestConfigFromEnvChild is the child half of TestConfigFromEnv_Credentials
// and TestConfigWithEnvDefaults_FillsCredentials: it prints the two resolved
// configs on marker lines and is skipped in a normal run.
func TestConfigFromEnvChild(t *testing.T) {
	if os.Getenv(configFromEnvChildKey) != "1" {
		t.Skip("child-process helper for the ConfigFromEnv tests")
	}
	c := chstore.ConfigFromEnv()
	fmt.Printf("\n%s%s\n", configFromEnvMarker, joinCfg(c))
	// A config the caller assembled for its own reasons: a run identity and a
	// scratch database, no connection coordinates.
	f := chstore.ConfigWithEnvDefaults(chstore.Config{RunId: "run-x", Database: "scratch"})
	fmt.Printf("\n%s%s\n", envDefaultsMarker, joinCfg(f))
}

func joinCfg(c chstore.Config) string {
	return strings.Join([]string{c.URL, c.User, c.Password, c.Database, c.Table, c.RunId}, "\t")
}

// The registry resolves each entry once and caches it, so the environment
// cannot be varied in-process — the first case to run would fix the values for
// every later one. Both tests therefore read a fresh child of this test binary,
// with inherited CLICKHOUSE_* scrubbed so a developer's shell cannot colour the
// result.
func TestConfigFromEnv_Credentials(t *testing.T) {
	def := chstore.Defaults()

	bare := runConfigChild(t, nil, configFromEnvMarker)
	assert.Equal(t, joinCfg(def), bare, "no CLICKHOUSE_* set must match Defaults")

	authed := runConfigChild(t, []string{
		"CLICKHOUSE_ENDPOINT=http://ch:8123",
		"CLICKHOUSE_USER=alice",
		"CLICKHOUSE_PASSWORD=hunter2",
	}, configFromEnvMarker)
	want := def
	want.URL, want.User, want.Password = "http://ch:8123", "alice", "hunter2"
	assert.Equal(t, joinCfg(want), authed)

	// CLICKHOUSE_DATABASE does not move the facts table: it is a fixed
	// location in the schema, not the session's default database.
	dbSet := runConfigChild(t, []string{"CLICKHOUSE_DATABASE=somewhere_else"}, configFromEnvMarker)
	assert.Equal(t, joinCfg(def), dbSet)
}

func TestConfigWithEnvDefaults_FillsCredentials(t *testing.T) {
	def := chstore.Defaults()

	got := runConfigChild(t, []string{"CLICKHOUSE_PASSWORD=hunter2"}, envDefaultsMarker)
	want := chstore.Config{
		URL:      def.URL,
		User:     def.User,
		Password: "hunter2",
		Database: "scratch", // caller's, not the env's or the default's
		Table:    def.Table,
		RunId:    "run-x", // survives — the pre-fix Defaults() swap dropped it
	}
	assert.Equal(t, joinCfg(want), got)
}

// A config that names every field is returned unchanged, whatever the
// environment says.
func TestConfigWithEnvDefaults_KeepsCallerFields(t *testing.T) {
	in := chstore.Config{
		URL:      "http://pinned:8123/",
		User:     "reader",
		Password: "pinned-secret",
		Database: "scratch",
		Table:    "facts2",
		RunId:    "run-y",
	}
	assert.Equal(t, in, chstore.ConfigWithEnvDefaults(in))
}

func runConfigChild(t *testing.T, extra []string, marker string) (line string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConfigFromEnvChild$", "-test.v")
	cmd.Env = append(scrubClickHouseEnv(os.Environ()), configFromEnvChildKey+"=1")
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	for l := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(l, marker); ok {
			return strings.TrimRight(after, "\r")
		}
	}
	require.FailNow(t, "child printed no "+marker+" line", string(out))
	return
}

func scrubClickHouseEnv(environ []string) (out []string) {
	out = make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "CLICKHOUSE_") {
			continue
		}
		out = append(out, kv)
	}
	return
}
