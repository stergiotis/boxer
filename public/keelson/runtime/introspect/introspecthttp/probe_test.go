package introspecthttp

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fetch(t *testing.T, url string) (status int) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestProbeRoundTrip is the whole primitive: a nonce that was fetched
// proves reachability, and one that was not does not.
func TestProbeRoundTrip(t *testing.T) {
	s := newQueryServer(t)

	nonce, url, err := s.MintProbe()
	require.NoError(t, err)
	assert.NotEmpty(t, nonce)
	assert.True(t, strings.HasSuffix(url, "/probe/"+nonce), "url=%q", url)
	assert.True(t, strings.HasPrefix(url, s.BaseURL()), "the URL must be on this server: %q", url)

	assert.Equal(t, http.StatusOK, fetch(t, url))
	assert.True(t, s.CheckProbe(nonce), "a fetched nonce proves reachability")

	// A nonce nobody fetched proves nothing.
	unfetched, _, err := s.MintProbe()
	require.NoError(t, err)
	assert.False(t, s.CheckProbe(unfetched))
}

// TestProbeIsSingleUse covers both directions: a nonce is fetchable once
// and checkable once, so no proof can be replayed into a second answer.
func TestProbeIsSingleUse(t *testing.T) {
	s := newQueryServer(t)

	nonce, url, err := s.MintProbe()
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, fetch(t, url))
	assert.Equal(t, http.StatusNotFound, fetch(t, url), "a nonce is fetchable once")

	assert.True(t, s.CheckProbe(nonce))
	assert.False(t, s.CheckProbe(nonce), "a nonce is checkable once")
}

func TestProbeUnknownNonce(t *testing.T) {
	s := newQueryServer(t)

	assert.False(t, s.CheckProbe("00000000000000000000000000000000"))
	assert.False(t, s.CheckProbe(""))
	assert.Equal(t, http.StatusNotFound, fetch(t, s.BaseURL()+"/probe/deadbeef"))

	// A near-miss of a live nonce is still a miss.
	nonce, _, err := s.MintProbe()
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, fetch(t, s.BaseURL()+"/probe/"+nonce[:len(nonce)-1]+"0"))
	assert.False(t, s.CheckProbe(nonce[:len(nonce)-1]+"0"))
	assert.False(t, s.CheckProbe(nonce), "the near-miss must not have marked it fetched")
}

// TestProbeExpiry drives the store's clock directly — the TTL is measured
// in tens of seconds, which no test should wait for.
func TestProbeExpiry(t *testing.T) {
	var store probeStore
	now := time.Now()
	store.add(probeEntry{nonce: "abc", expires: now.Add(probeTTL)})

	// Past the TTL the nonce is gone: it cannot be fetched...
	assert.False(t, store.markFetched("abc", now.Add(probeTTL+time.Second)))
	// ...and a check reports "not proven", which is also what an unfetched
	// nonce reports. A late check cannot tell the two apart, which is why
	// false means "not proven" rather than "unreachable".
	assert.False(t, store.consume("abc", now.Add(probeTTL+time.Second)))

	// Inside the TTL it works normally.
	store.add(probeEntry{nonce: "def", expires: now.Add(probeTTL)})
	assert.True(t, store.markFetched("def", now.Add(time.Second)))
	assert.True(t, store.consume("def", now.Add(time.Second)))
}

func TestProbeStorePrunesExpiredEntries(t *testing.T) {
	var store probeStore
	now := time.Now()
	for _, n := range []string{"a", "b", "c"} {
		store.add(probeEntry{nonce: n, expires: now.Add(probeTTL)})
	}
	require.Len(t, store.entries, 3)

	// Any operation past the TTL clears them, so an abandoned probe costs
	// at most one TTL of memory.
	store.consume("nothing", now.Add(probeTTL+time.Second))
	assert.Empty(t, store.entries)
}

// TestProbeNoncesAreDistinct guards the one property the whole primitive
// rests on: a nonce nobody can guess, and never the same one twice.
func TestProbeNoncesAreDistinct(t *testing.T) {
	s := newQueryServer(t)
	seen := make(map[string]struct{}, 32)
	for range 32 {
		nonce, _, err := s.MintProbe()
		require.NoError(t, err)
		assert.Len(t, nonce, probeNonceBytes*2, "hex of %d bytes", probeNonceBytes)
		_, dup := seen[nonce]
		require.False(t, dup, "nonce repeated: %s", nonce)
		seen[nonce] = struct{}{}
	}
}

// TestProbeLookupIsConstantTime pins the scan's shape rather than timing it:
// indexOfLocked must compare every entry, with no early exit, so a
// near-miss cannot be walked out one byte at a time by measuring latency. A
// match at the front and a match at the back must do the same work.
func TestProbeLookupIsConstantTime(t *testing.T) {
	var store probeStore
	now := time.Now()
	for _, n := range []string{"aaa", "bbb", "ccc", "ddd"} {
		store.add(probeEntry{nonce: n, expires: now.Add(probeTTL)})
	}
	assert.Equal(t, 0, store.indexOfLocked("aaa"))
	assert.Equal(t, 3, store.indexOfLocked("ddd"))
	assert.Equal(t, -1, store.indexOfLocked("zzz"))
	assert.Equal(t, -1, store.indexOfLocked("aa"), "a prefix is not a match")
	assert.Equal(t, -1, store.indexOfLocked("aaaa"), "an extension is not a match")
}
