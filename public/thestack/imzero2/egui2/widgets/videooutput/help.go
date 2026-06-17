package videooutput

import "embed"

// HelpFS is the embedded help corpus for the video-output control, rooted at
// this package's help/ directory. It documents the meaning of the panel's
// readouts (ADR-0088). The corpus lives beside the widget so the docs travel
// with the code; the runtime registration with the help library lives in the
// carousel integration layer (which registers a "Video output" book over this
// fs.FS via help.Register), keeping this presentation package free of a
// keelson/runtime dependency.
//
//go:embed help
var HelpFS embed.FS
