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
	Alias string `json:",omitzero" desc:"one dataset to describe with its columns, sealed or not; every dataset when left out, with columns only for those not sealed"`
}

// DatasetInfo is one bound dataset.
type DatasetInfo struct {
	Alias       string       `desc:"the alias: read the dataset with keelson('<alias>')"`
	Destination string       `desc:"the destination a run reading it needs, as request_access names it"`
	Granted     bool         `desc:"true when the grant lists the destination"`
	Confined    bool         `json:",omitzero" desc:"true for a sealed dataset: reading it makes the window confined; its columns are listed when it is asked for by alias"`
	Columns     []ColumnInfo `json:",omitzero" desc:"its columns, when the grant lists it"`
	Error       string       `json:",omitzero" desc:"why its columns could not be read"`
	Waiting     string       `json:",omitzero" desc:"set while the alias is followed but not bound: why it waits; a run naming it fails until it binds"`
}

// DatasetList is list_datasets' result.
type DatasetList struct {
	Datasets  []DatasetInfo `desc:"the datasets bound in this window, by alias"`
	Truncated bool          `desc:"true when more are bound than are listed"`
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
	appops.ExternalRead(s, app.OperationSpec{Name: opListDatasets, Version: 1,
		Summary: "list the ad-hoc datasets bound in this window: the alias to read with keelson('<alias>'), the destination a run needs, and their columns",
		Agents:  true, Untrusted: true,
		Follows: []string{"request_access with a dataset's destination lets a run read it"}},
		func(sn opsSnap, call app.OperationCall, in DatasetArgs) (DatasetList, error) {
			switch {
			case !sn.mounted:
				return DatasetList{}, app.RefuseOperation("the window has not mounted")
			case sn.client == nil:
				return DatasetList{}, app.RefuseOperation("the window has no endpoint")
			}
			return listDatasets(sn.client, call.OnBehalfOf, in.Alias, sn.waiting)
		})
}

func listDatasets(client *Client, obo *app.OnBehalfOf, only string, waiting map[string]string) (out DatasetList, err error) {
	aliases := client.DatasetAliases()
	if only != "" {
		if why, ok := waiting[only]; ok {
			out.Datasets = []DatasetInfo{waitingDataset(only, why, obo)}
			return
		}
		if !slices.Contains(aliases, only) {
			return out, app.RefuseOperation("no dataset " + only + " is bound in this window; list_datasets without an alias lists them, bind_dataset binds one")
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
		d := DatasetInfo{Alias: alias, Destination: dests[0]}
		d.Granted = obo == nil
		for _, dest := range dests {
			d.Granted = d.Granted || slices.Contains(obo.Destinations, dest)
		}
		stmt := "SELECT * FROM keelson('" + alias + "') LIMIT 0"
		residual, _ := client.buildResidualOffline(stmt, nil)
		dec := client.previewDispatch(residual, "")
		d.Confined = dec.sensitivity == queryengine.SensitivityConfined
		if d.Granted && (!d.Confined || only != "") {
			d.Columns, d.Error = datasetColumns(client, stmt, dec, obo)
			out.sealedShown = out.sealedShown || (d.Confined && len(d.Columns) > 0)
		}
		out.Datasets = append(out.Datasets, d)
	}
	return
}

// waitingDataset is a followed alias not bound yet.
func waitingDataset(alias string, why string, obo *app.OnBehalfOf) (d DatasetInfo) {
	d = DatasetInfo{Alias: alias, Destination: DestinationKeelson(alias), Waiting: why}
	d.Granted = obo == nil || slices.Contains(obo.Destinations, d.Destination)
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
		inst.follower = adhocdata.NewDeferredFollower(adhocdata.FollowerConfig{Bus: inst.bus, Log: inst.log})
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
