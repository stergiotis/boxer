package sqlapplet

// An operable bundle view (ADR-0288 §SD8) offers agents a subset
// of play's operations through the receiver's own catalog. BundleViewOps
// declares them once per receiver type: each is play's operation under a
// bundle_ prefix, with play's arguments plus a bundle_view argument that
// names the view, and each is served by the play embedded in that view — play's own
// handler, so a run is play's run under play's agent limits. The catalog is
// static; an operation for a pane no view's bundle shows is declared and
// reported unavailable, with the reason.

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

const (
	// BundleOpPrefix prefixes every operation and resource an operable view
	// adds to a receiver's catalog.
	BundleOpPrefix = "bundle_"
	// bundleViewsMount is the key the views' snapshots are captured under.
	bundleViewsMount  = "bundle_views"
	opListBundleViews = BundleOpPrefix + "list_views"
)

// bundleViewArg is what every bundle_ operation takes beyond play's
// arguments.
type bundleViewArg struct {
	BundleView string
}

// BundleViewInfo is one operable view as bundle_list_views reports it.
type BundleViewInfo struct {
	View     string   `desc:"the view's name, what every bundle_ operation's bundle_view argument takes"`
	Bundle   string   `desc:"the bundle it shows"`
	Revision uint64   `json:",omitzero" desc:"the bundle revision it shows; zero while it waits"`
	Panes    []string `json:",omitzero" desc:"the result panes it shows; only their bundle_ pane operations are available"`
	Waiting  string   `json:",omitzero" desc:"what it waits for, when it does not show the bundle yet"`
}

// BundleViewList is bundle_list_views' result.
type BundleViewList struct {
	Views []BundleViewInfo `desc:"the window's operable bundle views, by name"`
}

// viewSnap is one view as a snapshot captured it.
type viewSnap struct {
	info BundleViewInfo
	snap app.OperationsSnapshotI
}

// viewsCapture is every operable view as a snapshot captured it.
type viewsCapture map[string]viewSnap

// BundleViewOps declares the operable views' operations in a receiver's
// catalog. views returns the receiver's bundle views by name, on the render
// goroutine; only operable ones are offered. A receiver showing one view
// may name it anything: the bundle_view argument may then be left out.
func BundleViewOps[A any, S any](set *appops.Set[A, S], views func(inst A) map[string]*BundleView) {
	operable := func(inst A) (out map[string]*BundleView) {
		out = make(map[string]*BundleView)
		for name, v := range views(inst) {
			if v != nil && v.Operable() {
				out[name] = v
			}
		}
		return
	}
	set.Mount(bundleViewsMount, func(inst A) any {
		capture := make(viewsCapture)
		for name, v := range operable(inst) {
			vs := viewSnap{info: BundleViewInfo{View: name, Bundle: v.Alias(), Revision: v.Revision(),
				Panes: slices.Clone(v.Panes()), Waiting: v.Waiting()}}
			if p := v.Inner(); p != nil {
				vs.snap = p.ServedOperations().Snapshot()
			}
			capture[name] = vs
		}
		return capture
	})
	appops.MountedQuery(set, bundleViewsMount, app.OperationSpec{Name: opListBundleViews, Version: 1,
		Summary: "list this window's operable bundle views: the bundle each shows and its panes",
		Agents:  true, Untrusted: true,
		Follows: []string{"every bundle_ operation names a view by its bundle_view argument; with one view it may be left out"}},
		func(v any, _ appops.None) (out BundleViewList, err error) {
			c, _ := v.(viewsCapture)
			for _, name := range slices.Sorted(maps.Keys(c)) {
				out.Views = append(out.Views, c[name].info)
			}
			return
		})
	for _, r := range play.OperableResources() {
		name := r.Name
		set.Resource(BundleOpPrefix+name, r.Summary+", per operable bundle view", func(inst A) any {
			vs := operable(inst)
			var b strings.Builder
			for _, view := range slices.Sorted(maps.Keys(vs)) {
				p := vs[view].Inner()
				if p == nil {
					continue
				}
				fmt.Fprintf(&b, "%s@%d=%v;", view, vs[view].Revision(), p.ServedOperations().ResourceValue(name))
			}
			return b.String()
		})
	}
	for _, o := range play.OperableOperations() {
		spec := bundleSpecOf(o.Spec)
		target, pane := o.Spec.Name, o.Pane
		set.MountedAvailable(spec.Name, bundleViewsMount, func(v any) (ok bool, reason string) {
			c, _ := v.(viewsCapture)
			return availableIn(c, target, pane)
		})
		if o.Spec.Class == app.OperationClassQuery {
			appops.MountedQueryRaw(set, bundleViewsMount, spec, func(v any, args []byte) (result []byte, err error) {
				c, _ := v.(viewsCapture)
				vs, err := pickView(c, args)
				if err != nil {
					return
				}
				if err = refusePane(vs.info.View, vs.info.Panes, pane); err != nil {
					return
				}
				if vs.snap == nil {
					return nil, app.RefuseOperation("the view " + vs.info.View + " waits: " + vs.info.Waiting)
				}
				return vs.snap.Query(target, args)
			})
			continue
		}
		appops.MountedCommandRaw(set, spec, func(inst A, call app.OperationCall, args []byte) (result []byte, err error) {
			vs := operable(inst)
			name, err := viewNameOf(args, slices.Collect(maps.Keys(vs)))
			if err != nil {
				return
			}
			v := vs[name]
			if err = refusePane(name, v.Panes(), pane); err != nil {
				return
			}
			p := v.Inner()
			if p == nil {
				return nil, app.RefuseOperation("the view " + name + " waits: " + v.Waiting())
			}
			// Availability was judged over every view; the command runs
			// on this one, so it is judged again here.
			if ok, reason := p.ServedOperations().Snapshot().Available(target); !ok {
				return nil, app.RefuseOperation("the view " + name + ": " + reason)
			}
			result, err = p.ServedOperations().ApplyCommand(call, target, args)
			if err == nil {
				v.commanded = true
			}
			return
		})
	}
}

