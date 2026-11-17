// Package notebook is the window a sealed notebook is worked on in
// (ADR-0371 §SD10): one document shown two ways. The Read face is the cell
// stack over what the notebook stores, with no server asked. The Records
// face is a play instance whose buffer is the notebook's records; taking
// the buffer back writes the statements into the text and leaves prose and
// presentation cells as they are.
//
// The Read face is a pane (notebook_live.go) that also reads the notebook
// live (ADR-0380): its records computed on the server, read-only, and shown
// as a bound notebook, with nothing stored. Another app embeds the same
// pane by NewEmbedded (notebook_embed.go).
//
// A notebook is opened from a lading snapshot, where its blobs lie beside
// it, or from the samples of package mdnotebook/samples.
//
// Collection runs every record once on the server and publishes the text
// with its blobs as a new snapshot; the person states the tree's
// confidentiality tier when they confirm it (notebook_collect.go).
//
// The text is edited elsewhere and comes back: in the editor app the host
// registers, whose saves replace the text (ADR-0365), or in mdedit, which
// is handed the text and hands it back by this app's launch config
// (notebook_handover.go).
//
// deferred: serving the embedded play's catalog from this window, as an
// applet window does. An agent works a notebook's statements in a play
// window of its own and hands the buffer here; the forwarding follows the
// first use that needs one window.
package notebook

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/notebook/launchcfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/keelson/runtime/externaledit"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// helpFS embeds the help book (apps/notebook/help/*.md): F1 opens it for
// the person, and the help service serves the same pages to a model
// (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// AppId is the app's manifest id; another app names it through the launch
// leaf.
const AppId app.AppIdT = launchcfg.AppId

var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "Notebook",
	Title:    "Notebook",
	Summary:  "Read a sealed notebook and work on its records in play",
	Icon:     icons.PhNotebook,
	Topics:   []app.TopicT{app.TopicData},
	Keywords: []string{"notebook", "report", "sealed", "live", "records", "markdown", "adr-0371", "adr-0380"},
	Surface:  app.SurfaceWindowed,
	Maturity: app.MaturityIncubating,
	// LaunchKind takes a notebook another app hands over (launchcfg).
	LaunchKind: launchcfg.Kind,
	// The open notebook is kept across a close (ADR-0148): by its snapshot,
	// with its text when that was edited here.
	Workingset: true,
	Operations: ops.Catalog(),
	Help:       help.MustSub(helpFS, "help"),
	Caps: append(append(task.ProducerCaps(), externaledit.ClientCaps("notebook: hand the notebook's text to the host's editor app and take each save back")...),
		app.SubjectFilter{Pattern: ladingpublish.Subject(mountTag), Direction: app.CapDirectionPub,
			Reason: "notebook: publish a collected notebook and its stored results to the lading store, under the notebook mount tag (ADR-0371 §SD10)"},
		app.SubjectFilter{Pattern: audioout.SubjectOpen, Direction: app.CapDirectionPub,
			Reason: "notebook: play a recording the notebook holds"},
		// The two below are declarations, not routes: nothing publishes on
		// `ch.server.read.*`. They say what this process dials from the
		// window's own code, where a reviewer looks for it (ADR-0026 §SD10).
		app.SubjectFilter{Pattern: "ch.server.read.lading", Direction: app.CapDirectionPub,
			Reason: "notebook: open a notebook and the stored results beside it from a lading snapshot"},
		app.SubjectFilter{Pattern: "ch.server.read.records", Direction: app.CapDirectionPub,
			Reason: "notebook: collection runs each record's statement once on the server the environment names (ADR-0371 §SD10), and a live reading runs them read-only (ADR-0380 §SD3)"},
	),
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1280,
		PreferredHeight: 860,
	},
}

// SampleSeed opens a sample notebook in a new window (ADR-0009 seed
// variable): what a scene drives, with no store behind it.
var SampleSeed = env.NewString(env.Spec{
	Name:        "BOXER_NOTEBOOK_SAMPLE",
	Description: "the sample notebook a new notebook window opens: minimal, report or renderers; for scenes and demos",
	Category:    env.CategoryE("boxer-notebook"),
})

// SnapshotSeed opens a notebook of a lading snapshot in a new window. The
// sample seed wins when both are set.
var SnapshotSeed = env.NewString(env.Spec{
	Name:        "BOXER_NOTEBOOK_SNAPSHOT",
	Description: "the notebook a new notebook window opens from the lading store, as <mount id, hex>@<snapshot, unix nanoseconds>:<path in the snapshot>",
	Category:    env.CategoryE("boxer-notebook"),
})

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("notebook app: failed to register factory")
	}
}
