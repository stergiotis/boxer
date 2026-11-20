package watchbill_test

import (
	"context"
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// ExampleEnqueue writes a job row and lets two workers race for it. The
// claim is one conditional update: exactly one worker wins, and the loser
// learns so from the same call. MemStore has the SQL store's semantics
// without a server, which is what tests and examples use.
func ExampleEnqueue() {
	ctx := context.Background()
	store := watchbill.NewMemStore()
	id, err := watchbill.Enqueue(ctx, store, watchbill.Request{Kind: "thumbnail", Subject: "img-42", ID: "job-1"})
	if err != nil {
		panic(err)
	}
	now := time.Now()
	due, err := store.Queue(ctx, nil, nil, now, 10)
	if err != nil {
		panic(err)
	}
	fmt.Println(id, due)

	job, wonA, err := store.Claim(ctx, id, "worker-a", now)
	if err != nil {
		panic(err)
	}
	_, wonB, err := store.Claim(ctx, id, "worker-b", now)
	if err != nil {
		panic(err)
	}
	fmt.Println(wonA, wonB, job.State, job.Attempt)
	// Output:
	// job-1 [job-1]
	// true false running 1
}
