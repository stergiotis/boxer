package appops

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opjson"
)

// View is an operation as a caller and a table see it: its declaration with
// the JSON Schemas of its arguments and result derived. A withdrawn catalog
// is one View with an empty Name and the Diagnostic set.
type View struct {
	App        app.AppIdT
	AppDisplay string
	Name       string
	Version    uint16
	Summary    string
	Class      app.OperationClassE
	Effect     app.OperationEffectE
	Reads      []string
	Writes     []string
	Refs       []string
	Follows    []string
	Agents     bool
	Untrusted  bool
	Gesture    string
	// Consent is the grant destination that covers a call without a
	// confirmation, as app.OperationConsent.Pattern writes it.
	Consent      string
	ArgsSchema   string
	ResultSchema string
	Diagnostic   string
}

// Views lists the operations of every registration, in registry order and
// catalog order within an app. Apps without a catalog contribute nothing;
// a withdrawn catalog contributes its diagnostic.
func Views(regs []app.Registration) (views []View) {
	for _, r := range regs {
		m := r.Manifest
		if r.OperationsDiagnostic != "" {
			views = append(views, View{App: m.Id, AppDisplay: m.Display, Diagnostic: r.OperationsDiagnostic})
			continue
		}
		if m.Operations == nil {
			continue
		}
		for _, o := range m.Operations.Operations {
			views = append(views, ViewOf(m, o))
		}
	}
	return
}

// ViewOf derives the view of one operation. A schema that cannot be derived
// cannot occur in a registered catalog, which validation already checked.
func ViewOf(m app.Manifest, o app.OperationSpec) (v View) {
	v = View{
		App: m.Id, AppDisplay: m.Display, Name: o.Name, Version: o.Version, Summary: o.Summary,
		Class: o.Class, Effect: o.Effect, Reads: o.Reads, Writes: o.Writes, Refs: o.Refs, Follows: o.Follows,
		Agents: o.Agents, Untrusted: o.Untrusted, Gesture: o.Gesture, Consent: o.Consent.Pattern(),
	}
	if s, err := opjson.Schema(o.Args); err == nil {
		v.ArgsSchema = string(s)
	}
	if s, err := opjson.Schema(o.Result); err == nil {
		v.ResultSchema = string(s)
	}
	return
}
