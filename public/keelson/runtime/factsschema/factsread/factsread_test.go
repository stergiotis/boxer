package factsread

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

func TestPrepare_ResolvesHandlesAndExpandsExtraction(t *testing.T) {
	out, err := Prepare("SELECT `id:id`, "+Scalar("symbol", vocab.MembRuntimeApp)+", "+
		ListFirst("stringArray", vocab.MembLogMessage)+" FROM boxer.facts", "boxer.facts")
	require.NoError(t, err)
	require.Contains(t, out, `"id:id:u64:`)
	require.Contains(t, out, "LW_VALUE_BY_TAG_EQUAL(")
	require.Contains(t, out, "LW_LIST_BY_TAG_EQUAL(")
	require.NotContains(t, out, "LW_GET")
	require.NotContains(t, out, "runtime-app", "the membership name is resolved to its id before the statement ships")
}

func TestPrepare_FailsHereOnWhatTheSchemaDoesNotKnow(t *testing.T) {
	_, err := Prepare("SELECT 1 FROM facts", "facts")
	require.Error(t, err, "an unqualified table")
	_, err = Prepare("SELECT LW_GET('noSuchSection', 'runtime-app', '"+PlainChannel+"') FROM boxer.facts", "boxer.facts")
	require.Error(t, err, "an unknown section")
	_, err = Prepare("SELECT LW_GET('symbol', 'no-such-membership', '"+PlainChannel+"') FROM boxer.facts", "boxer.facts")
	require.Error(t, err, "an unknown membership")
}
