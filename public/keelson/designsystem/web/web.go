// Package web serves the IDS lean classless CSS assets (ADR-0076): the
// hand-authored base + obsidian coverage (ids.css) and the generated colour
// tokens (ids-palette.css). It lets Go callers — e.g. the obsidian markdown
// renderer — embed or inline the IDS theme without reaching across package
// directories with go:embed.
package web

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed ids.css ids-palette.css
var files embed.FS

// FS returns the IDS CSS assets as a read-only filesystem. Serve them as-is for
// the linked two-file form (<link rel="stylesheet" href="ids.css">), where
// ids.css's @import pulls ids-palette.css from the same directory.
func FS() fs.FS {
	return files
}

// Stylesheet returns the IDS lean classless stylesheet as a single,
// self-contained CSS string suitable for inlining in a <style> block: the
// generated palette custom properties followed by ids.css with its
// `@import "ids-palette.css";` line folded out (the palette is concatenated in
// instead). The result includes the reading column, so it needs no companion
// body-layout block.
func Stylesheet() (css string) {
	palette, _ := files.ReadFile("ids-palette.css")
	base, _ := files.ReadFile("ids.css")
	inlined := strings.Replace(string(base), "@import \"ids-palette.css\";\n", "", 1)
	css = string(palette) + "\n" + inlined
	return
}
