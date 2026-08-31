package goplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// ParsedLWTag is the structured result of parsing an `lw:` tag value.
// Returned by SplitLW; consumed by ParsePlan and the sibling
// marshallreflect.buildPlan.
type ParsedLWTag struct {
	Membership string
	Section    string
	Column     string
	Flags      mappingplan.FieldFlags
}

// setChannelFlag installs the parsed channel on the in-progress flag
// set, rejecting two channel flags on one tag. Tokens like `,verbatim`
// and `,lowCardVerbatim` both map to MembershipChannelLowCardVerbatim
// (per ADR-0008 D3 SD9); attempting either after a different channel
// already set raises the same "declared twice" error.
func setChannelFlag(flags *mappingplan.FieldFlags, ch mappingplan.MembershipChannel, token string) (err error) {
	if flags.Channel != mappingplan.MembershipChannelLowCardRef {
		err = eb.Build().Str("flag", token).Str("alreadySet", flags.Channel.String()).Errorf("channel flag declared twice on one tag")
		return
	}
	flags.Channel = ch
	return
}

// SplitLW parses a value of the form
//
//	<membership>[,<section>[:<column>]][,<flag>][,<flag>…]
//
// into its components. Empty segments are tolerated and yield zero
// values; unknown flag tokens are an error. Exported so the sibling
// marshallreflect package can reuse the grammar without duplicating
// the parser.
func SplitLW(tag string) (out ParsedLWTag, err error) {
	parts := strings.Split(tag, ",")
	out.Membership = strings.TrimSpace(parts[0])
	if len(parts) >= 2 {
		s := strings.TrimSpace(parts[1])
		if before, after, ok := strings.Cut(s, ":"); ok {
			out.Section = before
			out.Column = after
		} else {
			out.Section = s
		}
	}
	if len(parts) < 3 {
		return
	}
	err = parseFlagTokens(parts[2:], &out.Flags)
	return
}

