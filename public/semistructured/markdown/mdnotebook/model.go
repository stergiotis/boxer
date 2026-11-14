package mdnotebook

// CellKindE is what a cell is, decided from its spelling alone.
type CellKindE uint8

const (
	// CellKindProse is a run of markdown between two other cells: text,
	// images, and every fence that is not a notebook's own.
	CellKindProse CellKindE = iota
	// CellKindRecord is a `sql` fence with an id: a named relation, computed
	// at collection and stored.
	CellKindRecord
	// CellKindPresentation is a `sql` fence with a renderer: a statement over
	// records, answered where the notebook is read.
	CellKindPresentation
	// CellKindMedia is an embed alone in its paragraph whose media type the
	// markdown widget does not draw: a recording. An image stays prose.
	CellKindMedia
)

var AllCellKinds = []CellKindE{CellKindProse, CellKindRecord, CellKindPresentation, CellKindMedia}

func (inst CellKindE) String() (s string) {
	switch inst {
	case CellKindProse:
		s = "prose"
	case CellKindRecord:
		s = "record"
	case CellKindPresentation:
		s = "presentation"
	case CellKindMedia:
		s = "media"
	default:
		s = "unknown"
	}
	return
}

// Cell is one cell of a notebook. Start and Stop are its byte range in the
// source, snapped to line boundaries; the ranges of a notebook's cells do
// not overlap and what lies between two of them is blank lines.
type Cell struct {
	Kind        CellKindE
	Start, Stop int
	// Id is the block id that follows the cell (`^id` on a line of its
	// own), "" for none. A prose cell keeps its ids in its text.
	Id string
	// Lang is a fence's first info word, lower-cased; Info the whole info
	// string.
	Lang string
	Info string
	// Body is a fence's content, without the fence lines.
	Body string
	// Renderer is a presentation cell's second info word.
	Renderer string
	// Name is a record's name in SQL: its id with `-` written `_`.
	Name string
	// Alt, Path and Title are a media cell's parts; MediaType is read off
	// the path's extension.
	Alt       string
	Path      string
	Title     string
	MediaType string
}

// RecordRef is one record as the frontmatter lists it.
type RecordRef struct {
	// Name is the record's name in SQL.
	Name string
	// Blob is the path of the stored result, relative to the notebook.
	Blob string
	// Of is the hash of the statement the blob was computed from, "" when
	// the frontmatter does not say.
	Of string
}

// ValRef is one `val:` link in a prose cell: a figure in a sentence.
type ValRef struct {
	// Cell indexes Notebook.Cells.
	Cell int
	// Label is the link's text, the value as it read when it was stored.
	Label string
	// Record and Column are what the target names.
	Record string
	Column string
	// Gloss is the link's title: the gloss token the value is shown
	// through, "" for the column's own or none.
	Gloss string
}

// ImageRef is one image a prose cell draws from a file beside the notebook.
type ImageRef struct {
	// Cell indexes Notebook.Cells.
	Cell int
	// Path is the file's path, relative to the notebook, as written.
	Path string
}

// Problem is something Read found that a reader should be told.
type Problem struct {
	// Cell indexes Notebook.Cells, -1 for the frontmatter.
	Cell int
	Text string
}

// Notebook is a notebook's text taken apart.
type Notebook struct {
	Src []byte
	// BodyStart is the offset after the frontmatter block.
	BodyStart int
	Title     string
	// Records are sorted by name.
	Records []RecordRef
	Cells   []Cell
	Vals    []ValRef
	// Images are the images of the prose cells, in document order.
	Images   []ImageRef
	Problems []Problem
	// Fences are the offsets of every fence the scan met in column 0, a
	// notebook's own or not: what the scan and the markdown parse must
	// agree on for a notebook edited by one to be read by the other.
	Fences []int
}

// Record finds a record by name.
func (inst *Notebook) Record(name string) (ref RecordRef, found bool) {
	for _, r := range inst.Records {
		if r.Name == name {
			return r, true
		}
	}
	return
}

// Text is a cell's source.
func (inst *Notebook) Text(cell int) (text string) {
	c := inst.Cells[cell]
	return string(inst.Src[c.Start:c.Stop])
}
