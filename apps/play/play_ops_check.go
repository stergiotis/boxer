package play

// play_ops_check.go: check, the query that runs an assertion over the
// sources of a result the window holds (ADR-0340 §SD3). An
// assertion is a read-only statement a model wrote to decide one claim it
// drew from a result; the window runs it, not the harness, because the
// endpoint, the parameters and the agent limits are the window's. The
// check writes nothing — no buffer, no result, no pane, no mark — and
// returns at most one row of cell text, with the digests of the two
// statements, so a reader can tell which result the claim was drawn from
// and whether the window still shows it.

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

const opCheck = "check"

// Bounds on a check (ADR-0340 §SD8): one row, a kilobyte of cell text,
// and how long it may run.
const (
	checkMaxRows  = 1
	checkMaxBytes = 1 << 10
	checkTimeout  = 30 * time.Second
)

// CheckArgs is check's argument.
type CheckArgs struct {
	ResultId  uint64 `desc:"the result the assertion rests on, as describe_result and sample_rows report it; refused when the window holds another"`
	Statement string `desc:"a read-only statement over the tables the result's statement read, returning one row: one cell for a value, one boolean for a claim that holds"`
}

// CheckResult is check's result.
type CheckResult struct {
	ResultId uint64   `desc:"the result the assertion was checked against"`
	Columns  []string `desc:"the row's columns, by name"`
	Cells    []string `desc:"the row's cells as text; a NULL cell is empty text and listed in Nulls"`
	Nulls    []int32  `json:",omitzero" desc:"the indexes into Columns of the cells that are NULL"`
	Rows     int64    `desc:"how many rows the statement returned; a check holds only one"`
	// StatementDigest and SourceDigest are the canonical forms' digests:
	// the assertion's and the statement that produced the result.
	StatementDigest string `desc:"a digest of the assertion in its canonical form"`
	SourceDigest    string `desc:"a digest of the result's statement in its canonical form, as describe_result reports it"`
	Endpoint        string `desc:"the endpoint the check ran on, as a grant names it"`
	Confined        bool   `json:",omitzero" desc:"true when the check read confined data"`
}

