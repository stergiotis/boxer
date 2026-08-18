package play

import (
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
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
// `create_query` rides along so a revision can be read WITHOUT calling
// LW_SURFACE_VERSION(): calling it would fail with unknown-function on
// exactly the servers whose version we most want to report, and a failed
// query poisons the lane for the whole listing. The definition text is
// already there, and parsing an integer out of it cannot fail that way.
// The same reading recovers the retired LW_PACK_VERSION on a server no
// current build has reconciled yet (ADR-0171 §SD2).
//
// `origin` and `create_query` are marked Obsolete in system.functions' own
// documentation but are still populated as of 26.7 (the same dependency
// ClickHouseDocsSource takes). If a server stops filling them the extras
// listing degrades — see vocabProbe.extras — while the declared-vs-installed
// diff, which only looks up names we already know, is unaffected.
// It lists EVERY origin. The tab wants only the user-defined ones and filters
// on `origin` itself (vocabExtras); the completion pane wants the built-ins
// too, since a built-in is as valid at an expression position as anything this
// build declares (ADR-0190 §SD12 B5). One lane, two readers — the alternative
// was a second listing of the same table differing only in a WHERE.
//
// `toString(origin)` is load-bearing, for the reason defaultDocsQuery already
// records about its own enum column: `origin` is an Enum8, and ClickHouse
// ships an Enum8 over Arrow as the raw int8 ordinal, so the names never cross
// the wire. Selected bare, every row reads back as "0"/"1" and the
// origin != 'System' test below is true for every function the server has —
// which listed the whole built-in set as user-defined, buried the tab's extras
// families under it, and cost a quadratic sort of that population on every
// frame ("imzero2: slow frame" for as long as the tab was open). Rendering the
// enum server-side is the only place its dictionary exists.
const vocabProbeQuery = "SELECT name, create_query, toString(origin) AS origin FROM system.functions ORDER BY name"

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
	//
	// A sorted container rather than a map because its one iterating reader —
	// the completion pane's expression provider — wants these names in
	// exactly one order, and a map yields them in a different one per range.
	// Ordering on compareFoldThenExact puts them in the order that reader
	// sorts by, and makes the iteration itself deterministic. The two point
	// lookups below pay for that in log N, which against two calls is
	// nothing.
	installed *containers.BinarySearchGrowingKV[string, string] // name -> create_query

	// userDefined is the subset whose origin is not 'System' — what the
	// Vocabulary tab means by "on this endpoint". Kept beside installed rather
	// than replacing it, because the two readers of this lane want different
	// halves of the same answer.
	userDefined map[string]string

	// surfaceVersion is the revision read out of LW_SURFACE_VERSION's
	// definition, or -1 when the function is absent or its body is not an
	// integer.
	surfaceVersion int

	// preSurfaceVersion is the same reading of the pack's retired marker,
	// which is the only revision a server provisioned before the surface
	// marker existed can report (ADR-0171 §SD2). It tells "never reconciled
	// by a current build" from "no leeway functions at all", and both from
	// a current install — three states one field cannot hold.
	preSurfaceVersion int
}

func newVocabProbe(client *Client) (inst *vocabProbe) {
	return &vocabProbe{
		lane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("vocabulary")},
			memory.NewGoAllocator(), vocabProbeTimeout),
		surfaceVersion:    -1,
		preSurfaceVersion: -1,
	}
}

// demand returns the endpoint's USER-DEFINED function set, requesting the
// listing on first ask. ready=false means the answer has not landed yet.
//
// The user-defined half, not the whole listing: this is what the Vocabulary tab
// means by "on this endpoint", and counting the server's own built-ins as
// extras would bury the tab under a few thousand names it never claimed
// anything about. [vocabProbe.demandAll] is the other half's accessor.
func (inst *vocabProbe) demand() (installed map[string]string, ready bool) {
	if inst == nil || inst.lane == nil {
		return nil, false
	}
	if inst.installed != nil {
		return inst.userDefined, true
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
	var origins arrow.Array
	if view.rec.NumCols() > 2 {
		origins = view.rec.Column(2)
	}
	// A builder rather than upserts: this fills once, reads nothing while it
	// fills, and the row count is known — the shape its docstring asks for.
	all := containers.NewBinarySearchGrowingKVBuilder[string, string](
		int(view.rec.NumRows()), compareFoldThenExact)
	// userDefined stays a map: nothing iterates it, and both its readers go
	// through vocabMarkInstalled, which only ever asks whether a name is in.
	inst.userDefined = make(map[string]string, 64)
	for row := range int(view.rec.NumRows()) {
		name := names.ValueStr(row)
		def := ""
		if defs != nil {
			def = defs.ValueStr(row)
		}
		all.Stage(name, def)
		if origins == nil || origins.ValueStr(row) != "System" {
			inst.userDefined[name] = def
		}
	}
	inst.installed = all.Freeze()
	inst.surfaceVersion = parseMarkerVersion(inst.installed.GetDefault(lwsqlsurface.VersionFunctionName, ""))
	inst.preSurfaceVersion = parseMarkerVersion(inst.installed.GetDefault(lwsqlsurface.PreSurfaceVersionFunctionName, ""))
	return inst.userDefined, true
}

func (inst *vocabProbe) close() {
	if inst != nil && inst.lane != nil {
		inst.lane.close()
	}
}

// parseMarkerVersion recovers a revision from a marker function's own
// definition, which is emitted as a bare integer body:
//
//	CREATE FUNCTION LW_SURFACE_VERSION AS () -> 1
//
// Returns -1 for an absent function or any body it cannot read as an
// integer — a server carrying a hand-edited marker reports unknown rather
// than a number that would misdescribe it.
func parseMarkerVersion(createQuery string) int {
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

// demandAll returns every function the endpoint reports, built-ins included —
// what a completion offering an expression position needs, since a built-in is
// as valid there as anything this build declares (ADR-0190 §SD12 B5).
//
// Same lane, same listing, same cache: it demands through [vocabProbe.demand]
// so a reader asking for either half starts the one probe.
func (inst *vocabProbe) demandAll() (installed *containers.BinarySearchGrowingKV[string, string], ready bool) {
	if _, ready = inst.demand(); !ready {
		return nil, false
	}
	return inst.installed, true
}
