// Package samples holds notebooks to read and to start from (ADR-0371),
// each with the blobs it names: `minimal`, a notebook in two records;
// `report`, a report that leads with its figures and ends with how they
// were collected; and `renderers`, one cell per renderer over the smallest
// record that satisfies it.
//
// Every record makes its own rows, so a sample collects against any
// ClickHouse. A sample is changed by editing its text and collecting it
// again; the integration lane of package mdnotebook holds each to what
// collection writes.
package samples

import (
	"embed"
	"io/fs"
)

//go:embed *.md *.blobs
var files embed.FS

// FS is the samples' directory: each notebook's text and its blobs beside
// it, which is the file system a notebook is loaded from.
func FS() fs.FS { return files }

// Names are the notebooks' files in [FS], in the order a reader meets them.
func Names() []string { return []string{"minimal.md", "report.md", "renderers.md"} }
