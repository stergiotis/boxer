// Package attention is the host's account of what the person is shown and
// what they owe: the levels events are shown at (ADR-0323) and the
// queue of decisions waiting on the person (ADR-0324).
//
// [Levels] classifies an event into a level on one of two scales — attention
// for events nothing waits on, waiting for events a turn is blocked on — from
// defaults, the person's table and per-conversation overrides; an override
// never crosses scales (ADR-0323 §SD2).
//
// [Queue] gathers the waiting items of its sources, orders them by level and
// age, and keeps the person's acknowledgements and shelves (ADR-0324 §SD1,
// §SD4). A source keeps its own state: the queue reads it each frame and
// keeps only what the person did to an item.
//
// [Flood] bounds how fast a stream, and all streams together, may add items
// (ADR-0324 §SD6).
package attention
