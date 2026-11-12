package windowhost

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ResolveLaunchKind is the slot resolution of an open by kind (ADR-0361
// §SD2, the composition shape of ADR-0310 §SD1): the launchable apps whose
// LaunchKind equals kind are the candidates, after the launch limit
// (ADR-0272) has removed the apps it removes from every launch. One
// candidate is the target; several take the app designated with
// app.Registry.DesignateForKind; several with no designation, or a
// designation that is not among them, and no candidate at all are refusals
// whose text says why, since a refusal reaches the caller as its reply.
// Registration order selects nothing (the keelson-app skill's X3).
func (inst *Inst) ResolveLaunchKind(kind string) (id app.AppIdT, err error) {
	var candidates []app.AppIdT
	for _, m := range inst.registry.LaunchableManifests() {
		if m.LaunchKind == kind {
			candidates = append(candidates, m.Id)
		}
	}
	switch len(candidates) {
	case 0:
		err = eb.Build().Str("kind", kind).Errorf("no app in this host takes launch kind %q", kind) //boxer:lint disable=CS013 reason="a refusal reaches the caller as its reply text, and the kind is the fact the caller can act on"
		return
	case 1:
		id = candidates[0]
		return
	}
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = string(c)
	}
	list := strings.Join(names, ", ")
	designated, ok := inst.registry.Designation(kind)
	if !ok {
		err = eb.Build().Str("kind", kind).Strs("candidates", names).Errorf("several apps take launch kind %q and none is designated: %s", kind, list) //boxer:lint disable=CS013 reason="a refusal reaches the caller as its reply text, and the candidates are what it must name"
		return
	}
	for _, c := range candidates {
		if c == designated {
			id = c
			return
		}
	}
	err = eb.Build().Str("kind", kind).Str("designated", string(designated)).Strs("candidates", names).Errorf("the app designated for launch kind %q is not launchable here (%s); candidates: %s", kind, string(designated), list) //boxer:lint disable=CS013 reason="a refusal reaches the caller as its reply text, and the designation and candidates are what it must name"
	return
}
