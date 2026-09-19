// Package scene runs imzero2 scenes (ADR-0248): launch an app on the headless
// host, drive it through the carrier, assert, capture, tear down.
//
// It is a library first. [Launch] starts a host and hands back a [Session]
// holding a connected driver, so a Go test in the integration lane can run
// trace fragments and do its own arithmetic between them (§SD5). The `imzero2
// scene` command in scenecmd is the same library pointed at scene documents.
//
// A scene document is markdown in the applet book's convention (ADR-0132 §SD1)
// — frontmatter, prose, role-marked fences — parsed here by a small scanner of
// its own: the convention is shared, the parser is not, so this package does
// not pull in the widget stack to read a file.
package scene