// parseFlagTokens parses the trailing comma-separated flag tokens of an
// lw: tag (everything after the membership and section slots) into
// flags. Shared by SplitLW and the tuple-grammar parsers (SplitTupleLW)
// so the flag vocabulary cannot drift between the two tag forms.
func parseFlagTokens(tokens []string, flags *mappingplan.FieldFlags) (err error) {
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		// Key=value flags (`const=<value>`, `ct=<canonical>`).
		if eq := strings.IndexByte(token, '='); eq > 0 {
			key := token[:eq]
			val := token[eq+1:]
			switch key {
			case "const":
				if flags.HasConst {
					err = eb.Build().Str("flag", key).Errorf("flag declared twice")
					return
				}
				flags.HasConst = true
				flags.ConstValue = val
			case "ct":
				if flags.CanonicalType != "" {
					err = eb.Build().Str("flag", key).Errorf("flag declared twice")
					return
				}
				if val == "" {
					err = eb.Build().Str("flag", key).Errorf("ct= requires a canonical-type string (e.g. ct=v for IPv4)")
					return
				}
				flags.CanonicalType = val
			default:
				err = eb.Build().Str("flag", key).Errorf("unknown key=value flag (recognised: const=<value>, ct=<canonical>)")
				return
			}
			continue
		}
		switch token {
		case "unit":
			if flags.Unit {
				err = eb.Build().Str("flag", token).Errorf("flag declared twice")
				return
			}
			flags.Unit = true
		case "explode":
			// Removed by ADR-0113 D1: the per-element (N×1) write shape is
			// authored as a nested `[]Attr` section; the default container
			// one-liner (1×N) needs no flag.
			err = eb.Build().Str("flag", token).Errorf("`,explode` was removed (ADR-0113 D1) — author a nested `[]Attr` section for one attribute per element")
			return
		case "verbatim", "lowCardVerbatim":
			// `,verbatim` retained as alias for `,lowCardVerbatim` per
			// ADR-0008 D3 SD9 — existing DTOs compile unchanged.
			if err = setChannelFlag(flags, mappingplan.MembershipChannelLowCardVerbatim, token); err != nil {
				return
			}
		case "lowCardRef":
			// Explicit spelling of the default channel. Needed so a tuple
			// `@membership` ref field can name its channel (`,lowCardRef`) at the
			// declaration site (ADR-0109); on a top-level field it is a no-op
			// alias for the empty default.
			if err = setChannelFlag(flags, mappingplan.MembershipChannelLowCardRef, token); err != nil {
				return
			}
		case "highCardRef":
			if err = setChannelFlag(flags, mappingplan.MembershipChannelHighCardRef, token); err != nil {
				return
			}
		case "highCardVerbatim":
			if err = setChannelFlag(flags, mappingplan.MembershipChannelHighCardVerbatim, token); err != nil {
				return
			}
		case "mixedLowCardRef":
			// ADR-0008 D3 Cut-2: the first carrier-paired channel. Pairs a
			// value field with a marshalltypes.MixedLowCardRef sibling
			// (id + params); see goplan.PlanBuilder + the Cut-2 update.
			if err = setChannelFlag(flags, mappingplan.MembershipChannelMixedLowCardRef, token); err != nil {
				return
			}
		case "mixedLowCardVerbatim":
			// ADR-0008 D3 Cut-2: the verbatim sibling of mixedLowCardRef —
			// pairs a value field with a marshalltypes.MixedLowCardVerbatim
			// sibling (name + params; the label embeds literally on the wire).
			if err = setChannelFlag(flags, mappingplan.MembershipChannelMixedLowCardVerbatim, token); err != nil {
				return
			}
		case "lowCardRefParametrized":
			// ADR-0008 D3 Cut-2: opaque-blob membership — pairs with a
			// marshalltypes.Parametrized sibling (params only; read via a
			// single Seq[[]byte]).
			if err = setChannelFlag(flags, mappingplan.MembershipChannelLowCardRefParametrized, token); err != nil {
				return
			}
		case "highCardRefParametrized":
			// ADR-0008 D3 Cut-2: high-card sibling of lowCardRefParametrized.
			if err = setChannelFlag(flags, mappingplan.MembershipChannelHighCardRefParametrized, token); err != nil {
				return
			}
		default:
			err = eb.Build().Str("flag", token).Errorf("unknown flag token (recognised: unit, verbatim / lowCardVerbatim, highCardRef, highCardVerbatim, const=<value>, ct=<canonical>)")
			return
		}
	}
	return
}

// TupleMembershipMarker is the reserved first token of the tag on a tuple
// element's membership field (`lw:"@membership,verbatim"`). Top-level lw:
// tags reject any `@`-prefixed membership so the marker cannot be mistaken
// for a literal verbatim label (ADR-0103).
const TupleMembershipMarker = "@membership"

// membershipMarkerChannel is the nested model's TYPE-based spelling of the
// `@membership,<channel flag>` tag (ADR-0113): an lw.* marker type's bare name
// to the channel it denotes. It lives here, beside the tag vocabulary, because
// both front-ends need it — marshallreflect reads the name off a live
// reflect.Type, marshallgen off the selector in the AST — and a marker that
// reached only one of them would be an accept-set divergence.
var membershipMarkerChannel = map[string]mappingplan.MembershipChannel{
	"Ref":          mappingplan.MembershipChannelLowCardRef,
	"HighRef":      mappingplan.MembershipChannelHighCardRef,
	"Verbatim":     mappingplan.MembershipChannelLowCardVerbatim,
	"HighVerbatim": mappingplan.MembershipChannelHighCardVerbatim,
}

// MembershipMarkerShape classifies an lw.* membership marker by its bare type
// name (lw.Ref → "Ref") into the FieldShape a front-end hands to
// AddTupleSliceField / AddNestedSliceField: the channel comes from the type,
// the canonical from the channel's wire value (a uint64 ref id, or the string
// name a verbatim channel embeds), and MarkerGoType carries the as-written
// newtype the codegen bridge emits its conversions from.
//
// ok is false when markerName is not a membership marker; each front-end words
// its own error there, because they recognise different marker families — the
// reflect codec also accepts the value-shape markers (lw.Single, the lanes)
// that marshallgen rejects (ADR-0113 P1). Callers promote the canonical with
// ScalarModifierHomogenousArray for a repeated (`[]lw.Ref`) membership.
func MembershipMarkerShape(markerName string) (shape FieldShape, ok bool, err error) {
	ch, ok := membershipMarkerChannel[markerName]
	if !ok {
		return
	}
	goType := "uint64"
	if ch.EmbedsLiteralName() {
		goType = "string"
	}
	shape.IsMembership = true
	shape.MembershipChannel = ch
	shape.MarkerGoType = "lw." + markerName
	shape.Canonical, err = ScalarCanonicalForGoType(goType)
	return
}

