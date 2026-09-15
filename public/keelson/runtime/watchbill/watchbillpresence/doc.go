// Package watchbillpresence is the facts-bound record store for the
// watchbill worker's presence (ADR-0237): one `boxer.facts` row per worker
// run when it starts and one when it stops cleanly, carrying what the run
// drains — its kinds, its queues, its concurrency — and nothing that
// changes while it runs. Liveness is the run's heartbeat (ADR-0223 §SD4),
// which the reader joins; a run that stopped without its row is one whose
// heartbeat went silent, exactly as the sweep sees it.
//
// The store is generated from [WorkerPresence] over the runtime vocabulary
// by gen_test.go, the lane doc/explanation/facts-bound-record-stores.md
// describes; chstore owns the table's DDL, so this store runs none.
package watchbillpresence
