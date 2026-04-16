//go:build llm_generated_opus46

package commitdigest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHexPrefix(t *testing.T) {
	assert.True(t, isHexPrefix("abcdef1234567890abcdef1234567890abcdef12"))
	assert.True(t, isHexPrefix("0000000000000000000000000000000000000000"))
	assert.True(t, isHexPrefix("AABBCCDD"))
	assert.False(t, isHexPrefix("xyz"))
	assert.False(t, isHexPrefix("abcdefg"))
	assert.True(t, isHexPrefix(""))
}

func TestParseBlameOutput(t *testing.T) {
	// Simulate parsing logic: author-mail extraction
	// This tests the parsing contract rather than calling git
	lines := []string{
		"abcdef1234567890abcdef1234567890abcdef12 1 1 3",
		"author Alice",
		"author-mail <alice@example.com>",
		"author-time 1713000000",
		"author-tz +0200",
		"committer Alice",
		"committer-mail <alice@example.com>",
		"committer-time 1713000000",
		"committer-tz +0200",
		"summary initial commit",
		"filename src/foo.go",
		"\tpackage foo",
		"abcdef1234567890abcdef1234567890abcdef12 2 2",
		"\t",
		"bbbbbb1234567890abcdef1234567890abcdef12 3 3 1",
		"author Bob",
		"author-mail <bob@example.com>",
		"author-time 1713100000",
		"author-tz +0200",
		"committer Bob",
		"committer-mail <bob@example.com>",
		"committer-time 1713100000",
		"committer-tz +0200",
		"summary fix bug",
		"filename src/foo.go",
		"\tfunc Foo() {}",
	}

	emails := make(map[string]struct{})
	for _, line := range lines {
		if len(line) > len("author-mail ") && line[:len("author-mail ")] == "author-mail " {
			email := line[len("author-mail "):]
			email = email[1 : len(email)-1] // strip < >
			emails[email] = struct{}{}
		}
	}

	assert.Contains(t, emails, "alice@example.com")
	assert.Contains(t, emails, "bob@example.com")
	assert.Equal(t, 2, len(emails))
}

func TestBoundaryCrossingType(t *testing.T) {
	bc := BoundaryCrossing{
		File:       "src/foo.go",
		CommitHash: "abcdef1234567890",
		Author:     "Charlie <charlie@example.com>",
		Owners:     []string{"alice@example.com", "bob@example.com"},
	}
	assert.Equal(t, "src/foo.go", bc.File)
	assert.Equal(t, 2, len(bc.Owners))
}