// IsMembershipMarker reports whether markerName names an lw.* membership
// marker type. Used by the front-ends' `[]S` detectors to route a slice whose
// element carries marker-typed memberships to the dynamic-membership builder.
func IsMembershipMarker(markerName string) bool {
	_, ok := membershipMarkerChannel[markerName]
	return ok
}

// SplitTupleOuterLW parses the tag of a tuple field — a slice-of-struct
// DTO field mapping one section, e.g. `Texts []LabeledText` with
// `lw:"string"`. The tag is the bare section name: a tuple field has no
// static membership (each element carries its own) and no flags (the
// channel is declared on the element's `@membership` field).
func SplitTupleOuterLW(tag string) (section string, err error) {
	parts := strings.Split(tag, ",")
	section = strings.TrimSpace(parts[0])
	if section == "" {
		err = eb.Build().Str("tag", tag).Errorf("tuple field tag must name its section (`lw:\"<section>\"`)")
		return
	}
	if strings.IndexByte(section, ':') >= 0 {
		err = eb.Build().Str("tag", tag).Errorf("tuple field tag names the whole section, not a sub-column — element fields declare `<section>:<column>`")
		return
	}
	for _, raw := range parts[1:] {
		if strings.TrimSpace(raw) != "" {
			err = eb.Build().Str("tag", tag).Str("token", strings.TrimSpace(raw)).Errorf("tuple field tag takes no flags — the membership channel belongs on the element's `@membership` field")
			return
		}
	}
	return
}

// ParsedTupleElemTag is the structured result of parsing the lw: tag on a
// field INSIDE a tuple element struct (SplitTupleElemLW). Exactly one of
// the two forms applies:
//
//   - `@membership[,<channel flag>]` — IsMembership true; the field holds
//     each attribute's membership value (Section / Column empty).
//   - `<section>[:<column>][,<flag>…]` — a value field mapping one
//     sub-column of the tuple's section (column defaults to "value").
type ParsedTupleElemTag struct {
	IsMembership bool
	Section      string
	Column       string
	Flags        mappingplan.FieldFlags
}

// SplitTupleElemLW parses the lw: tag on a tuple element struct's field.
// Element tags have no static-membership slot — the membership is dynamic
// per element — so the first token is either the `@membership` marker or
// the `<section>[:<column>]` target directly; trailing tokens are the
// shared flag vocabulary (parseFlagTokens). Which flags are legal on
// which element field is PlanBuilder.AddTupleSliceField's concern.
func SplitTupleElemLW(tag string) (out ParsedTupleElemTag, err error) {
	parts := strings.Split(tag, ",")
	head := strings.TrimSpace(parts[0])
	switch {
	case head == TupleMembershipMarker:
		out.IsMembership = true
	case strings.HasPrefix(head, "@"):
		err = eb.Build().Str("tag", tag).Str("token", head).Str("tupleMembershipMarker", TupleMembershipMarker).Errorf("unknown `@` marker in tuple element tag (recognised:)")
		return
	case head == "":
		err = eb.Build().Str("tag", tag).Errorf("tuple element tag must start with `%s` or `<section>:<column>`", TupleMembershipMarker)
		return
	default:
		if before, after, ok := strings.Cut(head, ":"); ok {
			out.Section = before
			out.Column = after
		} else {
			out.Section = head
		}
		if out.Section == "" {
			err = eb.Build().Str("tag", tag).Errorf("tuple element tag must name its section (`<section>:<column>`)")
			return
		}
	}
	err = parseFlagTokens(parts[1:], &out.Flags)
	return
}
