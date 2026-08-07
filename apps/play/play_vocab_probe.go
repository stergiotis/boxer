package play

import (
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// play_vocab_probe.go asks the endpoint which SQL user-defined functions it
// carries (ADR-0174 §SD2). It is the one population of the vocabulary that is
// data-source dependent: the client macros are expanded before a statement
// ships and the `ts*` family never leaves play, but a UDF exists only where
// somebody installed it.
//
// Endpoint dependence is structural rather than implemented here. The probe
// runs through a nodeLane over clientExecutor — the same machinery the docs
// lookup and the ts shadowing probe use — so it inherits endpoint routing,
// auth and the pre-execute stage, and retargeting (ADR-0134) changes what it
// is asking without this file knowing.

// vocabProbeQuery lists the server's user-defined functions.
//
// `create_query` rides along so the pack's revision can be read WITHOUT
// calling LW_PACK_VERSION(): calling it would fail with unknown-function on
// exactly the servers whose version we most want to report, and a failed
// query poisons the lane for the whole listing. The definition text is
// already there, and parsing an integer out of it cannot fail that way.
//
// `origin` and `create_query` are marked Obsolete in system.functions' own
// documentation but are still populated as of 26.7 (the same dependency
// ClickHouseDocsSource takes). If a server stops filling them the extras
// listing degrades — see vocabProbe.extras — while the declared-vs-installed
// diff, which only looks up names we already know, is unaffected.
const vocabProbeQuery = "SELECT name, create_query FROM system.functions " +
	"WHERE origin != 'System' ORDER BY name"

// vocabProbeTimeout bounds the probe. It is a catalog listing, not a result:
// a slow server must delay a panel, never a query.
const vocabProbeTimeout = 10 * time.Second

// vocabProbe caches the endpoint's user-defined function names. The lane IS
// the cache — its memo key is the SQL, which never changes — so the query
// runs once per session per endpoint and later demands are served from the
// held result.
type vocabProbe struct {
	lane *nodeLane

	// installed is nil until the probe answers. Nil means NOTHING KNOWN, not
	// "nothing installed": a panel that renders "missing" against an
	// unanswered probe would send someone to reprovision a healthy server.
	installed map[string]string // name -> create_query

	// packVersion is the revision read out of LW_PACK_VERSION's definition,
	// or -1 when the function is absent or its body is not an integer.
	packVersion int
}

func newVocabProbe(client *Client) (inst *vocabProbe) {
	return &vocabProbe{
		lane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("vocabulary")},
			memory.NewGoAllocator(), vocabProbeTimeout),
		packVersion: -1,
	}
}

// demand returns the endpoint's function set, requesting it on first ask.
// ready=false means the answer has not landed yet.
func (inst *vocabProbe) demand() (installed map[string]string, ready bool) {
	if inst == nil || inst.lane == nil {
		return nil, false
	}
	if inst.installed != nil {
		return inst.installed, true
	}
	view := inst.lane.demand(compiledNode{SQL: vocabProbeQuery})
	if view.rec == nil || view.rec.NumRows() == 0 {
		// A server with zero user-defined functions answers zero rows, which
		// is indistinguishable here from an unanswered probe. Both read as
		// not-ready, which errs toward silence: the panel says "asking" for
		// one extra frame rather than declaring a healthy roster missing.
		return nil, false
	}
	names := view.rec.Column(0)
	defs := view.rec.Column(1)
	inst.installed = make(map[string]string, int(view.rec.NumRows()))
	for row := range int(view.rec.NumRows()) {
		name := names.ValueStr(row)
		def := ""
		if defs != nil {
			def = defs.ValueStr(row)
		}
		inst.installed[name] = def
	}
	inst.packVersion = parsePackVersion(inst.installed[chpack.VersionFunctionName])
	return inst.installed, true
}

func (inst *vocabProbe) close() {
	if inst != nil && inst.lane != nil {
		inst.lane.close()
	}
}

// parsePackVersion recovers the pack revision from LW_PACK_VERSION's own
// definition, which the pack emits as a bare integer body:
//
//	CREATE FUNCTION LW_PACK_VERSION AS () -> 3
//
// Returns -1 for an absent function or any body it cannot read as an
// integer — a server carrying a hand-edited marker reports unknown rather
// than a number that would misdescribe it.
func parsePackVersion(createQuery string) int {
	_, body, found := strings.Cut(createQuery, "->")
	if !found {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(body))
	if err != nil {
		return -1
	}
	return n
}
