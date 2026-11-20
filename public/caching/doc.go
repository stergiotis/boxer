// Package caching is a read-through cache for work that can be suspended
// and replayed — ETL steps, build graphs, traversals — where fetching is
// expensive and computing is cheap. A miss does not fetch: it queues the key
// and marks the current work item pending, and the queued keys of many work
// items are fetched together, grouped by partition, before the pending
// items are replayed.
//
// [NewReadThroughCache] takes a capacity, an [ItemFetcherI] (the batch
// source) and the [FetchCriteria] that decide when a batch is worth
// fetching; options add a second-level stash ([WithStash], with
// [NewS3FIFOStash] or [NewMapStash]), freshness and negative-caching TTLs
// and error backoff. In a work item (the WorkItem loop), Get either returns
// the value or records the miss; IterateReadyWorkItems and
// IterateRestWorkItems fetch and yield the items to replay. README.md has
// the execution model in full.
//
// A work item should ask for every key it needs before it gives up on a
// miss; returning at the first miss discovers one key per round trip. The
// cache is single-goroutine: share it across goroutines only behind a lock
// of your own.
package caching
