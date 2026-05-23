//go:build llm_generated_opus47

package containers

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"testing"
)

// genStringKeys returns n unique lowercase-ASCII keys of fixed length.
// Deterministic for a given seed; rejection-samples to guarantee uniqueness
// so that BSKV cardinality matches map cardinality in the comparisons.
func genStringKeys(n, keylen int, seed uint64) []string {
	rnd := rand.New(rand.NewPCG(seed, seed^0xa5a5a5a5))
	seen := make(map[string]struct{}, n)
	keys := make([]string, 0, n)
	buf := make([]byte, keylen)
	for len(keys) < n {
		for j := range buf {
			buf[j] = byte('a' + rnd.IntN(26))
		}
		s := string(buf)
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		keys = append(keys, s)
	}
	return keys
}

var benchMatrix = []struct {
	N, K int
}{
	{8, 8}, {8, 64},
	{64, 8}, {64, 64},
	{512, 8}, {512, 64},
	{4096, 8}, {4096, 64},
}

func benchName(n, k int) string { return fmt.Sprintf("N=%d/K=%d", n, k) }

// genDuplicatedKeys returns a length-(unique*dup) slice in which each of
// `unique` distinct keys appears exactly `dup` times, interleaved via a
// deterministic shuffle. This mirrors the registry-style workload where
// multiple AddParents calls accumulate the same node-id across batches.
func genDuplicatedKeys(unique, dup, keylen int, seed uint64) []string {
	base := genStringKeys(unique, keylen, seed)
	out := make([]string, 0, unique*dup)
	for range dup {
		out = append(out, base...)
	}
	rnd := rand.New(rand.NewPCG(seed^0x5a5a5a5a, seed^0x12345678))
	rnd.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

var dupBenchMatrix = []struct {
	Unique, Dup int
}{
	{64, 8},   // 512 total inserts, 8× duplication
	{512, 8},  // 4096 total inserts, 8× duplication
	{64, 64},  // 4096 total inserts, 64× duplication (extreme)
}

func dupBenchName(unique, dup int) string {
	return fmt.Sprintf("Unique=%d/Dup=%d", unique, dup)
}

// --- BSKV ---------------------------------------------------------------

func BenchmarkBSKV_BuildSingle(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
				for j, k := range keys {
					kv.UpsertSingle(k, j)
				}
			}
		})
	}
}

func BenchmarkBSKV_BuildBatch(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
				for j, k := range keys {
					kv.UpsertBatch(k, j)
				}
				_ = kv.Has(keys[0]) // force the deferred sort+compact
			}
		})
	}
}

func BenchmarkBSKV_GetHit(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
		for j, k := range keys {
			kv.UpsertBatch(k, j)
		}
		_ = kv.Has(keys[0]) // pay sort cost outside the measured loop
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var sink int
			for i := 0; i < b.N; i++ {
				sink, _ = kv.Get(keys[i%tc.N])
			}
			_ = sink
		})
	}
}

func BenchmarkBSKV_GetMiss(b *testing.B) {
	for _, tc := range benchMatrix {
		// Two disjoint sets: insert one, probe the other.
		inKeys := genStringKeys(tc.N, tc.K, 1)
		missKeys := genStringKeys(tc.N, tc.K, 2)
		kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
		for j, k := range inKeys {
			kv.UpsertBatch(k, j)
		}
		_ = kv.Has(inKeys[0])
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var has bool
			for i := 0; i < b.N; i++ {
				has = kv.Has(missKeys[i%tc.N])
			}
			_ = has
		})
	}
}

func BenchmarkBSKV_IteratePairs(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
		for j, k := range keys {
			kv.UpsertBatch(k, j)
		}
		_ = kv.Has(keys[0])
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var sumLen, sumVal int
			for i := 0; i < b.N; i++ {
				for k, v := range kv.IteratePairs() {
					sumLen += len(k)
					sumVal += v
				}
			}
			_, _ = sumLen, sumVal
		})
	}
}

func BenchmarkBSKV_DeleteChurn(b *testing.B) {
	// Steady-state churn: delete one key, reinsert a fresh one, repeat.
	// Measures the cost of staying sorted under turnover.
	for _, tc := range benchMatrix {
		base := genStringKeys(tc.N, tc.K, 1)
		fresh := genStringKeys(tc.N, tc.K, 99)
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			b.StopTimer()
			kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
			for j, k := range base {
				kv.UpsertBatch(k, j)
			}
			_ = kv.Has(base[0])
			b.StartTimer()
			for i := 0; i < b.N; i++ {
				// Pick a slot deterministically; alternate between "delete an
				// existing key, insert a fresh one" and the reverse to keep
				// cardinality steady.
				if i%2 == 0 {
					kv.Delete(base[i%tc.N])
					kv.UpsertSingle(fresh[i%tc.N], i)
				} else {
					kv.Delete(fresh[(i-1)%tc.N])
					kv.UpsertSingle(base[(i-1)%tc.N], i)
				}
			}
		})
	}
}

func BenchmarkBSKV_MergeValue(b *testing.B) {
	merge := func(old, new int) int { return old + new }
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		kv := NewBinarySearchGrowingKVOrdered[string, int](tc.N)
		for j, k := range keys {
			kv.UpsertBatch(k, j)
		}
		_ = kv.Has(keys[0])
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv.MergeValue(keys[i%tc.N], 1, merge)
			}
		})
	}
}

// --- map[string]int baseline -------------------------------------------

func BenchmarkMap_Build(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m := make(map[string]int, tc.N)
				for j, k := range keys {
					m[k] = j
				}
			}
		})
	}
}

