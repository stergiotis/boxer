package marshallreflect

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// Role-filtered selection (ADR-0146 D3), realizing ADR-0073 E1: "on the read
// side only primary memberships are discriminative".
//
// A membership either DEFINES an attribute's identity (primary) or ANNOTATES an
// attribute some other membership defined (secondary). Only the first should
// locate a value. Without a classifier the codec cannot tell them apart, so it
// treats every membership as discriminative — which is what it has always done,
// and remains the default.
//
// # Why the default is nil and not PathPrefixClassifier
//
// membershiprole.PathPrefixClassifier marks primary by a path prefix (default
// "/"). Ordinary DTO memberships — `health`, `battery`, `droneStatus` — carry
// no such prefix, so under that policy they all classify as SECONDARY and every
// field would read back empty. It is the right default for the card / widget
// read paths, whose memberships are `/`-prefixed paths, and the wrong one here.
// Passing nil (the default) means "every membership is primary", so role
// filtering is inert until a caller supplies a policy that fits its data.
//
// # Use-aspects
//
// ADR-0073 F lets a section short-circuit the classifier by declaring
// AspectSectionMembershipsAllPrimary / …AllSecondary. Those aspects live in the
// schema IR, which a mappingplan.Plan does not carry — the codec knows a
// section's NAME and nothing else about it. A classifier that consults
// SectionContext.UseAspects therefore sees an empty set here unless the caller
// supplies them with WithSectionAspects. This is a trap worth stating plainly:
// a classifier written against the card path, where the driver supplies real
// aspects, will silently take its no-aspect branch in the codec.

// ReadOption configures a read (Unmarshal / Detect). The zero set of options is
// the behaviour every release before ADR-0146 M4 had.
type ReadOption func(*readOptions)

type readOptions struct {
	classifier     membershiprole.ClassifierI
	sectionAspects func(section string) useaspects.AspectSet
}

// WithRoleClassifier makes selection role-filtered: a membership the classifier
// calls secondary no longer locates a value, so an annotation tag cannot pull
// an attribute into a field that happens to share its name.
//
// Passing nil is the same as not passing the option at all.
func WithRoleClassifier(c membershiprole.ClassifierI) ReadOption {
	return func(o *readOptions) { o.classifier = c }
}

// WithSectionAspects supplies the per-section use-aspect set the classifier
// sees in SectionContext.UseAspects, keyed by lw: section name. Without it the
// set is empty, because a Plan does not carry the schema's use-aspects.
//
// Only meaningful together with WithRoleClassifier.
func WithSectionAspects(f func(section string) useaspects.AspectSet) ReadOption {
	return func(o *readOptions) { o.sectionAspects = f }
}

func buildReadOptions(opts []ReadOption) (o readOptions) {
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return
}

// roleFilter decides, per attribute membership, whether it discriminates. The
// zero value admits everything, which is the no-classifier default.
type roleFilter struct {
	classifier membershiprole.ClassifierI
	sec        membershiprole.SectionContext
	channel    mappingplan.MembershipChannel
}

// newRoleFilter builds the filter for one section. Returns the zero value when
// no classifier is configured, so the hot path costs one nil check per
// membership rather than a classify call.
func (o readOptions) newRoleFilter(section string, ch mappingplan.MembershipChannel) roleFilter {
	if o.classifier == nil {
		return roleFilter{}
	}
	sec := membershiprole.SectionContext{Name: naming.StylableName(section)}
	if o.sectionAspects != nil {
		sec.UseAspects = o.sectionAspects(section)
	}
	return roleFilter{classifier: o.classifier, sec: sec, channel: ch}
}

// admitsRef reports whether a ref-channel membership id discriminates.
func (f roleFilter) admitsRef(id uint64) bool {
	if f.classifier == nil {
		return true
	}
	return f.admits(membership.MembershipValue{
		Kind:    f.channel.Identity(),
		LowCard: f.channel.Cardinality() == mappingplan.ChannelCardinalityLow,
		Ref:     id,
	})
}

// admitsVerbatim reports whether a verbatim-channel membership name
// discriminates.
func (f roleFilter) admitsVerbatim(name string) bool {
	if f.classifier == nil {
		return true
	}
	return f.admits(membership.MembershipValue{
		Kind:     f.channel.Identity(),
		LowCard:  f.channel.Cardinality() == mappingplan.ChannelCardinalityLow,
		Verbatim: name,
	})
}

// admits runs the classifier. Anything the classifier does not call SECONDARY
// discriminates — a policy that returns MembershipRoleNone (no opinion) leaves
// the membership selecting, so an incomplete classifier degrades to today's
// behaviour rather than silently dropping fields.
func (f roleFilter) admits(mv membership.MembershipValue) bool {
	role, _ := f.classifier.Classify(f.sec, mv)
	return role != membershiprole.MembershipRoleSecondary
}
