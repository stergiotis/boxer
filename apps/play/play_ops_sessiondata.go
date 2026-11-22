package play

// play_ops_sessiondata.go: the agent's side of session data sets
// (ADR-0406). query's keep_as defines one from the statement it is given,
// list_datasets lists them beside the ad-hoc datasets bound in the window,
// and drop_dataset drops one. A data set lives in the data session of the
// task whose call defined it — every window and app working for the task
// reads it — so the window keeps no revision of its own to guard it: the
// service's revision of each data set is what a redefinition moves, and
// what it leaves stale is reported to the call that moved it.

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
)

const (
	opDropDataset = "drop_dataset"

	opsResSessionDatasets = "session_datasets"
)

// keepSampleRows is how many rows of a data set keep_as answers with when
// the call names no limit.
const keepSampleRows = 20

// sessionCall is work a call started off the frame: done once it finished,
// with what it answered or why it failed.
type sessionCall[T any] struct {
	mu   sync.Mutex
	done bool
	out  T
	err  error
}

func (inst *sessionCall[T]) finish(out T, err error) {
	inst.mu.Lock()
	inst.done, inst.out, inst.err = true, out, err
	inst.mu.Unlock()
}

// result is the call's answer and whether it has one yet.
func (inst *sessionCall[T]) result() (out T, err error, done bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.out, inst.err, inst.done
}

// startSessionCall runs fn off the frame under the window's lifetime.
func startSessionCall[T any](client *Client, fn func(ctx context.Context) (T, error)) (call *sessionCall[T]) {
	call = &sessionCall[T]{}
	started := client.Go(func(ctx context.Context) {
		out, err := fn(ctx)
		call.finish(out, err)
	})
	if !started {
		var zero T
		call.finish(zero, errors.New("the window is closing"))
	}
	return
}

// sessionView is the window's last reading of the data session its runs
// read: the task's while an agent's mark is on the window, the window's
// own otherwise. Refreshed off the frame.
type sessionView struct {
	mu    sync.Mutex
	infos []sessiondata.DatasetInfo
	task  bool
	err   string
	at    time.Time
	busy  bool
	// asked numbers the refreshes asked for; landed is the number of the
	// last one whose listing has landed, which began after it was asked.
	asked  uint64
	landed uint64
	// again is a refresh asked for while a listing was in flight, and the
	// task whose data session it lists.
	again    bool
	againObo *app.OnBehalfOf
}

// read is the view as last refreshed.
func (inst *sessionView) read() (infos []sessiondata.DatasetInfo, task bool, errText string, at time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.infos, inst.task, inst.err, inst.at
}

// landedSince reports whether the refresh ticket names, and every one
// before it, has landed.
func (inst *sessionView) landedSince(ticket uint64) (yes bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.landed >= ticket
}

// digest is the data sets as name@revision:state, compared across frames.
func (inst *sessionView) digest() (d string) {
	infos, task, _, _ := inst.read()
	parts := make([]string, 0, len(infos)+1)
	if task {
		parts = append(parts, "task")
	}
	for _, i := range infos {
		parts = append(parts, i.Name+"@"+strconv.FormatUint(i.Revision, 10)+":"+i.State.String())
	}
	return strings.Join(parts, ",")
}

// latchSessions moves the session_datasets resource to the last listing
// of the data session: a listing lands off the frame, and the resource
// moves only inside one, before the app's frame.
func (inst *PlayApp) latchSessions() {
	inst.sessionsSeen = inst.sessions.digest()
}

// sessionsShown reports whether the refresh ticket names has landed and
// the frame has latched it, so the session_datasets resource shows what
// that listing read.
func (inst *PlayApp) sessionsShown(ticket uint64) (yes bool) {
	return inst.sessions.landedSince(ticket) && inst.sessionsSeen == inst.sessions.digest()
}

