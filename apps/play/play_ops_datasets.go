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

import (
	"context"
	"slices"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

const opListDatasets = "list_datasets"

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
			return listDatasets(sn.client, call.OnBehalfOf, in.Alias)
		})
}

func listDatasets(client *Client, obo *app.OnBehalfOf, only string) (out DatasetList, err error) {
	aliases := client.DatasetAliases()
	if only != "" {
		if !slices.Contains(aliases, only) {
			return out, app.RefuseOperation("no dataset " + only + " is bound in this window; list_datasets without an alias lists them")
		}
		aliases = []string{only}
	}
	if len(aliases) > datasetMaxListed {
		aliases, out.Truncated = aliases[:datasetMaxListed], true
	}
	for _, alias := range aliases {
		d := DatasetInfo{Alias: alias, Destination: DestinationKeelson(alias)}
		d.Granted = obo == nil || slices.Contains(obo.Destinations, d.Destination)
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
