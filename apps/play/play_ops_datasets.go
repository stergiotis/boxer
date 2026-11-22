package play

// list_datasets: the ad-hoc datasets bound in this window (ADR-0134 §SD4),
// as an agent needs them. An alias is a table no catalogue enumerates — it
// exists because the window bound it — so without this a model has to guess
// the name it reads with keelson('<alias>').
//
// The columns are read with a LIMIT 0 run of the dataset on the host's
// introspection plane, under the agent limits like any run (ADR-0270 §SD2):
// a dataset whose alias the grant does not list is named with the
// destination to ask for. A sealed dataset's columns are its content's own
// shape, so they come only when that one dataset is asked for, and that
// result is labelled confined (ADR-0269 §SD7): a model that may not read
// confined content gets a data handle in its place, and the listing itself
// stays readable.
//
// bind_dataset: an agent's way to bind an alias another window published,
// in a window whose launch config did not declare it (ADR-0240 §SD7). The
// window follows the alias as it follows a declared one — resolving it off
// the frame, binding the newest live dataset, tracking republish and
// retract — except that binding it runs nothing: a run is the task's to ask
// for, under its limits.

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

const (
	opListDatasets = "list_datasets"
	opBindDataset  = "bind_dataset"

	opsResDatasets = "datasets"
	opsResFollowed = "followed_datasets"
)

// BindDatasetArgs is bind_dataset's argument.
type BindDatasetArgs struct {
	Alias string `desc:"the alias the dataset was published under, as its publisher names it; never a handle"`
	As    string `json:",omitzero" desc:"the name the buffer reads it by, keelson('<as>'), when it is not the alias (ADR-0288 local names); the grant still names it by its alias"`
}

// BindDatasetResult is bind_dataset's result.
type BindDatasetResult struct {
	Alias       string `desc:"the alias"`
	Bound       bool   `desc:"true when a live dataset is bound under it already; else the window binds the newest one as soon as it resolves, or once one is published"`
	Waiting     string `json:",omitzero" desc:"why it is not bound yet; list_datasets reports it again after the window asks"`
	ReadWith    string `desc:"the table expression that reads it"`
	Destination string `desc:"what the grant must list for a run reading it"`
}

// datasetMaxListed bounds how many datasets list_datasets describes.
const datasetMaxListed = 20

// DatasetArgs is list_datasets' argument.
type DatasetArgs struct {
	Name string `json:",omitzero" desc:"one dataset or data set to describe, by its alias or name: with its columns, sealed or not, and a session data set's definition; every one when left out, with columns only for those not sealed"`
}

// The homes a listed dataset lives in.
const (
	datasetHomeWindow  = "window"
	datasetHomeSession = "session"
)

// DatasetInfo is one dataset bound in the window, or one data set of the
// data session the window's runs read.
type DatasetInfo struct {
	Name        string       `desc:"the alias of an ad-hoc dataset, or the name of a session data set"`
	Home        string       `desc:"where it lives: window — an ad-hoc dataset this host holds in its own process, bound in this window; session — a data set kept in the task's data session on the ClickHouse server, which every window and app of the task reads"`
	ReadWith    string       `desc:"the table expression that reads it: keelson('<alias>') or session('<name>'); the two join with each other and with server tables in one statement"`
	Destination string       `desc:"the destination a run reading it needs, as request_access names it"`
	Granted     bool         `desc:"true when this task may read it: the grant lists the destination, or the task published it"`
	Published   bool         `json:",omitzero" desc:"true for an ad-hoc dataset this task published: the task reads it without a grant entry"`
	Bundle      string       `json:",omitzero" desc:"the bundle an ad-hoc dataset was published in"`
	Confined    bool         `json:",omitzero" desc:"true for sealed data, or a data set derived from it: reading it makes the result confined; its columns are listed when it is asked for by name"`
	Columns     []ColumnInfo `json:",omitzero" desc:"its columns, when the task may read it; a sealed dataset's only when it is asked for by name"`
	Error       string       `json:",omitzero" desc:"why its columns could not be read"`
	Waiting     string       `json:",omitzero" desc:"set while the alias is followed but not bound: why it waits; a run naming it fails until it binds"`
	// Its size and revision: a session data set's as the service last saw
	// its table, an ad-hoc dataset's as the dataset service holds it.
	Rows       uint64   `json:",omitzero" desc:"its rows"`
	Bytes      uint64   `json:",omitzero" desc:"what it takes: a session data set's table on the server, an ad-hoc dataset's Arrow stream on this host"`
	Revision   uint64   `json:",omitzero" desc:"its revision: a session data set's, one more for each redefinition; an ad-hoc dataset's, one more for each publish under its alias"`
	State      string   `json:",omitzero" desc:"a session data set's state: live; expired or stale (made again from its definition by the next statement that reads it); failed (its last build failed, Reason says why)"`
	Reason     string   `json:",omitzero" desc:"why a session data set is stale or failed"`
	Endpoint   string   `json:",omitzero" desc:"the server a session data set lives on, as a grant names it"`
	Reads      []string `json:",omitzero" desc:"the session data sets and ad-hoc datasets a session data set's definition reads"`
	Definition string   `json:",omitzero" desc:"a session data set's defining statement, when it is asked for by name"`
}

