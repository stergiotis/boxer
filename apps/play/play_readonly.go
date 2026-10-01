package play

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine/chserver"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A server user configured readonly=1 refuses every request that changes a
// setting, so play's own settings — readonly=2 on the Arrow path, the SD7
// log_comment stamp, the HTTP progress pair — make each of its runs fail
// before a row is read. Play degrades by what the server says (ADR-0181
// Update 2026-10-01):
//
//   - A run goes out as it always has. Only when the server refuses it with
//     READONLY (Code 164) does play ask the level once — SELECT
//     getSetting('readonly'), which carries no settings — remember it for
//     this user@endpoint, and send the run again degraded. A writable server
//     never sees the probe, and nothing is dropped on a guess.
//   - readonly ≥ 1: readonly=2 is not sent. The server already refuses writes
//     and DDL for this user, which is what readonly=2 was there for.
//   - readonly = 1: neither is the log_comment stamp, nor the progress
//     settings (the run reports when it completes). The endpoint label says
//     the runs go unstamped.
//
// A level once learned degrades every later run to that endpoint up front.

// readonlyProbeTimeout bounds the one-off probe; a server too slow to answer
// SELECT getSetting() is not going to serve the run either.
const readonlyProbeTimeout = 5 * time.Second

// readonlyLevels caches the server-reported readonly level per user@endpoint.
type readonlyLevels struct {
	mu     sync.Mutex
	levels map[string]uint8
}

func readonlyKey(user, target string) string { return user + "@" + target }

func (inst *readonlyLevels) get(key string) (level uint8, known bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	level, known = inst.levels[key]
	return
}

func (inst *readonlyLevels) put(key string, level uint8) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.levels == nil {
		inst.levels = make(map[string]uint8)
	}
	inst.levels[key] = level
}

// knownReadonlyLevel is the level learned for target, 0 when none was.
func (inst *Client) knownReadonlyLevel(target string) (level uint8) {
	level, _ = inst.readonly.get(readonlyKey(inst.cfg.User, target))
	return
}

// learnReadonlyLevel handles a refused run: when err is the server's
// READONLY refusal of a run sent degraded to sentLevel, it finds the level
// for target — learned meanwhile by a concurrent run, or asked of the server
// now — and remembers it. retry says the caller should send the run again
// degraded to level.
func (inst *Client) learnReadonlyLevel(ctx context.Context, eng *chserver.Engine, target string, sentLevel uint8, err error) (level uint8, retry bool) {
	if err == nil || !isReadonlyRefusal(err) {
		return
	}
	key := readonlyKey(inst.cfg.User, target)
	if known, ok := inst.readonly.get(key); ok {
		// Another run learned the level while this one was in flight:
		// retry at it. Sent at it already and still refused: something else
		// the run carries is refused, and the server's message says what.
		return known, known != sentLevel
	}
	pctx, cancel := context.WithTimeout(ctx, readonlyProbeTimeout)
	defer cancel()
	level, pErr := probeReadonlyLevel(pctx, eng)
	if pErr != nil || level == 0 {
		return 0, false
	}
	inst.readonly.put(key, level)
	return level, true
}

// isReadonlyRefusal recognises ClickHouse's READONLY error (Code 164) in a
// run's error text, which carries the server's own diagnostic.
func isReadonlyRefusal(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Code: 164.") || strings.Contains(msg, "(READONLY)")
}

// degradeForReadonly removes from req what a server holding this user at
// level refuses.
func degradeForReadonly(req *queryengine.Request, level uint8) {
	if level == 0 {
		return
	}
	delete(req.Settings, "readonly")
	if level == 1 {
		delete(req.Settings, "log_comment")
		req.OnProgress = nil
	}
}

// ReadonlyNote is the endpoint label's suffix for target: empty while no
// level was learned, or it was 0.
func (inst *Client) ReadonlyNote(target string) (note string) {
	switch inst.knownReadonlyLevel(target) {
	case 0:
		return ""
	case 1:
		return "read-only user, runs unstamped"
	default:
		return "read-only user"
	}
}

// probeReadonlyLevel asks the server for this user's readonly setting. It
// carries no settings of its own, so a readonly=1 user can answer it.
func probeReadonlyLevel(ctx context.Context, eng *chserver.Engine) (level uint8, err error) {
	st, _, err := eng.Deliver(ctx, queryengine.Request{
		SQL:    "SELECT getSetting('readonly')",
		Format: "TabSeparated",
	})
	if err != nil {
		return
	}
	defer func() { _ = st.Close() }()
	body, term, err := queryengine.Collect(st)
	if err != nil {
		return
	}
	if term.State != runstream.TerminalComplete {
		err = eh.Errorf("readonly probe did not complete: %w", term.Err)
		return
	}
	raw := strings.TrimSpace(string(body))
	u, pErr := strconv.ParseUint(raw, 10, 8)
	if pErr != nil {
		err = eb.Build().Str("answer", raw).Errorf("readonly probe answer is not a level: %w", pErr)
		return
	}
	level = uint8(u)
	return
}
