package introspecthttp

import "embed"

// HelpFS is the embedded help corpus for the keelson introspection facility
// (ADR-0094), rooted at the help/ directory. The runtime registers it as a
// non-app help book (see the carousel's introspection-help wiring), so the
// endpoint and its tables are discoverable from the Help app.
//
//go:embed help
var HelpFS embed.FS
