//go:build llm_generated_opus47

package persist

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubjectFor_BuildsExpectedShape(t *testing.T) {
	assert.Equal(t, "runtime.persist.play.tabs.get", SubjectFor("play", "tabs", OpGet))
	assert.Equal(t, "runtime.persist.imztop.theme.set", SubjectFor("imztop", "theme", OpSet))
	assert.Equal(t, "runtime.persist.foo.bar.delete", SubjectFor("foo", "bar", OpDelete))
}

func TestPersistReply_RoundTrip(t *testing.T) {
	cases := []PersistReply{
		{Found: true, Value: []byte("hello")},
		{Error: "boom"},
		{},
		{Found: false},
	}
	for _, want := range cases {
		b, err := MarshalReply(want)
		require.NoError(t, err)
		got, err := UnmarshalReply(b)
		require.NoError(t, err)
		assert.Equal(t, want.Found, got.Found)
		assert.Equal(t, want.Error, got.Error)
		// The codec scalar-blob path doesn't preserve the nil vs
		// empty-slice distinction — both collapse to a zero-length
		// read. Compare by content, not by header identity.
		assert.True(t, bytes.Equal(want.Value, got.Value), "Value: got %x, want %x", got.Value, want.Value)
	}
}

func TestUnmarshalReply_RejectsGarbage(t *testing.T) {
	_, err := UnmarshalReply([]byte("not json"))
	require.Error(t, err)
}

func TestParsePersistSubject_OK(t *testing.T) {
	alias, key, op, ok := parsePersistSubject("runtime.persist.play.tabs.get")
	require.True(t, ok)
	assert.Equal(t, "play", alias)
	assert.Equal(t, "tabs", key)
	assert.Equal(t, "get", op)
}

func TestParsePersistSubject_Rejects(t *testing.T) {
	cases := []string{
		"",
		"runtime.persist",
		"runtime.persist.play",
		"runtime.persist.play.tabs",
		"runtime.persist.play.tabs.get.extra",
		"other.prefix.play.tabs.get",
		"runtime.persist..tabs.get",   // empty alias
		"runtime.persist.play..get",   // empty key
		"runtime.persist.play.tabs.",  // empty op
	}
	for _, s := range cases {
		_, _, _, ok := parsePersistSubject(s)
		assert.False(t, ok, "expected rejection: %q", s)
	}
}
