//go:build llm_generated_opus46

package obsidian

import (
	"embed"
	"io/fs"

	idsweb "github.com/stergiotis/boxer/public/keelson/designsystem/web"
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

// StylesheetE selects which built-in stylesheet [Stylesheet] returns.
type StylesheetE uint8

const (
	// StylesheetObsidian is the default Obsidian-flavored light theme
	// (static/obsidian.css), the same bytes as [DefaultStylesheet].
	StylesheetObsidian StylesheetE = iota
	// StylesheetIDS is the IDS lean classless dark theme
	// (keelson/designsystem/web, ADR-0076). It is self-contained — it bundles
	// the colour tokens and a reading column, so no extra body-layout <style>
	// block is needed alongside it.
	StylesheetIDS
)

// Stylesheet returns the selected built-in stylesheet as a self-contained CSS
// string, ready to inline in a <style> block. Unknown values fall back to the
// Obsidian default. For the IDS theme's linked two-file form (ids.css +
// ids-palette.css), serve the files from [idsweb.FS] instead.
func Stylesheet(s StylesheetE) string {
	switch s {
	case StylesheetIDS:
		return idsweb.Stylesheet()
	default:
		return DefaultStylesheet()
	}
}
