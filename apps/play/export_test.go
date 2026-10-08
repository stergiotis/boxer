package play

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// ComposeResultDocForTest is publish_result's document, composed and
// checked by ComposeBundleDoc with whatever parser is installed, for the
// external tests that install sqlapplet's.
func ComposeResultDocForTest(bundle string, local string, sql string, tabs []string, title string, source string) (string, error) {
	doc, err := ComposeBundleDoc(resultBundleSpec(PublishResultArgs{Bundle: bundle, LocalName: local, Sql: sql, Tabs: tabs, Title: title}, local, source))
	return string(doc), err
}

// ParseAppletDocForTest exposes the installed applet document parser.
func ParseAppletDocForTest(path string, src []byte) (AppletDoc, error) {
	return parseAppletDoc(path, src)
}

// OperationSpecForTest finds an operation in play's catalog.
func OperationSpecForTest(name string) (spec app.OperationSpec, ok bool) {
	for _, s := range playOps.Catalog().Operations {
		if s.Name == name {
			return s, true
		}
	}
	return
}
