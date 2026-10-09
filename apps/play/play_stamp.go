package play

import (
	"encoding/hex"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// play_stamp.go is the SD7 identity stamp (ADR-0115): every query the
// client executes carries a compact JSON log_comment with
// {run_id, app, lane, authored_fp, sent_fp, chain_fp, env_fp}, so the
// server's own query_log is attributable with no boxer process running,
// and the queryrunsd capture pipeline lifts the identity into
// boxer.facts memberships (queryrunfacts.ParseStamp — the same
// logcomment.Stamp serialised here, single-sourcing the keys; ADR-0295
// §SD2).
//
// The four fingerprints are the entity spine's day-one anchors
// (doc/explanation/query-observability.md): authored = the buffer as
// typed, sent = the body after the pre-execute rewrites, chain = the
// rewrite regime that connects them, env = the parameter binding the
// definition was applied to. Interning the fingerprinted texts is
// ADR-0112's substrate; stamping them now means that history backfills
// instead of starting blind.

// stampFp is the stamp's content fingerprint: 64 bits of BLAKE3, hex —
// compact enough for a log_comment, stable across processes, and the
// same hash family the facts natural keys use. Identity correlation,
// not a security boundary.
func stampFp(s string) string {
	sum := blake3.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// SetStampIdentity records the process/run identity the stamps carry:
// the runtime's run id (MountContextI.RunId — joins captured runs to
// the runtime-start fact) and the app id (the Manifest Id, the same
// value the MembRuntimeApp membership carries elsewhere). Callable any
// time; empty values simply leave those stamp fields out. The launcher
// wires it at Mount; the standalone CLI never does, and its runs stamp
// lane + fingerprints only.
func (inst *Client) SetStampIdentity(runId string, appId string, instanceKey uint64) {
	inst.mu.Lock()
	inst.stampRunId = runId
	inst.stampAppId = appId
	inst.stampInstance = instanceKey
	inst.mu.Unlock()
}

// stampIdentity reads the identity triple under the URL lock.
func (inst *Client) stampIdentity() (runId string, appId string, instanceKey uint64) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.stampRunId, inst.stampAppId, inst.stampInstance
}

// chainFingerprint identifies the rewrite regime between the authored
// and sent texts: the ordered pre-execute catalog (name, order,
// late-boundness, fixed-point flag) plus the ADR-0121 selection-
// condition toggle, which rewrites the sent text but deliberately lives
// outside the registry. The registry carries no per-pass content hashes
// yet; when it grows them (the explanation page's "versions/content
// hashes"), they join this string and every chain fingerprint moves —
// which is the point.
func (inst *Client) chainFingerprint() string {
	var b strings.Builder
	for _, r := range inst.passes.Catalog() {
		if r.Stage != passreg.StagePreExecute {
			continue
		}
		b.WriteString(r.Name)
		b.WriteByte('|')
		b.WriteString(strconv.Itoa(r.Order))
		if r.LateBound {
			b.WriteString("|late")
		}
		if r.Properties.NeedsFixedPoint {
			b.WriteString("|fixedpoint")
		}
		b.WriteByte(';')
	}
	if inst.ExposeConditions() {
		b.WriteString("+exposeConditions")
	}
	return stampFp(b.String())
}

// envFingerprint hashes the canonical name→value binding a run resolves
// — the URL-riding parameters exactly as sent (SET-bound constants
// shadowing same-named signals, matching ExecuteArrowStream's Set
// order), sorted by name so map order cannot move the fingerprint.
// Empty binding → empty fingerprint (the field stays off the stamp):
// a definition applied to no environment.
func envFingerprint(params map[string]string, signals map[string]string) string {
	if len(params)+len(signals) == 0 {
		return ""
	}
	merged := make(map[string]string, len(params)+len(signals))
	maps.Copy(merged, signals)
	maps.Copy(merged, params)
	names := make([]string, 0, len(merged))
	for k := range merged {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k)
		b.WriteByte(0)
		b.WriteString(merged[k])
		b.WriteByte(0)
	}
	return stampFp(b.String())
}

