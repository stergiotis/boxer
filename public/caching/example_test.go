package caching_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/caching"
)

// upperFetcher stands in for a database: it serves every key of a batch in
// one call and counts the calls.
type upperFetcher struct{ calls int }

func (inst *upperFetcher) DeterminePartition(key string) uint64 { return 0 }

func (inst *upperFetcher) FetchItemSinglePartition(ctx context.Context, partition uint64, keys []string, target caching.ItemTargetI[string, string]) error {
	inst.calls++
	for _, k := range keys {
		target.AddItem(k, strings.ToUpper(k))
	}
	return nil
}

// ExampleNewReadThroughCache processes work items that each need two keys.
// A work item asks for everything it needs before giving up on a miss, so
// the misses of all items are fetched in one batch, and the items are then
// replayed against a warm cache. Replay order is not the order the items
// were suspended in.
func ExampleNewReadThroughCache() {
	f := &upperFetcher{}
	cache := caching.NewReadThroughCache[string, string, int](16, f, caching.FetchCriteria{})
	needs := map[int][2]string{1: {"a", "b"}, 2: {"b", "c"}}

	process := func(item int) (done bool) {
		for range cache.WorkItem(item) {
			x, okX := cache.Get(needs[item][0])
			y, okY := cache.Get(needs[item][1])
			if !okX || !okY {
				return false // suspended: both misses are queued
			}
			fmt.Println(item, x+y)
			done = true
		}
		return
	}
	for item := 1; item <= 2; item++ {
		process(item)
	}
	for item := range cache.IterateRestWorkItems(context.Background()) {
		process(item)
	}
	fmt.Println("fetch calls:", f.calls)
	// Unordered output:
	// 1 AB
	// 2 BC
	// fetch calls: 1
}