// refreshSessionsFor lists obo's task's data session, or the window's own
// when obo is nil, off the frame, and returns the refresh's ticket for
// sessionsShown. One asked for while a listing is in flight runs once
// that one lands, so a listing that began before a change never stands
// for a refresh asked after it.
func (inst *PlayApp) refreshSessionsFor(obo *app.OnBehalfOf) (ticket uint64) {
	c := inst.client
	if c == nil || c.sessionBus() == nil {
		return 0
	}
	v := &inst.sessions
	v.mu.Lock()
	v.asked++
	ticket = v.asked
	if v.busy {
		v.again, v.againObo = true, obo
		v.mu.Unlock()
		return
	}
	v.busy = true
	v.mu.Unlock()
	inst.listSessions(obo, ticket)
	return
}

// listSessions lists obo's data session off the frame for refresh ticket,
// then the refresh asked for meanwhile, if any.
func (inst *PlayApp) listSessions(obo *app.OnBehalfOf, ticket uint64) {
	c := inst.client
	v := &inst.sessions
	started := c.Go(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, schemaOpTimeout)
		infos, err := c.listSession(ctx, obo)
		cancel()
		v.mu.Lock()
		v.at, v.task, v.landed = time.Now(), obo != nil, max(v.landed, ticket)
		if err != nil {
			v.err = err.Error()
		} else {
			v.infos, v.err = infos, ""
		}
		again, next, nextObo := v.again, v.asked, v.againObo
		v.again, v.againObo, v.busy = false, nil, again
		v.mu.Unlock()
		if again {
			inst.listSessions(nextObo, next)
		}
	})
	if !started {
		// The window is closing: nothing will land, and nothing waits.
		v.mu.Lock()
		v.busy, v.again, v.againObo, v.landed = false, false, nil, max(v.landed, v.asked)
		v.mu.Unlock()
	}
}

// KeptDataset is what query's keep_as kept.
type KeptDataset struct {
	Name     string       `desc:"the data set's name"`
	ReadWith string       `desc:"the table expression later statements read it with: session('<name>')"`
	Revision uint64       `desc:"its revision: 1 for a new name, one more for each redefinition"`
	Rows     uint64       `desc:"its rows"`
	Bytes    uint64       `desc:"what its table takes on the server"`
	Columns  []ColumnInfo `desc:"its columns"`
	Endpoint string       `desc:"the server it lives on, as a grant names it"`
	Confined bool         `json:",omitzero" desc:"true when it was derived from sealed data, an ad-hoc dataset: every statement reading it is confined"`
	Replayed []string     `json:",omitzero" desc:"data sets made again from their definitions first, because the server had lost them, as name@revision"`
	Stale    []string     `json:",omitzero" desc:"data sets that read the redefined one: rebuilt from their definitions by the next statement that reads them"`
	Cite     string       `desc:"how an assertion citing this answer's result_id names the data"`
}

// keptOf is the answer of a definition.
func keptOf(res sessiondata.DefineResult, stale []string) (k *KeptDataset) {
	d := res.Dataset
	k = &KeptDataset{Name: d.Name, ReadWith: "session('" + d.Name + "')", Revision: d.Revision, Rows: d.Rows, Bytes: d.Bytes,
		Endpoint: DestinationClickHouse(d.Endpoint), Confined: d.Confined, Replayed: res.Replayed, Stale: stale,
		Cite: "an assertion resting on this result reads session('" + d.Name + "'), the table its statement read, not the tables the definition read"}
	for i, c := range d.Columns {
		if i == schemaMaxColumns {
			break
		}
		k.Columns = append(k.Columns, ColumnInfo{Name: c.Name, Type: c.Type})
	}
	return
}

// keepResult is a definition and the data sets it left stale.
type keepResult struct {
	res   sessiondata.DefineResult
	stale []string
}