// composeLogComment builds the full run stamp. authored is the buffer
// as handed to ExecuteArrowStream, sent the body BuildStatement
// produced; params/signals are the URL binding. Returns "" only when
// marshalling fails (structurally impossible for this struct).
func (inst *Client) composeLogComment(authored string, sent string, params map[string]string, signals map[string]string, opts *ExecOptions, agent *app.OnBehalfOf) string {
	runId, appId, instanceKey := inst.stampIdentity()
	authoredFp := stampFp(authored)
	inst.authored.note(authoredFp, authored)
	st := logcomment.Stamp{
		RunId:      runId,
		App:        appId,
		Instance:   instanceKey,
		AuthoredFp: authoredFp,
		SentFp:     stampFp(sent),
		ChainFp:    inst.chainFingerprint(),
		EnvFp:      envFingerprint(params, signals),
	}
	if opts != nil {
		st.Lane = opts.Label
	}
	if agent != nil {
		// The run is an agent task's work (ADR-0277 §SD7): the stamp names
		// the task and the dispatcher's call, so the captured row joins the
		// action record without a time window.
		st.Task, st.TaskEpoch, st.TaskCall = agent.Task, agent.Epoch, agent.Call
	}
	return marshalStamp(st)
}

// composeProbeLogComment is the attribution-only stamp for verdict
// probes (EXPLAIN AST): a probe is not an executed definition, so it
// carries identity but no fingerprints. Returns "" when there is no
// identity to stamp at all.
func (inst *Client) composeProbeLogComment(opts *ExecOptions) string {
	runId, appId, instanceKey := inst.stampIdentity()
	st := logcomment.Stamp{RunId: runId, App: appId, Instance: instanceKey}
	if opts != nil {
		st.Lane = opts.Label
	}
	if st == (logcomment.Stamp{}) {
		return ""
	}
	return marshalStamp(st)
}

func marshalStamp(st logcomment.Stamp) string {
	return logcomment.Marshal(st)
}

// Bounds on the authored-text memo: entries and the bytes they hold.
const (
	authoredMemoMaxEntries = 512
	authoredMemoMaxBytes   = 4 << 20
)

// authoredMemo maps an authored fingerprint back to the text it was taken
// over, for the texts this process stamped. The capture keeps only the
// fingerprint — interning the texts durably is the deferred S5 slice
// (ADR-0115, on ADR-0112's substrate) — so this is the light cut: a run
// this process issued shows its authored statement while the memo still
// holds it, and any other run shows the fingerprint alone. Oldest first
// out; a text seen again keeps its original place. The zero value is
// ready; safe for concurrent use (stamps are composed on run goroutines).
type authoredMemo struct {
	mu    sync.Mutex
	texts map[string]string
	order []string
	bytes int
}

// note records text under fp. A text larger than the whole budget is not
// kept.
func (inst *authoredMemo) note(fp string, text string) {
	if fp == "" || len(text) > authoredMemoMaxBytes {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, ok := inst.texts[fp]; ok {
		return
	}
	if inst.texts == nil {
		inst.texts = make(map[string]string)
	}
	for len(inst.order) > 0 && (len(inst.order) >= authoredMemoMaxEntries || inst.bytes+len(text) > authoredMemoMaxBytes) {
		old := inst.order[0]
		inst.order = inst.order[1:]
		inst.bytes -= len(inst.texts[old])
		delete(inst.texts, old)
	}
	inst.texts[fp] = text
	inst.order = append(inst.order, fp)
	inst.bytes += len(text)
}

// lookup returns the text stamped under fp, when the memo still holds it.
func (inst *authoredMemo) lookup(fp string) (text string, ok bool) {
	if fp == "" {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	text, ok = inst.texts[fp]
	return
}

// AuthoredText is the authored statement this client stamped under fp —
// the buffer as handed to the run, before the pre-execute rewrites. ok is
// false for a run another process issued, or one the memo has let go.
func (inst *Client) AuthoredText(fp string) (text string, ok bool) {
	return inst.authored.lookup(fp)
}
