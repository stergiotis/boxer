// Package notebookview is a semi-retained widget (ADR-0267) that draws a
// notebook: a markdown document of typed cells with the blobs it names
// (ADR-0371).
//
// Package mdnotebook reads the text; the view stacks the notebook's cells. A
// prose cell goes through the markdown widget, which is not extended; every
// other cell is drawn by its kind: a record as its folded statement, a
// presentation cell as its folded statement over its result, a media cell
// by its media type. Each of those is set in from a rule along its left
// edge, which spans the cell and takes the tone of its state: neutral as
// collected, warning when stale, error when it has nothing to show. A presentation
// cell names its renderer; `table` is built in and [RegisterRenderer] adds
// the rest, one process-wide set that the packages owning the drawings fill.
//
// Showing a notebook has two halves. [Prepare] reads the blobs, answers the
// presentation cells and decodes media; it draws nothing and runs on any
// goroutine. [View.Set] takes the result on the render goroutine, where the
// markdown is parsed. [View.SetNotebook] is both, for files that are at
// hand.
//
// What it does not do: play audio (a click on a recording is reported), or
// fold cells under headings.
//
// Readbacks in [Events] are one frame behind, as every widget's are.
package notebookview
