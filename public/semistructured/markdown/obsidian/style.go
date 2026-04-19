//go:build llm_generated_opus46

package obsidian

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFiles embed.FS

// StaticFS returns a read-only filesystem rooted at the static/ directory,
// containing the default stylesheet and any future static assets.
func StaticFS() fs.FS {
	sub, _ := fs.Sub(staticFiles, "static")
	return sub
}

// DefaultStylesheet returns the default CSS stylesheet as a string.
func DefaultStylesheet() string {
	b, _ := staticFiles.ReadFile("static/obsidian.css")
	return string(b)
}
