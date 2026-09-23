// Package host runs an application's [stevedore.HandlerI] as the external
// process a streaming framework drives (ADR-0252 §SD2, §SD3): one framed
// request in on stdin, one reply out, the failure reported the way the
// framework's processor expects, and the handler bounded in size, time and
// panics so the framework sees a status rather than a killed process.
//
// Two reply shapes exist among frameworks and both are supported. Under
// [ReplyStdout] — the upstream subprocess processor's shape — the reply
// payload goes to stdout and a failure is one line on stderr, which the
// framework reads as the pending message's error while leaving its content
// unchanged; nothing else may be written to stderr, so logs go to
// [Config.LogOutput]. Under [ReplyThreeFrame] the status, the payload and the
// log lines are three frames on stdout, and stderr is free.
package host