// DatasetList is list_datasets' result.
type DatasetList struct {
	Datasets  []DatasetInfo `desc:"the ad-hoc datasets bound in this window, by alias, then the session data sets of the task's data session, by name"`
	Truncated bool          `desc:"true when more are bound than are listed"`
	// SessionError says why the session data sets could not be listed.
	SessionError string `json:",omitzero" desc:"why the session data sets could not be listed; the ad-hoc datasets are listed all the same"`
	// sealedShown is set when a sealed dataset's columns are in the result.
	sealedShown bool
}

// ResultConfined labels the result confined when it carries a sealed
// dataset's columns.
func (inst DatasetList) ResultConfined() (confined bool) { return inst.sealedShown }

var _ app.ConfinedResultI = DatasetList{}

func addDatasetOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResDatasets, "the dataset aliases bound and those waiting for a dataset", func(inst *PlayLauncher) any {
		return inst.datasetDigest()
	})
	// The aliases followed, bound or not: only bind_dataset changes the
	// set — a bind landing later moves an alias within it — so binding
	// several in a row never conflicts with the window's own progress.
	s.Resource(opsResFollowed, "the dataset aliases the window follows, bound or waiting", func(inst *PlayLauncher) any {
		return inst.followedDigest()
	})
	appops.Command(s, app.OperationSpec{Name: opBindDataset, Version: 1,
		Summary: "bind an ad-hoc dataset alias in this window, so the buffer reads the dataset with keelson('<alias>')",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResFollowed}, Agents: true,
		Follows: []string{"the window resolves the alias off the frame and binds the newest live dataset under it; list_datasets lists it, with why it waits until it is bound",
			"until a dataset is live under the alias the window says it waits for it, and binds one when it is published",
			"the binding follows republishes and retracts for the life of the window; binding runs nothing",
			"a run reading it needs keelson:<alias> in the grant, whatever name it is bound under"}},
		func(inst *PlayLauncher, call app.OperationCall, in BindDatasetArgs) (out BindDatasetResult, err error) {
			return inst.bindDatasetAlias(in.Alias, in.As)
		})
	appops.ExternalRead(s, app.OperationSpec{Name: opListDatasets, Version: 2,
		Summary: "list the ad-hoc datasets bound in this window and the session data sets of the task's data session: what to read each with, the destination a run needs, and their columns",
		Reads:   []string{opsResSessionDatasets}, Agents: true, Untrusted: true,
		Follows: []string{"request_access with a dataset's destination lets a run read it",
			"a session data set comes with its rows, bytes, revision and state; asked for by name, with its defining statement",
			"query with keep_as defines a session data set, drop_dataset drops one"}},
		func(sn opsSnap, call app.OperationCall, in DatasetArgs) (DatasetList, error) {
			switch {
			case !sn.mounted:
				return DatasetList{}, app.RefuseOperation("the window has not mounted")
			case sn.client == nil:
				return DatasetList{}, app.RefuseOperation("the window has no endpoint")
			}
			ctx, cancel := context.WithTimeout(call.Context(), schemaOpTimeout)
			defer cancel()
			out, err := listDatasets(ctx, sn.client, call.OnBehalfOf, in.Name, sn.waiting)
			if err != nil {
				return out, err
			}
			obo := call.OnBehalfOf
			if obo == nil {
				obo = sn.client.agentMark.Load()
			}
			sessions, sealed, sErr := sessionDatasetInfos(ctx, sn.client, obo, in.Name)
			if sErr != nil {
				out.SessionError = sErr.Error()
			}
			out.Datasets = append(out.Datasets, sessions...)
			out.sealedShown = out.sealedShown || sealed
			if in.Name != "" && len(out.Datasets) == 0 {
				return out, app.RefuseOperation("no dataset or session data set " + in.Name + " is here; list_datasets without a name lists them, bind_dataset binds an ad-hoc dataset, query with keep_as keeps a data set")
			}
			return out, nil
		})
}

