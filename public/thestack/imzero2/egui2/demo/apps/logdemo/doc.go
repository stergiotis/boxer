// Package logdemo is the companion AppI for the logviewer widget: a
// small interactive panel that emits zerolog events on demand so the
// operator can watch them stream into the viewer's tail in real time.
//
// Open both apps via the host's Apps menu — the logdemo window in one
// pane, the logviewer in another. Emit a Quick-Action burst or flip on
// the stream toggle; the events land in boxer.facts when the host runs
// with BOXER_LOG_FACTS=1, and the viewer shows them on its next read.
// Without it the viewer says logs are not persisted.
//
// Like every M3 AppI in this carousel, logdemo uses factory dispatch:
// each Open() yields a fresh *App with its own emit counter, stream
// toggle, and custom-message buffer, so two open windows emit
// independently and the logviewer can pick them apart by the
// `logdemo_inst` structured field.
package logdemo
