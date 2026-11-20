// Package nanopass is the framework for rewriting ClickHouse SQL as a
// sequence of small passes, each taking SQL text and returning SQL text.
//
// [Parse] returns the concrete syntax tree of a statement ([ParseResult]);
// [ParseCanonical] parses with the stricter grammar the canonical form
// satisfies. A [Pass] is a named function over SQL with declared
// [PassProperties]; [LiftBodyPass] makes one from a plain string function,
// [Sequence] and [FixedPoint] compose passes, and [Pass.Run] applies one.
// The passes themselves live in the passes subpackage and the read-only
// questions (which tables, columns, functions; which kind of statement) in
// the analysis subpackage. [WalkCST], [FindAll] and [FindFirst] search a
// tree; [MacroExpander] expands registered macros.
//
// Downstream passes are written against the canonical (function-call) form.
// When a shape is not canonicalised, extend the canonicalize passes rather
// than teaching each consumer the other spellings. README.md and the
// nanopass-sql skill cover writing a pass; AssertProperties checks its
// declared properties over a corpus.
package nanopass
