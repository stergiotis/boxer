package play

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

// ComposeResultDocForTest exposes composeResultDoc to the external tests
// that parse it with sqlapplet, which this package cannot import.
func ComposeResultDocForTest(bundle string, local string, sql string, tabs []string, title string, source string) string {
	return composeResultDoc(PublishResultArgs{Bundle: bundle, LocalName: local, Sql: sql, Tabs: tabs, Title: title}, local, source)
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