// statementDigest is the digest of a statement in its canonical form with
// its whitespace removed — the canonical form quotes every identifier, so
// the tokens stay apart — so two spellings of one statement digest alike;
// a statement that does not parse digests as written.
func statementDigest(stmt string) (digest string) {
	text := strings.TrimSpace(stmt)
	if text == "" {
		return ""
	}
	if canonical, err := passes.CanonicalizeFull(128).Run(text); err == nil {
		text = strings.Join(strings.Fields(canonical), "")
	}
	sum := blake3.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// checkScope refuses an assertion naming a table outside the tables the
// source statement read: table references, and keelson() tables, the
// assertion's own CTEs excepted; a table the source named unqualified the
// assertion names unqualified too, so another database's table of the
// same name does not pass. An assertion naming no table at all is
// refused, so a tautology cannot be asserted (ADR-0340 §SD3). Both texts
// are compared as authored, before the client-side rewrite, so a bound
// dataset's alias meets itself and not its handle.
func checkScope(assertion string, source string) (err error) {
	apr, perr := nanopass.Parse(assertion)
	if perr != nil {
		return app.RefuseOperation("the assertion does not parse: " + perr.Error())
	}
	allowed := map[string]bool{}
	if spr, serr := nanopass.Parse(source); serr == nil {
		for _, t := range analysis.ExtractTables(spr) {
			allowed[t.Database+"."+t.Table] = true
		}
		for _, t := range keelsonsql.References(source) {
			allowed["keelson('"+t+"')"] = true
		}
	}
	ctes := map[string]bool{}
	if scopes, serr := nanopass.BuildScopes(apr, ""); serr == nil {
		for _, sc := range scopes {
			for _, d := range sc.CTEDefs {
				ctes[d.Name] = true
			}
		}
	}
	named := 0
	for _, t := range analysis.ExtractTables(apr) {
		if ctes[t.Table] {
			continue
		}
		named++
		if !allowed[t.Database+"."+t.Table] {
			name := t.Table
			if t.Database != "" {
				name = t.Database + "." + t.Table
			}
			return app.RefuseOperation("the assertion names " + name + ", which the result's statement did not read as such; name tables as that statement did")
		}
	}
	for _, t := range keelsonsql.References(assertion) {
		named++
		if !allowed["keelson('"+t+"')"] {
			return app.RefuseOperation("the assertion names keelson('" + t + "'), which the result's statement did not read")
		}
	}
	if named == 0 {
		return app.RefuseOperation("the assertion names no table of the result's statement, so it checks nothing")
	}
	return
}

// checkSnap is what a check reads of the window: the main result's id and
// label, the statement that produced it and the signal parameters it
// shipped.
type checkSnap struct {
	resultId  uint64
	confined  bool
	source    string
	sigParams map[string]string
}

func snapshotCheck(p *PlayApp) (s checkSnap) {
	if p.graph == nil {
		return
	}
	rec, _, _, _, _, _, _, _, id := p.graph.MainSnapshot()
	if rec != nil {
		rec.Release()
	}
	s.resultId, s.confined, s.source = uint64(id), p.graph.MainConfined(), p.graph.MainSQL()
	if len(p.lastSentSigParams) > 0 {
		s.sigParams = make(map[string]string, len(p.lastSentSigParams))
		for k, v := range p.lastSentSigParams {
			s.sigParams[strings.TrimPrefix(k, "param_")] = v
		}
	}
	return
}

func addCheckOp(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.ExternalRead(s, app.OperationSpec{Name: opCheck, Version: 1,
		Summary: "run an assertion over the tables the main result's statement read, under the agent limits, and return its one row: what a cited claim is checked by",
		Reads:   []string{opsResResult}, Agents: true, Untrusted: true,
		Follows: []string{"nothing changes in the window; the row and the two digests are the check's record"}},
		func(sn opsSnap, call app.OperationCall, in CheckArgs) (out CheckResult, err error) {
			if err = schemaReadable(sn, call.OnBehalfOf); err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
			defer cancel()
			return runCheck(ctx, sn.client, sn.check, call.OnBehalfOf, in)
		})
}

// runCheck is check over one window's state: the gate in SD3's order, then
// the statement, bounded.
func runCheck(ctx context.Context, client *Client, cs checkSnap, obo *app.OnBehalfOf, in CheckArgs) (out CheckResult, err error) {
	if err = statementBounds(in.Statement); err != nil {
		return
	}
	if cs.resultId == 0 || cs.source == "" {
		return out, app.RefuseOperation("the window holds no result to check against; run first")
	}
	if in.ResultId != cs.resultId {
		return out, app.ConflictOperation("another result is held now: " + strconv.FormatUint(cs.resultId, 10))
	}
	residual, params, _ := client.rewriteFor(obo, in.Statement, nil)
	dec := client.previewDispatch(residual, "")
	if obo != nil {
		if lerr := checkAgentLimits(residual, dec, obo, client.datasetAliasOf()); lerr != nil {
			if limit, ok := lerr.(*AgentLimitError); ok && limit.Destination != "" {
				return out, app.RefuseForDestinations(lerr.Error(), limit.Destination)
			}
			return out, app.RefuseOperation(lerr.Error())
		}
	}
	if err = checkScope(in.Statement, cs.source); err != nil {
		return
	}
	eng, err := client.engineFor(dec)
	if err != nil {
		return out, app.RefuseOperation("the check cannot be dispatched: " + err.Error())
	}
	// The row bound is in the statement itself — a wrapper that keeps two
	// rows, so a second row is seen and refused and no block of a wide
	// result is read — with the server's own bound beside it; the byte
	// bound is applied to the cells.
	q, ferr := passes.SetFormat("JSONCompact").Run("SELECT * FROM (" + residual + ") AS assertion LIMIT " + strconv.Itoa(checkMaxRows+1))
	if ferr != nil {
		return out, app.RefuseOperation("the assertion takes no format clause: " + ferr.Error())
	}
	req := queryengine.Request{
		SQL:         q,
		Format:      "JSONCompact",
		Params:      bareParams(q, cs.sigParams, params),
		Settings:    map[string]string{"readonly": "2", "max_result_rows": strconv.Itoa(checkMaxRows + 1), "result_overflow_mode": "break", "max_execution_time": strconv.Itoa(int(checkTimeout / time.Second))},
		Sensitivity: dec.sensitivity,
	}
	target, _ := dec.target()
	sentLevel := client.knownReadonlyLevel(target)
	degradeForReadonly(&req, sentLevel)
	body, derr := deliverBody(ctx, eng, req)
	if level, retry := client.learnReadonlyLevel(ctx, eng, target, sentLevel, derr); retry {
		degradeForReadonly(&req, level)
		body, derr = deliverBody(ctx, eng, req)
	}
	if derr != nil {
		return out, app.RefuseOperation("the check failed: " + derr.Error())
	}
	var parsed struct {
		Meta []struct {
			Name string `json:"name"`
		} `json:"meta"`
		Data [][]any `json:"data"`
		Rows int64   `json:"rows"`
	}
	if jerr := json.Unmarshal(body, &parsed); jerr != nil {
		return out, app.RefuseOperation("the check's result does not parse: " + jerr.Error())
	}
	out.ResultId, out.StatementDigest, out.SourceDigest = cs.resultId, statementDigest(in.Statement), statementDigest(cs.source)
	out.Endpoint, out.Confined = DestinationClickHouse(endpointHost(target)), cs.confined || dec.sensitivity == queryengine.SensitivityConfined
	out.Rows = max(parsed.Rows, int64(len(parsed.Data)))
	for _, m := range parsed.Meta {
		out.Columns = append(out.Columns, m.Name)
	}
	if out.Rows > checkMaxRows {
		return out, app.RefuseOperation("the assertion returns " + strconv.FormatInt(out.Rows, 10) + " rows; a check holds one")
	}
	if len(parsed.Data) == 0 {
		return out, app.RefuseOperation("the assertion returns no row; a check holds one")
	}
	bytes := 0
	for i, v := range parsed.Data[0] {
		if v == nil {
			out.Nulls = append(out.Nulls, int32(i))
			out.Cells = append(out.Cells, "")
			continue
		}
		cell := cellText(v)
		bytes += len(cell)
		if bytes > checkMaxBytes {
			return out, app.RefuseOperation("the row is longer than the byte bound (" + strconv.Itoa(checkMaxBytes>>10) + " KiB of cell text)")
		}
		out.Cells = append(out.Cells, cell)
	}
	if len(out.Cells) > 1 && len(out.Columns) > 1 {
		return out, app.RefuseOperation("the assertion returns " + strconv.Itoa(len(out.Cells)) + " columns; a check holds one cell")
	}
	return
}

// cellText is a JSONCompact cell as text: numbers and booleans as
// ClickHouse prints them, strings as they are.
func cellText(v any) (s string) {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// deliverBody sends req and collects its body, folding a failed terminal
// into the error.
func deliverBody(ctx context.Context, eng deliverI, req queryengine.Request) (body []byte, err error) {
	st, _, err := eng.Deliver(ctx, req)
	if err != nil {
		return
	}
	defer func() { _ = st.Close() }()
	body, term, err := queryengine.Collect(st)
	if err != nil {
		return
	}
	if term.State == runstream.TerminalFailed {
		err = term.Err
	}
	return
}

// deliverI is the one method of an engine a check needs.
type deliverI interface {
	Deliver(ctx context.Context, req queryengine.Request) (queryengine.StreamI, queryengine.Result, error)
}
