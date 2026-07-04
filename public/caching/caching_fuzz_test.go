package caching

// Fuzz harness over the model driver in caching_model_test.go: the fuzz
// input is consumed as the op stream (including the configuration draws),
// and every oracle invariant applies. `go test` runs the seed corpus as
// unit tests; `go test -fuzz=FuzzCacheOps` explores further.

import "testing"

func FuzzCacheOps(f *testing.F) {
	// Hand-picked seeds: config prefix (stash, criteria, negCache) followed
	// by op bytes. The op encoding is (selector, key, extras...) — see
	// runCacheModel.
	f.Add([]byte{0, 0, 0, 0, 0, 1, 1, 11, 1, 4, 2, 30, 5, 2, 0, 2, 11, 3})
	f.Add([]byte{1, 2, 1, 8, 3, 2, 5, 3, 11, 7, 6, 3, 12, 2, 0, 3, 10, 1})
	// MarkAsStale → failed refresh → clock advance → recovery.
	f.Add([]byte{0, 1, 0, 4, 5, 14, 0, 5, 5, 3, 5, 11, 9, 12, 1, 14, 0, 11, 8, 0, 5})
	// Negative caching: absent probe → replay → TTL expiry → re-probe.
	f.Add([]byte{1, 0, 1, 0, 1, 11, 4, 12, 2, 0, 1, 11, 9})
	// Eviction pressure: fills L1 past capacity with epoch advances.
	f.Add([]byte{0, 0, 0, 4, 0, 7, 0, 4, 1, 7, 1, 4, 2, 7, 2, 4, 3, 7, 3, 4, 4, 7, 4, 4, 5, 7, 5, 0, 0, 0, 1, 0, 2})

	f.Fuzz(func(t *testing.T, data []byte) {
		runCacheModel(t, &byteOpSource{data: data})
	})
}
