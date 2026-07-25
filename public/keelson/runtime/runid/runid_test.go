package runid

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMintCarriesEveryComponent pins the shape, since the whole change is
// that the host is now in it.
func TestMintCarriesEveryComponent(t *testing.T) {
	id := Mint("play", "main")

	parts := strings.Split(id, "-")
	require.Len(t, parts, 5, "id=%q", id)
	assert.Equal(t, "play", parts[0])
	assert.Equal(t, "main", parts[1])
	assert.Equal(t, HostToken(), parts[2], "the host component is what makes the id safe on a shared channel")
	assert.Equal(t, strconv.Itoa(os.Getpid()), parts[3])
	assert.True(t, Valid(id), "a minted id must pass the consumer's own check")
}

func TestMintDisambiguatesRuns(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for range 64 {
		id := Mint("play", "main")
		_, dup := seen[id]
		require.False(t, dup, "sequence repeated: %s", id)
		seen[id] = struct{}{}
	}
}

// TestMintSanitisesTheLabel covers the case that made sanitising necessary:
// a lane bound to a graph node carries that node's id as its label, and a
// node id is not a literal anybody vetted.
func TestMintSanitisesTheLabel(t *testing.T) {
	id := Mint("play", "bound-node'; DROP--")
	assert.True(t, Valid(id), "id=%q", id)
	assert.NotContains(t, id, "'", "a quote must never reach a statement built around the id")
	assert.NotContains(t, id, " ")

	// Subject wildcards likewise cannot be introduced by a label.
	id = Mint("play", "lane.*>")
	assert.True(t, Valid(id), "id=%q", id)
	assert.NotContains(t, id, "*")
	assert.NotContains(t, id, ">")
}

func TestMintBoundsLength(t *testing.T) {
	id := Mint(strings.Repeat("a", 200), strings.Repeat("b", 200))
	assert.LessOrEqual(t, len(id), MaxLen)
	assert.True(t, Valid(id), "even a truncated id must remain usable")
}

func TestToken(t *testing.T) {
	assert.Equal(t, "plain", Token("plain"))
	assert.Equal(t, "with_space", Token("with space"))
	assert.Equal(t, "a_b_c", Token("a.b:c"), "'.' and ':' cannot fake a subject separator")
	assert.Equal(t, "local", Token(""), "a component is never absent")
	// Sanitised-away is not absent: every character maps to one character,
	// so the fallback is only for genuinely empty input.
	assert.Equal(t, "____", Token("...."))
	assert.Len(t, Token(strings.Repeat("x", 100)), maxTokenLen)
}

func TestHostTokenIsUsable(t *testing.T) {
	tok := HostToken()
	assert.NotEmpty(t, tok)
	assert.Equal(t, tok, Token(tok), "the cached token must already be sanitised")
	assert.True(t, Valid(tok))
}

func TestValid(t *testing.T) {
	for _, ok := range []string{"play-main-box-7-3", "a", "a.b:c_d-e"} {
		assert.True(t, Valid(ok), "id=%q", ok)
	}
	for _, bad := range []string{
		"",                       // no id at all
		"has space",              // subject tokens have no spaces
		"quote'injection",        // would reach into a statement
		"wild*card", "deep>path", // would reach into the subject namespace
		strings.Repeat("x", MaxLen+1),
	} {
		assert.False(t, Valid(bad), "id=%q", bad)
	}
}
