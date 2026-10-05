package tabhost

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The flags bundle builds with are the ones go-build-env.sh gives every other
// shipped binary (ADR-0215); this test is what keeps the two definitions one.
func TestBuildFlagsMatchTheShellEnv(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "scripts", "dev", "go-build-env.sh"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^BOXER_GO_FLAGS="([^"]*)"`).FindSubmatch(b)
	require.NotNil(t, m, "go-build-env.sh sets BOXER_GO_FLAGS")
	require.Equal(t, strings.Fields(string(m[1])), goBuildFlags)
	for _, kv := range goBuildEnv {
		require.Regexp(t, regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(kv)+`$`), string(b))
	}
}

// The module's tags and toolchain pin are read as the shell env reads them.
func TestModuleTagsAndToolchain(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, "", readTags(dir))
	require.Equal(t, "", goModVersion(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tags"), []byte("a,b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme\n\ngo 1.27.0\n\nrequire x v1\n"), 0o644))
	require.Equal(t, "a,b", readTags(dir))
	require.Equal(t, "1.27.0", goModVersion(dir))
}
