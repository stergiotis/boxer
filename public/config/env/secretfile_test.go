package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSecretFixture registers a Sensitive and a plain variable on a fresh
// registry and returns the Sensitive one with its NAME_FILE companion.
func newSecretFixture(t *testing.T) (secret *StringVar, companion *PathVar) {
	t.Helper()
	resetRegistryForTest()
	secret = NewString(Spec{Name: "ENV_TEST_SECRETFILE_SECRET", Description: "a sensitive fixture", Sensitive: true})
	NewString(Spec{Name: "ENV_TEST_SECRETFILE_PLAIN", Description: "a plain fixture"})
	v, ok := LookupVar("ENV_TEST_SECRETFILE_SECRET_FILE")
	require.True(t, ok, "a Sensitive variable registers NAME_FILE")
	companion = v.(*PathVar)
	return
}

func TestSensitiveVariableGetsAFileCompanion(t *testing.T) {
	_, companion := newSecretFixture(t)
	assert.Equal(t, TypePath, companion.Spec().Type)
	assert.False(t, companion.Spec().Sensitive)
	_, ok := LookupVar("ENV_TEST_SECRETFILE_PLAIN_FILE")
	assert.False(t, ok, "a plain variable does not")
}

func TestSecretIsReadFromTheFileWhenUnset(t *testing.T) {
	secret, companion := newSecretFixture(t)
	path := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(path, []byte("s3cret\n"), 0o600))
	companion.SetForTest(t, path)

	secret.SetForTest(t, "")
	assert.Equal(t, "s3cret", secret.Get())
	assert.Equal(t, ValueSourceFile, secret.ValueSource())

	// The variable itself wins over the file.
	secret.SetForTest(t, "direct")
	assert.Equal(t, "direct", secret.Get())
	assert.Equal(t, ValueSourceEnv, secret.ValueSource())
}

func TestUnreadableSecretFileResolvesAsUnset(t *testing.T) {
	secret, companion := newSecretFixture(t)
	companion.SetForTest(t, filepath.Join(t.TempDir(), "missing"))
	secret.SetForTest(t, "")
	assert.Equal(t, "", secret.Get())
	assert.Equal(t, ValueSourceDefault, secret.ValueSource())
}
