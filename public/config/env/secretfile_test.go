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
	setForTest(t, companion, path)

	setForTest(t, secret, "")
	assert.Equal(t, "s3cret", secret.Get())
	assert.Equal(t, ValueSourceFile, secret.ValueSource())

	// The variable itself wins over the file.
	setForTest(t, secret, "direct")
	assert.Equal(t, "direct", secret.Get())
	assert.Equal(t, ValueSourceEnv, secret.ValueSource())
}

func TestUnreadableSecretFileResolvesAsUnset(t *testing.T) {
	secret, companion := newSecretFixture(t)
	setForTest(t, companion, filepath.Join(t.TempDir(), "missing"))
	setForTest(t, secret, "")
	assert.Equal(t, "", secret.Get())
	assert.Equal(t, ValueSourceDefault, secret.ValueSource())
}

func TestFileVarExposesTheCompanion(t *testing.T) {
	secret, companion := newSecretFixture(t)
	assert.Same(t, companion, secret.FileVar())
	assert.Equal(t, "ENV_TEST_SECRETFILE_SECRET", companion.CompanionOf())
	plain, _ := LookupVar("ENV_TEST_SECRETFILE_PLAIN")
	assert.Nil(t, plain.(*StringVar).FileVar())
}

func TestResolveReportsTheFileAsTheSource(t *testing.T) {
	secret, companion := newSecretFixture(t)
	path := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(path, []byte("s3cret"), 0o600))
	setForTest(t, companion, path)
	setForTest(t, secret, "")
	value, src := secret.Resolve()
	assert.Equal(t, "s3cret", value)
	assert.Equal(t, ValueSourceFile, src)
	_, set := secret.Lookup()
	assert.False(t, set, "Lookup sees the environment only, so a program can tell a file from a direct value")
}

// A package that declares NAME_FILE itself, before or after the Sensitive
// NAME, gets the companion rather than a conflict.
func TestExplicitCompanionDeclarationJoinsTheCompanion(t *testing.T) {
	explicit := Spec{Name: "ENV_TEST_JOIN_SECRET_FILE", Description: "the program's own description", CliFlagName: "secretFile"}
	for _, explicitFirst := range []bool{false, true} {
		resetRegistryForTest()
		var declared *PathVar
		if explicitFirst {
			declared = NewPath(explicit)
		}
		secret := NewString(Spec{Name: "ENV_TEST_JOIN_SECRET", Description: "a sensitive fixture", Sensitive: true})
		if !explicitFirst {
			declared = NewPath(explicit)
		}
		require.NoError(t, RegistrationErrors(), "explicitFirst=%v", explicitFirst)
		assert.Same(t, secret.FileVar(), declared, "explicitFirst=%v", explicitFirst)
		assert.Equal(t, "ENV_TEST_JOIN_SECRET", declared.CompanionOf())
		assert.Equal(t, "the program's own description", declared.Spec().Description, "the explicit declaration's metadata stands")
		assert.Equal(t, "secretFile", declared.Spec().CliFlagName)

		path := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(path, []byte("s3cret"), 0o600))
		setForTest(t, declared, path)
		setForTest(t, secret, "")
		assert.Equal(t, "s3cret", secret.Get())
	}
}

func TestIncompatibleCompanionDeclarationNamesTheMechanism(t *testing.T) {
	resetRegistryForTest()
	NewString(Spec{Name: "ENV_TEST_CLASH_SECRET", Description: "a sensitive fixture", Sensitive: true})
	NewPath(Spec{Name: "ENV_TEST_CLASH_SECRET_FILE", Description: "with a default", Default: "/etc/secret"})
	err := RegistrationErrors()
	require.ErrorIs(t, err, ErrConflictingDeclaration)
	assert.Contains(t, err.Error(), "companion of Sensitive ENV_TEST_CLASH_SECRET")
	assert.Contains(t, err.Error(), "FileVar()")
}
