// Package drive runs an application's [stevedore.HandlerI] and
// [stevedore.SinkI] in one process with no framework and no topic
// (ADR-0252, Updates 2026-09-23): a source yields requests, each is handled
// under the same deadline, panic recovery and retry the processor host
// applies, each item is landed, the sink is flushed, and only then is the
// checkpoint advanced. Failures no retry cured become dead-letter rows.
//
// It is for the cases a streaming framework is too much for: a tree of
// files, a list of paths, lines on stdin, a developer's loop, a test, and
// the appliance, which runs Go binaries only. It is a subcommand that runs
// and exits, not a service: it has no input beyond a tree, a reader and a
// list, no output beyond the sink, no routing, no metrics endpoint and no
// lease. A need past that line is the framework's, and the same handler
// and sink run there unchanged.
package drive
