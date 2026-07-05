package caching

// Spec-trace conformance: replays ITF traces exported from the Quint model
// (verification/formal/caching/versioned_cache.qnt) against the real
// implementation and asserts state agreement after every step — the
// mechanical binding between the spec and the code. Traces live in
// testdata/itf/ and are regenerated with `npm run traces` in the spec
// directory.
//
// The replay drives the public API wherever one exists (write-through
// Commit/Flush, reads, misses) and the exact internal transition where the
// spec action is finer-grained than the public surface: fetchReturn(k)
// delivers ONE key through admit() (performFetch flushes whole partitions),
// and demote/drop use demoteToStash / the stash directly (the adversarial
// eviction the spec quantifies over; a policy picks victims randomly).
// Inference note: a re-serve of an already-served version leaves the spec
// state unchanged (a stutter) and is skipped — state agreement is
// unaffected.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- ITF decoding (the subset the versioned_cache spec emits) ----------

type specEntry struct {
	present bool
	ver     int64
	pinned  bool
}

type specState struct {
	l1, l2   map[string]specEntry
	durable  map[string]int64
	written  map[string]int64
	served   map[string]int64
	fetchq   map[string]bool
	monotone bool
}

func itfInt(t *testing.T, v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case map[string]any:
		n, err := strconv.ParseInt(x["#bigint"].(string), 10, 64)
		require.NoError(t, err)
		return n
	}
	t.Fatalf("unexpected ITF int encoding: %T", v)
	return 0
}

func itfPairs(t *testing.T, v any) [][2]any {
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected #map, got %T", v)
	raw, ok := m["#map"].([]any)
	require.True(t, ok, "expected #map payload")
	out := make([][2]any, 0, len(raw))
	for _, p := range raw {
		pair := p.([]any)
		out = append(out, [2]any{pair[0], pair[1]})
	}
	return out
}

func itfIntMap(t *testing.T, v any) map[string]int64 {
	out := map[string]int64{}
	for _, p := range itfPairs(t, v) {
		out[p[0].(string)] = itfInt(t, p[1])
	}
	return out
}

func itfEntryMap(t *testing.T, v any) map[string]specEntry {
	out := map[string]specEntry{}
	for _, p := range itfPairs(t, v) {
		rec := p[1].(map[string]any)
		out[p[0].(string)] = specEntry{
			present: rec["present"].(bool),
			ver:     itfInt(t, rec["ver"]),
			pinned:  rec["pinned"].(bool),
		}
	}
	return out
}

