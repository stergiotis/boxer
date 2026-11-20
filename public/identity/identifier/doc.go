// Package identifier defines boxer's 64-bit tagged ids: a [TaggedId] is a
// tag ([IdTag]) in the high bits followed by an id body ([UntaggedId]) in
// the rest. The tag is the Fibonacci code of a small [TagValue], which is
// self-delimiting, so an id can be split without knowing its tag's width
// and two tags never collide (ADR-0106).
//
// [TagValue.GetTag] turns a tag value into its tag; [IdTag.ComposeId] and
// [UntaggedId.AddTag] build an id; [TaggedId.Split], [TaggedId.GetTag] and
// [TaggedId.RemoveTag] take one apart; [IdTag.SameTag] tests an id against a
// known tag with one mask. [IdGeneratorI] and [IdGeneratorFactoryI] are the
// interfaces id generators implement; the implementations are in the
// identgen packages.
//
// A body must fit below its tag: the wider the tag, the fewer body bits
// ([IdTag.GetMaxPossibleIdIncl]). AddTag panics on a body that overlaps the
// tag rather than producing an id that decodes to a different tag.
package identifier
