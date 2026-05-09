//go:build llm_generated_opus47

// Package membershiprole classifies Leeway memberships as primary or secondary
// and decides whether their parameters contribute to attribute identity.
//
// The classifier sits on the consumer side of [streamreadaccess.SinkI]. It
// consumes value-level membership instances (mirroring SinkI.AddMembership*
// shapes via [MembershipValue]) and returns a [MembershipRoleE] plus a
// [ParamTreatmentE]. A consumer such as a card emitter or schema-document
// writer uses the role to decide attribute keys ("byAttribute") versus
// annotations ("labels") in its output.
//
// The role decision is application policy, not a Leeway protocol concern. The
// package ships [DefaultClassifier] as a zero-config implementation that
// consults the section's [useaspects.AspectSectionMembershipsAllPrimary] /
// [useaspects.AspectSectionMembershipsAllSecondary] hints and falls back to a
// path-prefix naming convention for verbatim-shaped memberships.
//
// See ADR-0007 (`doc/adr/0007-leeway-membership-role-classifier.md`) for the
// design rationale.
package membershiprole
