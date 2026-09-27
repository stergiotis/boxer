// Package llmfacts is the facts-bound record store for the host's model
// calls (ADR-0254 §SD4): one `boxer.facts` row per completion the
// runtime.llm service answered or refused — who asked, why, what it cost,
// how it ended — and nothing of what was said. Bodies, when a deployment
// keeps them, are a kind of their own, [LlmMessage] (ADR-0264): one row per
// message of a retained request, so they can be ditched ([DitchMessagesSQL])
// without touching the counts.
//
// The store is generated from [LlmCall] over the runtime vocabulary by
// gen_test.go, the lane doc/explanation/facts-bound-record-stores.md
// describes; chstore owns the table's DDL, so this store runs none.
package llmfacts
