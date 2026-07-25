// Package runid mints and validates the identity of a query run — the E4
// extension point of doc/explanation/query-system-requirements.md.
//
// One id names a run everywhere anything is said about it, by parties that
// never meet: the server's system.processes row while it runs, its
// query_log row once it finishes, the captured fact, a pin, a KILL
// addressed at it, and the progress frames an observer publishes for it
// (R7). Because the client mints it rather than the server assigning one,
// all of them can name a run before it exists, and a second observer can
// watch a run it did not issue.
//
// # Uniqueness scope
//
// `<app>-<label>-<host>-<pid>-<seq>` is unique across every server any
// boxer process talks to, for as long as query_log retains it: seq
// disambiguates lanes within a process, the pid disambiguates processes on
// a box, and the host disambiguates boxes.
//
// The host component is what makes the id safe on a *shared* channel. While
// every result came back down the connection that asked for it, a
// host-scoped id was enough, and this package's ancestor omitted the host.
// Once results are federated — an async engine streaming onto a Kafka or
// NATS topic several boxes publish to — two processes on different hosts
// minting the same id would collide on the one key everything correlates
// by, silently. Widening the id keeps that a single identity (R7) rather
// than introducing a second, channel-scoped one that every join would then
// have to disambiguate.
//
// It is NOT a globally unique id in the UUID sense, and deliberately so: it
// is meant to be read by a human in a log line, and a lane reuses its id
// across runs so a superseding run can replace its predecessor server-side.
// Two runs of one lane are the same id at different times.
package runid

import (
	"os"
	"strconv"
	"sync/atomic"
)

// maxTokenLen bounds one component. Hostnames and generated lane labels can
// both be long, and the whole id has to stay inside [MaxLen] and stay
// readable in a log line.
const maxTokenLen = 32

// MaxLen bounds a whole run id. ClickHouse accepts far longer query ids;
// the bound is about keeping subjects, log lines and table cells sane.
const MaxLen = 128

// seq disambiguates runs within one process.
var seq atomic.Uint64

// hostToken caches the sanitised hostname. The hostname does not change
// under a running process in any way that should renumber its runs.
var hostToken = Token(hostname())

func hostname() (name string) {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return
}

// HostToken is the sanitised local hostname, or "local" when the hostname
// is unavailable. It names the box, and only matters when ids from several
// boxes meet.
func HostToken() (token string) {
	token = hostToken
	return
}

// Mint returns a run id for one lane of one app.
//
// app names the issuing program ("play"); label names the lane ("main",
// "map"). Both are sanitised, because a label is not always a literal —
// a lane bound to a graph node carries that node's id — and an id that
// reached a subject or a statement unsanitised would be a hole in both.
//
// Stability, not novelty, is the point of the label: a lane reuses its id,
// so a superseding run REPLACES its still-running predecessor server-side.
// A fresh sequence number per Mint is what distinguishes lanes, not runs.
func Mint(app string, label string) (id string) {
	n := seq.Add(1)
	id = Token(app) + "-" + Token(label) + "-" + hostToken + "-" +
		strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(n, 10)
	if len(id) > MaxLen {
		id = id[:MaxLen]
	}
	return
}

// Token sanitises s into one id component: every character outside
// [A-Za-z0-9_-] becomes '_', and the result is capped. An empty result
// falls back to "local", so a component is never absent — an id with a
// hole in it would parse as a different shape.
//
// The charset is deliberately narrower than [Valid] accepts: it excludes
// '.' and ':' so that a component can never introduce what looks like a
// subject-hierarchy separator.
func Token(s string) (token string) {
	if len(s) > maxTokenLen {
		s = s[:maxTokenLen]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			b = append(b, c)
			continue
		}
		b = append(b, '_')
	}
	if len(b) == 0 {
		return "local"
	}
	token = string(b)
	return
}

// Valid reports whether id is safe to use as a bus subject token and as a
// SQL string literal. The charset is `[A-Za-z0-9_.:-]` — wider than [Token]
// emits, because ids minted elsewhere legitimately use '.' and ':'.
//
// What it excludes is the point: the quote that would reach into a
// statement built around the id, and the '*' and '>' that would reach into
// the subject namespace. Consumers reject rather than escape, so an id that
// got in unchecked would be a hole in both.
func Valid(id string) (ok bool) {
	if id == "" || len(id) > MaxLen {
		return
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == ':'
		if !valid {
			return
		}
	}
	ok = true
	return
}
