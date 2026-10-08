// Package trail is the host's audit trail on `boxer.facts` (ADR-0277): model
// calls and their messages, agent actions, grant decisions and egress
// fetches, each a row composed from shared context components and one
// domain component.
//
// The context components carry the identifiers a join needs, one membership
// per identifier whoever writes it: [Origin] (run, app, window — stamped by
// the host), [Conversation] (conversation, turn, round — stated by the app),
// [Delegation] (task, epoch, dispatcher call — stamped by the dispatcher) and
// [Cause] (the model call that asked for a tool call — stated by the
// coordinator).
//
// [Recorder] is the one place rows are built: it stamps the run, composes
// natural keys, and owns the buffer and its flushes. Services hold the
// recorder and call its verbs; none of them touches the store.
//
// The store is generated from the DTOs over the runtime vocabulary by
// gen_test.go, the lane doc/explanation/facts-bound-record-stores.md
// describes; chstore owns the table's DDL, so this store runs none.
package trail
