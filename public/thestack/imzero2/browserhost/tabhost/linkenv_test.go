package tabhost

import (
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPageBase = "https://pages.example/boxer/demo/"

func refusedNames(refused []LinkEnvRefusal) (names []string) {
	for _, r := range refused {
		names = append(names, r.Name)
	}
	return
}

func TestAdmitLinkEnvRefusesWritesCredentialsAndForeignEndpoints(t *testing.T) {
	keep, refused := admitLinkEnv([]string{
		"BOXER_TAB_BASE=" + testPageBase,
		"IMZERO2_THEME=fresh",
		"BOXER_PLAY_FOCUS_MAP=1",
		"BOXER_PLAY_ALLOW_WRITES=1",
		"CLICKHOUSE_PASSWORD=x",
		"BOXER_LLM_APIKEY=x",
	}, nil)
	assert.ElementsMatch(t, []string{"BOXER_TAB_BASE", "IMZERO2_THEME", "BOXER_PLAY_FOCUS_MAP"}, keep)
	assert.ElementsMatch(t, []string{"BOXER_PLAY_ALLOW_WRITES", "CLICKHOUSE_PASSWORD", "BOXER_LLM_APIKEY"}, refusedNames(refused))
}

func TestAdmitLinkEnvEndpoint(t *testing.T) {
	for value, ok := range map[string]bool{
		"http://keelson.invalid/query":               true,
		"https://pages.example/ch/":                  true,
		"https://pages.example/other":                true,
		"http://pages.example/ch/":                   false, // another scheme is another origin
		"https://attacker.example/ch/":               false,
		"http://127.0.0.1:8123/":                     false,
		"https://user:pw@pages.example/ch/":          false,
		"/ch/":                                       false,
		"javascript:alert(1)":                        false,
		"http://something.invalid.attacker.example/": false,
	} {
		_, refused := admitLinkEnv([]string{"BOXER_TAB_BASE=" + testPageBase, "CLICKHOUSE_URL=" + value}, nil)
		assert.Equal(t, ok, len(refused) == 0, value)
	}
}

func TestAdmitLinkEnvAutorunOnlyForRead(t *testing.T) {
	for sql, ok := range map[string]bool{
		"":                                    true,
		"SELECT 1":                            true,
		"SELECT * FROM keelson('gfs_wind')":   true,
		"INSERT INTO t SELECT 1":              false,
		"SELECT * FROM url('http://x/', CSV)": false,
		"DROP TABLE t":                        false,
	} {
		env := []string{"BOXER_PLAY_AUTORUN=1"}
		if sql != "" {
			env = append(env, "BOXER_PLAY_SQL="+sql)
		}
		_, refused := admitLinkEnv(env, nil)
		assert.Equal(t, ok, !strings.Contains(strings.Join(refusedNames(refused), ","), "BOXER_PLAY_AUTORUN"), sql)
	}
}

func TestAdmitLinkEnvExtraNames(t *testing.T) {
	_, refused := admitLinkEnv([]string{"ACME_SEED=1"}, []string{"ACME_SEED"})
	assert.Empty(t, refused)
}

// workerHref matches a link's ?worker= value in the README and the published
// demo page.
var workerHref = regexp.MustCompile(`\?worker=([^"'<>\s]+)`)

// Every link the README and the demo page publish keeps every variable it
// sets: the policy must not break a published link.
func TestPublishedDemoLinksPassThePolicy(t *testing.T) {
	root, err := filepath.Abs("../../../../..")
	require.NoError(t, err)
	n := 0
	for _, rel := range []string{"README.md", ".github/pages/index.html"} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err, rel)
		for _, m := range workerHref.FindAllStringSubmatch(string(b), -1) {
			worker, err := url.QueryUnescape(html.UnescapeString(m[1]))
			require.NoError(t, err)
			_, rawQuery, _ := strings.Cut(worker, "?")
			q, err := url.ParseQuery(rawQuery)
			require.NoError(t, err, worker)
			env := append([]string{"BOXER_TAB_BASE=" + testPageBase}, q["env"]...)
			if !containsPrefix(env, "CLICKHOUSE_URL=") {
				env = append(env, "CLICKHOUSE_URL=https://pages.example/ch/")
			}
			_, refused := admitLinkEnv(env, nil)
			assert.Empty(t, refused, "%s: %s", rel, worker)
			n++
		}
	}
	require.Greater(t, n, 5, "the README and the demo page carry several links")
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestAdmitLinkEnvironmentUnsetsRefusedVariables(t *testing.T) {
	t.Setenv("BOXER_PLAY_ALLOW_WRITES", "1")
	t.Setenv("IMZERO2_THEME", "fresh")
	(&Program{}).admitLinkEnvironment()
	_, set := os.LookupEnv("BOXER_PLAY_ALLOW_WRITES")
	assert.False(t, set)
	assert.Equal(t, "fresh", os.Getenv("IMZERO2_THEME"))
}