// staleDependants are the data sets of infos that read name, directly or
// through others, and are stale.
func staleDependants(infos []sessiondata.DatasetInfo, name string) (stale []string) {
	reads := map[string]bool{name: true}
	for changed := true; changed; {
		changed = false
		for _, i := range infos {
			if reads[i.Name] {
				continue
			}
			for _, dep := range i.Dependencies {
				if reads[dep] {
					reads[i.Name], changed = true, true
					break
				}
			}
		}
	}
	for _, i := range infos {
		if i.Name != name && reads[i.Name] && i.State == sessiondata.DatasetStateStale {
			stale = append(stale, i.Name)
		}
	}
	sort.Strings(stale)
	return
}

// startKeep defines a data set off the frame for obo's task, or the
// window's data session when obo is nil, and lists what it left stale.
func (inst *PlayApp) startKeep(obo *app.OnBehalfOf, name string, sql string, orderBy []string) (call *sessionCall[keepResult]) {
	c := inst.client
	return startSessionCall(c, func(ctx context.Context) (out keepResult, err error) {
		ctx, cancel := context.WithTimeout(ctx, sessiondata.RequestTimeout)
		defer cancel()
		if obo != nil {
			if err = c.sessionDefineLimits(ctx, obo, sql); err != nil {
				return
			}
		}
		out.res, err = c.defineSession(ctx, obo, name, sql, orderBy)
		if err != nil {
			return
		}
		if infos, lErr := c.listSession(ctx, obo); lErr == nil {
			out.stale = staleDependants(infos, name)
		}
		return
	})
}

// keepGrant refuses a definition for obo whose grant lacks the endpoint or
// the right to keep session tables on it, naming the first missing.
func keepGrant(client *Client, obo *app.OnBehalfOf) (err error) {
	target, err := client.sessionTarget()
	if err != nil {
		return app.RefuseOperation(err.Error())
	}
	if obo == nil {
		return
	}
	host := endpointHost(target)
	for _, d := range []string{sessiondata.DestinationServer(host), sessiondata.DestinationSession(host)} {
		if !slices.Contains(obo.Destinations, d) {
			return app.RefuseForDestinations("keeping a data set needs "+d+" in the task's grant", d)
		}
	}
	return
}

// sessionRefusal is a failed session call as an operation's refusal: a
// missing grant destination named for the coordinator to ask for.
func sessionRefusal(err error) error {
	if limit, ok := errors.AsType[*AgentLimitError](err); ok && limit.Destination != "" {
		return app.RefuseForDestinations(err.Error(), limit.Destination)
	}
	return app.RefuseOperation(err.Error())
}

// sampleStatement reads the first rows of a data set.
func sampleStatement(name string, limit uint32) (sql string) {
	if limit == 0 || limit > keepSampleRows {
		limit = keepSampleRows
	}
	return "SELECT * FROM session('" + name + "') LIMIT " + strconv.FormatUint(uint64(limit), 10)
}

// dropCall is a drop_dataset call in flight: the drop off the frame, the
// task it drops for, and the refresh of the window's listing it waits for
// once the drop is done.
type dropCall struct {
	call    *sessionCall[[]string]
	obo     *app.OnBehalfOf
	refresh uint64
}

// DropDatasetArgs is drop_dataset's argument.
type DropDatasetArgs struct {
	Name    string `desc:"the session data set to drop, as list_datasets names it"`
	Cascade bool   `json:",omitzero" desc:"drop the data sets that read it too; without it a data set others read is refused with their names"`
}

// DropDatasetResult is drop_dataset's result.
type DropDatasetResult struct {
	Dropped []string `desc:"the data sets dropped, as name@revision"`
}

func addSessionDatasetOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResSessionDatasets, "the session data sets of the data session the window's runs read, as name@revision:state", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.sessionsSeen
	})
	appops.Command(s, app.OperationSpec{Name: opDropDataset, Version: 1,
		Summary: "drop a session data set from the task's data session: its definition and its table on the server",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSessionDatasets}, AnyRevision: true, Agents: true, Async: true,
		Follows: []string{"every window and app of the task stops reading it; a statement naming it is refused",
			"query with keep_as defines it again"}},
		func(inst *PlayLauncher, call app.OperationCall, in DropDatasetArgs) (out DropDatasetResult, err error) {
			p := inst.inner
			switch {
			case p == nil:
				return out, app.RefuseOperation("the window has not mounted")
			case p.client == nil || p.client.sessionBus() == nil:
				return out, app.RefuseOperation("the window is not wired to the session data service")
			case !sessiondata.ValidName(in.Name):
				return out, app.RefuseOperation("name a data set by its identifier, as list_datasets lists it")
			}
			obo := call.OnBehalfOf
			if obo == nil {
				obo = p.client.agentMark.Load()
			}
			c := p.client
			if p.dropCalls == nil {
				p.dropCalls = make(map[string]*dropCall, 1)
			}
			p.dropCalls[call.Key] = &dropCall{obo: obo, call: startSessionCall(c, func(ctx context.Context) ([]string, error) {
				ctx, cancel := context.WithTimeout(ctx, sessiondata.RequestTimeout)
				defer cancel()
				return c.dropSession(ctx, obo, in.Name, in.Cascade)
			})}
			return out, nil
		})
	appops.Settles(s, opDropDataset, func(inst *PlayLauncher, call app.OperationCall) (out DropDatasetResult, settled bool, err error) {
		p := inst.inner
		if p == nil {
			return out, true, app.RefuseOperation("the window has not mounted")
		}
		dc := p.dropCalls[call.Key]
		if dc == nil {
			return out, true, app.RefuseOperation("this window no longer knows the drop the call started; list_datasets says what the session holds")
		}
		dropped, dErr, done := dc.call.result()
		if !done {
			return out, false, nil
		}
		if dErr == nil {
			// The call settles once the window's listing shows the drop,
			// so the revision it reports is the one the window holds.
			if dc.refresh == 0 {
				dc.refresh = p.refreshSessionsFor(dc.obo)
			}
			if dc.refresh != 0 && !p.sessionsShown(dc.refresh) {
				return out, false, nil
			}
		}
		delete(p.dropCalls, call.Key)
		if dErr != nil {
			p.refreshSessionsFor(dc.obo)
			return out, true, sessionRefusal(dErr)
		}
		return DropDatasetResult{Dropped: dropped}, true, nil
	})
}

// sessionDatasetInfos is list_datasets' rows for the data session obo's
// runs read: one per data set, with its columns unless it is confined and
// not the one asked for.
func sessionDatasetInfos(ctx context.Context, client *Client, obo *app.OnBehalfOf, only string) (out []DatasetInfo, sealedShown bool, err error) {
	infos, err := client.listSession(ctx, obo)
	if err != nil {
		return
	}
	for _, i := range infos {
		if only != "" && i.Name != only {
			continue
		}
		dest := DestinationClickHouse(i.Endpoint)
		d := DatasetInfo{Name: i.Name, Home: datasetHomeSession, ReadWith: "session('" + i.Name + "')", Destination: dest,
			Granted: obo == nil || slices.Contains(obo.Destinations, dest), Confined: i.Confined,
			Rows: i.Rows, Bytes: i.Bytes, Revision: i.Revision, State: i.State.String(), Reason: i.Reason, Endpoint: dest}
		d.Reads = append(append(d.Reads, i.Dependencies...), i.Attachments...)
		if only != "" {
			d.Definition = i.SQL
		}
		if !i.Confined || only != "" {
			for k, c := range i.Columns {
				if k == schemaMaxColumns {
					break
				}
				d.Columns = append(d.Columns, ColumnInfo{Name: c.Name, Type: c.Type})
			}
			sealedShown = sealedShown || (i.Confined && len(d.Columns) > 0)
		}
		out = append(out, d)
	}
	return
}
