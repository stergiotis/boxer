package schemaview

import "embed"

// HelpFS is the embedded help corpus for the schema inspector, rooted at this
// package's help/ directory. It documents the navigator's glyph vocabulary so a
// reader can decode the tree without the demo description or doc.go. The corpus
// lives beside the widget so the docs travel with the code; the runtime
// registration with the help library lives in the carousel integration layer
// (which registers a "Schema inspector" book over this fs.FS via help.Register),
// keeping this presentation package free of a keelson/runtime dependency.
//
//go:embed help
var HelpFS embed.FS
