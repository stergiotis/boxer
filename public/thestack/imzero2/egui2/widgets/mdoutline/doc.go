// Package mdoutline is the heading outline of a markdown document: the
// hierarchy a document's headings make, nested by level, drawn through the
// native tree widget with a section that folds away, a selection that
// follows the reader's position and a reveal that opens the way to it.
//
// The package is in two halves, the split the tree widget's own adopters
// use. [Model] is pure: it turns [markdown.HeadingInfo] into the columnar
// shape [tree.Render] takes and is testable without a binding in sight.
// [View] holds the widget's state across frames and draws the frame; its
// [View.Render] reports a clicked heading and leaves what a click means to
// the host, since an editor takes the caret there and a reader scrolls a
// preview.
//
// [SectionAt] is the other direction: a byte offset in the source resolved
// to the section it lies in, which is what a host feeds back as the current
// section so the selection follows the caret.
package mdoutline
