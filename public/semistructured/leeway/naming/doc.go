// Package naming is the vocabulary of names in a leeway schema: a
// [StylableName] (a table, section or column name that converts between
// naming styles without losing what it refers to) and a [Key] (an
// identifier used as is, such as a streaming or co-section group).
//
// [MakeStylableName] and [MakeKey] validate and convert; their Must
// variants panic and are meant for literals. [ConvertNameStyle] renders a
// name in a target [NamingStyleE], and [JoinComponents] builds a name from
// parts.
//
// Conversion is not always reversible: a component with a digit followed by
// a letter can split differently in another style. [IsStyleStable] tests a
// name and a style before converting into one that must survive
// validation.
package naming
