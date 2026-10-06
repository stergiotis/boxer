package play

// endpoint_functions: the endpoint half of the Vocabulary pane (ADR-0174),
// read by the call itself. list_functions reports whether a server
// function is installed only once the pane's probe has landed, and never
// sends it; this external read asks the endpoint directly, under the
// grant, and leaves the pane's cached probe alone, so the two can disagree
// for a moment. It provisions nothing: installation stays the process-level
// reconcile ADR-0174 describes.

import (
	"context"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

const opEndpointFunctions = "endpoint_functions"

// endpointMaxNames bounds each list of names.
const endpointMaxNames = 200

// endpointFunctionsQuery lists the endpoint's user-defined functions. The
// definition text rides along only for the two marker functions, whose
// bodies carry the surface revision (parseMarkerVersion); every other body
// stays on the server. toString(origin) for the reason vocabProbeQuery
// records: an Enum8 crosses Arrow and JSON as its ordinal otherwise.
const endpointFunctionsQuery = "SELECT name, if(name IN ({v:String}, {p:String}), create_query, '') AS def " +
	"FROM system.functions WHERE toString(origin) != 'System' ORDER BY name FORMAT JSONEachRow"

// EndpointFunctionsArgs is endpoint_functions' argument.
type EndpointFunctionsArgs struct{}

// MacroGap is a client macro whose expansion calls functions the endpoint
// lacks.
type MacroGap struct {
	Name    string   `desc:"the client macro"`
	Missing []string `desc:"the server functions its expansion calls that the endpoint does not carry"`
}

// EndpointFunctions is endpoint_functions' result.
type EndpointFunctions struct {
	Destination string `desc:"the endpoint asked, as a grant names it"`
	UserDefined int32  `desc:"how many user-defined functions the endpoint carries"`
	// The surface revision, read from the marker functions' bodies.
	SurfaceVersion    int32      `json:",omitzero" desc:"the leeway SQL surface revision the endpoint was provisioned with; left out when it carries no surface marker"`
	PreSurfaceVersion int32      `json:",omitzero" desc:"the retired pack revision of an endpoint provisioned before the surface marker existed"`
	BuildVersion      int32      `desc:"the surface revision this build writes"`
	Skew              string     `json:",omitzero" desc:"the surface revision against this build's, as the Vocabulary pane's status line says it; provisioning the endpoint is the fix for a skew, not a different query"`
	Installed         int32      `desc:"how many of the server functions this build declares the endpoint carries"`
	Missing           []string   `json:",omitzero" desc:"server functions this build declares that the endpoint does not carry; a query calling one fails with unknown function"`
	MacrosBroken      []MacroGap `json:",omitzero" desc:"client macros whose expansion calls server functions the endpoint lacks"`
	// Extras are names only: the bodies are whoever has DDL's text.
	Undeclared []string `json:",omitzero" desc:"user-defined functions on the endpoint that no roster of this build declares; a query may still call them, and lookup_docs reads one"`
	Withdrawn  []string `json:",omitzero" desc:"functions this repository shipped and withdrew that the endpoint still carries; reprovisioning drops them"`
	Truncated  bool     `desc:"true when the bound of 200 names cut a list"`
}

func addEndpointFunctionOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: the undeclared names are the endpoint's text.
	appops.ExternalRead(s, app.OperationSpec{Name: opEndpointFunctions, Version: 1,
		Summary: "ask the endpoint which user-defined functions it carries: what this build declares and it lacks, client macros that would fail, its surface revision, and the functions no roster declares",
		Agents:  true, Untrusted: true,
		Follows: []string{"the endpoint is asked now, under the grant; the Vocabulary pane's own probe is not changed",
			"nothing is installed: provisioning is not an operation"}},
		func(sn opsSnap, call app.OperationCall, in EndpointFunctionsArgs) (out EndpointFunctions, err error) {
			if err = schemaReadable(sn, call.OnBehalfOf); err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), schemaOpTimeout)
			defer cancel()
			return endpointFunctions(ctx, sn.client)
		})
}

func endpointFunctions(ctx context.Context, client *Client) (out EndpointFunctions, err error) {
	type row struct {
		Name string `json:"name"`
		Def  string `json:"def"`
	}
	raw, err := client.queryTabSeparated(ctx, endpointFunctionsQuery, map[string]string{
		"v": lwsqlsurface.VersionFunctionName, "p": lwsqlsurface.PreSurfaceVersionFunctionName})
	if err != nil {
		return
	}
	rows, err := jsonRows[row](raw)
	if err != nil {
		return
	}
	installed := make(map[string]string, len(rows))
	for _, r := range rows {
		installed[r.Name] = r.Def
	}
	return endpointFunctionsOf(endpointDestination(client), installed), nil
}

// endpointFunctionsOf is the reading of one listing, the user-defined
// functions by name with the marker bodies.
func endpointFunctionsOf(dest string, installed map[string]string) (out EndpointFunctions) {
	out.Destination, out.UserDefined, out.BuildVersion = dest, int32(len(installed)), int32(lwsqlsurface.Version)
	surface := parseMarkerVersion(installed[lwsqlsurface.VersionFunctionName])
	pre := parseMarkerVersion(installed[lwsqlsurface.PreSurfaceVersionFunctionName])
	if surface >= 0 {
		out.SurfaceVersion = int32(surface)
	}
	if pre >= 0 {
		out.PreSurfaceVersion = int32(pre)
	}
	if line, ok := vocabSurfaceSkew(surface, pre); ok {
		out.Skew = line
	} else {
		out.Skew = "no surface marker: the leeway functions were never provisioned here (this build writes v" + strconv.Itoa(lwsqlsurface.Version) + ")"
	}
	entries := vocabDeclared(sqlvocab.Default)
	vocabMarkInstalled(entries, installed)
	add := func(list *[]string, name string) {
		if len(*list) == endpointMaxNames {
			out.Truncated = true
			return
		}
		if !slices.Contains(*list, name) {
			*list = append(*list, opsLabel(name))
		}
	}
	seenMacro := map[string]bool{}
	for _, e := range entries {
		switch {
		case e.Where == sqlvocab.WhereServer && e.Declared:
			if _, ok := installed[e.Name]; ok {
				out.Installed++
			} else {
				add(&out.Missing, e.Name)
			}
		case e.Where == sqlvocab.WhereClient && len(e.Dependencies) > 0 && !seenMacro[e.Name]:
			missing := e.MissingDeps
			if len(installed) == 0 {
				// vocabMarkInstalled reads an empty set as "not known";
				// here the endpoint answered that it carries none.
				missing = e.Dependencies
			}
			if len(missing) == 0 {
				continue
			}
			seenMacro[e.Name] = true
			if len(out.MacrosBroken) == endpointMaxNames {
				out.Truncated = true
				continue
			}
			out.MacrosBroken = append(out.MacrosBroken, MacroGap{Name: e.Name, Missing: slices.Clone(missing)})
		}
	}
	for _, e := range vocabExtras(installed, entries) {
		if e.Family == vocabFamilyWithdrawn {
			add(&out.Withdrawn, e.Name)
			continue
		}
		add(&out.Undeclared, e.Name)
	}
	return
}