func listDatasets(ctx context.Context, client *Client, obo *app.OnBehalfOf, only string, waiting map[string]string) (out DatasetList, err error) {
	aliases := client.DatasetAliases()
	if only != "" {
		if why, ok := waiting[only]; ok {
			out.Datasets = []DatasetInfo{waitingDataset(only, why, obo)}
			return
		}
		if !slices.Contains(aliases, only) {
			// It may name a session data set, which the caller lists.
			return out, nil
		}
		aliases = []string{only}
	} else {
		for _, alias := range slices.Sorted(maps.Keys(waiting)) {
			out.Datasets = append(out.Datasets, waitingDataset(alias, waiting[alias], obo))
		}
	}
	if len(aliases) > datasetMaxListed {
		aliases, out.Truncated = aliases[:datasetMaxListed], true
	}
	for _, alias := range aliases {
		n := client.datasetNameOf(alias)
		dests := n.destinations()
		d := DatasetInfo{Name: alias, Home: datasetHomeWindow, ReadWith: "keelson('" + alias + "')", Destination: dests[0], Bundle: n.bundle}
		if res, ok := client.boundDatasetState(ctx, alias, n.alias); ok {
			d.Rows, d.Bytes, d.Revision = res.Rows, res.Bytes, res.Revision
			if d.Bundle == "" {
				d.Bundle = res.Origin.Bundle
			}
			if n.publisherTask == "" {
				n.publisherTask = res.Origin.PublisherTask
			}
		}
		d.Published = obo != nil && n.publisherTask != "" && n.publisherTask == obo.Task
		d.Granted = obo == nil || n.coveredFor(obo, alias)
		stmt := "SELECT * FROM keelson('" + alias + "') LIMIT 0"
		residual, _ := client.buildResidualOffline(stmt, nil)
		dec := client.dispatchResidual(residual, "")
		d.Confined = dec.sensitivity == queryengine.SensitivityConfined
		if d.Granted && (!d.Confined || only != "") {
			d.Columns, d.Error = datasetColumns(client, stmt, dec, obo)
			out.sealedShown = out.sealedShown || (d.Confined && len(d.Columns) > 0)
		}
		out.Datasets = append(out.Datasets, d)
	}
	return
}

// boundDatasetState is what the dataset service holds of the dataset
// bound under local: the newest live one under alias, when it is still the
// one bound. ok is false when it is not, when the client has no bus, or
// when the service does not answer.
func (inst *Client) boundDatasetState(ctx context.Context, local string, alias string) (res adhocdata.ResolveResult, ok bool) {
	bus := inst.sessionBus()
	inst.mu.RLock()
	handle := inst.datasetBindings[local]
	inst.mu.RUnlock()
	if bus == nil || handle == "" || adhocdata.IsHandle(alias) {
		return
	}
	res, err := adhocdata.ResolveRequest(ctx, bus, alias)
	return res, err == nil && res.Handle == handle
}

// waitingDataset is a followed alias not bound yet.
func waitingDataset(alias string, why string, obo *app.OnBehalfOf) (d DatasetInfo) {
	dests := adhocdata.ReadDestinations(alias)
	d = DatasetInfo{Name: alias, Home: datasetHomeWindow, ReadWith: "keelson('" + alias + "')", Destination: dests[0], Waiting: why}
	d.Granted = obo == nil
	for _, dest := range dests {
		d.Granted = d.Granted || slices.Contains(obo.Destinations, dest)
	}
	return
}