// bundleSpecOf is play's spec as an operable view offers it: prefixed, its
// resources prefixed, and its arguments play's plus the view.
func bundleSpecOf(s app.OperationSpec) (out app.OperationSpec) {
	out = s
	out.Name = BundleOpPrefix + s.Name
	out.Summary = s.Summary + " — in a bundle view"
	out.Args = withViewArg(s.Args)
	out.Reads = prefixed(s.Reads)
	out.Writes = prefixed(s.Writes)
	out.Refs = nil
	out.Agents = true
	return
}

func prefixed(names []string) (out []string) {
	for _, n := range names {
		out = append(out, BundleOpPrefix+n)
	}
	return
}

// withViewArg is args with a BundleView field in front — not View, which
// play's own arguments use (set_dist_options). The bytes of the struct it
// makes decode into args as they are: a decoder ignores the field args lacks.
func withViewArg(args reflect.Type) (t reflect.Type) {
	fields := []reflect.StructField{{Name: "BundleView", Type: reflect.TypeFor[string](),
		Tag: `json:",omitzero" desc:"the bundle view to address, as bundle_list_views names it; may be left out when the window has one"`}}
	if args != nil {
		for i := range args.NumField() {
			fields = append(fields, args.Field(i))
		}
	}
	return reflect.StructOf(fields)
}

// viewNameOf reads the view argument, defaulting to the only view there is.
func viewNameOf(args []byte, names []string) (name string, err error) {
	if len(args) > 0 {
		var a bundleViewArg
		if a, err = buscodec.Decode[bundleViewArg](args); err != nil {
			return
		}
		name = a.BundleView
	}
	switch {
	case name != "" && slices.Contains(names, name):
		return
	case name != "":
		return "", app.RefuseOperation("no operable bundle view named " + name + "; bundle_list_views lists them")
	case len(names) == 1:
		return names[0], nil
	case len(names) == 0:
		return "", app.RefuseOperation("the window shows no operable bundle view")
	}
	return "", app.RefuseOperation("the window shows several bundle views; name one with bundle_view, as bundle_list_views lists them")
}

func pickView(c viewsCapture, args []byte) (vs viewSnap, err error) {
	name, err := viewNameOf(args, slices.Collect(maps.Keys(c)))
	if err != nil {
		return
	}
	return c[name], nil
}

func refusePane(view string, panes []string, pane string) (err error) {
	if pane != "" && !slices.Contains(panes, pane) {
		return app.RefuseOperation("the view " + view + " does not show the " + pane + " pane: its bundle's document does not name it")
	}
	return
}

// availableIn says whether some view could serve target: one that shows
// pane, when the operation belongs to one, and where play would run it.
func availableIn(c viewsCapture, target string, pane string) (ok bool, reason string) {
	if len(c) == 0 {
		return false, "the window shows no operable bundle view"
	}
	reason = "no view's bundle shows the " + pane + " pane"
	for _, name := range slices.Sorted(maps.Keys(c)) {
		vs := c[name]
		if pane != "" && !slices.Contains(vs.info.Panes, pane) {
			continue
		}
		if vs.snap == nil {
			reason = "the view " + name + " waits: " + vs.info.Waiting
			continue
		}
		if ok, reason = vs.snap.Available(target); ok {
			return
		}
	}
	return false, reason
}
