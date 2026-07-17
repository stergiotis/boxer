package play

import (
	"encoding/hex"
	"encoding/json"
	"maps"
	"sort"
	"strconv"
	"strings"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
)

// play_stamp.go is the SD7 identity stamp (ADR-0115): every query the
// client executes carries a compact JSON log_comment with
// {run_id, app, lane, authored_fp, sent_fp, chain_fp, env_fp}, so the
// server's own query_log is attributable with no boxer process running,
// and the queryrunsd capture pipeline lifts the identity into
// runtime.facts memberships (queryrunfacts.ParseStamp — the same struct
// serialised here, single-sourcing the keys).
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
func (inst *Client) SetStampIdentity(runId string, appId string) {
	inst.mu.Lock()
	inst.stampRunId = runId
	inst.stampAppId = appId
	inst.mu.Unlock()
}

// stampIdentity reads the identity pair under the URL lock.
func (inst *Client) stampIdentity() (runId string, appId string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.stampRunId, inst.stampAppId
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
func (inst *Client) composeLogComment(authored string, sent string, params map[string]string, signals map[string]string, opts *ExecOptions) string {
	runId, appId := inst.stampIdentity()
	st := queryrunfacts.Stamp{
		RunId:      runId,
		App:        appId,
		AuthoredFp: stampFp(authored),
		SentFp:     stampFp(sent),
		ChainFp:    inst.chainFingerprint(),
		EnvFp:      envFingerprint(params, signals),
	}
	if opts != nil {
		st.Lane = opts.Label
	}
	return marshalStamp(st)
}

// composeProbeLogComment is the attribution-only stamp for verdict
// probes (EXPLAIN AST): a probe is not an executed definition, so it
// carries identity but no fingerprints. Returns "" when there is no
// identity to stamp at all.
func (inst *Client) composeProbeLogComment(opts *ExecOptions) string {
	runId, appId := inst.stampIdentity()
	st := queryrunfacts.Stamp{RunId: runId, App: appId}
	if opts != nil {
		st.Lane = opts.Label
	}
	if st == (queryrunfacts.Stamp{}) {
		return ""
	}
	return marshalStamp(st)
}

func marshalStamp(st queryrunfacts.Stamp) string {
	b, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(b)
}