func loadSpecTrace(t *testing.T, path string) []specState {
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc struct {
		States []map[string]any `json:"states"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	states := make([]specState, 0, len(doc.States))
	for _, s := range doc.States {
		st := specState{
			l1:       itfEntryMap(t, s["l1"]),
			l2:       itfEntryMap(t, s["l2"]),
			durable:  itfIntMap(t, s["durable"]),
			written:  itfIntMap(t, s["written"]),
			served:   itfIntMap(t, s["served"]),
			fetchq:   map[string]bool{},
			monotone: s["monotone"].(bool),
		}
		for _, k := range s["fetchq"].(map[string]any)["#set"].([]any) {
			st.fetchq[k.(string)] = true
		}
		states = append(states, st)
	}
	return states
}

// --- action inference from consecutive states ---------------------------

type specAction struct {
	name string
	key  string
}

func changedKey(a, b map[string]int64) (string, bool) {
	for k, v := range b {
		if a[k] != v {
			return k, true
		}
	}
	return "", false
}

func inferAction(t *testing.T, s0, s1 specState) (specAction, bool) {
	if k, ok := changedKey(s0.written, s1.written); ok {
		return specAction{"write", k}, true
	}
	if _, ok := changedKey(s0.durable, s1.durable); ok {
		return specAction{"flushAll", ""}, true
	}
	for k := range s1.fetchq {
		if !s0.fetchq[k] {
			return specAction{"readMiss", k}, true
		}
	}
	for k := range s0.fetchq {
		if !s1.fetchq[k] {
			return specAction{"fetchReturn", k}, true
		}
	}
	if k, ok := changedKey(s0.served, s1.served); ok {
		if s0.l2[k].present && s1.l1[k].present {
			return specAction{"readHitL2", k}, true
		}
		return specAction{"readHitL1", k}, true
	}
	// A stash promotion that RE-serves an already-served version moves the
	// entry without changing served — still a readHitL2, not a stutter.
	for k := range s1.l1 {
		if !s0.l1[k].present && s1.l1[k].present && s0.l2[k].present && !s1.l2[k].present {
			return specAction{"readHitL2", k}, true
		}
	}
	for k := range s1.l1 {
		if s0.l1[k].present && !s1.l1[k].present {
			if s1.l2[k].present && !s0.l2[k].present {
				return specAction{"demote", k}, true
			}
			return specAction{"dropL1", k}, true
		}
		if s0.l2[k].present && !s1.l2[k].present && s0.l1[k].present == s1.l1[k].present {
			return specAction{"dropL2", k}, true
		}
	}
	// Identical states: a stuttering re-serve (spec served/monotone
	// unchanged) — nothing to replay.
	return specAction{}, false
}

// --- replay --------------------------------------------------------------

func replaySpecTrace(t *testing.T, states []specState) {
	f := NewMockFetcher() // transport unused: fetchReturn is replayed via admit
	stash := NewSliceStash[string, int](10)
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{},
		WithVersioning[string, int, int](func(v int) int64 { return int64(v) }),
		WithStash[string, int, int](stash))

	durable := map[string]int64{}
	dirtyPins := map[string]bool{}

	stashPeek := func(k string) (specEntry, bool) {
		for i, sk := range stash.keys {
			if sk == k {
				return specEntry{present: true, ver: stash.entries[i].Ver}, true
			}
		}
		return specEntry{}, false
	}

	assertAgreement := func(step int, want specState) {
		for k, w := range want.l1 {
			got, ok := c.primaryStore[k]
			require.Equal(t, w.present, ok, "step %d: L1 presence of %s", step, k)
			if ok {
				require.Equal(t, w.ver, got.ver, "step %d: L1 version of %s", step, k)
				require.Equal(t, w.pinned, got.pinned, "step %d: pin of %s", step, k)
			}
		}
		for k, w := range want.l2 {
			got, ok := stashPeek(k)
			require.Equal(t, w.present, ok, "step %d: L2 presence of %s", step, k)
			if ok {
				require.Equal(t, w.ver, got.ver, "step %d: L2 version of %s", step, k)
			}
		}
		for k := range want.fetchq {
			_, queued := c.keysToFetchSet[k]
			require.True(t, queued, "step %d: %s must be queued", step, k)
		}
		require.Equal(t, len(want.fetchq), len(c.keysToFetchSet), "step %d: queue size", step)
		require.True(t, want.monotone, "step %d: safe traces never regress", step)
	}

	assertAgreement(0, states[0])
	for i := 1; i < len(states); i++ {
		act, ok := inferAction(t, states[i-1], states[i])
		if !ok {
			continue // stutter
		}
		s1 := states[i]
		switch act.name {
		case "write":
			ver := s1.written[act.key]
			c.AddItem(act.key, int(ver))
			c.Pin(act.key)
			dirtyPins[act.key] = true
		case "flushAll":
			for k, v := range s1.written {
				durable[k] = v
			}
			for k := range dirtyPins {
				c.Unpin(k)
				delete(dirtyPins, k)
			}
		case "readMiss":
			_, has := c.Get(act.key)
			require.False(t, has, "step %d: readMiss(%s) must miss", i, act.key)
		case "fetchReturn":
			// One-key delivery through the exact gate the flush path uses.
			delete(c.keysToFetchSet, act.key)
			if durable[act.key] > 0 {
				c.admit(act.key, int(durable[act.key]))
			}
		case "readHitL1", "readHitL2":
			v, has := c.Get(act.key)
			require.True(t, has, "step %d: %s(%s) must hit", i, act.name, act.key)
			require.Equal(t, s1.served[act.key], int64(v), "step %d: served version of %s", i, act.key)
		case "demote":
			item, ok := c.primaryStore[act.key]
			require.True(t, ok && !item.pinned, "step %d: demote(%s) needs an unpinned L1 entry", i, act.key)
			c.demoteToStash(act.key, item)
		case "dropL1":
			item, ok := c.primaryStore[act.key]
			require.True(t, ok && !item.pinned, "step %d: dropL1(%s) needs an unpinned L1 entry", i, act.key)
			c.demoteToStash(act.key, item)
			c.stash.Delete(act.key)
		case "dropL2":
			c.stash.Delete(act.key)
		default:
			t.Fatalf("step %d: unknown action %q", i, act.name)
		}
		assertAgreement(i, s1)
	}
}

// TestSpecTraceConformance replays every committed spec trace. A failure
// means the implementation's transition semantics drifted from the
// machine-checked model (or vice versa).
func TestSpecTraceConformance(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "itf", "*.itf.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no committed spec traces — regenerate with `npm run traces` in verification/formal/caching")
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			replaySpecTrace(t, loadSpecTrace(t, fmt.Sprintf("%s", p)))
		})
	}
}
