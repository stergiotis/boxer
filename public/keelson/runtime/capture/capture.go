package capture

import (
	"image"
	"slices"
	"strconv"
	"strings"
)

// FormatE is what a capture produces.
type FormatE string

const (
	// FormatSvg is the window's shapes as SVG.
	FormatSvg FormatE = "svg"
	// FormatPng is the windows' pixels as PNG.
	FormatPng FormatE = "png"
)

// Request is one capture, as the PEP receives it.
type Request struct {
	// Windows are the instance keys to capture; a request names at least one.
	Windows []uint64
	Format  FormatE
	// Crop, in logical points, keeps only this part of the frame; nil keeps
	// all of it.
	Crop *image.Rectangle
}

// Facts are what the PDP reads and never writes (the PIP's answers for one
// request).
type Facts struct {
	// Covered reports whether the subject's grant covers a window.
	Covered func(window uint64) bool
}

// EffectE is a decision's verdict.
type EffectE uint8

const (
	EffectDeny   EffectE = 0
	EffectPermit EffectE = 1
)

// Decision is the PDP's answer.
type Decision struct {
	Effect EffectE
	// Reason says why a capture was denied.
	Reason string
	// Policy names the policy that decided.
	Policy      string
	Obligations []Obligation
}

// Obligation is a condition a permitted capture carries. A named, versioned
// value: the record names each one applied, with its version.
type Obligation struct {
	Name    string
	Version uint32
	// Scope is set for ObligationScope.
	Scope *ScopeParams
}

// ObligationScope is what a capture draws: the windows, and a crop.
const ObligationScope = "scope"

// ScopeParams are the scope obligation's parameters.
type ScopeParams struct {
	Windows []uint64
	Crop    *image.Rectangle
}

// String names an obligation with its version, as the record keeps it.
func (inst Obligation) String() string {
	return inst.Name + "@" + strconv.FormatUint(uint64(inst.Version), 10)
}

// PolicyI is a policy decision point.
type PolicyI interface {
	Name() string
	Decide(req Request, facts Facts) Decision
}

// DenyOverrides composes policies: any deny wins, and the obligations of
// every permit are joined. With no policies the answer is deny.
func DenyOverrides(policies ...PolicyI) PolicyI {
	return denyOverrides(policies)
}

type denyOverrides []PolicyI

func (inst denyOverrides) Name() string {
	names := make([]string, 0, len(inst))
	for _, p := range inst {
		names = append(names, p.Name())
	}
	return "deny-overrides(" + strings.Join(names, ",") + ")"
}

func (inst denyOverrides) Decide(req Request, facts Facts) (d Decision) {
	if len(inst) == 0 {
		return Decision{Effect: EffectDeny, Reason: "no policy decides captures", Policy: inst.Name()}
	}
	d = Decision{Effect: EffectPermit, Policy: inst.Name()}
	for _, p := range inst {
		pd := p.Decide(req, facts)
		if pd.Effect != EffectPermit {
			if pd.Policy == "" {
				pd.Policy = p.Name()
			}
			return pd
		}
		d.Obligations = append(d.Obligations, pd.Obligations...)
	}
	return
}

// GrantPolicy is ADR-0269's grant rule: every window a capture names has an
// entry in the subject's grant, in any mode. It attaches the scope
// obligation, so only those windows are drawn.
type GrantPolicy struct{}

func (inst GrantPolicy) Name() string { return "grant" }

func (inst GrantPolicy) Decide(req Request, facts Facts) Decision {
	if len(req.Windows) == 0 {
		return Decision{Effect: EffectDeny, Reason: "a capture names at least one window", Policy: inst.Name()}
	}
	var uncovered []string
	for _, w := range req.Windows {
		if facts.Covered == nil || !facts.Covered(w) {
			uncovered = append(uncovered, strconv.FormatUint(w, 10))
		}
	}
	if len(uncovered) > 0 {
		return Decision{Effect: EffectDeny, Policy: inst.Name(),
			Reason: "the grant does not cover window " + strings.Join(uncovered, ", ")}
	}
	return Decision{Effect: EffectPermit, Policy: inst.Name(), Obligations: []Obligation{{
		Name: ObligationScope, Version: 1,
		Scope: &ScopeParams{Windows: slices.Clone(req.Windows), Crop: req.Crop},
	}}}
}