// datasetColumns reads a dataset's schema from an empty run of it.
func datasetColumns(client *Client, stmt string, dec dispatchDecision, obo *app.OnBehalfOf) (cols []ColumnInfo, errText string) {
	ctx, cancel := context.WithTimeout(context.Background(), schemaOpTimeout)
	defer cancel()
	rdr, rs, _, err := client.ExecuteArrowStream(ctx, stmt, memory.NewGoAllocator(), &ExecOptions{Agent: obo}, nil, dec)
	if err != nil {
		return nil, err.Error()
	}
	defer func() {
		rdr.Release()
		_ = rs.Close()
	}()
	for i, f := range rdr.Schema().Fields() {
		if i == schemaMaxColumns {
			break
		}
		cols = append(cols, ColumnInfo{Name: f.Name, Type: f.Type.String()})
	}
	return
}

// bindDatasetAlias follows alias in this window under the name as (the
// alias when empty), building the follower when the launch config declared
// none. It runs on the render goroutine and resolves nothing there: the
// follower asks on its next Sync's worker round.
func (inst *PlayLauncher) bindDatasetAlias(alias string, as string) (out BindDatasetResult, err error) {
	p := inst.inner
	switch {
	case p == nil:
		return out, app.RefuseOperation("the window has not mounted")
	case p.client == nil:
		return out, app.RefuseOperation("the window has no endpoint")
	case !validDatasetIdentifier(alias):
		return out, app.RefuseOperation("an alias is a bare identifier: letters, digits and _, at most 64 bytes")
	case adhocdata.IsHandle(alias):
		return out, app.RefuseOperation("that is a dataset handle; bind the alias its publisher names")
	case as != "" && !validDatasetIdentifier(as):
		return out, app.RefuseOperation("as is a bare identifier: letters, digits and _, at most 64 bytes")
	}
	local := as
	if local == "" {
		local = alias
	}
	out = BindDatasetResult{Alias: alias, ReadWith: "keelson('" + local + "')", Destination: DestinationKeelson(alias)}
	if slices.Contains(p.client.DatasetAliases(), local) {
		if n := p.client.datasetNameOf(local); n.alias != alias {
			return out, app.RefuseOperation("the name " + local + " is bound to " + n.alias + " already; bind this alias under another name with as")
		}
		out.Bound = true
		return
	}
	if inst.follower == nil {
		inst.follower = adhocdata.NewDeferredFollower(inst.ioCtx, adhocdata.FollowerConfig{Bus: inst.bus, Log: inst.log})
		if inst.follower == nil {
			return out, app.RefuseOperation("the window has no bus to resolve datasets over")
		}
	}
	if !inst.follower.FollowAs(alias, local) {
		if inst.follower.LocalName(alias) != local {
			return out, app.RefuseOperation("this window follows " + alias + " under another name already, or another alias holds " + local)
		}
	}
	if local != alias {
		p.client.setDatasetOrigin(local, alias, "")
	}
	out.Waiting = inst.follower.Waiting()[alias]
	return
}

// followedDigest is the aliases followed, bound or waiting, compared across
// frames.
func (inst *PlayLauncher) followedDigest() (digest string) {
	if inst.inner == nil || inst.inner.client == nil {
		return ""
	}
	set := make(map[string]struct{})
	for _, a := range inst.inner.client.DatasetAliases() {
		set[a] = struct{}{}
	}
	if inst.follower != nil {
		for _, a := range inst.follower.Pending() {
			set[a] = struct{}{}
		}
	}
	return strings.Join(slices.Sorted(maps.Keys(set)), ",")
}

// datasetDigest is the bound aliases and the pending ones, compared across
// frames; the follower binds on the render goroutine, inside a frame.
func (inst *PlayLauncher) datasetDigest() (digest string) {
	if inst.inner == nil || inst.inner.client == nil {
		return ""
	}
	digest = strings.Join(inst.inner.client.DatasetAliases(), ",")
	if inst.follower != nil {
		digest += "|" + strings.Join(inst.follower.Pending(), ",")
	}
	return
}