func BenchmarkMap_GetHit(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		m := make(map[string]int, tc.N)
		for j, k := range keys {
			m[k] = j
		}
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var sink int
			for i := 0; i < b.N; i++ {
				sink = m[keys[i%tc.N]]
			}
			_ = sink
		})
	}
}

func BenchmarkMap_GetMiss(b *testing.B) {
	for _, tc := range benchMatrix {
		inKeys := genStringKeys(tc.N, tc.K, 1)
		missKeys := genStringKeys(tc.N, tc.K, 2)
		m := make(map[string]int, tc.N)
		for j, k := range inKeys {
			m[k] = j
		}
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var has bool
			for i := 0; i < b.N; i++ {
				_, has = m[missKeys[i%tc.N]]
			}
			_ = has
		})
	}
}

func BenchmarkMap_Iterate(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		m := make(map[string]int, tc.N)
		for j, k := range keys {
			m[k] = j
		}
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var sumLen, sumVal int
			for i := 0; i < b.N; i++ {
				for k, v := range m {
					sumLen += len(k)
					sumVal += v
				}
			}
			_, _ = sumLen, sumVal
		})
	}
}

func BenchmarkMap_DeleteChurn(b *testing.B) {
	for _, tc := range benchMatrix {
		base := genStringKeys(tc.N, tc.K, 1)
		fresh := genStringKeys(tc.N, tc.K, 99)
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			b.StopTimer()
			m := make(map[string]int, tc.N)
			for j, k := range base {
				m[k] = j
			}
			b.StartTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					delete(m, base[i%tc.N])
					m[fresh[i%tc.N]] = i
				} else {
					delete(m, fresh[(i-1)%tc.N])
					m[base[(i-1)%tc.N]] = i
				}
			}
		})
	}
}

// BenchmarkBSKV_GetHit_GeneralCtor measures the production Get path when
// the caller uses NewBinarySearchGrowingKV with a function-value
// comparator (cmp.Compare[string] here). Compare to BenchmarkBSKV_GetHit
// (which uses NewBinarySearchGrowingKVOrdered) to see the cost of the
// per-comparison indirect call that the Ordered constructor's inlined
// dispatch avoids.
func BenchmarkBSKV_GetHit_GeneralCtor(b *testing.B) {
	for _, tc := range benchMatrix {
		keys := genStringKeys(tc.N, tc.K, 1)
		kv := NewBinarySearchGrowingKV[string, int](tc.N, cmp.Compare[string])
		for j, k := range keys {
			kv.UpsertBatch(k, j)
		}
		_ = kv.Has(keys[0])
		b.Run(benchName(tc.N, tc.K), func(b *testing.B) {
			b.ReportAllocs()
			var sink int
			for i := 0; i < b.N; i++ {
				sink, _ = kv.Get(keys[i%tc.N])
			}
			_ = sink
		})
	}
}

// --- Heavy-duplicate build paths ----------------------------------------
//
// Stresses the compaction path that the registry workload exercises: the
// same key reappears many times across UpsertBatch calls before any read.
// BSKV_BuildSingle here pays binary-search + overwrite per insert (no
// shift after the first occurrence). BSKV_BuildBatch pays append + a
// single sort+compact on first read. Map_Build is the trivial baseline.

// BuildBatch_Dup pre-sizes for total inserts (Unique*Dup). The deferred
// slices reach final length = total before compaction, so this is the right
// hint when the caller knows how many UpsertBatch calls are coming.
func BenchmarkBSKV_BuildBatch_Dup(b *testing.B) {
	for _, tc := range dupBenchMatrix {
		keys := genDuplicatedKeys(tc.Unique, tc.Dup, 8, 7)
		b.Run(dupBenchName(tc.Unique, tc.Dup), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv := NewBinarySearchGrowingKVOrdered[string, int](tc.Unique * tc.Dup)
				for j, k := range keys {
					kv.UpsertBatch(k, j)
				}
				_ = kv.Has(keys[0]) // force the deferred sort+compact
			}
		})
	}
}

// BuildBatch_DupUndersized hints the FINAL unique count, not the total
// insert count. The deferred slices grow past capacity and trigger several
// reallocations. Demonstrates that the natural-looking "size for the
// final container" hint is a footgun when batches contain duplicates.
func BenchmarkBSKV_BuildBatch_DupUndersized(b *testing.B) {
	for _, tc := range dupBenchMatrix {
		keys := genDuplicatedKeys(tc.Unique, tc.Dup, 8, 7)
		b.Run(dupBenchName(tc.Unique, tc.Dup), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv := NewBinarySearchGrowingKVOrdered[string, int](tc.Unique)
				for j, k := range keys {
					kv.UpsertBatch(k, j)
				}
				_ = kv.Has(keys[0])
			}
		})
	}
}

// BuildSingle_Dup sized to the final unique count — the natural hint for
// the single path, which replaces in place after the first occurrence and
// so never grows past unique.
func BenchmarkBSKV_BuildSingle_Dup(b *testing.B) {
	for _, tc := range dupBenchMatrix {
		keys := genDuplicatedKeys(tc.Unique, tc.Dup, 8, 7)
		b.Run(dupBenchName(tc.Unique, tc.Dup), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				kv := NewBinarySearchGrowingKVOrdered[string, int](tc.Unique)
				for j, k := range keys {
					kv.UpsertSingle(k, j)
				}
			}
		})
	}
}

func BenchmarkMap_Build_Dup(b *testing.B) {
	for _, tc := range dupBenchMatrix {
		keys := genDuplicatedKeys(tc.Unique, tc.Dup, 8, 7)
		b.Run(dupBenchName(tc.Unique, tc.Dup), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m := make(map[string]int, tc.Unique)
				for j, k := range keys {
					m[k] = j
				}
			}
		})
	}
}
