// Package functional holds small generic helpers over iterators and values:
// projecting and swapping iter.Seq2 pairs ([IterLeftOnly], [IterRightOnly],
// [IterInterchanged]), lifting an iter.Seq into an iter.Seq2
// ([MakeIter2FromIter1], [MakeIter2FromIter1Indexed]), one-value iterators,
// [TranslateEmpty], and [NilIteratorValueType], the element type of an
// iterator used only as a scope (`for range x.KeepIter()`). The option
// subpackage is an Option type.
//
// The iterator helpers are lazy: each pulls from its source sequence as it
// is pulled, so a source that can be ranged over only once can be wrapped
// only once.
package functional
