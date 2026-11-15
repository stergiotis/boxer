// Package mdnotebook reads, checks and collects a sealed notebook: one
// markdown document that is a sequence of typed cells, with the blobs its
// cells name (ADR-0371).
//
// [Read] takes the text apart with a line scanner. A record, a presentation
// cell and a media cell start in column 0, and everything between two of
// them is one prose cell, so the cells tile the text (ADR-0371 §SD1).
//
// [Load] reads the stored records the frontmatter lists and checks each
// blob against its name; [Problems] and [StaleRecords] say what stands
// between the notebook and being sealed. [Evaluate] answers a presentation
// cell over the records through the trivial evaluator of ADR-0290, so
// reading computes nothing per row and needs no server.
//
// [Collect] is the one step that does need a server: it computes every
// record once, in document order, and returns the text sealed over the
// results. [Buffer] is the same records as the one statement to try in
// play; [FromBuffer] is a buffer as a new notebook's records, and
// [SetRecords] writes one back into a notebook's text, keeping the rest.
// Package samples holds notebooks to read and to start from.
//
// A programme that shows data it already holds writes the text with
// [Writer] and hands its relations over with [Bind], in place of blobs:
// nothing is stored, and [Problems] holds the notebook to what was handed
// over. [Seal] stores a bound notebook; it is the half of [Collect] that
// needs no server.
//
// The package has no UI. The widget that draws a notebook is
// widgets/notebookview.
package mdnotebook
